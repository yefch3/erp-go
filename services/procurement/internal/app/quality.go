package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type QualityTaskLine struct {
	ID, POItemID                                             int64
	ProductName, Spec, UOM, OrderedQty, RequestedQty         string
	QualifiedQty, UnresolvedQty, FinalResult                 string
	IssueDescription, HandlingSuggestion, ApprovedReleaseQty string
	ReleaseDecidedByName                                     string
	ReleaseDecidedAt                                         *time.Time
}
type QualityRoundLine struct {
	TaskLineID                                                                               int64
	Result, InspectedQty, QualifiedQty, UnqualifiedQty, IssueDescription, HandlingSuggestion string
}
type QualityRound struct {
	ID                              int64
	RoundNo                         int32
	InspectedAt                     time.Time
	Location, InspectorName, Remark string
	Lines                           []QualityRoundLine
}
type QualityFile struct {
	ID, RoundID, TaskLineID                                                 int64
	Category, ObjectKey, FileName, ContentType, UploadedByName, DownloadURL string
	SizeBytes                                                               int64
	Supplemental                                                            bool
	UploadedAt                                                              time.Time
}
type QualityTask struct {
	ID, POID                                                                                          int64
	TaskNo, PONo, SupplierName                                                                        string
	BatchNo                                                                                           int32
	Status, ExpectedDate, Location, ContactName, ContactPhone, Remark, RequestedByName, InspectorName string
	RequestedAt                                                                                       time.Time
	StartedAt, CompletedAt                                                                            *time.Time
	Lines                                                                                             []QualityTaskLine
	Rounds                                                                                            []QualityRound
	Files                                                                                             []QualityFile
}
type ApplyQualityLine struct {
	POItemID int64
	Qty      string
}
type ApplyQualityInput struct {
	POID                                                      int64
	ExpectedDate, Location, ContactName, ContactPhone, Remark string
	Lines                                                     []ApplyQualityLine
}
type SubmitQualityLine struct {
	TaskLineID                                                                               int64
	Result, InspectedQty, QualifiedQty, UnqualifiedQty, IssueDescription, HandlingSuggestion string
}
type SubmitQualityInput struct {
	InspectedAt, Location, Remark string
	Lines                         []SubmitQualityLine
}
type DecideQualityLine struct {
	TaskLineID int64
	Qty        string
}
type RegisterQualityFileInput struct {
	RoundID, TaskLineID                  int64
	Category, Key, FileName, ContentType string
	SizeBytes                            int64
	Supplemental                         bool
}

func (s *Service) visibleQualityTo(ctx context.Context, op Operator) (Visibility, error) {
	if s.scopes == nil {
		return Visibility{All: true, ScopeType: "ALL"}, nil
	}
	return s.scopes.VisibleEmployees(ctx, op.ID, "quality")
}

