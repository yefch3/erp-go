package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// BizTypePurchaseOrder is what the approval engine knows this document as.
// Approval never learns what a purchase order is; it routes on this string
// and an amount.
const BizTypePurchaseOrder = "PURCHASE_ORDER"

const (
	poDraft    = "DRAFT"
	poPending  = "PENDING_APPROVAL"
	poOrdered  = "ORDERED"
	poPartial  = "PARTIALLY_RECEIVED"
	poReceived = "RECEIVED"
)

// OrderLine is one requirement going onto an order, with the price agreed
// with the supplier.
type OrderLine struct {
	RequirementID int64
	Qty           string
	UnitPrice     string
	// Filled by an Excel import. Ordinary order entry leaves it blank because
	// the order line takes its unit directly from the selected requirement.
	UomCode string
}

type CreateOrderInput struct {
	SupplierID   int64
	SupplierCode string
	SupplierName string
	// 应付到期日（YYYY-MM-DD），这张单的钱什么时候该付出去。**员工手填**，
	// 空表示还没定。
	//
	// 和 SupplierCode / SupplierName 那种「从主数据取、不听浏览器的」快照
	// 相反：它没有主数据来源，就是这份单自己的一部分。同一家供应商这批货
	// 谈 30 天、下批谈预付，都是常事。
	PayableDueDate       string
	Currency             string
	ExpectedDate         string
	Remark               string
	Lines                []OrderLine
	FulfillmentMode      string
	DeliveryLocationType string
	DeliveryPortID       int64
	DeliveryPortCode     string
	DeliveryPortName     string
	WarehouseID          int64
	WarehouseName        string
	DeliveryAddress      string
	SourceChangeReason   string
}

// ReceiptLine is one order line arriving.
type ReceiptLine struct {
	POItemID int64
	Qty      string
}

// purchaseReceivedEvent is what inventory acts on. It carries the product
// snapshot rather than only ids, because inventory creates the stock row on
// first receipt and has nothing else to name it with.
type purchaseReceivedEvent struct {
	POID        int64          `json:"po_id"`
	PONo        string         `json:"po_no"`
	ReceiptNo   string         `json:"receipt_no"`
	WarehouseID int64          `json:"warehouse_id"`
	OperatorID  int64          `json:"operator_id"`
	Lines       []receivedLine `json:"lines"`
}

type receivedLine struct {
	ProductID   int64  `json:"product_id"`
	SkuID       int64  `json:"sku_id"`
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	UomID       int64  `json:"uom_id"`
	UomCode     string `json:"uom_code"`
	Qty         string `json:"qty"`
	// What was paid per unit, and in what. Without these the price stops at
	// the purchase order and inventory has no idea what anything cost — which
	// is exactly how a system ends up unable to answer what the goods it just
	// shipped were worth.
	UnitCost string `json:"unit_cost"`
	Currency string `json:"currency"`
}

// CreateOrder turns picked requirements into one order for one supplier.
//
// Several requirements on one document is the point of the whole thing: three
// contracts each short 500 become a single order for 1500, which is the only
// way a buyer gets a better price. So an order references requirements, and a
// requirement may be split across several orders when no supplier can cover
// it alone.
//
// A line may not exceed what the requirement still has outstanding. Ordering
// 2000 against a requirement for 1500 is either a typo or somebody stocking
// up, and stocking up belongs on a requirement of its own where it is visible
// rather than buried inside a contract's order.
type parsedOrderLine struct {
	qty, price decimal.Decimal
	uomCode    string
}

type preparedOrder struct {
	in   CreateOrderInput
	want map[int64]parsedOrderLine
	ids  []int64
}

// prepareOrder 统一校验采购单和解析明细。
// requireDelivery 为 true 时执行 P8 新采购流程的履约地点校验；旧模板导入先生成草稿，允许稍后补齐配送信息。
func (s *Service) prepareOrder(ctx context.Context, in CreateOrderInput, requireDelivery bool) (preparedOrder, error) {
	if in.SupplierID == 0 {
		return preparedOrder{}, apierr.Invalid("PO_SUPPLIER_REQUIRED", "请选择供应商")
	}
	supplier, err := s.supplierForOrder(ctx, in.SupplierID)
	if err != nil {
		return preparedOrder{}, err
	}
	// Supplier code/name are immutable document snapshots, but their source is
	// master data at write time—not display strings supplied by the browser.
	// 账期同理：从主数据取，不听浏览器的——它决定这张单什么时候该付钱。
	in.SupplierCode, in.SupplierName = supplier.Code, supplier.Name
	// 到期日相反：它没有主数据来源，是员工在这份单上填的。只校验格式——
	// 不校验它是不是在过去，补录一张上个月就该付的单是正当操作。
	if err := validBusinessDate(in.PayableDueDate, "PO_DUE_DATE_INVALID", "应付到期日"); err != nil {
		return preparedOrder{}, err
	}
	if len(in.Lines) == 0 {
		return preparedOrder{}, apierr.Invalid("PO_LINES_REQUIRED", "采购单明细不能为空")
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.FulfillmentMode == "" {
		in.FulfillmentMode = "DIRECT_SHIP"
	}
	if in.DeliveryLocationType == "" {
		in.DeliveryLocationType = "PORT"
	}
	if in.FulfillmentMode != "DIRECT_SHIP" && in.FulfillmentMode != "WAREHOUSE" {
		return preparedOrder{}, apierr.Invalid("PO_FULFILLMENT_INVALID", "请选择直发或入库后发货")
	}
	if requireDelivery && in.FulfillmentMode == "WAREHOUSE" {
		in.DeliveryLocationType = "WAREHOUSE"
		if in.WarehouseID == 0 {
			return preparedOrder{}, apierr.Invalid("PO_WAREHOUSE_REQUIRED", "入库后发货必须选择仓库")
		}
	} else if requireDelivery && in.DeliveryLocationType == "PORT" && in.DeliveryPortID == 0 && strings.TrimSpace(in.DeliveryPortName) == "" {
		return preparedOrder{}, apierr.Invalid("PO_DELIVERY_PORT_REQUIRED", "直发到港口时请选择收货港口")
	} else if requireDelivery && in.DeliveryLocationType == "CUSTOM" && strings.TrimSpace(in.DeliveryAddress) == "" {
		return preparedOrder{}, apierr.Invalid("PO_DELIVERY_ADDRESS_REQUIRED", "自定义收货地点不能为空")
	}

	want := make(map[int64]parsedOrderLine, len(in.Lines))
	ids := make([]int64, 0, len(in.Lines))
	for _, l := range in.Lines {
		qty, err := decimal.NewFromString(l.Qty)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return preparedOrder{}, apierr.Invalid("PO_QTY_INVALID", "采购数量必须大于 0")
		}
		price, err := decimal.NewFromString(orZero(l.UnitPrice))
		if err != nil || price.IsNegative() {
			return preparedOrder{}, apierr.Invalid("PO_PRICE_INVALID", "单价不能为负数")
		}
		if _, dup := want[l.RequirementID]; dup {
			return preparedOrder{}, apierr.Invalid("PO_LINE_DUPLICATED",
				"同一采购需求在一张采购单里只能出现一次")
		}
		want[l.RequirementID] = parsedOrderLine{qty: qty, price: price, uomCode: strings.TrimSpace(l.UomCode)}
		ids = append(ids, l.RequirementID)
	}
	return preparedOrder{in: in, want: want, ids: ids}, nil
}

