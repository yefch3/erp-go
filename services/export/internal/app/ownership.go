package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// Biz types a transfer can name. The chain is walked from whichever end the
// caller happens to be looking at.
const (
	BizTypeQuotation = "QUOTATION"
)

// deal is the pair a transfer moves: a quotation and, if one exists, the live
// contract written from it. Either half may be absent — a quotation that was
// never contracted, or (once standalone contracts exist) a contract with no
// quotation behind it.
type deal struct {
	quotation *store.GetQuotationRow
	contract  *store.GetContractRow
}

// ownerOf reads the current owner off whichever half is present. Both halves
// always agree, which is the invariant this whole file exists to keep.
func (d deal) ownerID() int64 {
	if d.quotation != nil {
		return d.quotation.SalesEmployeeID
	}
	return d.contract.SalesEmployeeID
}

func (d deal) ownerName() string {
	if d.quotation != nil {
		return d.quotation.SalesEmployee
	}
	return d.contract.SalesEmployee
}

// TransferOwnership hands a deal to somebody else.
//
// The unit is the deal, not the document. A quotation and its contract are
// 1:1, and moving one without the other strands the new owner: a cancelled
// contract can be written again, but only by the quotation's owner, so a
// contract transferred alone leaves them holding a dead document with no way
// forward and — under a SELF scope — no sight of what is blocking them.
func (s *Service) TransferOwnership(ctx context.Context, tenantID int64, bizType string, bizID, toEmployeeID int64, reason string, op Operator) ([]store.ListOwnershipTransfersRow, error) {
	if toEmployeeID == 0 {
		return nil, apierr.Invalid("EX_TRANSFER_TARGET_REQUIRED", "请选择接收人")
	}
	d, err := s.resolveDeal(ctx, tenantID, bizType, bizID)
	if err != nil {
		return nil, err
	}
	fromID, fromName := d.ownerID(), d.ownerName()
	if fromID == toEmployeeID {
		return nil, apierr.Invalid("EX_TRANSFER_SAME_OWNER", "接收人已经是当前负责人")
	}
	// You cannot give away what you cannot see, and you cannot push work onto
	// somebody outside your own remit. Both ends are checked against the same
	// scope, so a supervisor moves documents within their team and no further.
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return nil, err
	}
	if !allowedOwner(visible, fromID) {
		return nil, apierr.Permission("EX_TRANSFER_SOURCE_OUT_OF_SCOPE",
			"该单据不在你的管理范围内")
	}
	if !allowedOwner(visible, toEmployeeID) {
		return nil, apierr.Permission("EX_TRANSFER_TARGET_OUT_OF_SCOPE",
			"接收人不在你的管理范围内")
	}
	to, err := s.directory.Get(ctx, toEmployeeID)
	if err != nil {
		return nil, err
	}
	if to.Status != "ACTIVE" {
		return nil, apierr.Invalid("EX_TRANSFER_TARGET_INACTIVE", "接收人已停用").
			WithMeta("employee", to.Name)
	}
	// An approval already running was routed along the SUBMITTER's reporting
	// line. Swapping the owner underneath it would leave tasks sitting with
	// people who are no longer anybody's manager for this document, so the
	// transfer waits until the approval is finished or withdrawn.
	if d.contract != nil && d.contract.Status == "PENDING_APPROVAL" {
		return nil, apierr.Conflict("EX_TRANSFER_IN_APPROVAL",
			"合同正在审批中，请先撤回或等审批结束再转移").
			WithMeta("contract_no", d.contract.ContractNo)
	}

	var moved []store.ListOwnershipTransfersRow
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		moved = nil // a retried transaction must not append twice
		record := func(bizType, bizNo string, id int64) error {
			row, err := q.RecordOwnershipTransfer(ctx, store.RecordOwnershipTransferParams{
				TenantID: tenantID, BizType: bizType, BizID: id, BizNo: bizNo,
				FromEmployeeID: fromID, FromEmployee: fromName,
				ToEmployeeID: toEmployeeID, ToEmployee: to.Name,
				Reason: reason, TransferredBy: op.ID, TransferredByName: op.Name,
			})
			if err != nil {
				return err
			}
			moved = append(moved, store.ListOwnershipTransfersRow(row))
			return nil
		}
		if d.quotation != nil {
			if _, err := q.SetQuotationOwner(ctx, store.SetQuotationOwnerParams{
				TenantID: tenantID, ID: d.quotation.ID,
				OwnerID: toEmployeeID, OwnerName: to.Name, UpdatedBy: op.ID,
			}); err != nil {
				return err
			}
			if err := record(BizTypeQuotation, d.quotation.QuoteNo, d.quotation.ID); err != nil {
				return err
			}
		}
		if d.contract != nil {
			if _, err := q.SetContractOwner(ctx, store.SetContractOwnerParams{
				TenantID: tenantID, ID: d.contract.ID,
				OwnerID: toEmployeeID, OwnerName: to.Name, UpdatedBy: op.ID,
			}); err != nil {
				return err
			}
			if err := record(BizTypeContract, d.contract.ContractNo, d.contract.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return moved, nil
}

// resolveDeal walks from whichever document the caller named to the whole
// chain. Reads are unscoped here on purpose: the scope check that matters is
// the one on the owner, done by the caller, and it must fail with a message
// about the transfer rather than a bare "not found".
func (s *Service) resolveDeal(ctx context.Context, tenantID int64, bizType string, bizID int64) (deal, error) {
	switch bizType {
	case BizTypeQuotation:
		quote, _, err := s.GetQuotation(ctx, tenantID, bizID)
		if err != nil {
			return deal{}, err
		}
		d := deal{quotation: &quote}
		live, err := s.q.LiveContractForQuotation(ctx, store.LiveContractForQuotationParams{
			TenantID: tenantID, QuotationID: &bizID,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return deal{}, err
			}
			return d, nil // never contracted, or the contract was written off
		}
		full, err := s.GetContract(ctx, tenantID, live.ID, 0)
		if err != nil {
			return deal{}, err
		}
		d.contract = &full.Contract
		return d, nil

	case BizTypeContract:
		view, err := s.GetContract(ctx, tenantID, bizID, 0)
		if err != nil {
			return deal{}, err
		}
		d := deal{contract: &view.Contract}
		if view.Contract.QuotationID == 0 {
			return d, nil
		}
		quote, _, err := s.GetQuotation(ctx, tenantID, view.Contract.QuotationID)
		if err != nil {
			return deal{}, err
		}
		d.quotation = &quote
		return d, nil

	default:
		return deal{}, apierr.Invalid("EX_TRANSFER_BIZ_TYPE_INVALID", "不支持的单据类型").
			WithMeta("biz_type", bizType)
	}
}

// ListOwnershipTransfers is the handover history of one document. It is
// scoped like reading that document, so whoever can open a contract can see
// how it got to them.
func (s *Service) ListOwnershipTransfers(ctx context.Context, tenantID int64, bizType string, bizID int64, op Operator) ([]store.ListOwnershipTransfersRow, error) {
	switch bizType {
	case BizTypeQuotation:
		if _, _, err := s.GetQuotationFor(ctx, tenantID, bizID, op); err != nil {
			return nil, err
		}
	case BizTypeContract:
		if _, err := s.GetContractFor(ctx, tenantID, bizID, 0, op); err != nil {
			return nil, err
		}
	default:
		return nil, apierr.Invalid("EX_TRANSFER_BIZ_TYPE_INVALID", "不支持的单据类型").
			WithMeta("biz_type", bizType)
	}
	return s.q.ListOwnershipTransfers(ctx, store.ListOwnershipTransfersParams{
		TenantID: tenantID, BizType: bizType, BizID: bizID,
	})
}
