package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/shopspring/decimal"
)

type offerWorkbookLabels struct {
	sheet, title, quoteNo, customer, contact, currency, incoterm, loadingPort, dischargePort, payment, validUntil, delivery, total string
	headers                                                                                                                        []string
}

func workbookLabels(language string) offerWorkbookLabels {
	switch strings.ToUpper(strings.TrimSpace(language)) {
	case "EN":
		return offerWorkbookLabels{"Customer Quotation", "CUSTOMER QUOTATION", "Quotation No.", "Customer", "Contact", "Currency", "Incoterm", "Port of Loading", "Port of Discharge", "Payment Terms", "Valid Until", "Delivery", "Total", []string{"#", "Product", "Specification", "Quantity", "Unit", "Unit Price", "Amount", "Remark"}}
	case "ES":
		return offerWorkbookLabels{"Cotización", "COTIZACIÓN PARA EL CLIENTE", "N.º de cotización", "Cliente", "Contacto", "Moneda", "Incoterm", "Puerto de carga", "Puerto de destino", "Condiciones de pago", "Válida hasta", "Entrega", "Total", []string{"#", "Producto", "Especificación", "Cantidad", "Unidad", "Precio unitario", "Importe", "Observaciones"}}
	default:
		return offerWorkbookLabels{"客户报价", "客户报价单", "报价单号", "客户", "联系人", "币种", "贸易术语", "装运港", "目的港", "付款方式", "有效期至", "交货日期", "合计", []string{"序号", "产品", "规格", "数量", "单位", "单价", "金额", "备注"}}
	}
}

type offerWorkbookLine struct {
	product, specification, quantity, unit, unitPrice, amount, remark string
}

func buildOfferWorkbook(number string, body OfferBody, lines []offerWorkbookLine, total string) ([]byte, error) {
	labels := workbookLabels(body.DocumentLanguage)
	rows := [][]string{
		{labels.title},
		{labels.quoteNo, number, labels.customer, body.Customer, labels.contact, body.Contact},
		{labels.currency, body.Currency, labels.incoterm, body.Incoterm, labels.payment, body.Payment},
		{labels.loadingPort, body.LoadingPort, labels.dischargePort, body.DestinationPort},
		{labels.validUntil, body.ValidUntil, labels.delivery, body.Delivery},
		{},
		labels.headers,
	}
	formulas := make(map[string]xlsx.Formula, len(lines)+1)
	numbers := make(map[string]string, len(lines)*2)
	firstDataRow := len(rows) + 1
	for index, line := range lines {
		rowNumber := len(rows) + 1
		rows = append(rows, []string{strconv.Itoa(index + 1), line.product, line.specification, line.quantity, line.unit, line.unitPrice, line.amount, line.remark})
		numbers[fmt.Sprintf("D%d", rowNumber)] = line.quantity
		numbers[fmt.Sprintf("F%d", rowNumber)] = line.unitPrice
		formulas[fmt.Sprintf("G%d", rowNumber)] = xlsx.Formula{Expression: fmt.Sprintf("D%d*F%d", rowNumber, rowNumber), CachedValue: line.amount}
	}
	totalRow := len(rows) + 1
	rows = append(rows, []string{"", "", "", "", "", labels.total, total, body.Remark})
	if len(lines) > 0 {
		formulas[fmt.Sprintf("G%d", totalRow)] = xlsx.Formula{Expression: fmt.Sprintf("SUM(G%d:G%d)", firstDataRow, totalRow-1), CachedValue: total}
	}
	return xlsx.BuildWithFormulasAndNumbers(labels.sheet, rows, formulas, numbers)
}

func offerWorkbook(view OfferView) ([]byte, error) {
	body := view.Body
	lines := make([]offerWorkbookLine, 0, len(body.Lines))
	for _, line := range body.Lines {
		lines = append(lines, offerWorkbookLine{
			product: line.Product, specification: offerSpecification(line.OfferProduct, view.Source), quantity: line.Quantity,
			unit: line.Unit, unitPrice: line.UnitPrice, amount: line.Amount, remark: line.Remark,
		})
	}
	return buildOfferWorkbook(view.Source.Number, body, lines, body.Total)
}

func categoryOfferWorkbook(view OfferView) ([]byte, error) {
	body := view.Body
	if len(body.CustomerSelections) == 0 {
		return nil, apierr.Invalid("OFFER_CATEGORY_EMPTY", "请至少选择一项客户报价")
	}
	prices := make(map[string]OfferNegotiation, len(body.Negotiations))
	for _, negotiation := range body.Negotiations {
		prices[negotiation.ProductID+":"+negotiation.Category+":"+negotiation.QuoteID] = negotiation
	}
	lines := make([]offerWorkbookLine, 0, len(body.CustomerSelections))
	total := decimal.Zero
	for _, selection := range body.CustomerSelections {
		negotiation := prices[selection.ProductID+":"+selection.Category+":"+selection.QuoteID]
		price := strings.TrimSpace(negotiation.ProposedPrice)
		if price == "" {
			price = strings.TrimSpace(negotiation.InitialPrice)
		}
		if price == "" {
			price = strings.TrimSpace(selection.CFRUnitPrice)
		}
		unitPrice, err := decimal.NewFromString(price)
		if err != nil || unitPrice.IsNegative() {
			return nil, apierr.Invalid("OFFER_CUSTOMER_PRICE_REQUIRED", "请先完成初始核算，或填写最新对客单价")
		}
		quantity, err := decimal.NewFromString(selection.Product.Quantity)
		if err != nil || !quantity.IsPositive() {
			return nil, apierr.Invalid("OFFER_CFR_QUANTITY", "产品数量必须大于零")
		}
		amount := unitPrice.Mul(quantity).Round(2)
		total = total.Add(amount)
		lines = append(lines, offerWorkbookLine{
			product: selection.Product.Product, specification: offerSpecification(selection.Product, view.Source), quantity: selection.Product.Quantity,
			unit: selection.Product.Unit, unitPrice: unitPrice.StringFixed(4), amount: amount.StringFixed(2), remark: selection.Product.Remark,
		})
	}
	body.Currency = "USD"
	body.Incoterm = "CFR"
	return buildOfferWorkbook(view.Source.Number, body, lines, total.StringFixed(2))
}