func (s *Service) createPreparedOrder(ctx context.Context, tx pgx.Tx, tenantID int64, prepared preparedOrder, op Operator) (store.CreatePurchaseOrderRow, error) {
	q := s.q.WithTx(tx)
	in, want, ids := prepared.in, prepared.want, prepared.ids
	var head store.CreatePurchaseOrderRow
	reqs, err := q.RequirementsForOrder(ctx, store.RequirementsForOrderParams{
		TenantID: tenantID, Ids: ids,
	})
	if err != nil {
		return head, err
	}
	if len(reqs) != len(ids) {
		return head, apierr.Invalid("PO_REQUIREMENT_NOT_FOUND", "有采购需求不存在，请刷新后重试")
	}
	reserved := make(map[int64]decimal.Decimal)
	imported := false
	for _, line := range want {
		imported = imported || line.uomCode != ""
	}
	if imported {
		rows, err := q.DraftReservedQtyForRequirements(ctx, store.DraftReservedQtyForRequirementsParams{
			TenantID: tenantID, Ids: ids,
		})
		if err != nil {
			return head, err
		}
		for _, row := range rows {
			qty, err := decimal.NewFromString(row.ReservedQty)
			if err != nil {
				return head, err
			}
			reserved[row.RequirementID] = qty
		}
	}

	total := decimal.Zero
	allExecutionRequote := true
	executionContractNo := ""
	var executionContractID int64
	var quoteID, scenarioID, inheritedSupplierID, factoryID int64
	var quoteNo, factoryCode, factoryName string
	for _, r := range reqs {
		p := want[r.ID]
		if r.Status == "CANCELLED" || r.Status == "SUPERSEDED" {
			return head, apierr.Invalid("PO_REQUIREMENT_CLOSED",
				"「"+r.ProductName+"」的采购需求已关闭，不能下单").
				WithMeta("status", r.Status)
		}
		if p.uomCode != "" && !strings.EqualFold(p.uomCode, strings.TrimSpace(r.UomCode)) {
			return head, apierr.Invalid("PO_IMPORT_UNIT_MISMATCH", "Excel 单位与采购需求不一致").
				WithMeta("excel_unit", p.uomCode).WithMeta("requirement_unit", r.UomCode)
		}
		open, err := decimal.NewFromString(r.OpenQty)
		if err != nil {
			return head, err
		}
		available := open.Sub(reserved[r.ID])
		if p.qty.GreaterThan(available) {
			// The guard that keeps an order honest about what it is for.
			return head, apierr.Invalid("PO_EXCEEDS_REQUIREMENT",
				"「"+r.ProductName+"」下单数量超过需求未下单部分").
				WithMeta("requested", p.qty.String()).
				WithMeta("open", available.String())
		}
		total = total.Add(p.qty.Mul(p.price))
		if r.Status != "WAITING_REQUOTE" {
			allExecutionRequote = false
		} else if executionContractNo == "" {
			executionContractNo = r.ContractNo
			executionContractID = r.ContractID
		} else if executionContractNo != r.ContractNo {
			return head, apierr.Invalid("PO_EXECUTION_CONTRACT_MIXED", "实单重新询价一次只能处理一份外销合同")
		}
		if r.Source == "CUSTOMER_QUOTATION" && r.Status != "WAITING_REQUOTE" {
			if quoteID == 0 {
				quoteID, quoteNo, scenarioID = r.QuotationID, r.QuotationNo, r.CostScenarioID
				inheritedSupplierID = r.InheritedSupplierID
				factoryID, factoryCode, factoryName = r.InheritedFactoryID, r.InheritedFactoryCode, r.InheritedFactoryName
			}
			if r.QuotationID != quoteID || r.InheritedSupplierID != inheritedSupplierID {
				return head, apierr.Invalid("PO_QUOTATION_SUPPLIER_MIXED", "一张采购单只能包含同一客户报价、同一供应商的待下单明细")
			}
			// 换一家供应商要说明白为什么。
			//
			// 报给客户的价是按成本方案里那家工厂算出来的，换一家，利润就
			// 跟着变——这是必须留痕的偏离，不是随手改个下拉框。但也不能像
			// 从前那样一口回绝：这批只供得了 80 吨、剩下 20 吨得另找一家，
			// 是这行的常事，堵死了员工就只能在系统外面办。
			//
			// 用的是这里现成的规矩：偏离已确认方案，写原因即可放行，原因
			// 随单存档（source_change_reason）。
			if in.SupplierID != r.InheritedSupplierID && strings.TrimSpace(in.SourceChangeReason) == "" {
				return head, apierr.Invalid("PO_SUPPLIER_CHANGE_REASON_REQUIRED",
					"「"+r.ProductName+"」的已确认成本方案定的是「"+r.InheritedSupplierName+"」，改向其他供应商采购必须填写原因").
					WithMeta("scenario_supplier", r.InheritedSupplierName)
			}
			if !strings.EqualFold(in.Currency, r.SourceCurrency) || !p.price.Equal(decimal.RequireFromString(r.SourceUnitPrice)) {
				if strings.TrimSpace(in.SourceChangeReason) == "" {
					return head, apierr.Invalid("PO_SOURCE_CHANGE_REASON_REQUIRED", "修改确认报价的币种或单价时必须填写原因")
				}
			}
		}
	}

	// 这份报价已经给这家供应商开过单了——接着办上一张，还是再来一张？
	//
	// 00024 之前这两种情况由一条唯一索引一起挡住。可它们的处理方式正相反：
	// 停在草稿的那张是「上次没办完」，该接着办完；已经确认下单的那张是
	// 「这次要补购」，该放行。分开判断，各走各的路。
	if quoteID > 0 {
		existing, err := q.LiveOrdersForQuotationSupplier(ctx, store.LiveOrdersForQuotationSupplierParams{
			TenantID: tenantID, QuotationID: quoteID, SupplierID: in.SupplierID,
		})
		if err != nil {
			return head, err
		}
		for _, prior := range existing {
			// 还没确认的草稿。前端认这个错误码，会把上一张调出来接着办，
			// 免得留下两张半成品。措辞保持原样，别把前端的判断打散。
			if prior.Status == poDraft || prior.Status == poPending || prior.Status == "REJECTED" {
				return head, apierr.Conflict("PO_QUOTATION_ALREADY_ORDERED",
					"该客户报价与供应商已经生成采购单").WithMeta("po_no", prior.PoNo)
			}
		}
		if len(existing) > 0 && strings.TrimSpace(in.SourceChangeReason) == "" {
			last := existing[len(existing)-1]
			return head, apierr.Invalid("PO_QUOTATION_REORDER_REASON_REQUIRED",
				"这份客户报价已经向「"+in.SupplierName+"」下过采购单（"+last.PoNo+"）。"+
					"补购剩下的数量请填写原因；如果只是重复提交，请关掉本页").
				WithMeta("po_no", last.PoNo)
		}
	}

	// The number is drawn only once every line has passed. Asking earlier
	// would burn one on each refusal, and refusals are routine here, so
	// the order series would jump and look like lost paperwork.
	for attempt := 0; attempt < 2; attempt++ {
		no := executionContractNo
		var err error
		if !allExecutionRequote || no == "" {
			no, err = s.numbering.Next(ctx, "PURCHASE_ORDER")
			if err != nil {
				return head, err
			}
		}
		// PostgreSQL marks a transaction failed after a unique violation. A
		// savepoint lets us roll back only the collided insert and safely draw
		// one new number without losing the requirement locks.
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			return head, err
		}
		head, err = store.New(savepoint).CreatePurchaseOrder(ctx, store.CreatePurchaseOrderParams{
			TenantID: tenantID, PoNo: no, SupplierID: in.SupplierID,
			SupplierCode: in.SupplierCode, SupplierName: in.SupplierName,
			PayableDueDate: in.PayableDueDate,
			Currency:       in.Currency, TotalAmount: total.StringFixed(2),
			ExpectedDate: in.ExpectedDate, BuyerID: op.ID, BuyerName: op.Name,
			Remark: in.Remark, SourceQuotationID: quoteID, SourceQuotationNo: quoteNo,
			SourceCostScenarioID: scenarioID, FactoryID: factoryID, FactoryCode: factoryCode, FactoryName: factoryName,
			FulfillmentMode: in.FulfillmentMode, DeliveryLocationType: in.DeliveryLocationType,
			DeliveryPortID: in.DeliveryPortID, DeliveryPortCode: in.DeliveryPortCode, DeliveryPortName: in.DeliveryPortName,
			WarehouseID: in.WarehouseID, WarehouseName: in.WarehouseName,
			DeliveryAddress: in.DeliveryAddress, SourceChangeReason: in.SourceChangeReason,
		})
		if err == nil {
			if err = savepoint.Commit(ctx); err != nil {
				return head, err
			}
			break
		}
		_ = savepoint.Rollback(ctx)
		var pgErr *pgconn.PgError
		if attempt == 0 && errors.As(err, &pgErr) && pgErr.Code == "23505" {
			continue
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return head, apierr.Conflict("PO_NUMBER_CONFLICT", "采购单号生成冲突，请重试")
		}
		return head, err
	}
	if allExecutionRequote {
		_, err := tx.Exec(ctx, `UPDATE purchase_orders SET source_business_id=$3,export_contract_no=$4,business_document_no=$4,payment_terms=$5 WHERE tenant_id=$1 AND id=$2`, tenantID, head.ID, executionContractID, executionContractNo, strings.TrimSpace(in.Remark))
		if err != nil {
			return head, err
		}
	}
	for _, r := range reqs {
		p := want[r.ID]
		if _, err := q.CreatePurchaseOrderItem(ctx, store.CreatePurchaseOrderItemParams{
			TenantID: tenantID, PoID: head.ID, RequirementID: r.ID,
			ProductID: r.ProductID, SkuID: r.SkuID,
			ProductCode: r.ProductCode, ProductName: r.ProductName, Spec: r.Spec,
			UomID: r.UomID, UomCode: r.UomCode,
			Qty: p.qty.String(), UnitPrice: p.price.String(),
			Amount: p.qty.Mul(p.price).StringFixed(2),
		}); err != nil {
			return head, err
		}
	}
	return head, nil
}

