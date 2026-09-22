package app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type QualitySourceLine struct{ ProductName, Spec, UOM, Qty string }
type QualitySourceOrder struct {
	ID, ExistingTaskID int64
	PONo, SupplierName string
	Lines              []QualitySourceLine
}

// The same quality data scope as the task queue, without exposing purchase prices.
func (s *Service) ListQualitySourceOrders(ctx context.Context, tenantID, id int64, keyword string, page, size int32, op Operator) ([]QualitySourceOrder, int64, error) {
	visible, err := s.visibleQualityTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	ids := visible.EmployeeIDs
	if ids == nil {
		ids = []int64{}
	}
	page, size = normalizePage(page, size)
	rows, err := s.pool.Query(ctx, `SELECT o.id,o.po_no,o.supplier_name,
 COALESCE((SELECT t.id FROM quality_inspection_tasks t WHERE t.tenant_id=o.tenant_id AND t.po_id=o.id ORDER BY t.id LIMIT 1),0),COUNT(*) OVER()
 FROM purchase_orders o WHERE o.tenant_id=$1 AND ($2=0 OR o.id=$2)
 AND o.business_type='PROCUREMENT'
 AND (o.status IN ('ORDERED','PARTIALLY_RECEIVED') OR EXISTS(SELECT 1 FROM quality_inspection_tasks t WHERE t.tenant_id=o.tenant_id AND t.po_id=o.id))
 AND ($3 OR o.buyer_id=ANY($4) OR EXISTS(SELECT 1 FROM purchase_order_items oi
 JOIN purchase_requirements pr ON pr.id=oi.requirement_id AND pr.tenant_id=oi.tenant_id
 WHERE oi.po_id=o.id AND oi.tenant_id=o.tenant_id AND pr.owner_id=ANY($4)))
 AND ($5='' OR o.po_no ILIKE '%'||$5||'%' OR o.supplier_name ILIKE '%'||$5||'%')
 ORDER BY o.id DESC LIMIT $6 OFFSET $7`, tenantID, id, visible.All, ids, strings.TrimSpace(keyword), size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	orders := []QualitySourceOrder{}
	var total int64
	for rows.Next() {
		var o QualitySourceOrder
		if err = rows.Scan(&o.ID, &o.PONo, &o.SupplierName, &o.ExistingTaskID, &total); err != nil {
			rows.Close()
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	// Product details are fetched only after selecting one order.
	if id != 0 {
		if len(orders) == 0 {
			return nil, 0, apierr.NotFound("QUALITY_SOURCE_NOT_FOUND", "采购单不存在或不在可质检范围内")
		}
		lines, e := s.pool.Query(ctx, `SELECT product_name,spec,uom_code,qty::text FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2 ORDER BY id`, tenantID, id)
		if e != nil {
			return nil, 0, e
		}
		defer lines.Close()
		for lines.Next() {
			var l QualitySourceLine
			if e = lines.Scan(&l.ProductName, &l.Spec, &l.UOM, &l.Qty); e != nil {
				return nil, 0, e
			}
			orders[0].Lines = append(orders[0].Lines, l)
		}
		if e = lines.Err(); e != nil {
			return nil, 0, e
		}
	}
	return orders, total, nil
}

func (s *Service) CreateQualityInspection(ctx context.Context, tenantID int64, in ApplyQualityInput, op Operator) (QualityTask, error) {
	if in.POID <= 0 {
		return QualityTask{}, apierr.Invalid("QUALITY_PO_REQUIRED", "请选择采购单")
	}
	if utf8.RuneCountInString(in.ContactName) > 100 || utf8.RuneCountInString(in.ContactPhone) > 64 {
		return QualityTask{}, apierr.Invalid("QUALITY_CONTACT_TOO_LONG", "联系人最多 100 字，联系电话最多 64 字")
	}
	if in.ExpectedDate != "" {
		if _, err := time.Parse("2006-01-02", in.ExpectedDate); err != nil {
			return QualityTask{}, apierr.Invalid("QUALITY_DATE_INVALID", "预计质检日期格式无效")
		}
	}
	orders, _, err := s.ListQualitySourceOrders(ctx, tenantID, in.POID, "", 1, 1, op)
	if err != nil {
		return QualityTask{}, err
	}
	if orders[0].ExistingTaskID != 0 {
		return s.GetQualityTask(ctx, tenantID, orders[0].ExistingTaskID)
	}
	return s.ApplyQualityInspection(ctx, tenantID, in, op)
}
