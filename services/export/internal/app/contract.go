package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// Terms is the negotiable-but-not-priced part of a contract version. Amounts
// are deliberately absent: changing a price is a new version, not an edit.
type Terms struct {
	BuyerName, BuyerAddress   string
	SellerName, SellerAddress string
	Incoterm                  string
	PortOfLoading             string
	PortOfDischarge           string
	PaymentMethod             string
	DeliveryDate              string
	Text                      string
}

// ContractView is one contract with the version being looked at and its lines.
type ContractView struct {
	Contract store.GetContractRow
	Version  store.GetContractVersionRow
	Items    []store.ListContractItemsRow
	Versions []store.ListContractVersionsRow
}

// CreateContractFromQuotation turns an accepted offer into a draft contract.
// Prices, lines and the exchange-rate snapshot are inherited verbatim: the
// rate promised when the offer was made is the rate the contract is priced at,
// however far the market has moved since.
func (s *Service) CreateContractFromQuotation(ctx context.Context, tenantID, quotationID int64, terms Terms, op Operator) (ContractView, error) {
	quote, quoteItems, err := s.GetQuotationFor(ctx, tenantID, quotationID, op)
	if err != nil {
		return ContractView{}, err
	}
	// The contract inherits the quotation's salesperson as its owner, so only
	// that person may write it up. This is stricter than the data scope on
	// purpose: a wide scope exists to supervise other people's work, not to
	// open a deal in their name. Whoever needs to take it over transfers the
	// quotation first, and the audit trail says so.
	if quote.SalesEmployeeID != op.ID {
		return ContractView{}, apierr.Permission("EX_QUOTE_NOT_OWNER",
			"只有报价单负责人可以生成合同，请先转移负责人").
			WithMeta("owner", quote.SalesEmployee)
	}
	if quote.Status != "ACCEPTED" {
		return ContractView{}, apierr.Conflict("EX_QUOTE_NOT_ACCEPTED",
			"只有客户已接受的报价单可以生成合同").WithMeta("status", quote.Status)
	}
	if len(quoteItems) == 0 {
		return ContractView{}, apierr.Invalid("EX_ITEMS_REQUIRED", "报价单没有明细，无法生成合同")
	}
	customer, err := s.customers.Get(ctx, quote.CustomerID)
	if err != nil {
		return ContractView{}, err
	}
	// The customs code lives on the product, not on the quotation line, so it
	// is fetched once per line here and then frozen into the contract.
	hsCodes, err := s.hsCodesFor(ctx, quoteItems)
	if err != nil {
		return ContractView{}, err
	}
	contractNo, err := s.number.Next(ctx, "CONTRACT")
	if err != nil {
		return ContractView{}, err
	}

	filled := s.fillTerms(terms, quote, customer)
	var contractID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		contractID, err = q.CreateContract(ctx, store.CreateContractParams{
			TenantID: tenantID, ContractNo: contractNo,
			QuotationID: quote.ID, QuoteNo: quote.QuoteNo,
			CustomerID: quote.CustomerID, CustomerName: quote.CustomerName,
			SalesEmployeeID: quote.SalesEmployeeID, SalesEmployee: quote.SalesEmployee,
			CreatedBy: op.ID,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return apierr.Conflict("EX_QUOTE_ALREADY_CONTRACTED", "该报价单已经生成过合同")
			}
			return err
		}
		versionID, err := q.CreateContractVersion(ctx, store.CreateContractVersionParams{
			TenantID: tenantID, ContractID: contractID, VersionNo: 1,
			BuyerName: filled.BuyerName, BuyerAddress: filled.BuyerAddress,
			SellerName: filled.SellerName, SellerAddress: filled.SellerAddress,
			Currency: quote.Currency, Incoterm: filled.Incoterm,
			PortOfLoading: filled.PortOfLoading, PortOfDischarge: filled.PortOfDischarge,
			PaymentMethod: filled.PaymentMethod, DeliveryDate: filled.DeliveryDate,
			Terms: filled.Text, TotalAmount: quote.TotalAmount, BaseAmount: quote.BaseAmount,
			FxRate: quote.FxRate, FxRateAt: quote.FxRateAt,
			FxSource: quote.FxSource, FxBaseCurrency: quote.FxBaseCurrency,
			ChangeReason: "", CreatedBy: op.ID,
		})
		if err != nil {
			return err
		}
		for _, line := range quoteItems {
			if err := q.AddContractItem(ctx, store.AddContractItemParams{
				TenantID: tenantID, ContractVersionID: versionID, LineNo: line.LineNo,
				ProductID: line.ProductID, SkuID: line.SkuID,
				ProductCode: line.ProductCode, ProductName: line.ProductName, Spec: line.Spec,
				Qty: line.Qty, UomID: line.UomID, UomCode: line.UomCode,
				UnitPrice: line.UnitPrice, Amount: line.Amount,
				HsCode: hsCodes[line.ProductID], Remark: line.Remark,
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

// fillTerms defaults the parties from what the system already knows, so the
// common case is one click and the caller only overrides what differs.
func (s *Service) fillTerms(in Terms, quote store.GetQuotationRow, customer Customer) Terms {
	return Terms{
		BuyerName:       orDefault(in.BuyerName, customer.Name),
		BuyerAddress:    orDefault(in.BuyerAddress, customer.Address),
		SellerName:      orDefault(in.SellerName, s.seller.Name),
		SellerAddress:   orDefault(in.SellerAddress, s.seller.Address),
		Incoterm:        orDefault(in.Incoterm, quote.Incoterm),
		PortOfLoading:   orDefault(in.PortOfLoading, quote.PortOfLoading),
		PortOfDischarge: orDefault(in.PortOfDischarge, quote.PortOfDischarge),
		PaymentMethod:   orDefault(in.PaymentMethod, quote.PaymentMethod),
		DeliveryDate:    in.DeliveryDate,
		Text:            in.Text,
	}
}

func (s *Service) hsCodesFor(ctx context.Context, lines []store.ListQuotationItemsRow) (map[int64]string, error) {
	codes := make(map[int64]string, len(lines))
	for _, line := range lines {
		if _, seen := codes[line.ProductID]; seen {
			continue
		}
		product, err := s.products.Get(ctx, line.ProductID)
		if err != nil {
			return nil, err
		}
		codes[line.ProductID] = product.HsCode
	}
	return codes, nil
}

// UpdateContract edits the version currently being drafted, lines included.
// The check is on the version rather than the contract, so it covers a first
// draft, a draft an approver sent back, and a change version opened against a
// contract already in force.
//
// Lines are editable here because "the price is wrong" is the most common
// reason a contract comes back; sending it round again unchanged would be the
// only other option.
func (s *Service) UpdateContract(ctx context.Context, tenantID, id int64, terms Terms, items []ItemInput, op Operator) (ContractView, error) {
	view, err := s.GetContract(ctx, tenantID, id, 0)
	if err != nil {
		return ContractView{}, err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return ContractView{}, err
	}
	if view.Version.Status != "DRAFT" {
		return ContractView{}, apierr.Conflict("EX_CONTRACT_NOT_DRAFT",
			"只有草稿版本可以修改，已提交审批的版本请先撤回或重新变更").
			WithMeta("version_status", view.Version.Status)
	}

	total, err := decimal.NewFromString(view.Version.TotalAmount)
	if err != nil {
		return ContractView{}, err
	}
	var lines []store.ListContractItemsRow
	if len(items) > 0 {
		priced, sum, err := s.priceLines(ctx, items)
		if err != nil {
			return ContractView{}, err
		}
		lines, total = pricedToItems(priced), sum
	}
	// The rate stays as inherited: correcting a typo must not re-price the
	// deal at today's market.
	rate := Rate{Source: view.Version.FxSource, Base: view.Version.FxBaseCurrency}
	if rate.Rate, err = decimal.NewFromString(view.Version.FxRate); err != nil {
		return ContractView{}, err
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rows, err := q.UpdateContractVersion(ctx, store.UpdateContractVersionParams{
			TenantID: tenantID, ID: view.Version.ID,
			BuyerName:     orDefault(terms.BuyerName, view.Version.BuyerName),
			BuyerAddress:  terms.BuyerAddress,
			SellerName:    orDefault(terms.SellerName, view.Version.SellerName),
			SellerAddress: terms.SellerAddress,
			Incoterm:      orDefault(terms.Incoterm, view.Version.Incoterm),
			PortOfLoading: terms.PortOfLoading, PortOfDischarge: terms.PortOfDischarge,
			PaymentMethod: terms.PaymentMethod, DeliveryDate: terms.DeliveryDate,
			Terms:       terms.Text,
			TotalAmount: total.StringFixed(2), BaseAmount: baseAmount(total, rate).StringFixed(2),
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			// Someone submitted it between the read and this write.
			return apierr.Conflict("EX_CONTRACT_NOT_DRAFT", "只有草稿版本可以修改")
		}
		if len(lines) == 0 {
			return nil
		}
		if err := q.DeleteContractItems(ctx, store.DeleteContractItemsParams{
			TenantID: tenantID, ContractVersionID: view.Version.ID,
		}); err != nil {
			return err
		}
		return writeContractItems(ctx, q, tenantID, view.Version.ID, lines)
	})
	if err != nil {
		return ContractView{}, err
	}
	return s.GetContract(ctx, tenantID, id, 0)
}

// writeContractItems is shared by every path that lays down a version's lines.
func writeContractItems(ctx context.Context, q *store.Queries, tenantID, versionID int64, lines []store.ListContractItemsRow) error {
	for _, line := range lines {
		if err := q.AddContractItem(ctx, store.AddContractItemParams{
			TenantID: tenantID, ContractVersionID: versionID, LineNo: line.LineNo,
			ProductID: line.ProductID, SkuID: line.SkuID,
			ProductCode: line.ProductCode, ProductName: line.ProductName, Spec: line.Spec,
			Qty: line.Qty, UomID: line.UomID, UomCode: line.UomCode,
			UnitPrice: line.UnitPrice, Amount: line.Amount,
			HsCode: line.HsCode, Remark: line.Remark,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ChangeContract opens a new version of a contract already in force. The old
// version keeps running until the new one is approved and signed.
func (s *Service) ChangeContract(ctx context.Context, tenantID, id int64, terms Terms, reason string, items []ItemInput, op Operator) (ContractView, error) {
	if reason == "" {
		return ContractView{}, apierr.Invalid("EX_CHANGE_REASON_REQUIRED", "变更必须填写变更原因")
	}
	view, err := s.GetContract(ctx, tenantID, id, 0)
	if err != nil {
		return ContractView{}, err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return ContractView{}, err
	}
	if view.Contract.Status != "EFFECTIVE" && view.Contract.Status != "EXECUTING" {
		return ContractView{}, apierr.Conflict("EX_CONTRACT_NOT_EFFECTIVE",
			"只有已生效的合同才能发起变更").WithMeta("status", view.Contract.Status)
	}
	// Only an unfinished attempt blocks a new one. A change that was turned
	// down is over, and must not wedge the contract so it can never be
	// changed again.
	if view.Version.Status == "DRAFT" || view.Version.Status == "PENDING_APPROVAL" {
		return ContractView{}, apierr.Conflict("EX_CHANGE_IN_FLIGHT",
			"已有一个变更版本在处理中，请先完成或作废它")
	}
	nextVersionNo := view.Version.VersionNo + 1

	// Build on the version actually in force, not on whatever was tried last:
	// a rejected attempt is not what the parties are operating under.
	base, err := s.GetContract(ctx, tenantID, id, view.Contract.CurrentVersionID)
	if err != nil {
		return ContractView{}, err
	}

	// Re-price only when new lines were supplied; otherwise the change is to
	// the terms alone and the amounts carry over untouched.
	lines := base.Items
	total, err := decimal.NewFromString(base.Version.TotalAmount)
	if err != nil {
		return ContractView{}, err
	}
	if len(items) > 0 {
		priced, sum, err := s.priceLines(ctx, items)
		if err != nil {
			return ContractView{}, err
		}
		lines, total = pricedToItems(priced), sum
	}
	rate := Rate{Source: base.Version.FxSource, Base: base.Version.FxBaseCurrency}
	if rate.Rate, err = decimal.NewFromString(base.Version.FxRate); err != nil {
		return ContractView{}, err
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		versionID, err := q.CreateContractVersion(ctx, store.CreateContractVersionParams{
			TenantID: tenantID, ContractID: id, VersionNo: nextVersionNo,
			BuyerName:     orDefault(terms.BuyerName, base.Version.BuyerName),
			BuyerAddress:  orDefault(terms.BuyerAddress, base.Version.BuyerAddress),
			SellerName:    orDefault(terms.SellerName, base.Version.SellerName),
			SellerAddress: orDefault(terms.SellerAddress, base.Version.SellerAddress),
			Currency:      base.Version.Currency,
			Incoterm:      orDefault(terms.Incoterm, base.Version.Incoterm),
			PortOfLoading: orDefault(terms.PortOfLoading, base.Version.PortOfLoading),
			// Ports and payment method fall back to the version being replaced,
			// so a change that only moves a date does not blank the rest.
			PortOfDischarge: orDefault(terms.PortOfDischarge, base.Version.PortOfDischarge),
			PaymentMethod:   orDefault(terms.PaymentMethod, base.Version.PaymentMethod),
			DeliveryDate:    orDefault(terms.DeliveryDate, base.Version.DeliveryDate),
			Terms:           orDefault(terms.Text, base.Version.Terms),
			TotalAmount:     total.StringFixed(2),
			BaseAmount:      baseAmount(total, rate).StringFixed(2),
			// The rate is inherited too: a change to the delivery date must
			// not silently re-price the deal at today's market.
			FxRate: base.Version.FxRate, FxRateAt: base.Version.FxRateAt,
			FxSource: base.Version.FxSource, FxBaseCurrency: base.Version.FxBaseCurrency,
			ChangeReason: reason, CreatedBy: op.ID,
		})
		if err != nil {
			return err
		}
		return writeContractItems(ctx, q, tenantID, versionID, lines)
	})
	if err != nil {
		return ContractView{}, err
	}
	return s.GetContract(ctx, tenantID, id, 0)
}

// priceLines resolves and prices explicit line input, reusing the quotation
// rules so a contract line and a quotation line can never disagree on how an
// amount is computed.
func (s *Service) priceLines(ctx context.Context, items []ItemInput) ([]priced, decimal.Decimal, error) {
	lines := make([]priced, 0, len(items))
	total := decimal.Zero
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
		product, err := s.products.Get(ctx, item.ProductID)
		if err != nil {
			return nil, decimal.Zero, err
		}
		if product.Status != "ACTIVE" {
			return nil, decimal.Zero, apierr.Invalid("EX_PRODUCT_INACTIVE", "产品已停用，不能签入合同").
				WithMeta("product", product.Code)
		}
		amount := qty.Mul(price).Round(2)
		total = total.Add(amount)
		lines = append(lines, priced{in: item, product: product, qty: qty, price: price, amount: amount})
	}
	return lines, total, nil
}

func pricedToItems(lines []priced) []store.ListContractItemsRow {
	out := make([]store.ListContractItemsRow, 0, len(lines))
	for i, l := range lines {
		var sku *int64
		if l.in.SkuID != 0 {
			sku = &l.in.SkuID
		}
		out = append(out, store.ListContractItemsRow{
			LineNo: int32(i + 1), ProductID: l.product.ID, SkuID: sku,
			ProductCode: l.product.Code, ProductName: l.product.Name, Spec: l.in.Spec,
			Qty: l.qty.String(), UomID: l.product.UomID, UomCode: l.product.UomCode,
			UnitPrice: l.price.String(), Amount: l.amount.StringFixed(2),
			HsCode: l.product.HsCode, Remark: l.in.Remark,
		})
	}
	return out
}

// ---------------------------------------------------------------- reads

// GetContractFor is GetContract with the caller's data scope applied. Reads
// that a user can reach go through it; internal reads (the Kafka consumer
// advancing a contract, a lifecycle check) use GetContract, which has no
// caller to scope against.
func (s *Service) GetContractFor(ctx context.Context, tenantID, id, versionID int64, op Operator) (ContractView, error) {
	view, err := s.GetContract(ctx, tenantID, id, versionID)
	if err != nil {
		return ContractView{}, err
	}
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return ContractView{}, err
	}
	// Either it is within the scope, or you were asked to approve it. Being
	// handed a task on a document has to imply the right to read it.
	if !allowedOwner(visible, view.Contract.SalesEmployeeID) &&
		!contains(s.involvedIn(ctx, op, BizTypeContract), id) {
		// Deliberately "not found" rather than "forbidden": telling someone a
		// contract exists but is none of their business is itself a leak.
		return ContractView{}, apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
	}
	return view, nil
}

// mustOwnContract is the write-side counterpart of GetContractFor. The read
// side unions in the documents you were asked to approve; the write side does
// not, because being asked to sign off on a contract is precisely not
// permission to rewrite it first.
func (s *Service) mustOwnContract(ctx context.Context, op Operator, view ContractView) error {
	return s.mayWrite(ctx, op, view.Contract.SalesEmployeeID,
		"EX_CONTRACT_NOT_OWNER", "只能操作自己负责的合同")
}

func contains(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func allowedOwner(v Visibility, ownerID int64) bool {
	if v.All {
		return true
	}
	for _, id := range v.EmployeeIDs {
		if id == ownerID {
			return true
		}
	}
	return false
}

// GetContract returns one contract with a chosen version; versionID 0 means
// the newest one, which is what a user editing or reviewing wants to see.
func (s *Service) GetContract(ctx context.Context, tenantID, id, versionID int64) (ContractView, error) {
	contract, err := s.q.GetContract(ctx, store.GetContractParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ContractView{}, apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
		}
		return ContractView{}, err
	}
	if versionID == 0 {
		versionID, err = s.q.LatestContractVersionID(ctx,
			store.LatestContractVersionIDParams{TenantID: tenantID, ContractID: id})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ContractView{}, err
		}
	}
	version, err := s.q.GetContractVersion(ctx,
		store.GetContractVersionParams{TenantID: tenantID, ID: versionID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ContractView{}, apierr.NotFound("EX_CONTRACT_VERSION_NOT_FOUND", "合同版本不存在")
		}
		return ContractView{}, err
	}
	if version.ContractID != id {
		return ContractView{}, apierr.NotFound("EX_CONTRACT_VERSION_NOT_FOUND", "合同版本不属于该合同")
	}
	items, err := s.q.ListContractItems(ctx,
		store.ListContractItemsParams{TenantID: tenantID, ContractVersionID: version.ID})
	if err != nil {
		return ContractView{}, err
	}
	versions, err := s.q.ListContractVersions(ctx,
		store.ListContractVersionsParams{TenantID: tenantID, ContractID: id})
	if err != nil {
		return ContractView{}, err
	}
	return ContractView{Contract: contract, Version: version, Items: items, Versions: versions}, nil
}

func (s *Service) ListContracts(ctx context.Context, tenantID int64, keyword string, customerID int64, status string, page, size int32, op Operator) ([]store.ListContractsRow, int64, error) {
	page, size = normalizePage(page, size)
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListContracts(ctx, store.ListContractsParams{
		TenantID: tenantID, Keyword: keyword, CustomerID: customerID, Status: status,
		VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
		InvolvedIds: s.involvedIn(ctx, op, BizTypeContract),
		RowLimit:    size, RowOffset: (page - 1) * size,
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

// contractSummary is what the approval todo list shows. It travels with the
// instance so the approval service never calls back to ask what it is
// approving. Keep it to what an approver decides on at a glance; the fields
// are omitted when they would only add noise.
type contractSummary struct {
	Customer string `json:"customer"`
	// Amount and currency in one string: the todo list renders values as
	// given, and "6450.00" without "EUR" is not a number anyone can approve.
	Amount   string `json:"amount"`
	Delivery string `json:"delivery_date,omitempty"`
	// Only meaningful for a change; version 1 needs no explanation.
	VersionNo int32  `json:"version_no,omitempty"`
	Reason    string `json:"change_reason,omitempty"`
}

func summaryJSON(view ContractView) string {
	s := contractSummary{
		Customer: view.Contract.CustomerName,
		Amount:   view.Version.TotalAmount + " " + view.Version.Currency,
		Delivery: view.Version.DeliveryDate,
	}
	if view.Version.VersionNo > 1 {
		s.VersionNo = view.Version.VersionNo
		s.Reason = view.Version.ChangeReason
	}
	body, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(body)
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
