package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// ExistingContractInput is a signed agreement being handed to the ERP after
// some or none of its work happened elsewhere. It is not a shortcut for an
// unsigned draft: the paper contract is already authoritative.
type ExistingContractInput struct {
	CustomerID            int64
	Currency              string
	Terms                 Terms
	Items                 []ItemInput
	ExternalContractNo    string
	SalesEmployeeID       int64
	SignedDate            string
	EffectiveDate         string
	OpeningReceivedAmount string
	FilePending           bool
	ProcurementEmployeeID int64
	SupplierID            int64
}

// ImportExistingContract records one immutable opening snapshot and emits an
// effective-contract event marked for direct reconstruction of the historical
// purchase order and its opening receipt progress.
func (s *Service) ImportExistingContract(ctx context.Context, tenantID int64, in ExistingContractInput, op Operator) (ContractView, error) {
	if in.CustomerID == 0 {
		return ContractView{}, apierr.Invalid("EX_CUSTOMER_REQUIRED", "请选择客户")
	}
	if strings.TrimSpace(in.Currency) == "" {
		return ContractView{}, apierr.Invalid("EX_CURRENCY_REQUIRED", "请选择币种")
	}
	if len(in.Items) == 0 {
		return ContractView{}, apierr.Invalid("EX_ITEMS_REQUIRED", "请至少录入一条合同明细")
	}
	if in.ProcurementEmployeeID == 0 {
		return ContractView{}, apierr.Invalid("EX_PROCUREMENT_OWNER_REQUIRED", "请选择原负责采购人员")
	}
	if in.SupplierID == 0 {
		return ContractView{}, apierr.Invalid("EX_SUPPLIER_REQUIRED", "请选择原供应商")
	}
	if s.directory == nil {
		return ContractView{}, apierr.Invalid("EX_PROCUREMENT_OWNER_UNAVAILABLE", "暂时无法核对负责采购人员")
	}
	procurementEmployee, err := s.directory.Get(ctx, in.ProcurementEmployeeID)
	if err != nil {
		return ContractView{}, err
	}
	if procurementEmployee.Status != "ACTIVE" {
		return ContractView{}, apierr.Invalid("EX_PROCUREMENT_OWNER_INACTIVE", "原负责采购人员已停用")
	}
	for value, rule := range map[string][2]string{
		in.SignedDate:              {"EX_SIGNED_DATE_INVALID", "签订日期"},
		in.EffectiveDate:           {"EX_EFFECTIVE_DATE_INVALID", "生效日期"},
		in.Terms.DeliveryDate:      {"EX_DELIVERY_DATE_INVALID", "交货日期"},
		in.Terms.ReceivableDueDate: {"EX_DUE_DATE_INVALID", "应收到期日"},
	} {
		if err := validBusinessDate(value, rule[0], rule[1]); err != nil {
			return ContractView{}, err
		}
	}
	if in.SignedDate == "" || in.EffectiveDate == "" {
		return ContractView{}, apierr.Invalid("EX_EXISTING_CONTRACT_DATES_REQUIRED", "请填写签订日期和生效日期")
	}
	signed, _ := time.Parse("2006-01-02", in.SignedDate)
	effective, _ := time.Parse("2006-01-02", in.EffectiveDate)
	if effective.Before(signed) {
		return ContractView{}, apierr.Invalid("EX_EFFECTIVE_BEFORE_SIGNED", "生效日期不能早于签订日期")
	}

	customer, err := s.customers.Get(ctx, in.CustomerID)
	if err != nil {
		return ContractView{}, err
	}
	if customer.Status != "ACTIVE" {
		return ContractView{}, apierr.Invalid("EX_CUSTOMER_INACTIVE", "客户已停用，不能录入合同").WithMeta("customer", customer.Name)
	}

	ownerID, ownerName := op.ID, op.Name
	if in.SalesEmployeeID != 0 && in.SalesEmployeeID != op.ID {
		visible, err := s.visibleTo(ctx, op)
		if err != nil {
			return ContractView{}, err
		}
		if !allowedOwner(visible, in.SalesEmployeeID) {
			return ContractView{}, apierr.Permission("EX_CONTRACT_OWNER_OUT_OF_SCOPE", "只能选择本人或管理范围内的负责销售")
		}
		if s.directory == nil {
			return ContractView{}, apierr.Invalid("EX_CONTRACT_OWNER_UNAVAILABLE", "暂时无法核对负责销售")
		}
		employee, err := s.directory.Get(ctx, in.SalesEmployeeID)
		if err != nil {
			return ContractView{}, err
		}
		if employee.Status != "ACTIVE" {
			return ContractView{}, apierr.Invalid("EX_CONTRACT_OWNER_INACTIVE", "负责销售已停用")
		}
		ownerID, ownerName = employee.ID, employee.Name
	}

	// An inherited paper contract can mention a product that has never been
	// created in Product Master.  Preserve that wording and unit as a contract
	// snapshot instead of forcing Sales to create unrelated master data first.
	priced, total, err := s.priceExistingLines(ctx, in.Items)
	if err != nil {
		return ContractView{}, err
	}
	// openingDecimal returns *apierr.Error so callers can add the line number.
	// Do not assign its nil pointer into the existing `error` interface: a
	// typed nil inside an interface compares non-nil and used to escape as a
	// phantom error, then panic in the gRPC error interceptor.
	openingReceived, openingErr := openingDecimal(in.OpeningReceivedAmount, "EX_OPENING_RECEIVED_INVALID", "已收款金额")
	if openingErr != nil {
		return ContractView{}, openingErr
	}
	if openingReceived.GreaterThan(total) {
		return ContractView{}, apierr.Invalid("EX_OPENING_RECEIVED_EXCEEDS_TOTAL", "已收款金额不能超过合同总金额")
	}
	type openingLine struct{ procured, arrived, shipped decimal.Decimal }
	opening := make([]openingLine, len(priced))
	for i, line := range priced {
		// Sales does not need to know the historical procurement cost while
		// taking over an offline contract. Keep it explicitly unknown/zero;
		// procurement can supplement it later in its own workflow.
		line.in.PurchaseUnitPrice = "0"
		priced[i] = line
		values := []struct {
			value, code, label string
			target             *decimal.Decimal
		}{
			{line.in.OpeningProcuredQty, "EX_OPENING_PROCURED_INVALID", "已落实采购数量", &opening[i].procured},
			{line.in.OpeningArrivedQty, "EX_OPENING_ARRIVED_INVALID", "已到货数量", &opening[i].arrived},
			{line.in.OpeningShippedQty, "EX_OPENING_SHIPPED_INVALID", "已发运数量", &opening[i].shipped},
		}
		for _, value := range values {
			parsed, err := openingDecimal(value.value, value.code, value.label)
			if err != nil {
				return ContractView{}, err.WithMeta("line", strconv.Itoa(i+1))
			}
			if parsed.GreaterThan(line.qty) {
				return ContractView{}, apierr.Invalid(value.code, value.label+"不能超过合同数量").WithMeta("line", strconv.Itoa(i+1))
			}
			*value.target = parsed
		}
		// This intake path reconstructs an order that was already placed before
		// the ERP took over. The whole contract line therefore belongs to the
		// historical PO; arrival progress is recorded separately below.
		opening[i].procured = line.qty
	}

	rate, err := s.rates.Latest(ctx, in.Currency)
	if err != nil {
		return ContractView{}, err
	}
	hs, err := s.hsCodesForItems(ctx, in.Items)
	if err != nil {
		return ContractView{}, err
	}
	contractNo, err := s.number.Next(ctx, "CONTRACT")
	if err != nil {
		return ContractView{}, err
	}
	terms := Terms{
		BuyerName: orDefault(in.Terms.BuyerName, customer.Name), BuyerAddress: orDefault(in.Terms.BuyerAddress, customer.Address),
		SellerName: orDefault(in.Terms.SellerName, s.seller.Name), SellerAddress: orDefault(in.Terms.SellerAddress, s.seller.Address),
		Incoterm: orDefault(in.Terms.Incoterm, "FOB"), PortOfLoading: in.Terms.PortOfLoading,
		PortOfDischarge: in.Terms.PortOfDischarge, PaymentMethod: in.Terms.PaymentMethod,
		DeliveryDate: in.Terms.DeliveryDate, Text: in.Terms.Text,
	}

	var contractID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		contractID, err = q.CreateExistingContract(ctx, store.CreateExistingContractParams{
			TenantID: tenantID, ContractNo: contractNo, ExternalContractNo: strings.TrimSpace(in.ExternalContractNo),
			CustomerID: customer.ID, CustomerName: customer.Name, SalesEmployeeID: ownerID, SalesEmployee: ownerName,
			ReceivableDueDate: in.Terms.ReceivableDueDate, OpeningReceivedAmount: openingReceived.StringFixed(2),
			FilePending: in.FilePending, SignedDate: in.SignedDate, EffectiveDate: in.EffectiveDate, CreatedBy: op.ID,
		})
		if err != nil {
			return translateUnique(err, "EX_EXTERNAL_CONTRACT_NO_TAKEN", "该原合同号已存在，请核对后再保存")
		}
		versionID, err := q.CreateContractVersion(ctx, store.CreateContractVersionParams{
			TenantID: tenantID, ContractID: contractID, VersionNo: 1,
			BuyerName: terms.BuyerName, BuyerAddress: terms.BuyerAddress, SellerName: terms.SellerName, SellerAddress: terms.SellerAddress,
			Currency: in.Currency, Incoterm: terms.Incoterm, PortOfLoading: terms.PortOfLoading,
			PortOfDischarge: terms.PortOfDischarge, PaymentMethod: terms.PaymentMethod, DeliveryDate: terms.DeliveryDate,
			Terms: terms.Text, TotalAmount: total.StringFixed(2), BaseAmount: baseAmount(total, rate).StringFixed(2),
			FxRate: rate.Rate.String(), FxRateAt: tsFrom(rate.At), FxSource: rate.Source,
			FxBaseCurrency: rate.Base, ChangeReason: "", CreatedBy: op.ID,
		})
		if err != nil {
			return err
		}
		event := contractEffectiveEvent{
			ContractID: contractID, ContractNo: contractNo, VersionID: versionID, VersionNo: 1,
			CustomerID: customer.ID, CustomerName: customer.Name, Currency: in.Currency,
			TotalAmount: total.StringFixed(2), DeliveryDate: terms.DeliveryDate, Incoterm: terms.Incoterm,
			PortOfDischarge: terms.PortOfDischarge,
			SalesEmployeeID: ownerID, SalesEmployee: ownerName,
			ExistingContract: true, ProcurementEmployeeID: procurementEmployee.ID,
			ProcurementEmployee: procurementEmployee.Name, SupplierID: in.SupplierID,
		}
		for i, line := range priced {
			var sku *int64
			var skuValue int64
			if line.in.SkuID != 0 {
				sku = &line.in.SkuID
				skuValue = line.in.SkuID
			}
			itemID, err := q.AddExistingContractItem(ctx, store.AddExistingContractItemParams{
				TenantID: tenantID, ContractVersionID: versionID, LineNo: int32(i + 1), ProductID: line.product.ID, SkuID: sku,
				ProductCode: line.product.Code, ProductName: line.product.Name, Spec: line.in.Spec,
				Qty: line.qty.String(), UomID: line.product.UomID, UomCode: line.product.UomCode,
				UnitPrice: line.price.String(), Amount: line.amount.StringFixed(2), HsCode: hs[line.product.ID], Remark: line.in.Remark,
				OpeningProcuredQty: opening[i].procured.String(), OpeningArrivedQty: opening[i].arrived.String(), OpeningShippedQty: opening[i].shipped.String(),
			})
			if err != nil {
				return err
			}
			event.Items = append(event.Items, effectiveEventItem{
				LineNo: int32(i + 1), ItemID: itemID, ProductID: line.product.ID, SkuID: skuValue,
				ProductCode: line.product.Code, ProductName: line.product.Name, Spec: line.in.Spec, Qty: line.qty.String(),
				RequiredQty: "0", UomID: line.product.UomID, UomCode: line.product.UomCode,
				UnitPrice: line.price.String(), Amount: line.amount.StringFixed(2), HsCode: hs[line.product.ID],
				PurchaseUnitPrice: line.in.PurchaseUnitPrice, OpeningArrivedQty: opening[i].arrived.String(),
			})
		}
		if err := q.SetContractVersionStatus(ctx, store.SetContractVersionStatusParams{TenantID: tenantID, ID: versionID, NewStatus: "APPROVED"}); err != nil {
			return err
		}
		if err := q.FinalizeExistingContract(ctx, store.FinalizeExistingContractParams{TenantID: tenantID, ID: contractID, VersionID: versionID, UpdatedBy: op.ID}); err != nil {
			return err
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		return outbox.Append(ctx, tx, outbox.Event{TenantID: tenantID, AggregateType: "contract", AggregateID: strconv.FormatInt(contractID, 10), EventType: "ContractEffective", Payload: payload})
	})
	if err != nil {
		return ContractView{}, err
	}
	return s.GetContract(ctx, tenantID, contractID, 0)
}