func (s *Service) CreateOrder(ctx context.Context, tenantID int64, in CreateOrderInput, op Operator) (store.CreatePurchaseOrderRow, error) {
	prepared, err := s.prepareOrder(ctx, in, true)
	if err != nil {
		return store.CreatePurchaseOrderRow{}, err
	}
	var head store.CreatePurchaseOrderRow
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var createErr error
		head, createErr = s.createPreparedOrder(ctx, tx, tenantID, prepared, op)
		return createErr
	})
	if err != nil {
		return store.CreatePurchaseOrderRow{}, err
	}
	s.nudge(ctx, tenantID)
	return head, nil
}

// UpdateOrder replaces the editable snapshot and all lines in one
// transaction. Draft lines reserve no demand, so replacement needs no release;
// the same locked requirement validation used by creation is repeated here.
func (s *Service) UpdateOrder(
	ctx context.Context,
	tenantID, id int64,
	in CreateOrderInput,
	op Operator,
) (store.UpdatePurchaseOrderDraftRow, error) {
	prepared, err := s.prepareOrder(ctx, in, true)
	if err != nil {
		return store.UpdatePurchaseOrderDraftRow{}, err
	}
	in, want, ids := prepared.in, prepared.want, prepared.ids

	var updated store.UpdatePurchaseOrderDraftRow
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, lockErr := q.GetPurchaseOrderForUpdate(ctx, store.GetPurchaseOrderForUpdateParams{
			TenantID: tenantID, ID: id,
		})
		if lockErr == pgx.ErrNoRows {
			return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
		}
		if lockErr != nil {
			return lockErr
		}
		if head.Status != poDraft && head.Status != "REJECTED" {
			return apierr.Conflict("PO_NOT_EDITABLE", "只有草稿或已驳回的采购单可以编辑").
				WithMeta("status", head.Status)
		}

		reqs, reqErr := q.RequirementsForOrder(ctx, store.RequirementsForOrderParams{
			TenantID: tenantID, Ids: ids,
		})
		if reqErr != nil {
			return reqErr
		}
		if len(reqs) != len(ids) {
			return apierr.Invalid("PO_REQUIREMENT_NOT_FOUND", "有采购需求不存在，请刷新后重试")
		}
		total := decimal.Zero
		for _, requirement := range reqs {
			line := want[requirement.ID]
			if requirement.Status != "PENDING" && requirement.Status != "PARTIALLY_ORDERED" && requirement.Status != "WAITING_REQUOTE" {
				return apierr.Invalid("PO_REQUIREMENT_CLOSED", "「"+requirement.ProductName+"」的采购需求已关闭，不能下单").
					WithMeta("status", requirement.Status)
			}
			open, parseErr := decimal.NewFromString(requirement.OpenQty)
			if parseErr != nil {
				return parseErr
			}
			if line.qty.GreaterThan(open) {
				return apierr.Invalid("PO_EXCEEDS_REQUIREMENT", "「"+requirement.ProductName+"」下单数量超过需求未下单部分").
					WithMeta("requested", line.qty.String()).WithMeta("open", open.String())
			}
			if requirement.Source == "CUSTOMER_QUOTATION" && requirement.Status != "WAITING_REQUOTE" {
				// 换成另一份报价始终不行——那已经是另一笔生意了。换供应商
				// 则与新建单一个规矩：写明原因就放行。
				if head.SourceQuotationID == 0 || requirement.QuotationID != head.SourceQuotationID {
					return apierr.Invalid("PO_QUOTATION_SOURCE_CHANGED", "报价转入的采购单不能改为其他报价")
				}
				if requirement.InheritedSupplierID != in.SupplierID &&
					strings.TrimSpace(in.SourceChangeReason) == "" {
					return apierr.Invalid("PO_SUPPLIER_CHANGE_REASON_REQUIRED",
						"「"+requirement.ProductName+"」的已确认成本方案定的是「"+requirement.InheritedSupplierName+"」，改向其他供应商采购必须填写原因").
						WithMeta("scenario_supplier", requirement.InheritedSupplierName)
				}
				if !strings.EqualFold(in.Currency, requirement.SourceCurrency) ||
					!line.price.Equal(decimal.RequireFromString(requirement.SourceUnitPrice)) {
					if strings.TrimSpace(in.SourceChangeReason) == "" {
						return apierr.Invalid("PO_SOURCE_CHANGE_REASON_REQUIRED", "修改确认报价的币种或单价时必须填写原因")
					}
				}
			}
			total = total.Add(line.qty.Mul(line.price))
		}

		updated, reqErr = q.UpdatePurchaseOrderDraft(ctx, store.UpdatePurchaseOrderDraftParams{
			TenantID: tenantID, ID: id, SupplierID: in.SupplierID,
			SupplierCode: in.SupplierCode, SupplierName: in.SupplierName,
			PayableDueDate: in.PayableDueDate,
			Currency:       in.Currency, TotalAmount: total.StringFixed(2),
			ExpectedDate: in.ExpectedDate, BuyerID: op.ID, BuyerName: op.Name,
			Remark: in.Remark, FulfillmentMode: in.FulfillmentMode,
			DeliveryLocationType: in.DeliveryLocationType,
			DeliveryPortID:       in.DeliveryPortID, DeliveryPortCode: in.DeliveryPortCode,
			DeliveryPortName: in.DeliveryPortName, WarehouseID: in.WarehouseID,
			WarehouseName: in.WarehouseName, DeliveryAddress: in.DeliveryAddress,
			SourceChangeReason: in.SourceChangeReason,
		})
		if reqErr != nil {
			return reqErr
		}
		if reqErr = q.DeletePurchaseOrderItems(ctx, store.DeletePurchaseOrderItemsParams{
			TenantID: tenantID, PoID: id,
		}); reqErr != nil {
			return reqErr
		}
		for _, requirement := range reqs {
			line := want[requirement.ID]
			if _, reqErr = q.CreatePurchaseOrderItem(ctx, store.CreatePurchaseOrderItemParams{
				TenantID: tenantID, PoID: id, RequirementID: requirement.ID,
				ProductID: requirement.ProductID, SkuID: requirement.SkuID,
				ProductCode: requirement.ProductCode, ProductName: requirement.ProductName,
				Spec: requirement.Spec, UomID: requirement.UomID, UomCode: requirement.UomCode,
				Qty: line.qty.String(), UnitPrice: line.price.String(),
				Amount: line.qty.Mul(line.price).StringFixed(2),
			}); reqErr != nil {
				return reqErr
			}
		}
		return nil
	})
	if err != nil {
		return store.UpdatePurchaseOrderDraftRow{}, err
	}
	s.nudge(ctx, tenantID)
	return updated, nil
}

