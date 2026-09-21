package app

import (
	"context"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
	"strings"
)

func createImportedCustomer(ctx context.Context, q *store.Queries, tenantID, operatorID int64, code string, row CustomerImportRow) (store.Customer, error) {
	countryCode, _ := normaliseCountry(row.CountryCode)
	currency := strings.ToUpper(strings.TrimSpace(row.Currency))
	if currency == "" {
		currency = "USD"
	}
	customerType := strings.ToUpper(strings.TrimSpace(row.CustomerType))
	businessStatus := strings.ToUpper(strings.TrimSpace(row.BusinessStatus))
	if businessStatus == "" {
		businessStatus = "PROSPECT"
	}
	return q.CreateCustomer(ctx, store.CreateCustomerParams{
		TenantID: tenantID, Code: code, Name: strings.TrimSpace(row.Name), Country: "", CountryCode: countryCode,
		Address: strings.TrimSpace(row.Address), Currency: currency, PaymentTerm: row.PaymentTerm, Remark: row.Remark,
		ShortName: strings.TrimSpace(row.ShortName), EnglishName: strings.TrimSpace(row.EnglishName), CustomerType: customerType, Industry: strings.TrimSpace(row.Industry), Source: strings.ToUpper(strings.TrimSpace(row.Source)),
		Tags: splitImportTags(row.Tags), Website: strings.TrimSpace(row.Website), PrimaryLanguage: strings.TrimSpace(row.PrimaryLanguage), Timezone: strings.TrimSpace(row.Timezone), RegisteredName: strings.TrimSpace(row.RegisteredName),
		RegistrationNo: strings.TrimSpace(row.RegistrationNo), TaxID: strings.TrimSpace(row.TaxID), InvoiceTitle: strings.TrimSpace(row.InvoiceTitle), InvoiceTaxNo: strings.TrimSpace(row.InvoiceTaxNo), InvoiceRemark: strings.TrimSpace(row.InvoiceRemark),
		CreditLimitMinor: 0, CreditCurrency: currency, CreditStatus: "NORMAL",
		BusinessStatus: businessStatus, OperatorID: operatorID,
	})
}
