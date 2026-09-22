package app

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"strings"
	"time"
	"unicode/utf8"
)

// IAM's supplier scope grants All exclusively to the active tenant's highest
// role (also used by supplier deletion). Quality's All includes inspectors and
// must never be used to authorize deletion. Fail closed without IAM.
func (s *Service) CanDeleteQualityTask(ctx context.Context, op Operator) (bool, error) {
	if s.scopes == nil {
		return false, nil
	}
	scope, err := s.scopes.VisibleEmployees(ctx, op.ID, "supplier")
	return scope.All, err
}

func (s *Service) UpdateQualityBasics(ctx context.Context, tenantID, id int64, in ApplyQualityInput, op Operator) (QualityTask, error) {
	if err := s.AuthorizeQualityTask(ctx, tenantID, id, op); err != nil {
		return QualityTask{}, err
	}
	if in.ExpectedDate != "" {
		if _, err := time.Parse("2006-01-02", in.ExpectedDate); err != nil {
			return QualityTask{}, apierr.Invalid("QUALITY_DATE_INVALID", "预计质检日期格式无效")
		}
	}
	if utf8.RuneCountInString(in.ContactName) > 100 || utf8.RuneCountInString(in.ContactPhone) > 64 {
		return QualityTask{}, apierr.Invalid("QUALITY_CONTACT_TOO_LONG", "联系人最多 100 字，联系电话最多 64 字")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return QualityTask{}, err
	}
	defer tx.Rollback(ctx)
	var status string
	var before []byte
	err = tx.QueryRow(ctx, `SELECT status,to_jsonb(t) FROM quality_inspection_tasks t WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&status, &before)
	if err == pgx.ErrNoRows {
		return QualityTask{}, apierr.NotFound("QUALITY_TASK_NOT_FOUND", "质检任务不存在")
	}
	if err != nil {
		return QualityTask{}, err
	}
	if status == "COMPLETED" {
		return QualityTask{}, apierr.Conflict("QUALITY_COMPLETED_IMMUTABLE", "已完成质检单不能修改基本资料")
	}
	var after []byte
	err = tx.QueryRow(ctx, `UPDATE quality_inspection_tasks t SET expected_date=NULLIF($3,'')::date,inspection_location=$4,contact_name=$5,contact_phone=$6,remark=$7,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING to_jsonb(t)`, tenantID, id, in.ExpectedDate, strings.TrimSpace(in.Location), strings.TrimSpace(in.ContactName), strings.TrimSpace(in.ContactPhone), strings.TrimSpace(in.Remark)).Scan(&after)
	if err != nil {
		return QualityTask{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO quality_task_audit(tenant_id,task_id,action,actor_id,actor_name,snapshot) VALUES($1,$2,'UPDATE_BASICS',$3,$4,jsonb_build_object('before',$5::jsonb,'after',$6::jsonb))`, tenantID, id, op.ID, op.Name, before, after)
	if err != nil {
		return QualityTask{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return QualityTask{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetQualityTask(ctx, tenantID, id)
}

func (s *Service) DeleteQualityTask(ctx context.Context, tenantID, id int64, op Operator) error {
	allowed, err := s.CanDeleteQualityTask(ctx, op)
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.Permission("QUALITY_DELETE_FORBIDDEN", "只有最高权限管理员可以删除质检单")
	}
	if err = s.AuthorizeQualityTask(ctx, tenantID, id, op); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var started *time.Time
	var snapshot []byte
	err = tx.QueryRow(ctx, `SELECT status,started_at,to_jsonb(t) FROM quality_inspection_tasks t WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&status, &started, &snapshot)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("QUALITY_TASK_NOT_FOUND", "质检任务不存在")
	}
	if err != nil {
		return err
	}
	var hasRecords bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quality_inspection_rounds WHERE tenant_id=$1 AND task_id=$2) OR EXISTS(SELECT 1 FROM quality_inspection_files WHERE tenant_id=$1 AND task_id=$2)`, tenantID, id).Scan(&hasRecords)
	if err != nil {
		return err
	}
	if status != "WAITING" || started != nil || hasRecords {
		return apierr.Conflict("QUALITY_DELETE_NOT_EMPTY", "只能删除尚未开始且没有质检结果和附件的质检单")
	}
	_, err = tx.Exec(ctx, `INSERT INTO quality_task_audit(tenant_id,task_id,action,actor_id,actor_name,snapshot) VALUES($1,$2,'DELETE',$3,$4,jsonb_build_object('task',$5::jsonb,'lines',(SELECT COALESCE(jsonb_agg(to_jsonb(l)),'[]'::jsonb) FROM quality_inspection_task_lines l WHERE tenant_id=$1 AND task_id=$2)))`, tenantID, id, op.ID, op.Name, snapshot)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM quality_inspection_tasks WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	return nil
}