func (s *Service) supplierForOrder(ctx context.Context, id int64) (Supplier, error) {
	if s.suppliers == nil {
		return Supplier{}, apierr.Internal("PO_SUPPLIER_DIRECTORY_UNAVAILABLE", "供应商主数据服务未配置")
	}
	supplier, err := s.suppliers.Get(ctx, id)
	if err != nil {
		return Supplier{}, err
	}
	if supplier.ID == 0 || supplier.Status != "ACTIVE" {
		return Supplier{}, apierr.Invalid("PO_SUPPLIER_INACTIVE", "供应商不存在或已停用，请重新选择")
	}
	return supplier, nil
}

// SubmitOrder 将采购员确认完成的草稿提交给审批服务。
// 提交只改变审批状态，不占用采购需求；只有审批通过后才正式计入已采购数量。
func (s *Service) SubmitOrder(ctx context.Context, tenantID, id int64, op Operator) (string, int64, error) {
	head, err := s.GetOrder(ctx, tenantID, id)
	if err != nil {
		return "", 0, err
	}
	if head.Status != poDraft && head.Status != "REJECTED" {
		return "", 0, apierr.Conflict("PO_NOT_SUBMITTABLE", "只有草稿或已驳回的采购单可以提交审批").
			WithMeta("status", head.Status)
	}
	items, err := s.q.PurchaseOrderItems(ctx, store.PurchaseOrderItemsParams{
		TenantID: tenantID, PoID: id,
	})
	if err != nil {
		return "", 0, err
	}
	if len(items) == 0 {
		return "", 0, apierr.Invalid("PO_LINES_REQUIRED", "采购单没有明细，无法提交")
	}

	instanceID, err := s.approvals.Submit(ctx, ApprovalSubmission{
		BizType: BizTypePurchaseOrder, BizID: head.ID, BizNo: head.PoNo,
		Summary:     orderSummary(head, items),
		SubmitterID: op.ID, SubmitterName: op.Name,
		Amount: head.TotalAmount,
	})
	if err != nil {
		return "", 0, err
	}
	if err := s.q.SetPurchaseOrderSubmitted(ctx, store.SetPurchaseOrderSubmittedParams{
		TenantID: tenantID, ID: id, InstanceID: instanceID,
	}); err != nil {
		return "", 0, err
	}
	s.nudge(ctx, tenantID)
	return poPending, instanceID, nil
}

