package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// DirectContractInput is a contract written up without a quotation behind it.
type DirectContractInput struct {
	CustomerID int64
	Currency   string
	Terms      Terms
	Items      []ItemInput
}

// CreateContract writes up a contract that never was a quotation.
//
// This is the normal case for a trader who negotiates by email and receives a
// signed PDF back: the paper is the agreement, and the system's job is to
// hold the numbers that the paper implies so shipping and collection have
// something to work against. Forcing every such deal through a quotation
// first would be asking people to invent a document nobody sent.
//
// The quotation path is left exactly as it was. It carries a rule this one
// cannot have — only the quotation's owner may turn it into a contract —
// because there is an existing owner to protect. Here the person writing it
// up is the first owner there has ever been.
func (s *Service) CreateContract(ctx context.Context, tenantID int64, in DirectContractInput, op Operator) (ContractView, error) {
	if in.CustomerID == 0 {
		return ContractView{}, apierr.Invalid("EX_CUSTOMER_REQUIRED", "请选择客户")
	}
	if in.Currency == "" {
		return ContractView{}, apierr.Invalid("EX_CURRENCY_REQUIRED", "请选择币种")
	}
	// 日期一路当字符串传到 SQL，那句是 nullif(...)::date。不校验的话，一个
	// 打错的月份不会得到「格式不对」，而是 PostgreSQL 的 22007 冒到网关，
	// 最后变成 500「系统错误」。
	if err := validBusinessDate(in.Terms.ReceivableDueDate, "EX_DUE_DATE_INVALID", "应收到期日"); err != nil {
		return ContractView{}, err
	}
	// Lines are required even though the agreement itself is an uploaded
	// file. Nothing downstream can read a PDF: shipping needs to know how
	// much is still owed, and collection needs an amount to match money
	// against. A contract with no lines would look complete and be inert.
	if len(in.Items) == 0 {
		return ContractView{}, apierr.Invalid("EX_ITEMS_REQUIRED",
			"请至少录入一条合同明细——发货进度和收款对账都要靠它")
	}

	customer, err := s.customers.Get(ctx, in.CustomerID)
	if err != nil {
		return ContractView{}, err
	}
	if customer.Status != "ACTIVE" {
		return ContractView{}, apierr.Invalid("EX_CUSTOMER_INACTIVE", "客户已停用，不能签约").
			WithMeta("customer", customer.Name)
	}

	priced, total, err := s.priceLines(ctx, in.Items)
	if err != nil {
		return ContractView{}, err
	}
	lines := pricedToItems(priced)

	// Read once, store with the document. Everything downstream reads the
	// stored copy — same rule as a quotation, for the same reason.
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
		BuyerName:       orDefault(in.Terms.BuyerName, customer.Name),
		BuyerAddress:    orDefault(in.Terms.BuyerAddress, customer.Address),
		SellerName:      orDefault(in.Terms.SellerName, s.seller.Name),
		SellerAddress:   orDefault(in.Terms.SellerAddress, s.seller.Address),
		Incoterm:        orDefault(in.Terms.Incoterm, "FOB"),
		PortOfLoading:   in.Terms.PortOfLoading,
		PortOfDischarge: in.Terms.PortOfDischarge,
		PaymentMethod:   in.Terms.PaymentMethod,
		DeliveryDate:    in.Terms.DeliveryDate,
		Text:            in.Terms.Text,
	}

	var contractID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		contractID, err = q.CreateContract(ctx, store.CreateContractParams{
			TenantID: tenantID, ContractNo: contractNo,
			// No quotation. The column is nullable and the unique index that
			// enforces one contract per quotation is partial, so rows with no
			// quotation do not collide with each other.
			QuotationID: 0, QuoteNo: "",
			CustomerID: customer.ID, CustomerName: customer.Name,
			SalesEmployeeID: op.ID, SalesEmployee: op.Name,
			// 应收到期日跟着条款一起进来，落在合同主表上。**从报价生成合同
			// 那条路（contract.go）也有同样一句**——两条建合同的路，谁漏了
			// 谁那边的到期日就悄悄没了。
			ReceivableDueDate: in.Terms.ReceivableDueDate,
			CreatedBy:         op.ID,
		})
		if err != nil {
			return translateUnique(err, "EX_CONTRACT_NO_TAKEN", "合同号已存在")
		}
		versionID, err := q.CreateContractVersion(ctx, store.CreateContractVersionParams{
			TenantID: tenantID, ContractID: contractID, VersionNo: 1,
			BuyerName: terms.BuyerName, BuyerAddress: terms.BuyerAddress,
			SellerName: terms.SellerName, SellerAddress: terms.SellerAddress,
			Currency: in.Currency, Incoterm: terms.Incoterm,
			PortOfLoading: terms.PortOfLoading, PortOfDischarge: terms.PortOfDischarge,
			PaymentMethod: terms.PaymentMethod, DeliveryDate: terms.DeliveryDate,
			Terms:       terms.Text,
			TotalAmount: total.StringFixed(2),
			BaseAmount:  baseAmount(total, rate).StringFixed(2),
			FxRate:      rate.Rate.String(), FxRateAt: tsFrom(rate.At),
			FxSource: rate.Source, FxBaseCurrency: rate.Base,
			ChangeReason: "", CreatedBy: op.ID,
		})
		if err != nil {
			return err
		}
		for _, line := range lines {
			if err := q.AddContractItem(ctx, store.AddContractItemParams{
				TenantID: tenantID, ContractVersionID: versionID, LineNo: line.LineNo,
				ProductID: line.ProductID, SkuID: line.SkuID,
				ProductCode: line.ProductCode, ProductName: line.ProductName, Spec: line.Spec,
				Qty: line.Qty, UomID: line.UomID, UomCode: line.UomCode,
				UnitPrice: line.UnitPrice, Amount: line.Amount,
				HsCode: hs[line.ProductID], Remark: line.Remark,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ContractView{}, err
	}
	return s.GetContract(ctx, tenantID, contractID, 0)
}

// hsCodesForItems is hsCodesFor over raw input rather than quotation rows.
// The customs code lives on the product and is frozen onto the contract, so a
// product reclassified next year does not rewrite what was declared.
func (s *Service) hsCodesForItems(ctx context.Context, items []ItemInput) (map[int64]string, error) {
	products, err := s.productsByID(ctx, dedupeIDs(productIDsOfItems(items)))
	if err != nil {
		return nil, err
	}
	codes := make(map[int64]string, len(products))
	for id, p := range products {
		codes[id] = p.HsCode
	}
	return codes, nil
}
