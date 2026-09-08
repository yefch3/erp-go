package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
	"github.com/shopspring/decimal"
)

func (s *Service) confirmOffer(ctx context.Context, op grpcx.Operator, id int64, view OfferView) error {
	b := view.Body
	if len(b.Transports) > 0 {
		accepted := false
		for _, t := range b.Transports {
			accepted = accepted || t.Accepted
		}
		if !accepted {
			return apierr.Invalid("OFFER_ACCEPTED_TRANSPORT", "请记录客户实际接受的运输方案和对应产品数量")
		}
	}
	if err := validBusinessDate(b.Delivery, "OFFER_DELIVERY", "交货日期"); err != nil {
		return err
	}
	rate, at, err := effectivePair(b.Rates, "USD", b.Currency)
	if err != nil {
		return err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	total, err := decimal.NewFromString(b.Total)
	if err != nil {
		return err
	}
	quoteNo, err := s.number.Next(ctx, "QUOTATION")
	if err != nil {
		return err
	}
	contractNo, err := s.number.Next(ctx, "CONTRACT")
	if err != nil {
		return err
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockOfferInquiry(ctx, tx, op.TenantID, id); err != nil {
			return err
		}
		var revision, existing int64
		if err := tx.QueryRow(ctx, `SELECT revision,COALESCE(contract_id,0) FROM customer_offers WHERE tenant_id=$1 AND case_id=$2 FOR UPDATE`, op.TenantID, id).Scan(&revision, &existing); err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		if revision != view.Revision {
			return apierr.Conflict("OFFER_REVISION", "报价已变化，请刷新后检查再确认")
		}
		q := s.q.WithTx(tx)
		baseAmount := total.DivRound(rate, 2).StringFixed(2)
		quotationID, err := q.CreateQuotation(ctx, store.CreateQuotationParams{TenantID: op.TenantID, QuoteNo: quoteNo, CustomerID: offerID(b.CustomerID), CustomerName: b.Customer, ContactID: offerID(b.ContactID), ContactName: b.Contact, Currency: b.Currency, Incoterm: b.Incoterm, PortOfLoading: b.LoadingPort, PortOfDischarge: b.DestinationPort, PaymentMethod: b.Payment, ValidUntil: b.ValidUntil, FxRate: rate.StringFixed(8), FxRateAt: tsFrom(at), FxSource: "MANUAL_EFFECTIVE", FxBaseCurrency: "USD", TotalAmount: b.Total, BaseAmount: baseAmount, Remark: b.Remark, SalesEmployeeID: op.EmployeeID, SalesEmployee: op.Name, CreatedBy: op.EmployeeID, SourceSourcingCaseID: id})
		if err != nil {
			return err
		}
		for i, line := range b.Lines {
			if err := q.AddQuotationItem(ctx, store.AddQuotationItemParams{TenantID: op.TenantID, QuotationID: quotationID, LineNo: int32(i + 1), ProductID: 0, ProductName: line.Product, Spec: offerSpecification(line.OfferProduct, view.Source), Qty: line.Quantity, UomCode: line.Unit, UnitPrice: line.UnitPrice, Amount: line.Amount, Remark: line.Remark}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE quotations SET status='ACCEPTED',responded_at=now() WHERE tenant_id=$1 AND id=$2`, op.TenantID, quotationID); err != nil {
			return err
		}
		contractID, err := q.CreateContract(ctx, store.CreateContractParams{TenantID: op.TenantID, ContractNo: contractNo, QuotationID: quotationID, QuoteNo: quoteNo, CustomerID: offerID(b.CustomerID), CustomerName: b.Customer, SalesEmployeeID: op.EmployeeID, SalesEmployee: op.Name, CreatedBy: op.EmployeeID})
		if err != nil {
			return err
		}
		versionID, err := q.CreateContractVersion(ctx, store.CreateContractVersionParams{TenantID: op.TenantID, ContractID: contractID, VersionNo: 1, BuyerName: b.Customer, SellerName: s.seller.Name, SellerAddress: s.seller.Address, Currency: b.Currency, Incoterm: b.Incoterm, PortOfLoading: b.LoadingPort, PortOfDischarge: b.DestinationPort, PaymentMethod: b.Payment, DeliveryDate: b.Delivery, Terms: b.Remark, TotalAmount: b.Total, BaseAmount: baseAmount, FxRate: rate.StringFixed(8), FxRateAt: tsFrom(at), FxSource: "MANUAL_EFFECTIVE", FxBaseCurrency: "USD", CreatedBy: op.EmployeeID})
		if err != nil {
			return err
		}
		for i, line := range b.Lines {
			if err := q.AddContractItem(ctx, store.AddContractItemParams{TenantID: op.TenantID, ContractVersionID: versionID, LineNo: int32(i + 1), ProductID: 0, ProductName: line.Product, Spec: offerSpecification(line.OfferProduct, view.Source), Qty: line.Quantity, UomCode: line.Unit, UnitPrice: line.UnitPrice, Amount: line.Amount, Remark: line.Remark}); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE customer_offers SET quotation_id=$3,contract_id=$4,confirmed_at=now(),updated_at=now(),updated_by=$5,revision=revision+1 WHERE tenant_id=$1 AND case_id=$2`, op.TenantID, id, quotationID, contractID, op.EmployeeID)
		return err
	})
}

func offerPDF(view OfferView) ([]byte, error) {
	b := view.Body
	q := store.GetQuotationRow{QuoteNo: view.Source.Number, CustomerName: b.Customer, Currency: b.Currency, Incoterm: b.Incoterm, PortOfLoading: b.LoadingPort, PortOfDischarge: b.DestinationPort, PaymentMethod: b.Payment, ValidUntil: b.ValidUntil, TotalAmount: b.Total, ContactName: b.Contact, Remark: "交货日期: " + b.Delivery + "\n" + b.Remark}
	items := make([]store.ListQuotationItemsRow, 0, len(b.Lines))
	for i, line := range b.Lines {
		items = append(items, store.ListQuotationItemsRow{LineNo: int32(i + 1), ProductName: line.Product, Spec: offerSpecification(line.OfferProduct, view.Source), Qty: line.Quantity, UomCode: line.Unit, UnitPrice: line.UnitPrice, Amount: line.Amount, Remark: line.Remark})
	}
	shipments := []store.ListQuotationShipmentsRow{}
	for i, tr := range b.Transports {
		var logistics struct {
			Company         string `json:"company"`
			Route           string `json:"route"`
			LoadingPort     string `json:"loadingPort"`
			DestinationPort string `json:"destinationPort"`
			Departure       string `json:"departure"`
			Arrival         string `json:"arrival"`
			ValidUntil      string `json:"validUntil"`
		}
		for _, source := range view.Source.Quotes {
			if source.ID == tr.QuoteID {
				if err := json.Unmarshal(source.Body, &logistics); err != nil {
					return nil, err
				}
			}
		}
		status := "候选方案"
		if tr.Accepted {
			status = "客户已选择"
		}
		shipments = append(shipments, store.ListQuotationShipmentsRow{BatchNo: int32(i + 1), CarrierForwarder: logistics.Company, ServiceOptionName: tr.Title, Currency: tr.Currency, FreightAmount: tr.Price, PortOfLoading: logistics.LoadingPort, PortOfDischarge: logistics.DestinationPort, EstimatedDeparture: logistics.Departure, EstimatedArrival: logistics.Arrival, ValidUntil: logistics.ValidUntil, Remark: status + "；" + tr.Remark})
	}
	// Only explicit customer-facing fields are passed to the PDF renderer.
	// Source prices, internal formulas and effective-rate snapshots stay internal.
	return buildQuotationPDFWithShipments(q, items, shipments)
}

// Flatten the frozen template's customer-facing specifications deterministically
// for both quotation PDF and contract lines; internal calculation fields never enter here.
func offerSpecification(p OfferProduct, source OfferInquiry) string {
	var template struct {
		Fields []struct {
			Key   string `json:"fieldKey"`
			Label string `json:"displayName"`
		} `json:"fields"`
	}
	_ = json.Unmarshal(source.Body.Template, &template)
	labels := map[string]string{}
	for _, f := range template.Fields {
		labels[f.Key] = f.Label
	}
	parts := []string{}
	if strings.TrimSpace(p.Specification) != "" {
		parts = append(parts, p.Specification)
	}
	keys := make([]string, 0, len(p.CustomFields))
	for k := range p.CustomFields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if value := strings.TrimSpace(p.CustomFields[k]); value != "" {
			label := labels[k]
			if label == "" {
				label = k
			}
			parts = append(parts, label+": "+value)
		}
	}
	return strings.Join(parts, "; ")
}