// commitOrderRequirements 在采购单和全部采购需求均已锁定的事务中执行。
// 它既防止重复占用同一需求，也保证“批准成功”和“进入已下单”不会分裂。
func commitOrderRequirements(ctx context.Context, q *store.Queries, tenantID, poID int64) error {
	items, err := q.PurchaseOrderItemsForUpdate(ctx, store.PurchaseOrderItemsForUpdateParams{
		TenantID: tenantID, PoID: poID,
	})
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return apierr.Invalid("PO_LINES_REQUIRED", "采购单没有明细，无法确认")
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.RequirementID)
	}
	requirements, err := q.RequirementsForOrder(ctx, store.RequirementsForOrderParams{
		TenantID: tenantID, Ids: ids,
	})
	if err != nil {
		return err
	}
	if reason := approvalRequirementConflict(requirements, items); reason != "" {
		return apierr.Conflict("PO_REQUIREMENT_CHANGED", reason)
	}
	for _, item := range items {
		if _, err = q.AddRequirementOrdered(ctx, store.AddRequirementOrderedParams{
			TenantID: tenantID, ID: item.RequirementID, Qty: item.Qty,
		}); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.Conflict("PO_REQUIREMENT_CHANGED", "采购需求状态或剩余数量已变化，请刷新后重试")
			}
			return err
		}
	}
	return q.SetPurchaseOrderOrdered(ctx, store.SetPurchaseOrderOrderedParams{
		TenantID: tenantID, ID: poID,
	})
}

