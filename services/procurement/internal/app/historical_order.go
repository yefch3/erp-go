package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/shopspring/decimal"
)

type HistoricalOrderLine struct {
	ProductID, UomID                       int64
	ProductCode                            string
	ID                                     int64
	ProductName, Spec, UOM, Qty, UnitPrice string
}
type HistoricalOrderInput struct {
	PayableDueDate                                                                                 string
	ID, SupplierID                                                                                 int64
	PONo, OriginalDate, Currency, ExpectedDate, ContactName, ContactPhone, Remark, DeliveryAddress string
	Confirm                                                                                        bool
	Lines                                                                                          []HistoricalOrderLine
}
type HistoricalOrderMeta struct {
	Historical, PricesComplete                                      bool
	OriginalDate, ContactName, ContactPhone, RecordedBy, RecordedAt string
}
type HistoricalOrderResult struct {
	ID           int64
	PONo, Status string
}

func (s *Service) HistoricalOrderMeta(ctx context.Context, tenantID, id int64) (HistoricalOrderMeta, error) {
	var m HistoricalOrderMeta
	err := s.pool.QueryRow(ctx, `SELECT original_date::text,contact_name,contact_phone,prices_complete,recorded_by_name,recorded_at::text FROM historical_purchase_orders WHERE tenant_id=$1 AND po_id=$2`, tenantID, id).Scan(&m.OriginalDate, &m.ContactName, &m.ContactPhone, &m.PricesComplete, &m.RecordedBy, &m.RecordedAt)
	if err == pgx.ErrNoRows {
		return m, nil
	}
	m.Historical = err == nil
	return m, err
}

