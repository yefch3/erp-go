// Package app holds the procurement use cases. The first of them is the one
// that makes this service worth having: turning a contract that took effect
// into a list of things somebody has to go and buy.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// Operator is who is asking. Procurement does not resolve names or
// permissions itself; the gateway has already done both.
type Operator struct {
	ID   int64
	Name string
}

// Numbering issues document numbers. masterdata owns every number in the
// system; procurement asks rather than inventing a format of its own.
type Numbering interface {
	Next(ctx context.Context, bizType string) (string, error)
}

type Rate struct {
	Rate         decimal.Decimal
	At           time.Time
	Source, Base string
}

type Rates interface {
	Latest(context.Context, string) (Rate, error)
}

// Scopes asks IAM whose sourcing cases the caller may access. Procurement
// owns the documents; IAM owns the organisation chart and role policy.
type Scopes interface {
	VisibleEmployees(ctx context.Context, employeeID int64, module string) (Visibility, error)
}

type Visibility struct {
	All         bool
	EmployeeIDs []int64
	ScopeType   string
}

// Approvals is the approval engine. It never learns what a purchase order is
// — it routes on a business type and an amount and reports a decision.
type Approvals interface {
	Submit(ctx context.Context, in ApprovalSubmission) (int64, error)
}

// Suppliers resolves the current master-data record before an order snapshot
// is written. Names and codes sent by a browser are display values, not facts.
type Suppliers interface {
	Get(ctx context.Context, id int64) (Supplier, error)
}

type Supplier struct {
	ID       int64
	Code     string
	Name     string
	Currency string
	Status   string
}

// Warehouses resolves the custody destination selected for a receipt.
type Warehouses interface {
	Get(ctx context.Context, id int64) (Warehouse, error)
}

// Files is the object storage invoice scans and inquiry originals live in.
// Browser uploads go through presigned URLs so the bytes never pass through
// this service; Put is the one exception — the inquiry Excel arrives inside
// the create request itself, so the service stores it on the caller's
// behalf.
type Files interface {
	PresignPut(ctx context.Context, key string) (url string, expires int32, err error)
	PresignGet(ctx context.Context, key string) (string, error)
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Remove(ctx context.Context, key string) error
}

type Warehouse struct {
	ID     int64
	Name   string
	Type   string
	Status string
}

// ApprovalSubmission is one document entering an approval flow.
type ApprovalSubmission struct {
	BizType       string
	BizID         int64
	BizNo         string
	Summary       string
	SubmitterID   int64
	SubmitterName string
	// What the company is about to spend. The approval engine routes on it,
	// so a large order can require more signatures than a small one.
	Amount string
}

// Deps are the outside services procurement talks to.
type Deps struct {
	Numbering  Numbering
	Approvals  Approvals
	Suppliers  Suppliers
	Warehouses Warehouses
	Rates      Rates
	Scopes     Scopes
	// Optional in tests; main always wires it. Nil degrades attachment
	// endpoints to a clean error instead of a panic.
	Files Files
	// Optional: without it the pages still work, they just need a refresh.
	Live *livefeed.Publisher
}

type Service struct {
	pool       *pgxpool.Pool
	q          *store.Queries
	numbering  Numbering
	approvals  Approvals
	suppliers  Suppliers
	warehouses Warehouses
	rates      Rates
	scopes     Scopes
	files      Files
	live       *livefeed.Publisher

	// Three-way-match tolerance, zero unless the operator widens it.
	// See UseMatchTolerance for what the two numbers mean.
	matchTolPct decimal.Decimal
	matchTolAbs decimal.Decimal

	// Book currency for fx snapshots; empty means the default (CNY).
	// See fxsnapshot.go.
	baseCurrency string
}

func New(pool *pgxpool.Pool, d Deps) *Service {
	return &Service{
		pool: pool, q: store.New(pool),
		numbering: d.Numbering, approvals: d.Approvals,
		suppliers: d.Suppliers, warehouses: d.Warehouses, rates: d.Rates,
		scopes: d.Scopes, files: d.Files, live: d.Live,
	}
}