// ApplyApprovalDecision is what the Kafka consumer calls. Approving an order
// is the moment the company commits to buying, so this is where requirements
// finally count as ordered.
func (s *Service) ApplyApprovalDecision(
	ctx context.Context,
	tenantID, poID, approvalInstanceID int64,
	result, comment string,
	claim EventClaim,
) (string, error) {
	status := ""
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// 认领与这一笔业务写入同生共死：崩溃一起回滚，提交一起落库。
		// 见 eventclaim.go。
		if err := claim(ctx, tx); err != nil {
			return err
		}
		q := s.q.WithTx(tx)
		head, err := q.GetPurchaseOrderForUpdate(ctx, store.GetPurchaseOrderForUpdateParams{
			TenantID: tenantID, ID: poID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
		}
		if err != nil {
			return err
		}
		// A rejected order can be submitted again, producing a new approval
		// instance. A delayed event from the old instance must not decide the new
		// submission merely because both events name the same purchase order.
		if head.ApprovalInstanceID != approvalInstanceID {
			status = head.Status
			return nil
		}
		if head.Status != poPending {
			// A redelivered decision. The first one already moved it.
			status = head.Status
			return nil
		}
		items, err := q.PurchaseOrderItemsForUpdate(ctx, store.PurchaseOrderItemsForUpdateParams{
			TenantID: tenantID, PoID: poID,
		})
		if err != nil {
			return err
		}

		if result != "APPROVED" {
			if err := q.SetPurchaseOrderRejected(ctx, store.SetPurchaseOrderRejectedParams{
				TenantID: tenantID, ID: poID, Reason: approvalRejectReason(result, comment),
			}); err != nil {
				return err
			}
			status = "REJECTED"
			return nil
		}

		ids := make([]int64, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.RequirementID)
		}
		reqs, err := q.RequirementsForOrder(ctx, store.RequirementsForOrderParams{
			TenantID: tenantID, Ids: ids,
		})
		if err != nil {
			return err
		}
		if reason := approvalRequirementConflict(reqs, items); reason != "" {
			if err := q.SetPurchaseOrderRejected(ctx, store.SetPurchaseOrderRejectedParams{
				TenantID: tenantID, ID: poID, Reason: reason,
			}); err != nil {
				return err
			}
			status = "REJECTED"
			return nil
		}

		for _, it := range items {
			if _, err := q.AddRequirementOrdered(ctx, store.AddRequirementOrderedParams{
				TenantID: tenantID, ID: it.RequirementID, Qty: it.Qty,
			}); err != nil {
				if err == pgx.ErrNoRows {
					return apierr.Conflict("PO_REQUIREMENT_CHANGED", "采购需求状态或剩余数量已变化，请重新建立采购单")
				}
				return err
			}
		}
		if err := q.SetPurchaseOrderOrdered(ctx, store.SetPurchaseOrderOrderedParams{
			TenantID: tenantID, ID: poID,
		}); err != nil {
			return err
		}
		status = poOrdered
		return nil
	})
	if err != nil {
		return "", err
	}
	s.nudge(ctx, tenantID)
	return status, nil
}

func approvalRejectReason(result, comment string) string {
	comment = strings.TrimSpace(comment)
	if comment != "" {
		return comment
	}
	if result == "RETURNED" {
		return "审批退回"
	}
	return "审批未通过"
}

// approvalRequirementConflict runs after every referenced requirement has
// been locked. Drafts deliberately do not reserve demand, so this second
// check is the point that prevents two independently valid drafts from both
// becoming commitments, and prevents an approval from reviving a requirement
// retired by a contract change.
func approvalRequirementConflict(
	reqs []store.RequirementsForOrderRow,
	items []store.PurchaseOrderItemsForUpdateRow,
) string {
	if len(reqs) != len(items) {
		return "采购需求已不存在或发生变化，请重新建立采购单"
	}
	byID := make(map[int64]store.RequirementsForOrderRow, len(reqs))
	for _, r := range reqs {
		byID[r.ID] = r
	}
	for _, it := range items {
		r, ok := byID[it.RequirementID]
		if !ok || (r.Status != "PENDING" && r.Status != "PARTIALLY_ORDERED" && r.Status != "WAITING_REQUOTE") {
			return "采购需求「" + it.ProductName + "」已经关闭或被其他采购单占用，请重新建立采购单"
		}
		open, err := decimal.NewFromString(r.OpenQty)
		if err != nil {
			return "采购需求「" + it.ProductName + "」的剩余数量无效，请重新建立采购单"
		}
		qty, err := decimal.NewFromString(it.Qty)
		if err != nil || qty.GreaterThan(open) {
			return "采购需求「" + it.ProductName + "」的剩余数量不足，请重新建立采购单"
		}
	}
	return ""
}

// CancelOrder withdraws an order and puts what it claimed back on the buying
// list. Only before anything has arrived: once goods are in the warehouse the
// order happened, and unwinding it is a return, not a cancellation.
func (s *Service) CancelOrder(ctx context.Context, tenantID, id int64, reason string, op Operator) error {
	if reason == "" {
		return apierr.Invalid("PO_REASON_REQUIRED", "请填写取消原因")
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.GetPurchaseOrderForUpdate(ctx, store.GetPurchaseOrderForUpdateParams{
			TenantID: tenantID, ID: id,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
		}
		if err != nil {
			return err
		}
		if head.Status != poDraft && head.Status != poOrdered && head.Status != "REJECTED" {
			return apierr.Conflict("PO_NOT_CANCELLABLE", "该状态的采购单不能取消").
				WithMeta("status", head.Status)
		}
		items, err := q.PurchaseOrderItemsForUpdate(ctx, store.PurchaseOrderItemsForUpdateParams{
			TenantID: tenantID, PoID: id,
		})
		if err != nil {
			return err
		}
		for _, it := range items {
			received, err := decimal.NewFromString(it.ReceivedQty)
			if err != nil {
				return err
			}
			if received.GreaterThan(decimal.Zero) {
				return apierr.Conflict("PO_ALREADY_RECEIVED",
					"该采购单已有到货，不能取消（请走退货流程）")
			}
			// Only an approved order ever claimed anything.
			if head.Status == poOrdered {
				if err := q.ReleaseRequirementOrdered(ctx, store.ReleaseRequirementOrderedParams{
					TenantID: tenantID, ID: it.RequirementID, Qty: it.Qty,
				}); err != nil {
					return err
				}
			}
		}
		return q.SetPurchaseOrderCancelled(ctx, store.SetPurchaseOrderCancelledParams{
			TenantID: tenantID, ID: id, Reason: reason,
		})
	})
	if err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	return nil
}

