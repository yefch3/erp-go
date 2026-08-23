package app

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// BizTypeContract is the label the approval engine files these documents
// under. It knows nothing else about contracts.
const BizTypeContract = "CONTRACT"

// SubmitContract hands the draft version to the approval engine.
//
// The approval instance is created BEFORE the local status change, because a
// remote side effect cannot be rolled back by our transaction. If the local
// commit then fails, a retry finds the instance already running and reuses it
// instead of erroring out, so the pair converges either way.
func (s *Service) SubmitContract(ctx context.Context, tenantID, id int64, op Operator) (string, int64, error) {
	view, err := s.GetContract(ctx, tenantID, id, 0)
	if err != nil {
		return "", 0, err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return "", 0, err
	}
	if err := readyToSubmit(view); err != nil {
		return "", 0, err
	}

	instanceID, err := s.approvals.Submit(ctx, ApprovalSubmission{
		BizType: BizTypeContract, BizID: view.Contract.ID, BizNo: view.Contract.ContractNo,
		Summary: summaryJSON(view), SubmitterID: op.ID, SubmitterName: op.Name,
		Amount: view.Version.BaseAmount,
	})
	if err != nil {
		return "", 0, err
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockContract(ctx, store.LockContractParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if locked.Status == "PENDING_APPROVAL" {
			return nil // a concurrent submit already moved it; nothing to do
		}
		if locked.Status != view.Contract.Status {
			return apierr.Conflict("EX_CONTRACT_STATUS_CHANGED", "合同状态已变化，请刷新后重试")
		}
		if _, err := q.MarkContractPendingApproval(ctx, store.MarkContractPendingApprovalParams{
			TenantID: tenantID, ID: id, UpdatedBy: op.ID,
		}); err != nil {
			return err
		}
		return q.SetContractVersionStatus(ctx, store.SetContractVersionStatusParams{
			TenantID: tenantID, ID: view.Version.ID, NewStatus: "PENDING_APPROVAL",
		})
	})
	if err != nil {
		return "", 0, err
	}
	return "PENDING_APPROVAL", instanceID, nil
}

// readyToSubmit collects the checks that turn a working draft into something
// an approver can be asked to sign off on.
func readyToSubmit(view ContractView) error {
	if view.Version.Status != "DRAFT" {
		return apierr.Conflict("EX_CONTRACT_NOT_DRAFT", "只有草稿版本可以提交审批").
			WithMeta("version_status", view.Version.Status)
	}
	switch view.Contract.Status {
	case "DRAFT", "EFFECTIVE", "EXECUTING":
	default:
		return apierr.Conflict("EX_CONTRACT_STATUS_TRANSITION", "当前状态不允许提交审批").
			WithMeta("status", view.Contract.Status)
	}
	if view.Version.DeliveryDate == "" {
		return apierr.Invalid("EX_DELIVERY_DATE_REQUIRED", "提交审批前必须填写交货日期")
	}
	if len(view.Items) == 0 {
		return apierr.Invalid("EX_ITEMS_REQUIRED", "合同没有明细，无法提交审批")
	}
	return nil
}

// ApplyApprovalDecision advances a contract on the strength of an approval
// event. It is idempotent: the same event delivered twice leaves the same
// state, because a contract that is no longer waiting on approval is skipped.
func (s *Service) ApplyApprovalDecision(ctx context.Context, tenantID, contractID int64, result string) (string, error) {
	view, err := s.GetContract(ctx, tenantID, contractID, 0)
	if err != nil {
		return "", err
	}
	if view.Contract.Status != "PENDING_APPROVAL" {
		return view.Contract.Status, nil
	}

	// Where a decision sends the contract. The three outcomes are genuinely
	// different: approval moves it forward, a return hands it back to be
	// fixed, and a rejection ends it.
	contractTo, versionTo := "PENDING_SIGN", "APPROVED"
	// Where "back" is depends on whether this was the first approval or a
	// change to a contract already running, which is what
	// status_before_approval remembers.
	before := view.Contract.StatusBeforeApproval
	if before == "" || before == "PENDING_APPROVAL" {
		before = "DRAFT"
	}
	switch result {
	case "APPROVED":
	case "RETURNED":
		contractTo, versionTo = before, "DRAFT"
	default: // REJECTED
		versionTo = "REJECTED"
		// A contract already in force keeps running under its old version;
		// only the rejected change dies. One that never took effect is over.
		contractTo = before
		if before == "DRAFT" {
			contractTo = "REJECTED"
		}
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockContract(ctx, store.LockContractParams{TenantID: tenantID, ID: contractID})
		if err != nil {
			return err
		}
		if locked.Status != "PENDING_APPROVAL" {
			return nil
		}
		if err := q.SetContractVersionStatus(ctx, store.SetContractVersionStatusParams{
			TenantID: tenantID, ID: view.Version.ID, NewStatus: versionTo,
		}); err != nil {
			return err
		}
		_, err = q.SetContractStatus(ctx, store.SetContractStatusParams{
			TenantID: tenantID, ID: contractID, NewStatus: contractTo, UpdatedBy: 0,
		})
		return err
	})
	if err != nil {
		return "", err
	}
	return contractTo, nil
}

