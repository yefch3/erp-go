package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

const BizTypeTravelReimbursement = "TRAVEL_REIMBURSEMENT"

type TravelReimbursement struct {
	ID, ClaimantID, ApprovalInstanceID, PaidBy                           int64
	ClaimNo, ClaimantName, DepartmentName, TripStart, TripEnd            string
	Origin, Destination, Purpose, Amount, Currency, PaymentAccount, Note string
	Status, RejectionReason, PaidAt, PaidByName, PaymentReference        string
	CreatedAt, UpdatedAt                                                 string
	ExpenseType, ExpenseDate                                             string
	Files                                                                []TravelReimbursementFile
	History                                                              []TravelReimbursementHistory
}
type TravelReimbursementFile struct {
	ID                                                             int64
	Category, FileName, ObjectKey, URL, UploadedByName, UploadedAt string
}
type TravelReimbursementHistory struct {
	ID                                                         int64
	Action, FromStatus, ToStatus, Detail, ActorName, CreatedAt string
}
type TravelReimbursementInput struct{ TripStart, TripEnd, Origin, Destination, Purpose, Amount, Currency, PaymentAccount, Note, ExpenseType, ExpenseDate string }

func validateTravelInput(in *TravelReimbursementInput) error {
	in.Origin = strings.TrimSpace(in.Origin)
	in.Destination = strings.TrimSpace(in.Destination)
	in.Purpose = strings.TrimSpace(in.Purpose)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.PaymentAccount = strings.TrimSpace(in.PaymentAccount)
	in.Note = strings.TrimSpace(in.Note)
	in.ExpenseType = strings.TrimSpace(in.ExpenseType)
	if in.ExpenseType == "" {
		in.ExpenseType = "TRAVEL" // Existing clients and historical claims remain travel.
	}
	in.ExpenseDate = strings.TrimSpace(in.ExpenseDate)
	switch in.ExpenseType {
	case "TRAVEL":
		start, e1 := time.Parse("2006-01-02", in.TripStart)
		end, e2 := time.Parse("2006-01-02", in.TripEnd)
		if e1 != nil || e2 != nil || end.Before(start) {
			return apierr.Invalid("PR_TRAVEL_DATE_INVALID", "请填写正确的出差起止日期")
		}
		if in.Origin == "" || in.Destination == "" {
			return apierr.Invalid("PR_TRAVEL_REQUIRED", "请填写出发地和目的地")
		}
		in.ExpenseDate = ""
	case "OFFICE_SUPPLIES", "OTHER":
		if _, err := time.Parse("2006-01-02", in.ExpenseDate); err != nil {
			return apierr.Invalid("PR_EXPENSE_DATE_INVALID", "请填写正确的费用日期")
		}
		// Do not save hidden stale travel fields when switching expense type.
		in.TripStart, in.TripEnd, in.Origin, in.Destination = "", "", "", ""
	default:
		return apierr.Invalid("PR_EXPENSE_TYPE_INVALID", "请选择有效的报销类型")
	}
	amount, e3 := decimal.NewFromString(strings.TrimSpace(in.Amount))
	if in.Purpose == "" || in.PaymentAccount == "" || len(in.Currency) != 3 {
		return apierr.Invalid("PR_TRAVEL_REQUIRED", "请完整填写事由、币种和收款账户")
	}
	if e3 != nil || !amount.IsPositive() || amount.Exponent() < -2 || amount.GreaterThan(maxMoney) {
		return apierr.Invalid("PR_TRAVEL_AMOUNT_INVALID", "报销金额必须是大于 0 且最多两位小数的数字")
	}
	in.Amount = amount.StringFixed(2)
	return nil
}