// SaveHistoricalOrder writes into the normal order/line tables, but never emits
// approval, inventory, payment or new-demand events. Closed supporting manual
// requirements satisfy the existing execution FK without entering the buy queue.
func (s *Service) SaveHistoricalOrder(ctx context.Context, tenantID int64, in HistoricalOrderInput, op Operator) (HistoricalOrderResult, error) {
	var out HistoricalOrderResult
	if tenantID <= 0 || op.ID <= 0 {
		return out, apierr.Permission("PO_OPERATOR_REQUIRED", "请重新登录")
	}
	if in.ID != 0 {
		if err := s.AuthorizeOrder(ctx, tenantID, in.ID, op); err != nil {
			return out, err
		}
	}
	supplier, err := s.supplierForOrder(ctx, in.SupplierID)
	if err != nil {
		return out, err
	}
	in.PONo = strings.TrimSpace(in.PONo)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if len(in.Currency) != 3 || in.Currency[0] < 'A' || in.Currency[0] > 'Z' || in.Currency[1] < 'A' || in.Currency[1] > 'Z' || in.Currency[2] < 'A' || in.Currency[2] > 'Z' {
		return out, apierr.Invalid("PO_CURRENCY_INVALID", "币种应为三个英文字母")
	}
	if err = validBusinessDate(in.PayableDueDate, "PO_DUE_DATE_INVALID", "应付到期日"); err != nil {
		return out, err
	}
	if in.OriginalDate == "" {
		return out, apierr.Invalid("PO_DATE_REQUIRED", "请填写原下单日期")
	}
	if err = validBusinessDate(in.OriginalDate, "PO_DATE_INVALID", "原下单日期"); err != nil {
		return out, err
	}
	if err = validBusinessDate(in.ExpectedDate, "PO_DATE_INVALID", "要求到货日期"); err != nil {
		return out, err
	}
	if utf8.RuneCountInString(in.PONo) > 50 || utf8.RuneCountInString(in.ContactName) > 100 || utf8.RuneCountInString(in.ContactPhone) > 64 {
		return out, apierr.Invalid("PO_TEXT_TOO_LONG", "单号或联系人资料过长")
	}
	if len(in.Lines) == 0 || len(in.Lines) > 500 {
		return out, apierr.Invalid("PO_LINES_REQUIRED", "请填写 1 至 500 条产品明细")
	}
	total := decimal.Zero
	complete := true
	for i := range in.Lines {
		l := &in.Lines[i]
		l.ProductName = strings.TrimSpace(l.ProductName)
		l.UOM = strings.TrimSpace(l.UOM)
		l.UnitPrice = strings.TrimSpace(l.UnitPrice)
		qty, e := decimal.NewFromString(l.Qty)
		if e != nil || !qty.IsPositive() || qty.Exponent() < -4 || qty.GreaterThanOrEqual(decimal.New(1, 14)) {
			return out, apierr.Invalid("PO_QTY_INVALID", fmt.Sprintf("第 %d 行数量须大于零，最多四位小数", i+1))
		}
		l.Qty = qty.String()
		if l.ProductName == "" || l.UOM == "" || utf8.RuneCountInString(l.ProductName) > 200 || utf8.RuneCountInString(l.Spec) > 300 || utf8.RuneCountInString(l.UOM) > 32 {
			return out, apierr.Invalid("PO_PRODUCT_REQUIRED", fmt.Sprintf("第 %d 行请填写产品名称和单位，并检查字段长度", i+1))
		}
		if l.UnitPrice == "" {
			complete = false
			continue
		}
		price, e := decimal.NewFromString(l.UnitPrice)
		if e != nil || price.IsNegative() || price.Exponent() < -4 || price.GreaterThanOrEqual(decimal.New(1, 14)) {
			return out, apierr.Invalid("PO_PRICE_INVALID", "单价须为非负数，最多四位小数")
		}
		total = total.Add(qty.Mul(price))
		if total.GreaterThanOrEqual(decimal.New(1, 16)) {
			return out, apierr.Invalid("PO_AMOUNT_INVALID", "采购金额过大")
		}
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		status := "DRAFT"
		oldStatus := "DRAFT"
		if in.ID != 0 {
			var historical bool
			e := tx.QueryRow(ctx, `SELECT o.status,EXISTS(SELECT 1 FROM historical_purchase_orders h WHERE h.tenant_id=o.tenant_id AND h.po_id=o.id) FROM purchase_orders o WHERE o.tenant_id=$1 AND o.id=$2 FOR UPDATE`, tenantID, in.ID).Scan(&oldStatus, &historical)
			if e != nil {
				return e
			}
			if !historical || (oldStatus != "DRAFT" && oldStatus != "ORDERED") {
				return apierr.Conflict("PO_HISTORY_LOCKED", "仅草稿或尚未收货的历史补录单可补齐资料")
			}
		} else {
			if in.PONo == "" {
				if s.numbering == nil {
					return apierr.Internal("PO_NUMBER_UNAVAILABLE", "编号服务不可用")
				}
				var e error
				in.PONo, e = s.numbering.Next(ctx, "PURCHASE_ORDER")
				if e != nil {
					return e
				}
			}
		}
		if oldStatus == "ORDERED" {
			status = "ORDERED"
			var oldSupplier int64
			var oldCurrency string
			var paid bool
			if e := tx.QueryRow(ctx, `SELECT supplier_id,currency,payment_requested_at IS NOT NULL FROM purchase_orders WHERE tenant_id=$1 AND id=$2`, tenantID, in.ID).Scan(&oldSupplier, &oldCurrency, &paid); e != nil {
				return e
			}
			if oldSupplier != in.SupplierID || oldCurrency != in.Currency || paid {
				return apierr.Conflict("PO_HISTORY_LOCKED", "已确认单不能更换供应商、币种，申请付款后不能修改")
			}
			var count int
			if e := tx.QueryRow(ctx, `SELECT count(*) FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2`, tenantID, in.ID).Scan(&count); e != nil {
				return e
			}
			if count != len(in.Lines) {
				return apierr.Conflict("PO_HISTORY_LINES_LOCKED", "已确认单不能增删产品")
			}
			seen := map[int64]bool{}
			for _, l := range in.Lines {
				var name, spec, uom, qty string
				var productID int64
				if seen[l.ID] {
					return apierr.Invalid("PO_LINE_DUPLICATE", "产品行重复")
				}
				seen[l.ID] = true
				if e := tx.QueryRow(ctx, `SELECT product_name,spec,uom_code,qty::text,product_id FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2 AND id=$3`, tenantID, in.ID, l.ID).Scan(&name, &spec, &uom, &qty, &productID); e != nil {
					return apierr.Invalid("PO_LINE_INVALID", "产品行不属于当前订单")
				}
				if productID != l.ProductID || name != l.ProductName || spec != l.Spec || uom != l.UOM || !decimal.RequireFromString(qty).Equal(decimal.RequireFromString(l.Qty)) {
					return apierr.Conflict("PO_HISTORY_LINES_LOCKED", "已确认单只能补齐资料与价格，不能改变产品或数量")
				}
			}
		}
		if in.Confirm {
			status = "ORDERED"
		}
		if in.ID == 0 {
			e := tx.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,buyer_id,buyer_name,remark,status,fulfillment_mode,delivery_location_type,delivery_address,ordered_at,payable_due_date) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,$9,$10,$11,$12::text,'DIRECT_SHIP','CUSTOM',$13,CASE WHEN $12::text='ORDERED' THEN $14::date::timestamptz ELSE NULL END,NULLIF($15,'')::date) RETURNING id`, tenantID, in.PONo, supplier.ID, supplier.Code, supplier.Name, in.Currency, total.StringFixed(2), in.ExpectedDate, op.ID, op.Name, in.Remark, status, in.DeliveryAddress, in.OriginalDate, in.PayableDueDate).Scan(&in.ID)
			if e != nil {
				return e
			}
		} else {
			_, e := tx.Exec(ctx, `UPDATE purchase_orders SET supplier_id=$3,supplier_code=$4,supplier_name=$5,currency=$6,total_amount=$7,expected_date=NULLIF($8,'')::date,remark=$9,status=$10::text,delivery_address=COALESCE(NULLIF($11,''),delivery_address),ordered_at=CASE WHEN $10::text='ORDERED' THEN $12::date::timestamptz ELSE NULL END,payable_due_date=NULLIF($13,'')::date,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, in.ID, supplier.ID, supplier.Code, supplier.Name, in.Currency, total.StringFixed(2), in.ExpectedDate, in.Remark, status, in.DeliveryAddress, in.OriginalDate, in.PayableDueDate)
			if e != nil {
				return e
			}
			if oldStatus == "DRAFT" { // Delete only private supporting requirements after their draft items.
				var reqIDs []int64
				rows, e := tx.Query(ctx, `SELECT requirement_id FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2`, tenantID, in.ID)
				if e != nil {
					return e
				}
				for rows.Next() {
					var id int64
					if e = rows.Scan(&id); e != nil {
						rows.Close()
						return e
					}
					reqIDs = append(reqIDs, id)
				}
				e = rows.Err()
				rows.Close()
				if e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2`, tenantID, in.ID); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1 AND id=ANY($2) AND source='MANUAL' AND status='SUPERSEDED'`, tenantID, reqIDs); e != nil {
					return e
				}
			}
		}
		for _, l := range in.Lines {
			if oldStatus == "ORDERED" {
				price := l.UnitPrice
				if price == "" {
					price = "0"
				}
				amount := decimal.RequireFromString(l.Qty).Mul(decimal.RequireFromString(price)).StringFixed(2)
				if _, e := tx.Exec(ctx, `UPDATE purchase_order_items SET unit_price=$4,amount=$5,price_missing=$6 WHERE tenant_id=$1 AND po_id=$2 AND id=$3`, tenantID, in.ID, l.ID, price, amount, l.UnitPrice == ""); e != nil {
					return e
				}
				continue
			}
			var reqID int64
			e := tx.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_name,spec,uom_code,required_qty,ordered_qty,source,status,owner_id,closed_reason,product_code,uom_id) VALUES($1,0,'',0,-nextval('purchase_requirements_id_seq'),$7,$2,$3,$4,$5,$5,'MANUAL','SUPERSEDED',$6,'历史采购单执行明细，不进入待采购需求',$8,$9) RETURNING id`, tenantID, l.ProductName, l.Spec, l.UOM, l.Qty, op.ID, l.ProductID, l.ProductCode, l.UomID).Scan(&reqID)
			if e != nil {
				return e
			}
			price := l.UnitPrice
			if price == "" {
				price = "0"
			}
			amount := decimal.RequireFromString(l.Qty).Mul(decimal.RequireFromString(price)).StringFixed(2)
			_, e = tx.Exec(ctx, `INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_name,spec,uom_code,qty,unit_price,amount,price_missing,product_code,uom_id) VALUES($1,$2,$3,$11,$4,$5,$6,$7,$8,$9,$10,$12,$13)`, tenantID, in.ID, reqID, l.ProductName, l.Spec, l.UOM, l.Qty, price, amount, l.UnitPrice == "", l.ProductID, l.ProductCode, l.UomID)
			if e != nil {
				return e
			}
		}
		_, e := tx.Exec(ctx, `INSERT INTO historical_purchase_orders(tenant_id,po_id,original_date,contact_name,contact_phone,prices_complete,recorded_by_id,recorded_by_name) VALUES($1,$2,$3::date,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,po_id) DO UPDATE SET original_date=excluded.original_date,contact_name=excluded.contact_name,contact_phone=excluded.contact_phone,prices_complete=excluded.prices_complete`, tenantID, in.ID, in.OriginalDate, in.ContactName, in.ContactPhone, complete, op.ID, op.Name)
		if e != nil {
			return e
		}
		out.ID = in.ID
		out.Status = status
		return tx.QueryRow(ctx, `SELECT po_no FROM purchase_orders WHERE tenant_id=$1 AND id=$2`, tenantID, in.ID).Scan(&out.PONo)
	})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return out, apierr.Conflict("PO_NUMBER_DUPLICATE", "当前公司已存在该采购单号，请勿重复补录")
	}
	if err == nil {
		s.nudge(ctx, tenantID)
	}
	return out, err
}

func (s *Service) RequireHistoricalPrices(ctx context.Context, tenantID, id int64) error {
	m, err := s.HistoricalOrderMeta(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if m.Historical && !m.PricesComplete {
		return apierr.Conflict("PO_PRICE_MISSING", "历史采购单价格尚未补齐，不能登记到货或付款")
	}
	return nil
}

func (s *Service) HistoricalPriceMissing(ctx context.Context, tenantID, id int64) (bool, error) {
	var b bool
	e := s.pool.QueryRow(ctx, `SELECT price_missing FROM purchase_order_items WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&b)
	return b, e
}