// SignContract records the customer's signature, which is the moment the
// version in hand becomes the one in force. Everything downstream — purchase
// demand, shipment plans, receivables — hangs off the event this appends.
func (s *Service) SignContract(ctx context.Context, tenantID, id int64, op Operator) (string, error) {
	view, err := s.GetContract(ctx, tenantID, id, 0)
	if err != nil {
		return "", err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return "", err
	}
	if view.Contract.Status != "PENDING_SIGN" {
		return "", apierr.Conflict("EX_CONTRACT_NOT_PENDING_SIGN", "只有待签署的合同可以签署").
			WithMeta("status", view.Contract.Status)
	}
	if view.Version.Status != "APPROVED" {
		return "", apierr.Conflict("EX_VERSION_NOT_APPROVED", "该版本尚未通过审批")
	}
	// Confirming a signature by hand has to be backed by the thing being
	// confirmed. Without this the button is an unsupported assertion: a
	// contract in force, downstream purchasing and shipping already moving,
	// and nothing on file showing the customer ever agreed.
	//
	// Scoped to this version, because a scan of v1 says nothing about the v2
	// that replaced it.
	signed, err := s.q.CountSignedAttachments(ctx, store.CountSignedAttachmentsParams{
		TenantID: tenantID, ContractVersionID: &view.Version.ID,
	})
	if err != nil {
		return "", err
	}
	if signed == 0 {
		return "", apierr.Invalid("EX_SIGNED_COPY_REQUIRED",
			"请先上传客户签署件（文件类型选「客户签回」），再确认签署").
			WithMeta("version_no", strconv.Itoa(int(view.Version.VersionNo)))
	}
	payload, err := json.Marshal(effectiveEvent(view))
	if err != nil {
		return "", err
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockContract(ctx, store.LockContractParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if locked.Status != "PENDING_SIGN" {
			return apierr.Conflict("EX_CONTRACT_NOT_PENDING_SIGN", "只有待签署的合同可以签署")
		}
		// The version that just took over retires the one it replaces.
		if err := q.SupersedeOtherVersions(ctx, store.SupersedeOtherVersionsParams{
			TenantID: tenantID, ContractID: id, KeepID: view.Version.ID,
		}); err != nil {
			return err
		}
		if err := q.SetContractCurrentVersion(ctx, store.SetContractCurrentVersionParams{
			TenantID: tenantID, ID: id, VersionID: view.Version.ID, UpdatedBy: op.ID,
		}); err != nil {
			return err
		}
		if _, err := q.SetContractStatus(ctx, store.SetContractStatusParams{
			TenantID: tenantID, ID: id, NewStatus: "EFFECTIVE", UpdatedBy: op.ID,
		}); err != nil {
			return err
		}
		// How the signature was established, recorded on the contract rather
		// than inferred later from whatever files happen to be attached.
		// This path is always MANUAL; the webhook will set PLATFORM itself.
		if err := q.MarkContractSigned(ctx, store.MarkContractSignedParams{
			TenantID: tenantID, ID: id, SignatureSource: SourceManual, UpdatedBy: op.ID,
		}); err != nil {
			return err
		}
		return outbox.Append(ctx, tx, outbox.Event{
			TenantID: tenantID, AggregateType: "contract",
			AggregateID: strconv.FormatInt(id, 10), EventType: "ContractEffective",
			Payload: payload,
		})
	})
	if err != nil {
		return "", err
	}
	return "EFFECTIVE", nil
}