// ReceiveOrder 统一登记采购到货。
// warehouseID 大于 0 表示货物进入公司管理的仓库，会发送库存入账事件；
// warehouseID 等于 0 表示直接交付港口、客户或指定地点，只更新采购交付进度，
// 不写入库存。两条路径共用同一套累计数量校验，避免重复或超量收货。
func (s *Service) ReceiveOrder(ctx context.Context, tenantID, poID int64, warehouseID int64, lines []ReceiptLine, remark string, op Operator) (string, error) {
	if len(lines) == 0 {
		return "", apierr.Invalid("PO_RECEIPT_LINES_REQUIRED", "收货明细不能为空")
	}
	directDelivery := warehouseID == 0
	if !directDelivery {
		if s.warehouses == nil {
			return "", apierr.Internal("PO_WAREHOUSE_DIRECTORY_UNAVAILABLE", "仓库目录未配置")
		}
		warehouse, err := s.warehouses.Get(ctx, warehouseID)
		if err != nil {
			return "", err
		}
		if warehouse.ID == 0 || warehouse.Status != "ACTIVE" {
			return "", apierr.Invalid("PO_WAREHOUSE_INACTIVE", "仓库不存在或已停用，请重新选择")
		}
		if warehouse.Type == "VIRTUAL" {
			return "", apierr.Invalid("PO_PHYSICAL_WAREHOUSE_REQUIRED", "收货只能登记到实体仓库")
		}
	}
	want := make(map[int64]decimal.Decimal, len(lines))
	for _, l := range lines {
		qty, err := decimal.NewFromString(l.Qty)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return "", apierr.Invalid("PO_QTY_INVALID", "收货数量必须大于 0")
		}
		want[l.POItemID] = qty
	}

	receiptNo := ""
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.GetPurchaseOrderForUpdate(ctx, store.GetPurchaseOrderForUpdateParams{
			TenantID: tenantID, ID: poID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
		}
		if err != nil {
			return err
		}
		if head.Status != poOrdered && head.Status != poPartial {
			return apierr.Conflict("PO_NOT_RECEIVABLE", "只有已下单的采购单可以收货").
				WithMeta("status", head.Status)
		}
		items, err := q.PurchaseOrderItemsForUpdate(ctx, store.PurchaseOrderItemsForUpdateParams{
			TenantID: tenantID, PoID: poID,
		})
		if err != nil {
			return err
		}
		byID := make(map[int64]store.PurchaseOrderItemsForUpdateRow, len(items))
		for _, it := range items {
			byID[it.ID] = it
		}
		accepted := make(map[int64]decimal.Decimal, len(want))

		// Validate every line before drawing a number. Over-receiving is a
		// routine refusal, and asking earlier would burn a receipt number on
		// each one — the series would jump and look like lost paperwork.
		out := make([]receivedLine, 0, len(lines))
		for itemID, qty := range want {
			it, ok := byID[itemID]
			if !ok {
				return apierr.Invalid("PO_ITEM_NOT_FOUND", "收货明细不属于这张采购单")
			}
			ordered, err := decimal.NewFromString(it.Qty)
			if err != nil {
				return err
			}
			already, err := decimal.NewFromString(it.ReceivedQty)
			if err != nil {
				return err
			}
			// Over-receiving is refused rather than absorbed. A supplier
			// sending more than was ordered is a conversation, not a silent
			// stock increase nobody agreed to pay for.
			if already.Add(qty).GreaterThan(ordered) {
				return apierr.Invalid("PO_EXCEEDS_ORDERED",
					"「"+it.ProductName+"」收货数量超过订购数量").
					WithMeta("ordered", ordered.String()).
					WithMeta("received", already.String()).
					WithMeta("now", qty.String())
			}
			if err := q.AddPurchaseOrderItemReceived(ctx, store.AddPurchaseOrderItemReceivedParams{
				TenantID: tenantID, ID: itemID, Qty: qty.String(),
			}); err != nil {
				return err
			}
			if _, err := q.AddRequirementReceived(ctx, store.AddRequirementReceivedParams{
				TenantID: tenantID, ID: it.RequirementID, Qty: qty.String(),
			}); err != nil {
				return err
			}
			accepted[itemID] = qty
			out = append(out, receivedLine{
				ProductID: it.ProductID, SkuID: it.SkuID,
				ProductCode: it.ProductCode, ProductName: it.ProductName,
				UomID: it.UomID, UomCode: it.UomCode, Qty: qty.String(),
				UnitCost: it.UnitPrice, Currency: head.Currency,
			})
		}

		no, err := s.numbering.Next(ctx, "INBOUND")
		if err != nil {
			return err
		}
		receipt, err := q.CreatePurchaseReceipt(ctx, store.CreatePurchaseReceiptParams{
			TenantID: tenantID, PoID: poID, ReceiptNo: no, WarehouseID: warehouseID,
			OperatorID: op.ID, OperatorName: op.Name, Remark: remark,
		})
		if err != nil {
			return err
		}
		receiptNo = receipt.ReceiptNo
		for itemID, qty := range accepted {
			if err := q.CreatePurchaseReceiptItem(ctx, store.CreatePurchaseReceiptItemParams{
				TenantID: tenantID, ReceiptID: receipt.ID, PoItemID: itemID, Qty: qty.String(),
			}); err != nil {
				return err
			}
		}

		progress, err := q.PurchaseOrderReceiptProgress(ctx, store.PurchaseOrderReceiptProgressParams{
			TenantID: tenantID, PoID: poID,
		})
		if err != nil {
			return err
		}
		next := poPartial
		if progress.OutstandingLines == 0 {
			next = poReceived
		}
		if err := q.SetPurchaseOrderStatus(ctx, store.SetPurchaseOrderStatusParams{
			TenantID: tenantID, ID: poID, NewStatus: next,
		}); err != nil {
			return err
		}

		// 直接交付不形成公司库存，因此只保存收货单和采购进度；只有进入
		// 公司管理仓库时才发布库存入账事件，防止港口直送被重复计入库存。
		if directDelivery {
			return nil
		}
		payload, err := json.Marshal(purchaseReceivedEvent{
			POID: poID, PONo: head.PoNo, ReceiptNo: receipt.ReceiptNo,
			WarehouseID: warehouseID, OperatorID: op.ID, Lines: out,
		})
		if err != nil {
			return err
		}
		return outbox.Append(ctx, tx, outbox.Event{
			TenantID: tenantID, AggregateType: "purchase_order",
			AggregateID: strconv.FormatInt(poID, 10),
			EventType:   "PurchaseReceived", Payload: payload,
		})
	})
	if err != nil {
		return "", err
	}
	s.nudge(ctx, tenantID)
	return receiptNo, nil
}

