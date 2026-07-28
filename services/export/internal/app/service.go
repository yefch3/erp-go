// Package app holds the export use cases: what was offered, what was
// contracted, at which price and under which exchange rate.
package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// Customer is the slice of a customer this service needs. Export never reads
// masterdata's tables; it asks, and copies what it must display later.
type Customer struct {
	ID       int64
	Name     string
	Address  string
	Currency string
	Status   string
	Contacts []Contact
}

// Contact is one person at the customer; the quotation records which of them
// it is addressed to, and where it would be sent.
type Contact struct {
	ID        int64
	Name      string
	Email     string
	IsPrimary bool
}

// Product is likewise the slice of a product a document line copies.
type Product struct {
	ID      int64
	Code    string
	Name    string
	UomID   int64
	UomCode string
	HsCode  string
	Status  string
}

// Rate is one exchange-rate reading, taken once per document.
type Rate struct {
	Rate   decimal.Decimal
	At     time.Time
	Source string
	Base   string
}

// Operator is who is acting, carried down from the JWT.
type Operator struct {
	ID   int64
	Name string
}

// Seller is our own side of a contract. It is configuration rather than
// master data for now; a proper company-entity table belongs in masterdata
// once more than one legal entity exists.
type Seller struct {
	Name    string
	Address string
}

// ApprovalSubmission is what starts an approval flow. The summary travels
// with it so approvers see what they are deciding on without the approval
// service ever calling back.
type ApprovalSubmission struct {
	BizType       string
	BizID         int64
	BizNo         string
	Summary       string
	SubmitterID   int64
	SubmitterName string
	// Base-currency total. Comparing 100,000 JPY against a threshold set in
	// USD would route large deals as small ones, so the amount that reaches
	// the approval engine is always normalised.
	Amount string
}

type Customers interface {
	Get(ctx context.Context, id int64) (Customer, error)
}

type Products interface {
	Get(ctx context.Context, id int64) (Product, error)
}

type Rates interface {
	Latest(ctx context.Context, currency string) (Rate, error)
}

type Numbering interface {
	Next(ctx context.Context, bizType string) (string, error)
}

// Scopes answers "whose documents may this person see". Export does not
// decide that itself: ownership rules belong with the organisation chart, and
// duplicating them here would let the two drift apart.
type Scopes interface {
	VisibleEmployees(ctx context.Context, employeeID int64, module string) (Visibility, error)
}

// Visibility is the answer. All short-circuits the id list.
type Visibility struct {
	All         bool
	EmployeeIDs []int64
	ScopeType   string
}

// Involvement answers "which documents was this person asked to act on".
// Separate from the data scope on purpose: the scope is a policy choice about
// whose records you may browse, while this is a correctness requirement —
// somebody asked to approve a document who cannot open it has been handed an
// impossible job.
type Involvement interface {
	MyDocuments(ctx context.Context, employeeID int64, bizType string) ([]int64, error)
}

// Approvals is the write side of the approval engine. Export calls it; it
// never calls export, and neither reads the other's tables.
type Approvals interface {
	Submit(ctx context.Context, in ApprovalSubmission) (int64, error)
}

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	customers Customers
	products  Products
	rates     Rates
	number    Numbering
	approvals Approvals
	files     Files
	scopes    Scopes
	involved  Involvement
	seller    Seller
}

func New(pool *pgxpool.Pool, c Customers, p Products, r Rates, n Numbering, a Approvals, f Files, sc Scopes, inv Involvement, seller Seller) *Service {
	return &Service{
		pool: pool, q: store.New(pool),
		customers: c, products: p, rates: r, number: n,
		approvals: a, files: f, scopes: sc, involved: inv, seller: seller,
	}
}

// involvedIn lists the documents of one type this person has been asked to
// act on. A failure to reach approval is not fatal here: it narrows what the
// user sees rather than widening it, and the data scope still applies.
func (s *Service) involvedIn(ctx context.Context, op Operator, bizType string) []int64 {
	if s.involved == nil || op.ID == 0 {
		return nil
	}
	ids, err := s.involved.MyDocuments(ctx, op.ID, bizType)
	if err != nil {
		return nil
	}
	return ids
}

// scopeModule is the key contracts and quotations are scoped under; it must
// match role_data_scopes.module in iam.
const scopeModule = "export"

// visibleTo resolves what this person may see. A failure to reach iam is not
// treated as "show everything": the safe direction for an access rule is to
// deny, and a visible error beats a silent leak.
func (s *Service) visibleTo(ctx context.Context, op Operator) (Visibility, error) {
	if s.scopes == nil {
		return Visibility{All: true, ScopeType: "ALL"}, nil
	}
	return s.scopes.VisibleEmployees(ctx, op.ID, scopeModule)
}

// mayWrite refuses a change to a document the caller's data scope does not
// cover. Reading is deliberately wider than writing: an approver can open what
// they were handed, and a supervisor can watch their team, but neither of
// those is a licence to edit somebody else's paperwork under their name.
func (s *Service) mayWrite(ctx context.Context, op Operator, ownerID int64, code, msg string) error {
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return err
	}
	if !allowedOwner(visible, ownerID) {
		return apierr.Permission(code, msg)
	}
	return nil
}

func normalizePage(page, size int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func itoa(n int) string {
	return decimal.NewFromInt(int64(n)).String()
}