// cancellableFrom are the states a contract can be written off from: not yet
// submitted, turned down by an approver, or awaiting a signature that will
// never come.
var cancellableFrom = map[string]bool{"DRAFT": true, "REJECTED": true, "PENDING_SIGN": true}

// CancelContract kills a deal that never got off the ground. A contract that
// has ever been in force is not cancelled but terminated, which is a
// different act with different consequences and is not modelled yet.
func (s *Service) CancelContract(ctx context.Context, tenantID, id int64, op Operator) (string, error) {
	view, err := s.GetContract(ctx, tenantID, id, 0)
	if err != nil {
		return "", err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return "", err
	}
	switch {
	case view.Contract.Status == "CANCELLED":
		return "CANCELLED", nil
	case view.Contract.Status == "PENDING_APPROVAL":
		return "", apierr.Conflict("EX_CONTRACT_IN_APPROVAL",
			"审批进行中的合同不能作废，请让审批人驳回后再作废")
	case !cancellableFrom[view.Contract.Status]:
		return "", apierr.Conflict("EX_CONTRACT_STATUS_TRANSITION", "当前状态不允许作废").
			WithMeta("status", view.Contract.Status)
	case view.Contract.CurrentVersionID != 0:
		return "", apierr.Conflict("EX_CONTRACT_ALREADY_EFFECTIVE",
			"该合同已经生效过，作废需要走终止流程")
	}
	status, err := s.q.SetContractStatus(ctx, store.SetContractStatusParams{
		TenantID: tenantID, ID: id, NewStatus: "CANCELLED", UpdatedBy: op.ID,
	})
	return status, err
}

// contractEffectiveEvent is what procurement, shipping and finance consume.
// It carries the lines, so no consumer has to call export back to find out
// what was actually sold.
type contractEffectiveEvent struct {
	ContractID   int64               `json:"contract_id"`
	ContractNo   string              `json:"contract_no"`
	VersionID    int64               `json:"version_id"`
	VersionNo    int32               `json:"version_no"`
	CustomerID   int64               `json:"customer_id"`
	CustomerName string              `json:"customer_name"`
	Currency     string              `json:"currency"`
	TotalAmount  string              `json:"total_amount"`
	DeliveryDate string              `json:"delivery_date"`
	Incoterm     string              `json:"incoterm"`
	// 合同负责人（A1）：采购需求生而继承它作为属主——合同是谁谈的，
	// 拆出来的采购动向就归谁看。
	SalesEmployeeID int64  `json:"sales_employee_id"`
	SalesEmployee   string `json:"sales_employee"`
	Items        []effectiveEventItem `json:"items"`
}

type effectiveEventItem struct {
	LineNo      int32  `json:"line_no"`
	ItemID      int64  `json:"contract_item_id"`
	ProductID   int64  `json:"product_id"`
	SkuID       int64  `json:"sku_id,omitempty"`
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	Qty         string `json:"qty"`
	UomID       int64  `json:"uom_id"`
	UomCode     string `json:"uom_code"`
	UnitPrice   string `json:"unit_price"`
	Amount      string `json:"amount"`
	HsCode      string `json:"hs_code,omitempty"`
}

func effectiveEvent(view ContractView) contractEffectiveEvent {
	items := make([]effectiveEventItem, 0, len(view.Items))
	for _, i := range view.Items {
		var sku int64
		if i.SkuID != nil {
			sku = *i.SkuID
		}
		items = append(items, effectiveEventItem{
			LineNo: i.LineNo, ItemID: i.ID, ProductID: i.ProductID, SkuID: sku,
			ProductCode: i.ProductCode, ProductName: i.ProductName,
			Qty: i.Qty, UomID: i.UomID, UomCode: i.UomCode,
			UnitPrice: i.UnitPrice, Amount: i.Amount, HsCode: i.HsCode,
		})
	}
	return contractEffectiveEvent{
		ContractID: view.Contract.ID, ContractNo: view.Contract.ContractNo,
		VersionID: view.Version.ID, VersionNo: view.Version.VersionNo,
		CustomerID: view.Contract.CustomerID, CustomerName: view.Contract.CustomerName,
		Currency: view.Version.Currency, TotalAmount: view.Version.TotalAmount,
		DeliveryDate: view.Version.DeliveryDate, Incoterm: view.Version.Incoterm,
		SalesEmployeeID: view.Contract.SalesEmployeeID, SalesEmployee: view.Contract.SalesEmployee,
		Items: items,
	}
}