type OrderFilter struct {
	Status  string
	Keyword string
	// 只看「批下来了、还没发给供应商」的单——工作台的行动数字（B4）。
	Unsent bool
}

func (s *Service) ListOrders(ctx context.Context, tenantID int64, f OrderFilter, page, size int32, operators ...Operator) ([]store.ListPurchaseOrdersRow, int64, error) {
	// Purchase orders are the shared execution ledger of the procurement
	// team. Anyone who passed the endpoint's procurement:order:read permission
	// sees every order in the tenant; buyer_id remains ownership information,
	// not a list-visibility fence. Write actions keep their own authorization.
	_ = operators
	page, size = normalizePage(page, size)
	rows, err := s.q.ListPurchaseOrders(ctx, store.ListPurchaseOrdersParams{
		TenantID: tenantID, Status: f.Status, Keyword: f.Keyword, Unsent: f.Unsent,
		ScopeAll: true,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

func (s *Service) GetOrder(ctx context.Context, tenantID, id int64) (store.GetPurchaseOrderRow, error) {
	row, err := s.q.GetPurchaseOrder(ctx, store.GetPurchaseOrderParams{TenantID: tenantID, ID: id})
	if err == pgx.ErrNoRows {
		return store.GetPurchaseOrderRow{}, apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
	}
	return row, err
}

func (s *Service) OrderItems(ctx context.Context, tenantID, poID int64) ([]store.PurchaseOrderItemsRow, error) {
	return s.q.PurchaseOrderItems(ctx, store.PurchaseOrderItemsParams{TenantID: tenantID, PoID: poID})
}

func (s *Service) OrderReceipts(ctx context.Context, tenantID, poID int64) ([]store.ListPurchaseReceiptsRow, error) {
	return s.q.ListPurchaseReceipts(ctx, store.ListPurchaseReceiptsParams{TenantID: tenantID, PoID: poID})
}

// orderSummary 生成审批待办中使用的采购单只读摘要。
// 审批人无需读取采购服务数据库，也能看到供应商、金额、交期和逐项采购内容。
func orderSummary(head store.GetPurchaseOrderRow, items []store.PurchaseOrderItemsRow) string {
	type line struct {
		Product string `json:"product"`
		Qty     string `json:"qty"`
		Price   string `json:"price"`
		Amount  string `json:"amount"`
	}
	body := struct {
		Supplier string `json:"supplier"`
		Currency string `json:"currency"`
		Amount   string `json:"amount"`
		Expected string `json:"expected_date"`
		Lines    []line `json:"lines"`
	}{
		Supplier: head.SupplierName, Currency: head.Currency,
		Amount: head.TotalAmount, Expected: head.ExpectedDate,
	}
	for _, item := range items {
		body.Lines = append(body.Lines, line{
			Product: item.ProductName, Qty: item.Qty, Price: item.UnitPrice, Amount: item.Amount,
		})
	}
	out, err := json.Marshal(body)
	if err != nil {
		return ""
	}
	return string(out)
}

func orZero(v string) string {
	if v == "" {
		return "0"
	}
	return v
}

// RequirementOrders lists the orders covering one requirement. This is what
// turns an outstanding line from "nobody has bought this" into "it is on
// PO-0007, due the 15th" — the same row, two entirely different next steps.
func (s *Service) RequirementOrders(ctx context.Context, tenantID, requirementID int64) ([]store.OrdersForRequirementRow, error) {
	return s.q.OrdersForRequirement(ctx, store.OrdersForRequirementParams{
		TenantID: tenantID, RequirementID: requirementID,
	})
}

// ReopenRequirement puts a closed requirement back on the buying list, for
// the case the close was wrong — stock that briefly covered it went elsewhere,
// or a contract change was reverted.
func (s *Service) ReopenRequirement(ctx context.Context, tenantID, id int64, op Operator) (string, error) {
	current, err := s.GetRequirement(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if current.Status == "PENDING" || current.Status == "PARTIALLY_ORDERED" {
		return "", apierr.Conflict("PR_ALREADY_OPEN", "该需求本来就是待采购状态").
			WithMeta("status", current.Status)
	}
	status, err := s.q.ReopenRequirement(ctx, store.ReopenRequirementParams{
		TenantID: tenantID, ID: id,
	})
	if err == pgx.ErrNoRows {
		// Either it has an order behind it, or it is already received.
		return "", apierr.Conflict("PR_CANNOT_REOPEN",
			"已下过采购单或已到货的需求不能重新打开").
			WithMeta("status", current.Status)
	}
	if err != nil {
		return "", err
	}
	s.nudge(ctx, tenantID)
	return status, nil
}