func (s *Service) AuthorizeQualityTask(ctx context.Context, tenantID, taskID int64, op Operator) error {
	visible, err := s.visibleQualityTo(ctx, op)
	if err != nil {
		return err
	}
	ids := visible.EmployeeIDs
	if ids == nil {
		ids = []int64{}
	}
	var allowed bool
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM quality_inspection_tasks t
		JOIN purchase_orders o ON o.id=t.po_id AND o.tenant_id=t.tenant_id
		WHERE t.tenant_id=$1 AND t.id=$2 AND ($3 OR o.buyer_id=ANY($4)
		 OR EXISTS(SELECT 1 FROM purchase_order_items oi
		 JOIN purchase_requirements pr ON pr.id=oi.requirement_id AND pr.tenant_id=oi.tenant_id
		 WHERE oi.po_id=o.id AND oi.tenant_id=o.tenant_id AND pr.owner_id=ANY($4))))`, tenantID, taskID, visible.All, ids).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.NotFound("QUALITY_TASK_NOT_FOUND", "质检任务不存在")
	}
	return nil
}

func parsePositiveQty(raw string) (decimal.Decimal, error) {
	v, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || !v.GreaterThan(decimal.Zero) {
		return decimal.Zero, apierr.Invalid("QUALITY_QTY_INVALID", "质检数量必须大于 0")
	}
	return v, nil
}

func (s *Service) ApplyQualityInspection(ctx context.Context, tenantID int64, in ApplyQualityInput, op Operator) (QualityTask, error) {
	if len(in.Lines) == 0 {
		return QualityTask{}, apierr.Invalid("QUALITY_LINES_REQUIRED", "请至少选择一个产品")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return QualityTask{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var poNo, supplier, status, businessType string
	err = tx.QueryRow(ctx, `SELECT po_no,supplier_name,status,business_type FROM purchase_orders WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, in.POID).Scan(&poNo, &supplier, &status, &businessType)
	if err == pgx.ErrNoRows {
		return QualityTask{}, apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
	}
	if err != nil {
		return QualityTask{}, err
	}
	if businessType != "PROCUREMENT" {
		return QualityTask{}, apierr.Conflict("QUALITY_PO_TYPE_INVALID", "仅采购订单可以申请工厂出货前质检")
	}
	if status != "ORDERED" && status != "PARTIALLY_RECEIVED" {
		return QualityTask{}, apierr.Conflict("QUALITY_PO_NOT_ORDERED", "采购单尚未进入已下单状态，不能申请质检")
	}
	var batch int32
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(batch_no),0)+1 FROM quality_inspection_tasks WHERE tenant_id=$1 AND po_id=$2`, tenantID, in.POID).Scan(&batch); err != nil {
		return QualityTask{}, err
	}
	taskNo := fmt.Sprintf("QI-%s-%02d", poNo, batch)
	var taskID int64
	err = tx.QueryRow(ctx, `INSERT INTO quality_inspection_tasks(tenant_id,po_id,task_no,batch_no,expected_date,inspection_location,contact_name,contact_phone,remark,requested_by,requested_by_name)
	 VALUES($1,$2,$3,$4,NULLIF($5,'')::date,$6,$7,$8,$9,$10,$11) RETURNING id`, tenantID, in.POID, taskNo, batch, in.ExpectedDate, strings.TrimSpace(in.Location), strings.TrimSpace(in.ContactName), strings.TrimSpace(in.ContactPhone), strings.TrimSpace(in.Remark), op.ID, op.Name).Scan(&taskID)
	if err != nil {
		return QualityTask{}, err
	}
	seen := map[int64]bool{}
	for _, line := range in.Lines {
		if line.POItemID <= 0 || seen[line.POItemID] {
			return QualityTask{}, apierr.Invalid("QUALITY_LINE_DUPLICATE", "同一产品不能在一次申请中重复")
		}
		seen[line.POItemID] = true
		qty, e := parsePositiveQty(line.Qty)
		if e != nil {
			return QualityTask{}, e
		}
		var product, spec, uom string
		var ordered, already decimal.Decimal
		e = tx.QueryRow(ctx, `SELECT i.product_name,i.spec,i.uom_code,i.qty,
		 COALESCE((SELECT SUM(l.requested_qty) FROM quality_inspection_task_lines l JOIN quality_inspection_tasks t ON t.id=l.task_id AND t.tenant_id=l.tenant_id WHERE l.tenant_id=i.tenant_id AND l.po_item_id=i.id),0)
		 FROM purchase_order_items i WHERE i.tenant_id=$1 AND i.po_id=$2 AND i.id=$3 FOR UPDATE`, tenantID, in.POID, line.POItemID).Scan(&product, &spec, &uom, &ordered, &already)
		if e == pgx.ErrNoRows {
			return QualityTask{}, apierr.Invalid("QUALITY_ITEM_INVALID", "申请中包含不属于该采购单的产品")
		}
		if e != nil {
			return QualityTask{}, e
		}
		if already.Add(qty).GreaterThan(ordered) {
			return QualityTask{}, apierr.Conflict("QUALITY_QTY_EXCEEDED", fmt.Sprintf("%s 累计送检数量超过采购数量", product))
		}
		_, e = tx.Exec(ctx, `INSERT INTO quality_inspection_task_lines(tenant_id,task_id,po_item_id,product_name,spec,uom_code,ordered_qty,requested_qty,unresolved_qty) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`, tenantID, taskID, line.POItemID, product, spec, uom, ordered, qty)
		if e != nil {
			return QualityTask{}, e
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return QualityTask{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetQualityTask(ctx, tenantID, taskID)
}

func (s *Service) ListQualityTasks(ctx context.Context, tenantID int64, tab, keyword string, page, size int32, op Operator) ([]QualityTask, int64, error) {
	page, size = normalizePage(page, size)
	visible, err := s.visibleQualityTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	ids := visible.EmployeeIDs
	if ids == nil {
		ids = []int64{}
	}
	completed := strings.EqualFold(tab, "COMPLETED")
	rows, err := s.pool.Query(ctx, `SELECT t.id,t.po_id,t.task_no,o.po_no,o.supplier_name,t.batch_no,t.status,COALESCE(t.expected_date::text,''),t.inspection_location,t.requested_by_name,t.requested_at,t.inspector_name,t.completed_at,COUNT(*) OVER()
	 FROM quality_inspection_tasks t JOIN purchase_orders o ON o.id=t.po_id AND o.tenant_id=t.tenant_id
	 WHERE t.tenant_id=$1 AND (($2 AND t.status='COMPLETED') OR (NOT $2 AND t.status<>'COMPLETED'))
	 AND ($3='' OR t.task_no ILIKE '%%'||$3||'%%' OR o.po_no ILIKE '%%'||$3||'%%' OR o.supplier_name ILIKE '%%'||$3||'%%')
	 AND ($4 OR o.buyer_id=ANY($5) OR EXISTS(SELECT 1 FROM purchase_order_items oi JOIN purchase_requirements pr ON pr.id=oi.requirement_id AND pr.tenant_id=oi.tenant_id WHERE oi.po_id=o.id AND oi.tenant_id=o.tenant_id AND pr.owner_id=ANY($5)))
	 ORDER BY t.requested_at DESC LIMIT $6 OFFSET $7`, tenantID, completed, strings.TrimSpace(keyword), visible.All, ids, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []QualityTask{}
	var total int64
	for rows.Next() {
		var q QualityTask
		if err = rows.Scan(&q.ID, &q.POID, &q.TaskNo, &q.PONo, &q.SupplierName, &q.BatchNo, &q.Status, &q.ExpectedDate, &q.Location, &q.RequestedByName, &q.RequestedAt, &q.InspectorName, &q.CompletedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, q)
	}
	return out, total, rows.Err()
}

func (s *Service) GetQualityTask(ctx context.Context, tenantID, id int64) (QualityTask, error) {
	var q QualityTask
	err := s.pool.QueryRow(ctx, `SELECT t.id,t.po_id,t.task_no,o.po_no,o.supplier_name,t.batch_no,t.status,COALESCE(t.expected_date::text,''),t.inspection_location,t.contact_name,t.contact_phone,t.remark,t.requested_by_name,t.requested_at,t.inspector_name,t.started_at,t.completed_at FROM quality_inspection_tasks t JOIN purchase_orders o ON o.id=t.po_id AND o.tenant_id=t.tenant_id WHERE t.tenant_id=$1 AND t.id=$2`, tenantID, id).Scan(&q.ID, &q.POID, &q.TaskNo, &q.PONo, &q.SupplierName, &q.BatchNo, &q.Status, &q.ExpectedDate, &q.Location, &q.ContactName, &q.ContactPhone, &q.Remark, &q.RequestedByName, &q.RequestedAt, &q.InspectorName, &q.StartedAt, &q.CompletedAt)
	if err == pgx.ErrNoRows {
		return q, apierr.NotFound("QUALITY_TASK_NOT_FOUND", "质检任务不存在")
	}
	if err != nil {
		return q, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,po_item_id,product_name,spec,uom_code,ordered_qty::text,requested_qty::text,qualified_qty::text,unresolved_qty::text,final_result,issue_description,handling_suggestion,approved_release_qty::text,release_decided_by_name,release_decided_at FROM quality_inspection_task_lines WHERE tenant_id=$1 AND task_id=$2 ORDER BY id`, tenantID, id)
	if err != nil {
		return q, err
	}
	for rows.Next() {
		var l QualityTaskLine
		if err = rows.Scan(&l.ID, &l.POItemID, &l.ProductName, &l.Spec, &l.UOM, &l.OrderedQty, &l.RequestedQty, &l.QualifiedQty, &l.UnresolvedQty, &l.FinalResult, &l.IssueDescription, &l.HandlingSuggestion, &l.ApprovedReleaseQty, &l.ReleaseDecidedByName, &l.ReleaseDecidedAt); err != nil {
			rows.Close()
			return q, err
		}
		q.Lines = append(q.Lines, l)
	}
	rows.Close()
	rr, err := s.pool.Query(ctx, `SELECT id,round_no,inspected_at,inspection_location,inspector_name,remark FROM quality_inspection_rounds WHERE tenant_id=$1 AND task_id=$2 ORDER BY round_no`, tenantID, id)
	if err != nil {
		return q, err
	}
	for rr.Next() {
		var r QualityRound
		if err = rr.Scan(&r.ID, &r.RoundNo, &r.InspectedAt, &r.Location, &r.InspectorName, &r.Remark); err != nil {
			rr.Close()
			return q, err
		}
		lr, e := s.pool.Query(ctx, `SELECT task_line_id,result,inspected_qty::text,qualified_qty::text,unqualified_qty::text,issue_description,handling_suggestion FROM quality_inspection_round_lines WHERE tenant_id=$1 AND round_id=$2 ORDER BY id`, tenantID, r.ID)
		if e != nil {
			rr.Close()
			return q, e
		}
		for lr.Next() {
			var l QualityRoundLine
			if e = lr.Scan(&l.TaskLineID, &l.Result, &l.InspectedQty, &l.QualifiedQty, &l.UnqualifiedQty, &l.IssueDescription, &l.HandlingSuggestion); e != nil {
				lr.Close()
				rr.Close()
				return q, e
			}
			r.Lines = append(r.Lines, l)
		}
		lr.Close()
		q.Rounds = append(q.Rounds, r)
	}
	rr.Close()
	fr, err := s.pool.Query(ctx, `SELECT id,COALESCE(round_id,0),COALESCE(task_line_id,0),category,object_key,file_name,content_type,size_bytes,supplemental,uploaded_by_name,uploaded_at FROM quality_inspection_files WHERE tenant_id=$1 AND task_id=$2 ORDER BY uploaded_at`, tenantID, id)
	if err != nil {
		return q, err
	}
	for fr.Next() {
		var f QualityFile
		if err = fr.Scan(&f.ID, &f.RoundID, &f.TaskLineID, &f.Category, &f.ObjectKey, &f.FileName, &f.ContentType, &f.SizeBytes, &f.Supplemental, &f.UploadedByName, &f.UploadedAt); err != nil {
			fr.Close()
			return q, err
		}
		if s.files != nil {
			f.DownloadURL, _ = s.files.PresignGet(ctx, f.ObjectKey)
		}
		q.Files = append(q.Files, f)
	}
	fr.Close()
	return q, nil
}