// priceExistingLines is deliberately narrower than priceLines. Ordinary
// system-authored contracts must still reference active product master data;
// only the takeover path may preserve a manually entered product snapshot.
func (s *Service) priceExistingLines(ctx context.Context, items []ItemInput) ([]priced, decimal.Decimal, error) {
	lines := make([]priced, 0, len(items))
	total := decimal.Zero
	productOf, err := s.prefetchProducts(ctx, productIDsOfItems(items))
	if err != nil {
		return nil, decimal.Zero, err
	}
	for i, item := range items {
		qty, err := decimal.NewFromString(item.Qty)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return nil, decimal.Zero, apierr.Invalid("EX_QTY_INVALID", "数量必须是大于 0 的数字").
				WithMeta("line", itoa(i+1))
		}
		price, err := decimal.NewFromString(item.UnitPrice)
		if err != nil || price.IsNegative() {
			return nil, decimal.Zero, apierr.Invalid("EX_PRICE_INVALID", "单价必须是不小于 0 的数字").
				WithMeta("line", itoa(i+1))
		}

		var product Product
		if item.ProductID != 0 {
			product, err = productOf(item.ProductID, i+1)
			if err != nil {
				return nil, decimal.Zero, err
			}
			if product.Status != "ACTIVE" {
				return nil, decimal.Zero, apierr.Invalid("EX_PRODUCT_INACTIVE", "产品已停用，不能签入合同").
					WithMeta("product", product.Code, "line", itoa(i+1))
			}
		} else {
			name := strings.TrimSpace(item.ProductName)
			uom := strings.ToUpper(strings.TrimSpace(item.UomCode))
			if name == "" {
				return nil, decimal.Zero, apierr.Invalid("EX_PRODUCT_NAME_REQUIRED", "产品名称必填").
					WithMeta("line", itoa(i+1))
			}
			if uom == "" {
				return nil, decimal.Zero, apierr.Invalid("EX_UOM_REQUIRED", "手工录入产品时计量单位必填").
					WithMeta("line", itoa(i+1))
			}
			item.ProductName = name
			item.UomCode = uom
			product = Product{Code: strings.TrimSpace(item.ProductCode), Name: name, UomCode: uom, Status: "SNAPSHOT"}
		}

		amount := qty.Mul(price).Round(2)
		total = total.Add(amount)
		lines = append(lines, priced{in: item, product: product, qty: qty, price: price, amount: amount})
	}
	return lines, total, nil
}