func (s *Service) CreateTravelReimbursement(ctx context.Context, tenantID int64, in TravelReimbursementInput, op Operator) (TravelReimbursement, error) {
	if err := validateTravelInput(&in); err != nil {
		return TravelReimbursement{}, err
	}
	dept := ""
	if s.people != nil {
		if p, err := s.people.Employee(ctx, op.ID); err == nil {
			dept = p.DepartmentName
		}
	}
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO travel_reimbursements
      (tenant_id,claim_no,claimant_id,claimant_name,department_name,trip_start,trip_end,origin,destination,purpose,amount,currency,payment_account,note,expense_type,expense_date)
      VALUES($1,'PENDING',$2,$3,$4,NULLIF($5,'')::date,NULLIF($6,'')::date,$7,$8,$9,$10::numeric,$11,$12,$13,$14,NULLIF($15,'')::date) RETURNING id`,
		tenantID, op.ID, op.Name, dept, in.TripStart, in.TripEnd, in.Origin, in.Destination, in.Purpose, in.Amount, in.Currency, in.PaymentAccount, in.Note, in.ExpenseType, in.ExpenseDate).Scan(&id)
	if err != nil {
		return TravelReimbursement{}, err
	}
	claimNo := fmt.Sprintf("TR-%s-%06d", time.Now().Format("20060102"), id)
	_, err = s.pool.Exec(ctx, `UPDATE travel_reimbursements SET claim_no=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, id, claimNo)
	if err != nil {
		return TravelReimbursement{}, err
	}
	_ = s.addTravelHistory(ctx, tenantID, id, "CREATED", "", "DRAFT", "", op)
	return s.GetTravelReimbursement(ctx, tenantID, id, op.ID, true)
}