func (s *Service) StartQualityTask(ctx context.Context, tenantID, id int64, op Operator) (QualityTask, error) {
	ct, err := s.pool.Exec(ctx, `UPDATE quality_inspection_tasks SET status='IN_PROGRESS',inspector_id=$3,inspector_name=$4,started_at=COALESCE(started_at,now()),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='WAITING'`, tenantID, id, op.ID, op.Name)
	if err != nil {
		return QualityTask{}, err
	}
	if ct.RowsAffected() != 1 {
		return QualityTask{}, apierr.Conflict("QUALITY_START_INVALID", "只有待质检任务可以开始")
	}
	s.nudge(ctx, tenantID)
	return s.GetQualityTask(ctx, tenantID, id)
}

func (s *Service) SubmitQualityRound(ctx context.Context, tenantID, id int64, in SubmitQualityInput, op Operator) (QualityTask, error) {
	if len(in.Lines) == 0 {
		return QualityTask{}, apierr.Invalid("QUALITY_ROUND_LINES_REQUIRED", "请填写本轮逐产品质检结果")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return QualityTask{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM quality_inspection_tasks WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&status); err == pgx.ErrNoRows {
		return QualityTask{}, apierr.NotFound("QUALITY_TASK_NOT_FOUND", "质检任务不存在")
	}
	if err != nil {
		return QualityTask{}, err
	}
	// A procurement request is already the handoff to Quality.  The inspector
	// can record the first round directly; requiring a separate "start" click
	// added no business decision and left a waiting task artificially blocked.
	if status != "WAITING" && status != "IN_PROGRESS" && status != "REINSPECTION" {
		return QualityTask{}, apierr.Conflict("QUALITY_ROUND_INVALID", "任务当前不能录入质检结果")
	}
	var roundNo int32
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(round_no),0)+1 FROM quality_inspection_rounds WHERE tenant_id=$1 AND task_id=$2`, tenantID, id).Scan(&roundNo); err != nil {
		return QualityTask{}, err
	}
	when := time.Now()
	if strings.TrimSpace(in.InspectedAt) != "" {
		when, err = time.Parse(time.RFC3339, in.InspectedAt)
		if err != nil {
			return QualityTask{}, apierr.Invalid("QUALITY_DATE_INVALID", "质检时间格式无效")
		}
	}
	var roundID int64
	if err = tx.QueryRow(ctx, `INSERT INTO quality_inspection_rounds(tenant_id,task_id,round_no,inspected_at,inspection_location,inspector_id,inspector_name,remark) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, tenantID, id, roundNo, when, strings.TrimSpace(in.Location), op.ID, op.Name, strings.TrimSpace(in.Remark)).Scan(&roundID); err != nil {
		return QualityTask{}, err
	}
	seen := map[int64]bool{}
	for _, line := range in.Lines {
		if seen[line.TaskLineID] {
			return QualityTask{}, apierr.Invalid("QUALITY_LINE_DUPLICATE", "同一产品不能重复填写")
		}
		seen[line.TaskLineID] = true
		result := strings.ToUpper(line.Result)
		if result != "PASS" && result != "PARTIAL" && result != "FAIL" {
			return QualityTask{}, apierr.Invalid("QUALITY_RESULT_INVALID", "质检结果必须为合格、部分合格或不合格")
		}
		ins, e := parsePositiveQty(line.InspectedQty)
		if e != nil {
			return QualityTask{}, e
		}
		good, e := decimal.NewFromString(line.QualifiedQty)
		if e != nil || good.IsNegative() {
			return QualityTask{}, apierr.Invalid("QUALITY_QTY_INVALID", "合格数量无效")
		}
		bad, e := decimal.NewFromString(line.UnqualifiedQty)
		if e != nil || bad.IsNegative() || !good.Add(bad).Equal(ins) {
			return QualityTask{}, apierr.Invalid("QUALITY_QTY_MISMATCH", "合格与不合格数量之和必须等于本轮质检数量")
		}
		if (result == "PASS" && !bad.IsZero()) || (result == "FAIL" && !good.IsZero()) || (result == "PARTIAL" && (good.IsZero() || bad.IsZero())) {
			return QualityTask{}, apierr.Invalid("QUALITY_RESULT_QTY_MISMATCH", "质检结论与合格数量不一致")
		}
		var unresolved, qualified decimal.Decimal
		if e = tx.QueryRow(ctx, `SELECT unresolved_qty,qualified_qty FROM quality_inspection_task_lines WHERE tenant_id=$1 AND task_id=$2 AND id=$3 FOR UPDATE`, tenantID, id, line.TaskLineID).Scan(&unresolved, &qualified); e == pgx.ErrNoRows {
			return QualityTask{}, apierr.Invalid("QUALITY_LINE_INVALID", "质检产品不存在")
		}
		if e != nil {
			return QualityTask{}, e
		}
		if ins.GreaterThan(unresolved) {
			return QualityTask{}, apierr.Conflict("QUALITY_REINSPECTION_EXCEEDED", "本轮质检数量超过尚未合格数量")
		}
		_, e = tx.Exec(ctx, `INSERT INTO quality_inspection_round_lines(tenant_id,round_id,task_line_id,result,inspected_qty,qualified_qty,unqualified_qty,issue_description,handling_suggestion) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenantID, roundID, line.TaskLineID, result, ins, good, bad, strings.TrimSpace(line.IssueDescription), strings.TrimSpace(line.HandlingSuggestion))
		if e != nil {
			return QualityTask{}, e
		}
		newGood := qualified.Add(good)
		newUnresolved := unresolved.Sub(good)
		final := "PARTIAL"
		if newGood.IsZero() {
			final = "FAIL"
		} else if newUnresolved.IsZero() {
			final = "PASS"
		}
		_, e = tx.Exec(ctx, `UPDATE quality_inspection_task_lines SET qualified_qty=$4,unresolved_qty=$5,final_result=$6,issue_description=$7,handling_suggestion=$8 WHERE tenant_id=$1 AND task_id=$2 AND id=$3`, tenantID, id, line.TaskLineID, newGood, newUnresolved, final, strings.TrimSpace(line.IssueDescription), strings.TrimSpace(line.HandlingSuggestion))
		if e != nil {
			return QualityTask{}, e
		}
	}
	var unresolvedCount int
	if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM quality_inspection_task_lines WHERE tenant_id=$1 AND task_id=$2 AND unresolved_qty>0`, tenantID, id).Scan(&unresolvedCount); err != nil {
		return QualityTask{}, err
	}
	next := "REINSPECTION"
	if unresolvedCount == 0 {
		next = "COMPLETED"
		_, err = tx.Exec(ctx, `UPDATE quality_inspection_task_lines SET approved_release_qty=qualified_qty WHERE tenant_id=$1 AND task_id=$2`, tenantID, id)
		if err != nil {
			return QualityTask{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE quality_inspection_tasks SET status=$3::varchar,inspector_id=$4,inspector_name=$5,started_at=COALESCE(started_at,now()),inspection_location=CASE WHEN $6='' THEN inspection_location ELSE $6 END,completed_at=CASE WHEN $3::text='COMPLETED' THEN now() ELSE NULL END,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, next, op.ID, op.Name, strings.TrimSpace(in.Location))
	if err != nil {
		return QualityTask{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return QualityTask{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetQualityTask(ctx, tenantID, id)
}

func (s *Service) DecideQualityRelease(ctx context.Context, tenantID, id int64, lines []DecideQualityLine, op Operator) (QualityTask, error) {
	if len(lines) == 0 {
		return QualityTask{}, apierr.Invalid("QUALITY_RELEASE_REQUIRED", "请填写允许先发的合格数量")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return QualityTask{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, l := range lines {
		qty, e := decimal.NewFromString(strings.TrimSpace(l.Qty))
		if e != nil || qty.IsNegative() {
			return QualityTask{}, apierr.Invalid("QUALITY_RELEASE_INVALID", "放行数量无效")
		}
		ct, e := tx.Exec(ctx, `UPDATE quality_inspection_task_lines SET approved_release_qty=$4,release_decided_by=$5,release_decided_by_name=$6,release_decided_at=now() WHERE tenant_id=$1 AND task_id=$2 AND id=$3 AND $4<=qualified_qty`, tenantID, id, l.TaskLineID, qty, op.ID, op.Name)
		if e != nil {
			return QualityTask{}, e
		}
		if ct.RowsAffected() != 1 {
			return QualityTask{}, apierr.Conflict("QUALITY_RELEASE_EXCEEDED", "允许先发数量不能超过当前合格数量")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return QualityTask{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetQualityTask(ctx, tenantID, id)
}

func (s *Service) PresignQualityFile(ctx context.Context, tenantID, taskID int64, fileName string) (string, string, int32, error) {
	if s.files == nil {
		return "", "", 0, apierr.Conflict("QUALITY_FILES_UNAVAILABLE", "附件服务暂不可用")
	}
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" || fileName == "." {
		return "", "", 0, apierr.Invalid("QUALITY_FILE_NAME_REQUIRED", "文件名不能为空")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quality_inspection_tasks WHERE tenant_id=$1 AND id=$2)`, tenantID, taskID).Scan(&exists); err != nil {
		return "", "", 0, err
	}
	if !exists {
		return "", "", 0, apierr.NotFound("QUALITY_TASK_NOT_FOUND", "质检任务不存在")
	}
	key := fmt.Sprintf("quality/%d/%d/%d-%s", tenantID, taskID, time.Now().UnixNano(), fileName)
	url, expires, err := s.files.PresignPut(ctx, key)
	return key, url, expires, err
}
func (s *Service) RegisterQualityFile(ctx context.Context, tenantID, taskID int64, in RegisterQualityFileInput, op Operator) (QualityTask, error) {
	if !strings.HasPrefix(in.Key, fmt.Sprintf("quality/%d/%d/", tenantID, taskID)) {
		return QualityTask{}, apierr.Invalid("QUALITY_FILE_KEY_INVALID", "附件标识无效")
	}
	category := strings.ToUpper(in.Category)
	switch category {
	case "PHOTO", "VIDEO", "REPORT", "THIRD_PARTY", "OTHER":
	default:
		return QualityTask{}, apierr.Invalid("QUALITY_FILE_CATEGORY_INVALID", "附件分类无效")
	}
	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM quality_inspection_tasks WHERE tenant_id=$1 AND id=$2`, tenantID, taskID).Scan(&status); err == pgx.ErrNoRows {
		return QualityTask{}, apierr.NotFound("QUALITY_TASK_NOT_FOUND", "质检任务不存在")
	} else if err != nil {
		return QualityTask{}, err
	}
	if status == "COMPLETED" && !in.Supplemental {
		return QualityTask{}, apierr.Conflict("QUALITY_COMPLETED_IMMUTABLE", "已完成任务只能补充附件")
	}
	if in.RoundID > 0 {
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quality_inspection_rounds WHERE tenant_id=$1 AND task_id=$2 AND id=$3)`, tenantID, taskID, in.RoundID).Scan(&ok); err != nil {
			return QualityTask{}, err
		}
		if !ok {
			return QualityTask{}, apierr.Invalid("QUALITY_ROUND_INVALID", "附件对应的质检轮次不存在")
		}
	}
	if in.TaskLineID > 0 {
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quality_inspection_task_lines WHERE tenant_id=$1 AND task_id=$2 AND id=$3)`, tenantID, taskID, in.TaskLineID).Scan(&ok); err != nil {
			return QualityTask{}, err
		}
		if !ok {
			return QualityTask{}, apierr.Invalid("QUALITY_LINE_INVALID", "附件对应的产品不存在")
		}
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO quality_inspection_files(tenant_id,task_id,round_id,task_line_id,category,object_key,file_name,content_type,size_bytes,supplemental,uploaded_by,uploaded_by_name) VALUES($1,$2,NULLIF($3,0),NULLIF($4,0),$5,$6,$7,$8,$9,$10,$11,$12)`, tenantID, taskID, in.RoundID, in.TaskLineID, category, in.Key, filepath.Base(in.FileName), in.ContentType, in.SizeBytes, in.Supplemental, op.ID, op.Name)
	if err != nil {
		return QualityTask{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetQualityTask(ctx, tenantID, taskID)
}
