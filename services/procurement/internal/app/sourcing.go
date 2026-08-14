package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type SourcingLineInput struct {
	RawText, Product, MaterialStandard, Grade, Thickness, Width, LengthOrForm string
	SurfaceRequirement, Coating, Tolerance, CoilWeight, CoilID, Packaging     string
	Delivery, PaymentTerms, Incoterm, Port, QuantityUnit, Remarks, Quantity   string
}

type NewSourcingCase struct {
	Title, CustomerName, ContactName, ContactEmail string
	CustomerID, SourceMailID, SourceAttachmentID   int64
	Lines                                          []SourcingLineInput
}

type SourcingCaseView struct {
	Head  store.GetSourcingCaseRow
	Lines []store.ListSourcingLinesRow
}

type SourcingFilter struct{ Status, Keyword string }

func (s *Service) CreateSourcingCase(ctx context.Context, tenantID int64, in NewSourcingCase, op Operator) (SourcingCaseView, error) {
	if len(in.Lines) == 0 {
		return SourcingCaseView{}, apierr.Invalid("SC_LINES_REQUIRED", "询价案件至少需要一条产品明细")
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = "客户询盘"
	}
	for _, line := range in.Lines {
		if line.Quantity == "" {
			continue
		}
		qty, err := decimal.NewFromString(line.Quantity)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return SourcingCaseView{}, apierr.Invalid("SC_QUANTITY_INVALID", "询价数量必须是大于 0 的数字")
		}
	}

	var id int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.CreateSourcingCase(ctx, store.CreateSourcingCaseParams{
			TenantID: tenantID, Title: strings.TrimSpace(in.Title), CustomerID: in.CustomerID,
			CustomerName: strings.TrimSpace(in.CustomerName), ContactName: strings.TrimSpace(in.ContactName),
			ContactEmail: strings.TrimSpace(in.ContactEmail), SourceMailID: in.SourceMailID,
			SourceAttachmentID: in.SourceAttachmentID, OwnerID: op.ID, OwnerName: op.Name,
		})
		if err != nil {
			return err
		}
		id = head.ID
		for i, line := range in.Lines {
			if err := q.CreateSourcingLine(ctx, sourcingLineParams(tenantID, id, int32(i+1), line)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return SourcingCaseView{}, err
	}
	return s.GetSourcingCase(ctx, tenantID, id)
}

func sourcingLineParams(tenantID, caseID int64, lineNo int32, in SourcingLineInput) store.CreateSourcingLineParams {
	return store.CreateSourcingLineParams{
		TenantID: tenantID, CaseID: caseID, LineNo: lineNo, RawText: in.RawText,
		Product: in.Product, MaterialStandard: in.MaterialStandard, Grade: in.Grade,
		Thickness: in.Thickness, Width: in.Width, LengthOrForm: in.LengthOrForm,
		SurfaceRequirement: in.SurfaceRequirement, Coating: in.Coating,
		Tolerance: in.Tolerance, CoilWeight: in.CoilWeight, CoilID: in.CoilID,
		Packaging: in.Packaging, Delivery: in.Delivery, PaymentTerms: in.PaymentTerms,
		Incoterm: in.Incoterm, Port: in.Port, QuantityUnit: in.QuantityUnit,
		Remarks: in.Remarks, Quantity: in.Quantity,
	}
}

func (s *Service) ListSourcingCases(ctx context.Context, tenantID int64, f SourcingFilter, page, size int32) ([]store.ListSourcingCasesRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListSourcingCases(ctx, store.ListSourcingCasesParams{
		TenantID: tenantID, Status: f.Status, Keyword: f.Keyword,
		RowOffset: (page - 1) * size, RowLimit: size,
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

func (s *Service) GetSourcingCase(ctx context.Context, tenantID, id int64) (SourcingCaseView, error) {
	head, err := s.q.GetSourcingCase(ctx, store.GetSourcingCaseParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return SourcingCaseView{}, apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	if err != nil {
		return SourcingCaseView{}, err
	}
	lines, err := s.q.ListSourcingLines(ctx, store.ListSourcingLinesParams{TenantID: tenantID, CaseID: id})
	if err != nil {
		return SourcingCaseView{}, err
	}
	return SourcingCaseView{Head: head, Lines: lines}, nil
}
