package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// 质检记录（A3）。收货—质检—入库的关系：收货即入码头库（库存事件在收货
// 时已发出，这里不再碰库存），质检挂在收货单上事后登记。PASS 生而
// RESOLVED——没有要处置的东西；FAIL 生而 OPEN，等一个处置决定（退货/
// 扣款/让步接收/返工）。未 RESOLVED 的质检和未 RESOLVED 的到货异常一起，
// 挡住结案的门。

type PurchaseInspection struct {
	ID, ReceiptID, POItemID                    int64
	ReceiptNo, Result                          string
	InspectedQty, DefectQty, Note              string
	Attachments                                []ProductionAttachment
	Status, Disposition, DispositionNote       string
	InspectedBy, InspectedAt                   string
	ResolvedBy, ResolvedAt                     string
}

var inspectionDispositions = map[string]bool{"RETURN": true, "DEDUCTION": true, "CONCESSION": true, "REWORK": true}

func (s *Service) RecordInspection(ctx context.Context, tenantID, poID int64, in PurchaseInspection, op Operator) (PurchaseInspection, error) {
	if in.Result != "PASS" && in.Result != "FAIL" {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_RESULT_INVALID", "质检结果只能是合格或不合格")
	}
	inspected, err := decimal.NewFromString(orZero(in.InspectedQty))
	if err != nil || inspected.IsNegative() {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_QTY_INVALID", "抽检数量不能为负数")
	}
	defect, err := decimal.NewFromString(orZero(in.DefectQty))
	if err != nil || defect.IsNegative() {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_DEFECT_QTY_INVALID", "不合格数量不能为负数")
	}
	// 不合格必须有数量或说明——一条什么都没说的 FAIL 既不能驱动扣款，
	// 也不能作为纠纷证据，只会永远挡着结案。
	if in.Result == "FAIL" && defect.IsZero() && strings.TrimSpace(in.Note) == "" {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_DETAIL_REQUIRED", "不合格需填写不合格数量或质检说明")
	}
	if in.Result == "PASS" && !defect.IsZero() {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_PASS_DEFECT", "合格的质检不能带不合格数量")
	}
	if _, err = s.GetOrder(ctx, tenantID, poID); err != nil {
		return PurchaseInspection{}, err
	}
	// 质检必须指向本单的一张收货单：没有到货就没有可检的东西。
	if in.ReceiptID == 0 {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_RECEIPT_REQUIRED", "请选择质检对应的收货单")
	}
	var exists bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_receipts WHERE tenant_id=$1 AND po_id=$2 AND id=$3)`, tenantID, poID, in.ReceiptID).Scan(&exists); err != nil || !exists {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_RECEIPT_INVALID", "收货记录不属于采购单")
	}
	if in.POItemID != 0 {
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2 AND id=$3)`, tenantID, poID, in.POItemID).Scan(&exists); err != nil || !exists {
			return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_ITEM_INVALID", "采购明细不属于采购单")
		}
	}
	status := "RESOLVED"
	if in.Result == "FAIL" {
		status = "OPEN"
	}
	attachments, _ := json.Marshal(in.Attachments)
	var id int64
	err = s.pool.QueryRow(ctx, `INSERT INTO purchase_inspections (tenant_id,po_id,receipt_id,po_item_id,result,inspected_qty,defect_qty,note,attachments,status,inspected_by_id,inspected_by_name) VALUES ($1,$2,$3,nullif($4,0),$5,$6::numeric,$7::numeric,$8,$9,$10,$11,$12) RETURNING id`,
		tenantID, poID, in.ReceiptID, in.POItemID, in.Result, inspected.String(), defect.String(), strings.TrimSpace(in.Note), attachments, status, op.ID, op.Name).Scan(&id)
	if err != nil {
		return PurchaseInspection{}, err
	}
	s.nudge(ctx, tenantID)
	return s.inspectionByID(ctx, tenantID, poID, id)
}

