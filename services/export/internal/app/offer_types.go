package app

import (
	"context"
	"encoding/json"
)

type OfferSource interface {
	ReadInquiry(context.Context, int64) (OfferInquiry, error)
}
type OfferAccess interface {
	HasPermission(context.Context, int64, string) (bool, error)
}
type OfferRates interface {
	EffectiveRates(context.Context) ([]OfferRate, error)
}
type OfferRate struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
	Value string `json:"value"`
	At    string `json:"at"`
}
type OfferInquiry struct {
	ID      string `json:"id"`
	Number  string `json:"number"`
	OwnerID string `json:"ownerId"`
	Owner   string `json:"owner"`
	State   string `json:"state"`
	Body    struct {
		Template        json.RawMessage `json:"template"`
		Attachments     json.RawMessage `json:"attachments"`
		CustomerID      string          `json:"customerId"`
		Customer        string          `json:"customer"`
		ContactID       string          `json:"contactId"`
		Contact         string          `json:"contact"`
		Delivery        string          `json:"delivery"`
		LoadingPort     string          `json:"loadingPort"`
		DestinationPort string          `json:"destinationPort"`
		Incoterm        string          `json:"incoterm"`
		Remark          string          `json:"remark"`
		Products        []OfferProduct  `json:"products"`
	} `json:"body"`
	Quotes []OfferSourceQuote `json:"quotes"`
}
type OfferProduct struct {
	Weight          string            `json:"weight"`
	Volume          string            `json:"volume"`
	Packaging       string            `json:"packaging"`
	PackageQuantity string            `json:"packageQuantity"`
	ID              string            `json:"id"`
	Product         string            `json:"product"`
	Specification   string            `json:"specification"`
	Quantity        string            `json:"quantity"`
	Unit            string            `json:"unit"`
	Delivery        string            `json:"delivery"`
	Remark          string            `json:"remark"`
	CustomFields    map[string]string `json:"customFields"`
}
type OfferSourceQuote struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Version int64           `json:"version"`
	Body    json.RawMessage `json:"body"`
}
type OfferLine struct {
	OfferProduct
	FactoryQuoteID  string           `json:"factoryQuoteId"`
	Calculation     OfferCalculation `json:"calculation"`
	CalculatedPrice string           `json:"calculatedPrice"`
	UnitPrice       string           `json:"unitPrice"`
	Amount          string           `json:"amount"`
}
type OfferTransport struct {
	QuoteID string `json:"quoteId"`
	// Candidate prices are customer-facing, independent of source costs.
	Title      string            `json:"title"`
	Currency   string            `json:"currency"`
	Price      string            `json:"price"`
	Remark     string            `json:"remark"`
	Accepted   bool              `json:"accepted"`
	Quantities map[string]string `json:"quantities"`
}
type OfferBody struct {
	CustomerID      string           `json:"customerId"`
	Customer        string           `json:"customer"`
	ContactID       string           `json:"contactId"`
	Contact         string           `json:"contact"`
	Currency        string           `json:"currency"`
	Delivery        string           `json:"delivery"`
	LoadingPort     string           `json:"loadingPort"`
	DestinationPort string           `json:"destinationPort"`
	Incoterm        string           `json:"incoterm"`
	Payment         string           `json:"payment"`
	ValidUntil      string           `json:"validUntil"`
	Remark          string           `json:"remark"`
	Lines           []OfferLine      `json:"lines"`
	Transports      []OfferTransport `json:"transports"`
	Rates           []OfferRate      `json:"rates"`
	Total           string           `json:"total"`
}
type OfferCommand struct {
	LineID   string    `json:"lineId"`
	Action   string    `json:"action"`
	CaseID   string    `json:"caseId"`
	Revision int64     `json:"revision"`
	Body     OfferBody `json:"body"`
}
type OfferView struct {
	Body        OfferBody    `json:"body"`
	Revision    int64        `json:"revision"`
	Status      string       `json:"status"`
	QuotationID string       `json:"quotationId"`
	ContractID  string       `json:"contractId"`
	CanEdit     bool         `json:"canEdit"`
	Source      OfferInquiry `json:"source"`
}
