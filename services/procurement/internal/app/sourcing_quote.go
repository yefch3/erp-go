package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type NewFactoryRFQ struct {
	CaseID, SupplierID                    int64
	ContactEmail, Currency, ResponseDueAt string
	SourcingLineIDs                       []int64
}

type SupplierQuoteLineInput struct {
	SourcingLineID                        int64
	Qty, UnitPrice, MOQ, LeadTime, Remark string
}

type NewSupplierQuote struct {
	FactoryRFQID                                                           int64
	QuotedAt, ValidUntil, Currency, PaymentTerms, Delivery, Remark, Source string
	Lines                                                                  []SupplierQuoteLineInput
}

func (s *Service) CreateFactoryRFQ(ctx context.Context, tenantID int64, in NewFactoryRFQ, op Operator) (store.ListFactoryRFQsRow, error) {
	if in.CaseID == 0 || in.SupplierID == 0 {
		return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_REQUIRED", "请选择询价案件和供应商")
	}
	if _, err := s.GetSourcingCase(ctx, tenantID, in.CaseID); err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	supplier, err := s.supplierForOrder(ctx, in.SupplierID)
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	if in.Currency == "" {
		in.Currency = supplier.Currency
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	caseLines, err := s.q.ListSourcingLines(ctx, store.ListSourcingLinesParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	allowed := make(map[int64]bool, len(caseLines))
	for _, line := range caseLines {
		allowed[line.ID] = line.Decision == "CONFIRMED"
	}
	if len(in.SourcingLineIDs) == 0 {
		for _, line := range caseLines {
			in.SourcingLineIDs = append(in.SourcingLineIDs, line.ID)
		}
	}
	for _, id := range in.SourcingLineIDs {
		if !allowed[id] {
			return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_LINE_UNCONFIRMED", "请先人工确认全部询价明细")
		}
	}
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.CreateFactoryRFQ(ctx, store.CreateFactoryRFQParams{TenantID: tenantID, CaseID: in.CaseID,
			SupplierID: supplier.ID, SupplierCode: supplier.Code, SupplierName: supplier.Name,
			ContactEmail: strings.TrimSpace(in.ContactEmail), Currency: strings.ToUpper(in.Currency),
			ResponseDueAt: in.ResponseDueAt, CreatedBy: op.ID, CreatedByName: op.Name})
		if err != nil {
			return err
		}
		id = head.ID
		for _, lineID := range in.SourcingLineIDs {
			if err := q.CreateFactoryRFQLine(ctx, store.CreateFactoryRFQLineParams{TenantID: tenantID, FactoryRfqID: id, CaseID: in.CaseID, SourcingLineID: lineID}); err != nil {
				return err
			}
		}
		return q.MarkSourcingCaseSourcing(ctx, store.MarkSourcingCaseSourcingParams{TenantID: tenantID, ID: in.CaseID})
	})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	rows, err := s.ListFactoryRFQs(ctx, tenantID, in.CaseID)
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return store.ListFactoryRFQsRow{}, apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
}

func (s *Service) ListFactoryRFQs(ctx context.Context, tenantID, caseID int64) ([]store.ListFactoryRFQsRow, error) {
	if _, err := s.GetSourcingCase(ctx, tenantID, caseID); err != nil {
		return nil, err
	}
	return s.q.ListFactoryRFQs(ctx, store.ListFactoryRFQsParams{TenantID: tenantID, CaseID: caseID})
}

func (s *Service) CreateSupplierQuote(ctx context.Context, tenantID int64, in NewSupplierQuote, op Operator) (store.CreateSupplierQuoteRow, error) {
	if in.FactoryRFQID == 0 || len(in.Lines) == 0 {
		return store.CreateSupplierQuoteRow{}, apierr.Invalid("SC_QUOTE_LINES_REQUIRED", "供应商报价明细不能为空")
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	if in.Source == "" {
		in.Source = "MANUAL"
	}
	var result store.CreateSupplierQuoteRow
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rfq, err := q.FactoryRFQForQuote(ctx, store.FactoryRFQForQuoteParams{TenantID: tenantID, ID: in.FactoryRFQID})
		if err != nil {
			return apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
		}
		rfqLines, err := q.FactoryRFQLines(ctx, store.FactoryRFQLinesParams{TenantID: tenantID, FactoryRfqID: in.FactoryRFQID})
		if err != nil {
			return err
		}
		allowed := make(map[int64]bool, len(rfqLines))
		for _, line := range rfqLines {
			allowed[line.SourcingLineID] = true
		}
		if len(in.Lines) != len(rfqLines) {
			return apierr.Invalid("SC_QUOTE_INCOMPLETE", "请填写全部询价明细的报价")
		}
		seen := map[int64]bool{}
		for _, line := range in.Lines {
			qty, qerr := decimal.NewFromString(line.Qty)
			price, perr := decimal.NewFromString(line.UnitPrice)
			if !allowed[line.SourcingLineID] || seen[line.SourcingLineID] {
				return apierr.Invalid("SC_QUOTE_LINE_INVALID", "报价明细与工厂询价不一致")
			}
			if qerr != nil || qty.LessThanOrEqual(decimal.Zero) || perr != nil || price.IsNegative() {
				return apierr.Invalid("SC_QUOTE_PRICE_INVALID", "数量必须大于 0，单价不能为负数")
			}
			seen[line.SourcingLineID] = true
		}
		result, err = q.CreateSupplierQuote(ctx, store.CreateSupplierQuoteParams{TenantID: tenantID, FactoryRfqID: in.FactoryRFQID,
			QuotedAt: in.QuotedAt, ValidUntil: in.ValidUntil, Currency: strings.ToUpper(in.Currency), PaymentTerms: in.PaymentTerms,
			Delivery: in.Delivery, Remark: in.Remark, Source: in.Source, CreatedBy: op.ID})
		if err != nil {
			return err
		}
		for _, line := range in.Lines {
			if err := q.CreateSupplierQuoteLine(ctx, store.CreateSupplierQuoteLineParams{TenantID: tenantID,
				SupplierQuoteID: result.ID, SourcingLineID: line.SourcingLineID, Qty: line.Qty, UnitPrice: line.UnitPrice,
				Moq: line.MOQ, LeadTime: line.LeadTime, Remark: line.Remark}); err != nil {
				return err
			}
		}
		if err := q.MarkFactoryRFQQuoted(ctx, store.MarkFactoryRFQQuotedParams{TenantID: tenantID, ID: in.FactoryRFQID}); err != nil {
			return err
		}
		return q.MarkSourcingCaseQuotesReceived(ctx, store.MarkSourcingCaseQuotesReceivedParams{TenantID: tenantID, ID: rfq.CaseID})
	})
	return result, err
}

func (s *Service) ListSupplierQuoteComparison(ctx context.Context, tenantID, caseID int64) ([]store.ListSupplierQuoteComparisonRow, error) {
	if _, err := s.GetSourcingCase(ctx, tenantID, caseID); err != nil {
		return nil, err
	}
	return s.q.ListSupplierQuoteComparison(ctx, store.ListSupplierQuoteComparisonParams{TenantID: tenantID, CaseID: caseID})
}