func (s *Service) UpdateTravelReimbursement(ctx context.Context, tenantID, id int64, in TravelReimbursementInput, op Operator) (TravelReimbursement, error) {
	if err := validateTravelInput(&in); err != nil {
		return TravelReimbursement{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TravelReimbursement{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := readTravelReimbursement(ctx, tx, tenantID, id, op.ID, false, true)
	if err != nil {
		return TravelReimbursement{}, err
	}
	if current.ClaimantID != op.ID {
		return TravelReimbursement{}, apierr.Permission("PR_TRAVEL_NOT_OWNER", "只能修改自己的报销申请")
	}
	if current.Status != "DRAFT" && current.Status != "REJECTED" && current.Status != "PENDING_PAYMENT" {
		return TravelReimbursement{}, apierr.Conflict("PR_TRAVEL_EDIT_LOCKED", "当前审批阶段不能修改报销申请")
	}
	next := "DRAFT"
	detail := ""
	if current.Status == "PENDING_PAYMENT" {
		detail = "关键资料已修改，原审批失效，需重新提交"
	}
	_, err = tx.Exec(ctx, `UPDATE travel_reimbursements SET trip_start=NULLIF($3,'')::date,trip_end=NULLIF($4,'')::date,origin=$5,destination=$6,purpose=$7,amount=$8::numeric,currency=$9,payment_account=$10,note=$11,status=$12,approval_instance_id=NULL,rejection_reason='',expense_type=$13,expense_date=NULLIF($14,'')::date,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, in.TripStart, in.TripEnd, in.Origin, in.Destination, in.Purpose, in.Amount, in.Currency, in.PaymentAccount, in.Note, next, in.ExpenseType, in.ExpenseDate)
	if err != nil {
		return TravelReimbursement{}, err
	}
	if err = insertTravelHistory(ctx, tx, tenantID, id, "UPDATED", current.Status, next, detail, op); err != nil {
		return TravelReimbursement{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TravelReimbursement{}, err
	}
	return s.GetTravelReimbursement(ctx, tenantID, id, op.ID, true)
}

func (s *Service) DeleteTravelReimbursement(ctx context.Context, tenantID, id int64, op Operator) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := readTravelReimbursement(ctx, tx, tenantID, id, op.ID, false, true)
		if err != nil {
			return err
		}
		if current.Status != "DRAFT" && current.Status != "REJECTED" {
			return apierr.Conflict("PR_TRAVEL_DELETE_LOCKED", "只有草稿或已退回的报销申请可以删除")
		}
		if _, err := tx.Exec(ctx, `UPDATE travel_reimbursements SET deleted_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
			return err
		}
		return insertTravelHistory(ctx, tx, tenantID, id, "DELETED", current.Status, "DELETED", "", op)
	})
}

func (s *Service) SubmitTravelReimbursement(ctx context.Context, tenantID, id int64, op Operator) (TravelReimbursement, error) {
	// Keep the row locked until the approval instance is recorded. A concurrent
	// deletion must see the submitted status before it can decide what to do.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TravelReimbursement{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := readTravelReimbursement(ctx, tx, tenantID, id, op.ID, false, true)
	if err != nil {
		return TravelReimbursement{}, err
	}
	if current.ClaimantID != op.ID {
		return TravelReimbursement{}, apierr.Permission("PR_TRAVEL_NOT_OWNER", "只能提交自己的报销申请")
	}
	if current.Status != "DRAFT" && current.Status != "REJECTED" {
		return TravelReimbursement{}, apierr.Conflict("PR_TRAVEL_NOT_SUBMITTABLE", "当前状态不能提交")
	}
	input := TravelReimbursementInput{TripStart: current.TripStart, TripEnd: current.TripEnd, Origin: current.Origin, Destination: current.Destination, Purpose: current.Purpose, Amount: current.Amount, Currency: current.Currency, PaymentAccount: current.PaymentAccount, ExpenseType: current.ExpenseType, ExpenseDate: current.ExpenseDate}
	if err := validateTravelInput(&input); err != nil {
		return TravelReimbursement{}, err
	}
	var docs int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM travel_reimbursement_files WHERE tenant_id=$1 AND reimbursement_id=$2 AND removed_at IS NULL`, tenantID, id).Scan(&docs); err != nil {
		return TravelReimbursement{}, err
	}
	if docs == 0 {
		return TravelReimbursement{}, apierr.Invalid("PR_TRAVEL_DOCUMENT_REQUIRED", "提交前至少上传一份发票、收据、行程单或付款凭证")
	}
	if s.approvals == nil {
		return TravelReimbursement{}, apierr.Internal("PR_APPROVAL_UNAVAILABLE", "审批服务暂不可用")
	}
	summary := fmt.Sprintf(`{"claim_no":%q,"claimant":%q,"amount":%q,"currency":%q,"purpose":%q,"expense_type":%q,"expense_date":%q}`, current.ClaimNo, current.ClaimantName, current.Amount, current.Currency, current.Purpose, current.ExpenseType, current.ExpenseDate)
	submission := ApprovalSubmission{BizType: BizTypeTravelReimbursement, BizID: id, BizNo: current.ClaimNo, Summary: summary, SubmitterID: op.ID, SubmitterName: op.Name, Amount: current.Amount}
	nextStatus := "PENDING_DEPARTMENT_CONFIRMATION"
	var instanceID int64
	if approval, ok := s.approvals.(interface {
		SubmitReimbursement(context.Context, ApprovalSubmission) (int64, bool, error)
	}); ok {
		var directFinance bool
		instanceID, directFinance, err = approval.SubmitReimbursement(ctx, submission)
		if directFinance {
			nextStatus = "PENDING_FINANCE_APPROVAL"
		}
	} else {
		instanceID, err = s.approvals.Submit(ctx, submission)
	}
	if err != nil {
		return TravelReimbursement{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE travel_reimbursements SET status=$4,approval_instance_id=$3,rejection_reason='',updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, instanceID, nextStatus)
	if err != nil {
		return TravelReimbursement{}, err
	}
	if err = insertTravelHistory(ctx, tx, tenantID, id, "SUBMITTED", current.Status, nextStatus, "", op); err != nil {
		return TravelReimbursement{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TravelReimbursement{}, err
	}
	return s.GetTravelReimbursement(ctx, tenantID, id, op.ID, true)
}

func (s *Service) ListTravelReimbursements(ctx context.Context, tenantID, operatorID int64, includeAll bool, status string) ([]TravelReimbursement, error) {
	involved := []int64{}
	if lookup, ok := s.approvals.(approvalInvolvement); !includeAll && ok {
		involved, _ = lookup.Involved(ctx, operatorID, BizTypeTravelReimbursement)
	}
	args := []any{tenantID, operatorID, strings.TrimSpace(status), includeAll, involved}
	rows, err := s.pool.Query(ctx, `SELECT id FROM travel_reimbursements WHERE tenant_id=$1 AND deleted_at IS NULL AND ($4 OR claimant_id=$2 OR id=ANY($5::bigint[])) AND ($3='' OR status=$3) ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]TravelReimbursement, 0, len(ids))
	for _, id := range ids {
		v, err := s.GetTravelReimbursement(ctx, tenantID, id, operatorID, true)
		if apierr.CodeFromError(err) == "PR_TRAVEL_NOT_FOUND" {
			continue // Deleted between the list query and loading its details.
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Service) GetTravelReimbursement(ctx context.Context, tenantID, id, operatorID int64, allowAll bool) (TravelReimbursement, error) {
	v, err := readTravelReimbursement(ctx, s.pool, tenantID, id, operatorID, allowAll, false)
	if err != nil {
		return v, err
	}
	v.Files, _ = s.travelFiles(ctx, tenantID, id)
	v.History, _ = s.travelHistory(ctx, tenantID, id)
	return v, nil
}

func readTravelReimbursement(ctx context.Context, db store.DBTX, tenantID, id, operatorID int64, allowAll, lock bool) (TravelReimbursement, error) {
	var v TravelReimbursement
	var approvalID, paidBy *int64
	var paidAt *time.Time
	query := `SELECT id,claim_no,claimant_id,claimant_name,department_name,COALESCE(trip_start::text,''),COALESCE(trip_end::text,''),origin,destination,purpose,amount::text,currency,payment_account,note,status,approval_instance_id,rejection_reason,paid_at,paid_by,paid_by_name,payment_reference,created_at::text,updated_at::text,expense_type,COALESCE(expense_date::text,'') FROM travel_reimbursements WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`
	if lock {
		query += " FOR UPDATE"
	}
	err := db.QueryRow(ctx, query, tenantID, id).Scan(&v.ID, &v.ClaimNo, &v.ClaimantID, &v.ClaimantName, &v.DepartmentName, &v.TripStart, &v.TripEnd, &v.Origin, &v.Destination, &v.Purpose, &v.Amount, &v.Currency, &v.PaymentAccount, &v.Note, &v.Status, &approvalID, &v.RejectionReason, &paidAt, &paidBy, &v.PaidByName, &v.PaymentReference, &v.CreatedAt, &v.UpdatedAt, &v.ExpenseType, &v.ExpenseDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, apierr.NotFound("PR_TRAVEL_NOT_FOUND", "报销申请不存在")
	}
	if err != nil {
		return v, err
	}
	if !allowAll && v.ClaimantID != operatorID {
		return v, apierr.Permission("PR_TRAVEL_NOT_VISIBLE", "无权查看该报销申请")
	}
	if approvalID != nil {
		v.ApprovalInstanceID = *approvalID
	}
	if paidBy != nil {
		v.PaidBy = *paidBy
	}
	if paidAt != nil {
		v.PaidAt = paidAt.Format("2006-01-02")
	}
	return v, nil
}

func (s *Service) PresignTravelFile(ctx context.Context, tenantID, id int64, fileName, category string, op Operator) (string, string, int32, error) {
	v, err := s.GetTravelReimbursement(ctx, tenantID, id, op.ID, false)
	if err != nil {
		return "", "", 0, err
	}
	if v.ClaimantID != op.ID {
		return "", "", 0, apierr.Permission("PR_TRAVEL_NOT_OWNER", "只能上传自己的报销凭证")
	}
	if v.Status != "DRAFT" && v.Status != "REJECTED" && v.Status != "PENDING_PAYMENT" {
		return "", "", 0, apierr.Conflict("PR_TRAVEL_DOCUMENT_LOCKED", "当前审批或付款状态不能修改报销凭证")
	}
	if s.files == nil {
		return "", "", 0, apierr.Internal("PR_FILE_UNAVAILABLE", "附件服务暂不可用")
	}
	category = strings.ToUpper(strings.TrimSpace(category))
	if !travelFileCategories[category] {
		return "", "", 0, apierr.Invalid("PR_TRAVEL_FILE_CATEGORY", "凭证类型不正确")
	}
	name := filepath.Base(strings.TrimSpace(fileName))
	if name == "" || name == "." {
		return "", "", 0, apierr.Invalid("PR_TRAVEL_FILE_NAME", "请选择文件")
	}
	key := fmt.Sprintf("travel-reimbursements/%d/%d/%d-%s", tenantID, id, time.Now().UnixNano(), name)
	url, expires, err := s.files.PresignPut(ctx, key)
	return key, url, expires, err
}

var travelFileCategories = map[string]bool{"INVOICE": true, "RECEIPT": true, "ITINERARY": true, "PAYMENT_PROOF": true, "OTHER": true}

func (s *Service) AttachTravelFile(ctx context.Context, tenantID, id int64, key, name, category string, op Operator) (TravelReimbursement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TravelReimbursement{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	v, err := readTravelReimbursement(ctx, tx, tenantID, id, op.ID, false, true)
	if err != nil {
		return v, err
	}
	if v.ClaimantID != op.ID {
		return v, apierr.Permission("PR_TRAVEL_NOT_OWNER", "只能上传自己的报销凭证")
	}
	if v.Status != "DRAFT" && v.Status != "REJECTED" && v.Status != "PENDING_PAYMENT" {
		return v, apierr.Conflict("PR_TRAVEL_DOCUMENT_LOCKED", "当前审批或付款状态不能修改报销凭证")
	}
	category = strings.ToUpper(strings.TrimSpace(category))
	if !travelFileCategories[category] || !strings.HasPrefix(key, fmt.Sprintf("travel-reimbursements/%d/%d/", tenantID, id)) {
		return v, apierr.Invalid("PR_TRAVEL_FILE_INVALID", "凭证资料不正确")
	}
	_, err = tx.Exec(ctx, `INSERT INTO travel_reimbursement_files(tenant_id,reimbursement_id,category,file_name,object_key,uploaded_by,uploaded_by_name) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenantID, id, category, filepath.Base(name), key, op.ID, op.Name)
	if err != nil {
		return v, err
	}
	if v.Status == "PENDING_PAYMENT" {
		if _, err = tx.Exec(ctx, `UPDATE travel_reimbursements SET status='DRAFT',approval_instance_id=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
			return v, err
		}
		err = insertTravelHistory(ctx, tx, tenantID, id, "DOCUMENT_ADDED", v.Status, "DRAFT", "主要凭证变化，原审批失效", op)
	} else {
		err = insertTravelHistory(ctx, tx, tenantID, id, "DOCUMENT_ADDED", v.Status, v.Status, category+" · "+filepath.Base(name), op)
	}
	if err != nil {
		return v, err
	}
	if err = tx.Commit(ctx); err != nil {
		return v, err
	}
	return s.GetTravelReimbursement(ctx, tenantID, id, op.ID, true)
}
func (s *Service) MarkTravelPaid(ctx context.Context, tenantID, id int64, paidAt, account, reference string, op Operator) (TravelReimbursement, error) {
	when, err := time.Parse("2006-01-02", paidAt)
	if err != nil {
		return TravelReimbursement{}, apierr.Invalid("PR_TRAVEL_PAID_DATE", "付款日期不正确")
	}
	_ = when
	result, err := s.pool.Exec(ctx, `UPDATE travel_reimbursements SET status='PAID',paid_at=$3::date,paid_by=$4,paid_by_name=$5,payment_account=$6,payment_reference=$7,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='PENDING_PAYMENT' AND deleted_at IS NULL`, tenantID, id, paidAt, op.ID, op.Name, strings.TrimSpace(account), strings.TrimSpace(reference))
	if err != nil {
		return TravelReimbursement{}, err
	}
	if result.RowsAffected() == 0 {
		return TravelReimbursement{}, apierr.Conflict("PR_TRAVEL_NOT_PAYABLE", "只有审批通过的报销可以付款")
	}
	_ = s.addTravelHistory(ctx, tenantID, id, "PAID", "PENDING_PAYMENT", "PAID", reference, op)
	return s.GetTravelReimbursement(ctx, tenantID, id, op.ID, true)
}
func (s *Service) ReverseTravelPayment(ctx context.Context, tenantID, id int64, reason string, op Operator) (TravelReimbursement, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return TravelReimbursement{}, apierr.Invalid("PR_TRAVEL_REVERSE_REASON", "冲销原因必填")
	}
	result, err := s.pool.Exec(ctx, `UPDATE travel_reimbursements SET status='PENDING_PAYMENT',paid_at=NULL,paid_by=NULL,paid_by_name='',payment_reference='',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='PAID' AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return TravelReimbursement{}, err
	}
	if result.RowsAffected() == 0 {
		return TravelReimbursement{}, apierr.Conflict("PR_TRAVEL_NOT_PAID", "只有已付款报销可以冲销")
	}
	_ = s.addTravelHistory(ctx, tenantID, id, "PAYMENT_REVERSED", "PAID", "PENDING_PAYMENT", reason, op)
	return s.GetTravelReimbursement(ctx, tenantID, id, op.ID, true)
}
func (s *Service) ApplyTravelApproval(ctx context.Context, tenantID, id, instanceID int64, result, comment string, actedBy int64) (string, error) {
	var next string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var from string
		var deleted, matchesInstance bool
		err := tx.QueryRow(ctx, `SELECT status,deleted_at IS NOT NULL,COALESCE(approval_instance_id=$3,false)
			FROM travel_reimbursements WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id, instanceID).Scan(&from, &deleted, &matchesInstance)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("PR_TRAVEL_NOT_FOUND", "报销申请不存在")
		}
		if err != nil {
			return err
		}
		// An approval event replay after a returned claim was deleted is safe to acknowledge.
		if deleted {
			next = "DELETED"
			return nil
		}
		if !matchesInstance {
			return apierr.NotFound("PR_TRAVEL_NOT_FOUND", "报销申请不存在")
		}
		next = from
		action := "APPROVAL_ADVANCED"
		switch result {
		case "ADVANCED":
			next = "PENDING_FINANCE_APPROVAL"
		case "APPROVED":
			next = "PENDING_PAYMENT"
			action = "APPROVED"
		case "REJECTED", "RETURNED":
			next = "REJECTED"
			action = "REJECTED"
		}
		if _, err := tx.Exec(ctx, `UPDATE travel_reimbursements SET status=$3,rejection_reason=CASE WHEN $3='REJECTED' THEN $4 ELSE '' END,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, next, comment); err != nil {
			return err
		}
		op := Operator{ID: actedBy, Name: fmt.Sprintf("员工 %d", actedBy)}
		return insertTravelHistory(ctx, tx, tenantID, id, action, from, next, comment, op)
	})
	if err != nil {
		return "", err
	}
	return next, nil
}
func (s *Service) addTravelHistory(ctx context.Context, tenantID, id int64, action, from, to, detail string, op Operator) error {
	return insertTravelHistory(ctx, s.pool, tenantID, id, action, from, to, detail, op)
}
func insertTravelHistory(ctx context.Context, db store.DBTX, tenantID, id int64, action, from, to, detail string, op Operator) error {
	_, err := db.Exec(ctx, `INSERT INTO travel_reimbursement_history(tenant_id,reimbursement_id,action,from_status,to_status,detail,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, id, action, from, to, detail, op.ID, op.Name)
	return err
}
func (s *Service) travelFiles(ctx context.Context, tenantID, id int64) ([]TravelReimbursementFile, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,category,file_name,object_key,uploaded_by_name,uploaded_at::text FROM travel_reimbursement_files WHERE tenant_id=$1 AND reimbursement_id=$2 AND removed_at IS NULL ORDER BY uploaded_at`, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TravelReimbursementFile{}
	for rows.Next() {
		var f TravelReimbursementFile
		if err := rows.Scan(&f.ID, &f.Category, &f.FileName, &f.ObjectKey, &f.UploadedByName, &f.UploadedAt); err != nil {
			return nil, err
		}
		if s.files != nil {
			f.URL, _ = s.files.PresignGet(ctx, f.ObjectKey)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func (s *Service) travelHistory(ctx context.Context, tenantID, id int64) ([]TravelReimbursementHistory, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,action,from_status,to_status,detail,actor_name,created_at::text FROM travel_reimbursement_history WHERE tenant_id=$1 AND reimbursement_id=$2 ORDER BY created_at,id`, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TravelReimbursementHistory{}
	for rows.Next() {
		var h TravelReimbursementHistory
		if err := rows.Scan(&h.ID, &h.Action, &h.FromStatus, &h.ToStatus, &h.Detail, &h.ActorName, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
