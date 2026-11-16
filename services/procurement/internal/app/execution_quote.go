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
	Selected                                        bool
	SelectedByID                                    int64
	SelectedByName, SelectedAt                      string
	QuoteCategory, Incoterm, CalculatedUnitPrice    string
	CalculationInput, CalculatedAt                  string
	CalculatedByID                                  int64
	CalculatedByName                                string
}

type SaveExecutionSupplierQuoteInput struct {
	ID, RequirementID, SupplierID                                       int64
	Currency, UnitPrice, ExpectedDate, PaymentTerms, ValidUntil, Remark string
	QuoteCategory, Incoterm, CalculatedUnitPrice, CalculationInput      string
}

func (s *Service) ListExecutionSupplierQuotes(ctx context.Context, tenantID, requirementID int64) ([]ExecutionSupplierQuote, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,requirement_id,supplier_id,supplier_code,supplier_name,currency,unit_price::text,
		coalesce(expected_date::text,''),payment_terms,coalesce(valid_until::text,''),remark,created_by_id,created_by_name,
		created_at::text,updated_at::text,selected,selected_by_id,selected_by_name,coalesce(selected_at::text,''),quote_category,incoterm,
		coalesce(calculated_unit_price::text,''),calculation_input::text,coalesce(calculated_at::text,''),calculated_by_id,calculated_by_name FROM purchase_execution_supplier_quotes
		WHERE tenant_id=$1 AND requirement_id=$2 ORDER BY updated_at DESC,id DESC`, tenantID, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExecutionSupplierQuote{}
	for rows.Next() {
		var q ExecutionSupplierQuote
		if err := rows.Scan(&q.ID, &q.RequirementID, &q.SupplierID, &q.SupplierCode, &q.SupplierName, &q.Currency, &q.UnitPrice,
			&q.ExpectedDate, &q.PaymentTerms, &q.ValidUntil, &q.Remark, &q.CreatedByID, &q.CreatedByName, &q.CreatedAt, &q.UpdatedAt,
			&q.Selected, &q.SelectedByID, &q.SelectedByName, &q.SelectedAt, &q.QuoteCategory, &q.Incoterm, &q.CalculatedUnitPrice, &q.CalculationInput, &q.CalculatedAt, &q.CalculatedByID, &q.CalculatedByName); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Service) SelectExecutionSupplierQuote(ctx context.Context, tenantID, requirementID, quoteID int64, op Operator) (ExecutionSupplierQuote, error) {
	if requirementID == 0 || quoteID == 0 {
		return ExecutionSupplierQuote{}, apierr.Invalid("EXECUTION_QUOTE_SELECTION_REQUIRED", "请选择最终工厂报价")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ExecutionSupplierQuote{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM purchase_requirements WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, requirementID).Scan(&status); err == pgx.ErrNoRows {
		return ExecutionSupplierQuote{}, apierr.NotFound("REQUIREMENT_NOT_FOUND", "采购需求不存在")
	} else if err != nil {
		return ExecutionSupplierQuote{}, err
	}
	if status != "WAITING_REQUOTE" {
		return ExecutionSupplierQuote{}, apierr.Conflict("EXECUTION_QUOTE_REQUIREMENT_STATE", "当前采购需求不能再选择实单报价")
	}
	var alreadySelected bool
	if err := tx.QueryRow(ctx, `SELECT selected FROM purchase_execution_supplier_quotes WHERE tenant_id=$1 AND requirement_id=$2 AND id=$3 FOR UPDATE`, tenantID, requirementID, quoteID).Scan(&alreadySelected); err == pgx.ErrNoRows {
		return ExecutionSupplierQuote{}, apierr.NotFound("EXECUTION_QUOTE_NOT_FOUND", "实单报价不存在")
	} else if err != nil {
		return ExecutionSupplierQuote{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE purchase_execution_supplier_quotes SET selected=FALSE,selected_by_id=0,selected_by_name='',selected_at=NULL WHERE tenant_id=$1 AND requirement_id=$2 AND selected`, tenantID, requirementID); err != nil {
		return ExecutionSupplierQuote{}, err
	}
	// Clicking the current choice again clears it. This lets a purchaser correct
	// an accidental choice without selecting a different supplier first.
	if !alreadySelected {
		if _, err := tx.Exec(ctx, `UPDATE purchase_execution_supplier_quotes SET selected=TRUE,selected_by_id=$4,selected_by_name=$5,selected_at=now(),updated_at=now() WHERE tenant_id=$1 AND requirement_id=$2 AND id=$3`, tenantID, requirementID, quoteID, op.ID, op.Name); err != nil {
			return ExecutionSupplierQuote{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ExecutionSupplierQuote{}, err
	}
	s.nudge(ctx, tenantID)
	return s.executionSupplierQuoteByID(ctx, tenantID, quoteID)
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
		in.Currency = strings.ToUpper(strings.TrimSpace(supplier.Currency))
	}
	if !validExecutionQuoteCurrency(in.Currency) {
		return ExecutionSupplierQuote{}, apierr.Invalid("EXECUTION_QUOTE_CURRENCY_INVALID", "币种请输入三位字母代码")
	}
	// Execution purchases use the factory's original price and currency.
	// Keep legacy wire fields but do not apply sales trade-term calculations.
	in.QuoteCategory, in.Incoterm = "", ""
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
			(tenant_id,requirement_id,supplier_id,supplier_code,supplier_name,currency,unit_price,expected_date,payment_terms,valid_until,remark,created_by_id,created_by_name,quote_category,incoterm)
			VALUES($1,$2,$3,$4,$5,$6,$7::numeric,nullif($8,'')::date,$9,nullif($10,'')::date,$11,$12,$13,$14,$15) RETURNING id`,
			tenantID, in.RequirementID, supplier.ID, supplier.Code, supplier.Name, in.Currency, price.String(), in.ExpectedDate, strings.TrimSpace(in.PaymentTerms), in.ValidUntil, strings.TrimSpace(in.Remark), op.ID, op.Name, in.QuoteCategory, strings.TrimSpace(in.Incoterm)).Scan(&id)
	} else {
		command, updateErr := s.pool.Exec(ctx, `UPDATE purchase_execution_supplier_quotes SET supplier_id=$4,supplier_code=$5,supplier_name=$6,currency=$7,unit_price=$8::numeric,
			expected_date=nullif($9,'')::date,payment_terms=$10,valid_until=nullif($11,'')::date,remark=$12,quote_category=$13,incoterm=$14,
			calculated_unit_price=NULL,calculation_input='{}',calculated_at=NULL,calculated_by_id=0,calculated_by_name='',updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND requirement_id=$3`, tenantID, in.ID, in.RequirementID, supplier.ID, supplier.Code, supplier.Name, in.Currency, price.String(), in.ExpectedDate, strings.TrimSpace(in.PaymentTerms), in.ValidUntil, strings.TrimSpace(in.Remark), in.QuoteCategory, strings.TrimSpace(in.Incoterm))
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
	var used bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_order_items WHERE tenant_id=$1 AND execution_quote_id=$2)`, tenantID, quoteID).Scan(&used); err != nil {
		return err
	}
	if used {
		return apierr.Conflict("EXECUTION_QUOTE_IN_USE", "该报价已生成采购订单，需保留作为订单核价记录")
	}
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
		coalesce(expected_date::text,''),payment_terms,coalesce(valid_until::text,''),remark,created_by_id,created_by_name,created_at::text,updated_at::text,
		selected,selected_by_id,selected_by_name,coalesce(selected_at::text,''),quote_category,incoterm,coalesce(calculated_unit_price::text,''),calculation_input::text,coalesce(calculated_at::text,''),calculated_by_id,calculated_by_name
		FROM purchase_execution_supplier_quotes WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&q.ID, &q.RequirementID, &q.SupplierID, &q.SupplierCode, &q.SupplierName, &q.Currency, &q.UnitPrice, &q.ExpectedDate, &q.PaymentTerms, &q.ValidUntil, &q.Remark, &q.CreatedByID, &q.CreatedByName, &q.CreatedAt, &q.UpdatedAt, &q.Selected, &q.SelectedByID, &q.SelectedByName, &q.SelectedAt, &q.QuoteCategory, &q.Incoterm, &q.CalculatedUnitPrice, &q.CalculationInput, &q.CalculatedAt, &q.CalculatedByID, &q.CalculatedByName)
	if err == pgx.ErrNoRows {
		return q, apierr.NotFound("EXECUTION_QUOTE_NOT_FOUND", "实单报价不存在")
	}
	return q, err
}

func validExecutionQuoteCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, letter := range currency {
		if letter < 'A' || letter > 'Z' {
			return false
		}
	}
	return true
}