func (s *Service) ResolveInspection(ctx context.Context, tenantID, poID, inspectionID int64, disposition, note string, op Operator) (PurchaseInspection, error) {
	if !inspectionDispositions[disposition] {
		return PurchaseInspection{}, apierr.Invalid("PO_INSPECTION_DISPOSITION_INVALID", "处置方式无效")
	}
	command, err := s.pool.Exec(ctx, `UPDATE purchase_inspections SET status='RESOLVED',disposition=$4,disposition_note=$5,resolved_by_id=$6,resolved_by_name=$7,resolved_at=now() WHERE tenant_id=$1 AND po_id=$2 AND id=$3 AND status='OPEN'`,
		tenantID, poID, inspectionID, disposition, strings.TrimSpace(note), op.ID, op.Name)
	if err != nil {
		return PurchaseInspection{}, err
	}
	if command.RowsAffected() == 0 {
		return PurchaseInspection{}, apierr.Conflict("PO_INSPECTION_NOT_OPEN", "质检记录不存在或已经处置")
	}
	s.nudge(ctx, tenantID)
	return s.inspectionByID(ctx, tenantID, poID, inspectionID)
}

const inspectionColumns = `i.id,i.receipt_id,coalesce(r.receipt_no,''),coalesce(i.po_item_id,0),i.result,i.inspected_qty::text,i.defect_qty::text,i.note,i.attachments,i.status,i.disposition,i.disposition_note,i.inspected_by_name,i.inspected_at::text,i.resolved_by_name,coalesce(i.resolved_at::text,'')`

func scanInspection(row pgx.Row) (PurchaseInspection, error) {
	var out PurchaseInspection
	var raw []byte
	err := row.Scan(&out.ID, &out.ReceiptID, &out.ReceiptNo, &out.POItemID, &out.Result, &out.InspectedQty, &out.DefectQty, &out.Note, &raw, &out.Status, &out.Disposition, &out.DispositionNote, &out.InspectedBy, &out.InspectedAt, &out.ResolvedBy, &out.ResolvedAt)
	if err != nil {
		return out, err
	}
	_ = json.Unmarshal(raw, &out.Attachments)
	return out, nil
}

func (s *Service) inspectionByID(ctx context.Context, tenantID, poID, id int64) (PurchaseInspection, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+inspectionColumns+` FROM purchase_inspections i LEFT JOIN purchase_receipts r ON r.id=i.receipt_id WHERE i.tenant_id=$1 AND i.po_id=$2 AND i.id=$3`, tenantID, poID, id)
	out, err := scanInspection(row)
	if err == pgx.ErrNoRows {
		return PurchaseInspection{}, apierr.NotFound("PO_INSPECTION_NOT_FOUND", "质检记录不存在")
	}
	return out, err
}

// CloseOrder 宣布一张采购单到此为止。三道闸：全部收满（RECEIVED）、
// 没有未解决的到货异常、没有未解决的质检。拒绝时把数量带在 meta 里，
// 前端能告诉人差在哪，而不是一句冷冰冰的「不行」。
func (s *Service) CloseOrder(ctx context.Context, tenantID, poID int64, op Operator) error {
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.GetPurchaseOrderForUpdate(ctx, store.GetPurchaseOrderForUpdateParams{
			TenantID: tenantID, ID: poID,
		}); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
			}
			return err
		}
		// 状态与 closed_at 都在锁下重读：ForUpdate 查询不带这两列，
		// 单独取一次比为一个调用点改生成代码便宜。
		var status string
		var closed bool
		if err := tx.QueryRow(ctx, `SELECT status, closed_at IS NOT NULL FROM purchase_orders WHERE tenant_id=$1 AND id=$2`, tenantID, poID).Scan(&status, &closed); err != nil {
			return err
		}
		if closed {
			return apierr.Conflict("PO_ALREADY_CLOSED", "采购单已经结案")
		}
		if status != poReceived {
			return apierr.Conflict("PO_NOT_CLOSABLE", "全部收货完成后才能结案").WithMeta("status", status)
		}
		var openExceptions, openInspections int64
		if err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM purchase_receipt_exceptions WHERE tenant_id=$1 AND po_id=$2 AND status='OPEN'),
			(SELECT count(*) FROM purchase_inspections WHERE tenant_id=$1 AND po_id=$2 AND status='OPEN')`,
			tenantID, poID).Scan(&openExceptions, &openInspections); err != nil {
			return err
		}
		if openExceptions > 0 || openInspections > 0 {
			return apierr.Conflict("PO_OPEN_ISSUES", "还有未解决的到货异常或质检，处理完才能结案").
				WithMeta("openExceptions", strconv.FormatInt(openExceptions, 10)).
				WithMeta("openInspections", strconv.FormatInt(openInspections, 10))
		}
		_, err := tx.Exec(ctx, `UPDATE purchase_orders SET closed_at=now(),closed_by_id=$3,closed_by_name=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, poID, op.ID, op.Name)
		return err
	})
	if err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	return nil
}
