package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type ExecutionSupplierQuote struct {
	ID, RequirementID, SupplierID, CreatedByID      int64
	SupplierCode, SupplierName, Currency, UnitPrice string
	ExpectedDate, PaymentTerms, ValidUntil, Remark  string
	CreatedByName, CreatedAt, UpdatedAt             string
}

type SaveExecutionSupplierQuoteInput struct {
	ID, RequirementID, SupplierID                                       int64
	Currency, UnitPrice, ExpectedDate, PaymentTerms, ValidUntil, Remark string
}

func (s *Service) ListExecutionSupplierQuotes(ctx context.Context, tenantID, requirementID int64) ([]ExecutionSupplierQuote, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,requirement_id,supplier_id,supplier_code,supplier_name,currency,unit_price::text,
		coalesce(expected_date::text,''),payment_terms,coalesce(valid_until::text,''),remark,created_by_id,created_by_name,
		created_at::text,updated_at::text FROM purchase_execution_supplier_quotes
		WHERE tenant_id=$1 AND requirement_id=$2 ORDER BY updated_at DESC,id DESC`, tenantID, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExecutionSupplierQuote{}
	for rows.Next() {
		var q ExecutionSupplierQuote
		if err := rows.Scan(&q.ID, &q.RequirementID, &q.SupplierID, &q.SupplierCode, &q.SupplierName, &q.Currency, &q.UnitPrice,
			&q.ExpectedDate, &q.PaymentTerms, &q.ValidUntil, &q.Remark, &q.CreatedByID, &q.CreatedByName, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Service) SaveExecutionSupplierQuote(ctx context.Context, tenantID int64, in SaveExecutionSupplierQuoteInput, op Operator) (ExecutionSupplierQuote, error) {
	if in.RequirementID == 0 {
		return ExecutionSupplierQuote{}, apierr.Invalid("EXECUTION_QUOTE_REQUIREMENT_REQUIRED", "请选择采购产品")
	}
	if _, err := s.GetRequirement(ctx, tenantID, in.RequirementID); err != nil {
		return ExecutionSupplierQuote{}, err
	}
	var requirementStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM purchase_requirements WHERE tenant_id=$1 AND id=$2`, tenantID, in.RequirementID).Scan(&requirementStatus); err != nil {
		return ExecutionSupplierQuote{}, err
	}
	if requirementStatus != "WAITING_REQUOTE" {
		return ExecutionSupplierQuote{}, apierr.Conflict("EXECUTION_QUOTE_REQUIREMENT_STATE", "只有待重新询价的产品可以保存实单报价")
	}
	supplier, err := s.supplierForOrder(ctx, in.SupplierID)
	if err != nil {
		return ExecutionSupplierQuote{}, err
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = supplier.Currency
	}
	price, err := decimal.NewFromString(in.UnitPrice)
	if err != nil || price.LessThanOrEqual(decimal.Zero) {
		return ExecutionSupplierQuote{}, apierr.Invalid("EXECUTION_QUOTE_PRICE_INVALID", "工厂报价单价必须大于 0")
	}
	for _, item := range []struct{ value, code, label string }{{in.ExpectedDate, "EXECUTION_QUOTE_EXPECTED_DATE_INVALID", "预计交货日期"}, {in.ValidUntil, "EXECUTION_QUOTE_VALID_UNTIL_INVALID", "报价有效期"}} {
		if item.value != "" {
			if _, err := time.Parse("2006-01-02", item.value); err != nil {
				return ExecutionSupplierQuote{}, apierr.Invalid(item.code, item.label+"无效")
			}
		}
	}
	if strings.TrimSpace(in.PaymentTerms) == "" {
		return ExecutionSupplierQuote{}, apierr.Invalid("EXECUTION_QUOTE_PAYMENT_TERMS_REQUIRED", "请填写付款条件")
	}
	var id int64
	if in.ID == 0 {
		err = s.pool.QueryRow(ctx, `INSERT INTO purchase_execution_supplier_quotes
			(tenant_id,requirement_id,supplier_id,supplier_code,supplier_name,currency,unit_price,expected_date,payment_terms,valid_until,remark,created_by_id,created_by_name)
			VALUES($1,$2,$3,$4,$5,$6,$7::numeric,nullif($8,'')::date,$9,nullif($10,'')::date,$11,$12,$13) RETURNING id`,
			tenantID, in.RequirementID, supplier.ID, supplier.Code, supplier.Name, in.Currency, price.String(), in.ExpectedDate, strings.TrimSpace(in.PaymentTerms), in.ValidUntil, strings.TrimSpace(in.Remark), op.ID, op.Name).Scan(&id)
	} else {
		command, updateErr := s.pool.Exec(ctx, `UPDATE purchase_execution_supplier_quotes SET supplier_id=$4,supplier_code=$5,supplier_name=$6,currency=$7,unit_price=$8::numeric,
			expected_date=nullif($9,'')::date,payment_terms=$10,valid_until=nullif($11,'')::date,remark=$12,updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND requirement_id=$3`, tenantID, in.ID, in.RequirementID, supplier.ID, supplier.Code, supplier.Name, in.Currency, price.String(), in.ExpectedDate, strings.TrimSpace(in.PaymentTerms), in.ValidUntil, strings.TrimSpace(in.Remark))
		if updateErr != nil {
			return ExecutionSupplierQuote{}, updateErr
		}
		if command.RowsAffected() == 0 {
			return ExecutionSupplierQuote{}, apierr.NotFound("EXECUTION_QUOTE_NOT_FOUND", "实单报价不存在")
		}
		id = in.ID
	}
	if err != nil {
		return ExecutionSupplierQuote{}, err
	}
	s.nudge(ctx, tenantID)
	return s.executionSupplierQuoteByID(ctx, tenantID, id)
}

func (s *Service) DeleteExecutionSupplierQuote(ctx context.Context, tenantID, requirementID, quoteID int64) error {
	command, err := s.pool.Exec(ctx, `DELETE FROM purchase_execution_supplier_quotes WHERE tenant_id=$1 AND id=$2 AND requirement_id=$3`, tenantID, quoteID, requirementID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return apierr.NotFound("EXECUTION_QUOTE_NOT_FOUND", "实单报价不存在")
	}
	s.nudge(ctx, tenantID)
	return nil
}

func (s *Service) executionSupplierQuoteByID(ctx context.Context, tenantID, id int64) (ExecutionSupplierQuote, error) {
	var q ExecutionSupplierQuote
	err := s.pool.QueryRow(ctx, `SELECT id,requirement_id,supplier_id,supplier_code,supplier_name,currency,unit_price::text,
		coalesce(expected_date::text,''),payment_terms,coalesce(valid_until::text,''),remark,created_by_id,created_by_name,created_at::text,updated_at::text
		FROM purchase_execution_supplier_quotes WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&q.ID, &q.RequirementID, &q.SupplierID, &q.SupplierCode, &q.SupplierName, &q.Currency, &q.UnitPrice, &q.ExpectedDate, &q.PaymentTerms, &q.ValidUntil, &q.Remark, &q.CreatedByID, &q.CreatedByName, &q.CreatedAt, &q.UpdatedAt)
	if err == pgx.ErrNoRows {
		return q, apierr.NotFound("EXECUTION_QUOTE_NOT_FOUND", "实单报价不存在")
	}
	return q, err
}