func openingDecimal(value, code, label string) (decimal.Decimal, *apierr.Error) {
	if strings.TrimSpace(value) == "" {
		return decimal.Zero, nil
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.IsNegative() {
		return decimal.Zero, apierr.Invalid(code, label+"必须是大于等于 0 的数字")
	}
	return parsed, nil
}

type existingCorrectionSnapshot struct {
	ExternalContractNo string `json:"externalContractNo"`
	SignedDate         string `json:"signedDate"`
	EffectiveDate      string `json:"effectiveDate"`
	ReceivableDueDate  string `json:"receivableDueDate"`
	BuyerName          string `json:"buyerName"`
	BuyerAddress       string `json:"buyerAddress"`
	SellerName         string `json:"sellerName"`
	SellerAddress      string `json:"sellerAddress"`
	Incoterm           string `json:"incoterm"`
	PortOfLoading      string `json:"portOfLoading"`
	PortOfDischarge    string `json:"portOfDischarge"`
	PaymentMethod      string `json:"paymentMethod"`
	DeliveryDate       string `json:"deliveryDate"`
	Terms              string `json:"terms"`
}

// correctExistingContract fixes transcription omissions without replacing
// the approved version or its line ids. Those ids are already referenced by
// procurement and shipping. Only descriptive terms can change; priced lines
// and all opening/executed quantities remain immutable.
func (s *Service) correctExistingContract(ctx context.Context, tenantID int64, view ContractView, terms Terms, items []ItemInput, meta ContractEditMeta, op Operator) (ContractView, error) {
	if view.Contract.Status == "CANCELLED" || view.Contract.CurrentVersionID != view.Version.ID || view.Version.Status != "APPROVED" {
		return ContractView{}, apierr.Conflict("EX_EXISTING_CONTRACT_NOT_EDITABLE", "只有当前执行中的已有合同可以纠错")
	}
	if len(items) != 0 {
		return ContractView{}, apierr.Conflict("EX_EXISTING_LINES_IMMUTABLE", "执行中的商品、数量和价格不能直接修改")
	}
	for value, rule := range map[string][2]string{
		meta.SignedDate:         {"EX_SIGNED_DATE_INVALID", "签订日期"},
		meta.EffectiveDate:      {"EX_EFFECTIVE_DATE_INVALID", "生效日期"},
		terms.DeliveryDate:      {"EX_DELIVERY_DATE_INVALID", "交货日期"},
		terms.ReceivableDueDate: {"EX_DUE_DATE_INVALID", "应收到期日"},
	} {
		if err := validBusinessDate(value, rule[0], rule[1]); err != nil {
			return ContractView{}, err
		}
	}
	if meta.SignedDate == "" || meta.EffectiveDate == "" {
		return ContractView{}, apierr.Invalid("EX_EXISTING_CONTRACT_DATES_REQUIRED", "请填写签订日期和生效日期")
	}
	signed, _ := time.Parse("2006-01-02", meta.SignedDate)
	effective, _ := time.Parse("2006-01-02", meta.EffectiveDate)
	if effective.Before(signed) {
		return ContractView{}, apierr.Invalid("EX_EFFECTIVE_BEFORE_SIGNED", "生效日期不能早于签订日期")
	}

	filled := Terms{
		BuyerName: orDefault(terms.BuyerName, view.Version.BuyerName), BuyerAddress: terms.BuyerAddress,
		SellerName: orDefault(terms.SellerName, view.Version.SellerName), SellerAddress: terms.SellerAddress,
		Incoterm: orDefault(terms.Incoterm, view.Version.Incoterm), PortOfLoading: terms.PortOfLoading,
		PortOfDischarge: terms.PortOfDischarge, PaymentMethod: terms.PaymentMethod,
		DeliveryDate: terms.DeliveryDate, ReceivableDueDate: terms.ReceivableDueDate, Text: terms.Text,
	}
	dateOf := func(value pgtype.Timestamptz) string {
		if !value.Valid {
			return ""
		}
		return value.Time.Format("2006-01-02")
	}
	before := existingCorrectionSnapshot{
		ExternalContractNo: view.Contract.ExternalContractNo, SignedDate: dateOf(view.Contract.SignedAt),
		EffectiveDate: dateOf(view.Contract.EffectiveAt), ReceivableDueDate: view.Contract.ReceivableDueDate,
		BuyerName: view.Version.BuyerName, BuyerAddress: view.Version.BuyerAddress,
		SellerName: view.Version.SellerName, SellerAddress: view.Version.SellerAddress,
		Incoterm: view.Version.Incoterm, PortOfLoading: view.Version.PortOfLoading,
		PortOfDischarge: view.Version.PortOfDischarge, PaymentMethod: view.Version.PaymentMethod,
		DeliveryDate: view.Version.DeliveryDate, Terms: view.Version.Terms,
	}
	after := existingCorrectionSnapshot{
		ExternalContractNo: strings.TrimSpace(meta.ExternalContractNo), SignedDate: meta.SignedDate,
		EffectiveDate: meta.EffectiveDate, ReceivableDueDate: filled.ReceivableDueDate,
		BuyerName: filled.BuyerName, BuyerAddress: filled.BuyerAddress,
		SellerName: filled.SellerName, SellerAddress: filled.SellerAddress,
		Incoterm: filled.Incoterm, PortOfLoading: filled.PortOfLoading,
		PortOfDischarge: filled.PortOfDischarge, PaymentMethod: filled.PaymentMethod,
		DeliveryDate: filled.DeliveryDate, Terms: filled.Text,
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)

	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockContract(ctx, store.LockContractParams{TenantID: tenantID, ID: view.Contract.ID})
		if err != nil {
			return err
		}
		if locked.Status == "CANCELLED" || locked.CurrentVersionID != view.Version.ID {
			return apierr.Conflict("EX_EXISTING_CONTRACT_NOT_EDITABLE", "合同状态或当前版本已经变化，请刷新后重试")
		}
		rows, err := q.CorrectExistingContractHeader(ctx, store.CorrectExistingContractHeaderParams{
			ExternalContractNo: after.ExternalContractNo, SignedDate: after.SignedDate,
			EffectiveDate: after.EffectiveDate, ReceivableDueDate: after.ReceivableDueDate,
			UpdatedBy: op.ID, TenantID: tenantID, ID: view.Contract.ID,
		})
		if err != nil {
			return translateUnique(err, "EX_EXTERNAL_CONTRACT_NO_TAKEN", "该原合同号已存在，请核对后再保存")
		}
		if rows == 0 {
			return apierr.Conflict("EX_EXISTING_CONTRACT_NOT_EDITABLE", "该合同当前不能编辑")
		}
		rows, err = q.CorrectExistingContractVersion(ctx, store.CorrectExistingContractVersionParams{
			BuyerName: after.BuyerName, BuyerAddress: after.BuyerAddress,
			SellerName: after.SellerName, SellerAddress: after.SellerAddress,
			Incoterm: after.Incoterm, PortOfLoading: after.PortOfLoading, PortOfDischarge: after.PortOfDischarge,
			PaymentMethod: after.PaymentMethod, DeliveryDate: after.DeliveryDate, Terms: after.Terms,
			TenantID: tenantID, ID: view.Version.ID,
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			return apierr.Conflict("EX_EXISTING_CONTRACT_NOT_EDITABLE", "该合同当前不能编辑")
		}
		return q.AddExistingContractCorrection(ctx, store.AddExistingContractCorrectionParams{
			TenantID: tenantID, ContractID: view.Contract.ID, ContractVersionID: view.Version.ID,
			BeforeData: beforeJSON, AfterData: afterJSON, CorrectedBy: op.ID, CorrectedByName: op.Name,
		})
	})
	if err != nil {
		return ContractView{}, err
	}
	return s.GetContract(ctx, tenantID, view.Contract.ID, 0)
}