// nudge tells every open purchasing page to re-read. Broadcast rather than
// addressed: a requirement belongs to the purchasing function, not to a
// person, so there is nobody in particular to notify.
//
// Always after the commit. A hint about a change that then rolled back would
// send browsers to read something that never happened.
func (s *Service) nudge(ctx context.Context, tenantID int64) {
	if s.live == nil {
		return
	}
	s.live.ToTenant(ctx, tenantID, livefeed.Event{Type: livefeed.RequirementChanged})
}

// RequirementFilter narrows a requirement list.
type RequirementFilter struct {
	Status     string
	ContractID int64
	Keyword    string
}

func (s *Service) ListRequirements(ctx context.Context, tenantID int64, f RequirementFilter, page, size int32) ([]store.ListRequirementsRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListRequirements(ctx, store.ListRequirementsParams{
		TenantID: tenantID, Status: f.Status, ContractID: f.ContractID, Keyword: f.Keyword,
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

func (s *Service) GetRequirement(ctx context.Context, tenantID, id int64) (store.GetRequirementRow, error) {
	row, err := s.q.GetRequirement(ctx, store.GetRequirementParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.GetRequirementRow{}, apierr.NotFound("PR_REQUIREMENT_NOT_FOUND", "采购需求不存在")
		}
		return store.GetRequirementRow{}, err
	}
	return row, nil
}

// CancelRequirement closes one the business will not buy against. A reason is
// mandatory: a requirement that came from a signed contract disappearing
// without explanation is exactly the thing an audit asks about.
func (s *Service) CancelRequirement(ctx context.Context, tenantID, id int64, reason string, op Operator) (string, error) {
	if reason == "" {
		return "", apierr.Invalid("PR_CANCEL_REASON_REQUIRED", "关闭采购需求必须填写原因")
	}
	current, err := s.GetRequirement(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if current.Status != "PENDING" {
		return "", apierr.Conflict("PR_REQUIREMENT_NOT_PENDING", "只有待采购的需求可以关闭").
			WithMeta("status", current.Status)
	}
	status, err := s.q.CancelRequirement(ctx, store.CancelRequirementParams{
		TenantID: tenantID, ID: id, Reason: reason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Someone else moved it between the read and the write.
			return "", apierr.Conflict("PR_REQUIREMENT_CHANGED", "需求状态已变化，请刷新后重试")
		}
		return "", err
	}
	s.nudge(ctx, tenantID)
	return status, nil
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

// ManualRequirement is a buyer raising one themselves.
type ManualRequirement struct {
	ProductID    int64
	SkuID        int64
	ProductCode  string
	ProductName  string
	Spec         string
	UomID        int64
	UomCode      string
	RequiredQty  string
	RequiredDate string
	Remark       string
}

// CreateRequirement raises a requirement with no contract behind it.
//
// This is an explicit exception independent of a customer contract. Ordinary
// contract-driven requirements are created automatically for the full sold
// quantity.
func (s *Service) CreateRequirement(ctx context.Context, tenantID int64, in ManualRequirement, op Operator) (store.GetRequirementRow, error) {
	if in.ProductID == 0 {
		return store.GetRequirementRow{}, apierr.Invalid("PR_PRODUCT_REQUIRED", "请选择产品")
	}
	qty, err := decimal.NewFromString(in.RequiredQty)
	if err != nil || qty.LessThanOrEqual(decimal.Zero) {
		return store.GetRequirementRow{}, apierr.Invalid("PR_QTY_INVALID", "采购数量必须大于 0")
	}
	id, err := s.q.CreateManualRequirement(ctx, store.CreateManualRequirementParams{
		TenantID: tenantID, ProductID: in.ProductID, SkuID: in.SkuID,
		ProductCode: in.ProductCode, ProductName: in.ProductName, Spec: in.Spec,
		UomID: in.UomID, UomCode: in.UomCode, RequiredQty: qty.String(),
		RequiredDate: in.RequiredDate, Remark: in.Remark,
	})
	if err != nil {
		return store.GetRequirementRow{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetRequirement(ctx, tenantID, id)
}
