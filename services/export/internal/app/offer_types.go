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
	Historical  bool            `json:"historical,omitempty"`
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Version     int64           `json:"version"`
	Body        json.RawMessage `json:"body"`
	Author      string          `json:"author"`
	SubmittedAt string          `json:"submittedAt"`
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
type OfferCategorySelection struct {
	ProductID            string `json:"productId"`
	Category             string `json:"category"`
	QuoteID              string `json:"quoteId"`
	SupplierFOBUnitPrice string `json:"supplierFobUnitPrice,omitempty"`
	ProductFreightTotal  string `json:"productFreightTotal,omitempty"`
	InlandFreightTotal   string `json:"inlandFreightTotal,omitempty"`
	CFRTotal             string `json:"cfrTotal,omitempty"`
	CFRUnitPrice         string `json:"cfrUnitPrice,omitempty"`
	FreightQuoteID       string `json:"freightQuoteId,omitempty"`
}
type OfferBody struct {
	CategoryWorkflow     bool                           `json:"categoryWorkflow,omitempty"`
	CustomerSelections   []OfferSelectionSnapshot       `json:"customerSelections"`
	CustomerLogistics    []OfferSourceQuote             `json:"customerLogistics"`
	SelectionSavedAt     string                         `json:"selectionSavedAt"`
	CategoryCalculations map[string]CategoryCalculation `json:"categoryCalculations"`
	Negotiations         []OfferNegotiation             `json:"negotiations"`
	DocumentLanguage     string                         `json:"documentLanguage"`
	PricingSnapshot      string                         `json:"pricingSnapshot,omitempty"`
	LogisticsQuoteID     string                         `json:"logisticsQuoteId"`
	CustomerID           string                         `json:"customerId"`
	Customer             string                         `json:"customer"`
	ContactID            string                         `json:"contactId"`
	Contact              string                         `json:"contact"`
	Currency             string                         `json:"currency"`
	QuoteFX              string                         `json:"quoteFx"`
	QuoteFXConfirmed     bool                           `json:"quoteFxConfirmed"`
	Delivery             string                         `json:"delivery"`
	LoadingPort          string                         `json:"loadingPort"`
	DestinationPort      string                         `json:"destinationPort"`
	Incoterm             string                         `json:"incoterm"`
	Payment              string                         `json:"payment"`
	ValidUntil           string                         `json:"validUntil"`
	Remark               string                         `json:"remark"`
	Lines                []OfferLine                    `json:"lines"`
	Transports           []OfferTransport               `json:"transports"`
	CategorySelections   []OfferCategorySelection       `json:"categorySelections"`
	Rates                []OfferRate                    `json:"rates"`
	Total                string                         `json:"total"`
	LogisticsAllocations []OfferLogisticsAllocation     `json:"logisticsAllocations"`
}

type CategoryCalculation struct {
	QuoteFX          string `json:"quoteFx"`
	QuoteFXConfirmed bool   `json:"quoteFxConfirmed"`
	PortCharge       string `json:"portCharge"`
	Loss             string `json:"loss"`
	Note             string `json:"note"`
}

type OfferNegotiation struct {
	OfferCategorySelection
	InitialPrice         string `json:"initialPrice"`
	CustomerCounterPrice string `json:"customerCounterPrice"`
	ProposedPrice        string `json:"proposedPrice"`
	Status               string `json:"status"`
	Note                 string `json:"note"`
}

// Snapshot of sales' proposed alternatives; customer acceptance happens later.
type OfferSelectionSnapshot struct {
	OfferCategorySelection
	Product OfferProduct     `json:"product"`
	Quote   OfferSourceQuote `json:"quote"`
}
type OfferLogisticsAllocation struct {
	ProductID string `json:"productId"`
	Product   string `json:"product"`
	Currency  string `json:"currency"`
	Amount    string `json:"amount"`
}
type OfferCommand struct {
	LineID   string    `json:"lineId"`
	Action   string    `json:"action"`
	CaseID   string    `json:"caseId"`
	Revision int64     `json:"revision"`
	Body     OfferBody `json:"body"`
}
type OfferView struct {
	PricingStale bool         `json:"pricingStale"`
	Body         OfferBody    `json:"body"`
	Revision     int64        `json:"revision"`
	Status       string       `json:"status"`
	QuotationID  string       `json:"quotationId"`
	ContractID   string       `json:"contractId"`
	CanEdit      bool         `json:"canEdit"`
	Source       OfferInquiry `json:"source"`
}
