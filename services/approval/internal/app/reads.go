package app

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

func instanceKey(id int64) string { return strconv.FormatInt(id, 10) }

// Task statuses a caller may filter on, plus the HANDLED shorthand for
// "everything I already acted on".
var taskScopes = map[string]bool{
	"": true, "HANDLED": true, statusApproved: true, statusRejected: true,
	statusReturned: true, "CANCELLED": true, taskSkipped: true,
}

var instanceScopes = map[string]bool{
	"": true, statusRunning: true, statusApproved: true, statusRejected: true,
	statusReturned: true, "CANCELLED": true,
}

// MyTasks returns one employee's approval tasks: the pending queue by
// default, or a past decision when a status is given. The caller passes its
// own employee id: nobody can list somebody else's queue.
func (s *Service) MyTasks(ctx context.Context, tenantID, assigneeID int64, bizType, status, keyword string, page, size int32) ([]store.ListMyTasksRow, int64, error) {
	if !taskScopes[status] {
		return nil, 0, apierr.Invalid("AP_STATUS_INVALID", "不支持的任务状态筛选").
			WithMeta("status", status)
	}
	page, size = normalizePage(page, size)
	rows, err := s.q.ListMyTasks(ctx, store.ListMyTasksParams{
		TenantID: tenantID, AssigneeID: assigneeID, BizType: bizType,
		Status: status, Keyword: keyword,
		RowLimit: size, RowOffset: (page - 1) * size,
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

// MySubmitted 查询当前员工本人发起的审批实例。员工编号来自认证后的 gRPC
// 元数据，不接受 HTTP 查询参数，因此不能借此读取其他员工发起的单据。
func (s *Service) MySubmitted(ctx context.Context, tenantID, submitterID int64, bizType, status, keyword string, page, size int32) ([]store.ListMySubmittedInstancesRow, int64, error) {
	if !instanceScopes[status] {
		return nil, 0, apierr.Invalid("AP_INSTANCE_STATUS_INVALID", "不支持的审批状态筛选").
			WithMeta("status", status)
	}
	page, size = normalizePage(page, size)
	rows, err := s.q.ListMySubmittedInstances(ctx, store.ListMySubmittedInstancesParams{
		TenantID: tenantID, SubmitterID: submitterID, BizType: bizType,
		Status: status, Keyword: keyword, RowLimit: size, RowOffset: (page - 1) * size,
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

// ListInstances is the approval history of one business document: every
// submission, newest first.
func (s *Service) ListInstances(ctx context.Context, tenantID int64, bizType string, bizID int64) ([]store.ApprovalInstance, error) {
	if bizType == "" || bizID == 0 {
		return nil, apierr.Invalid("AP_BIZ_REQUIRED", "单据类型和 ID 必填")
	}
	return s.q.ListInstancesByBiz(ctx, store.ListInstancesByBizParams{
		TenantID: tenantID, BizType: bizType, BizID: bizID,
	})
}

func (s *Service) GetInstance(ctx context.Context, tenantID, id int64) (store.ApprovalInstance, []store.ApprovalTask, error) {
	inst, err := s.q.GetInstance(ctx, store.GetInstanceParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ApprovalInstance{}, nil, apierr.NotFound("AP_INSTANCE_NOT_FOUND", "审批实例不存在")
		}
		return store.ApprovalInstance{}, nil, err
	}
	tasks, err := s.q.ListTasksByInstance(ctx, store.ListTasksByInstanceParams{TenantID: tenantID, InstanceID: id})
	return inst, tasks, err
}

func normalizePage(page, size int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

// MyInvolvedDocuments lists the documents this person has been asked to act
// on, whatever became of the task. Business services union it with their own
// visibility rules: an approver who cannot open the document they are asked
// to approve has been handed an impossible job.
func (s *Service) MyInvolvedDocuments(ctx context.Context, tenantID, employeeID int64, bizType string) ([]int64, error) {
	return s.q.MyInvolvedBizIds(ctx, store.MyInvolvedBizIdsParams{
		TenantID: tenantID, AssigneeID: employeeID, BizType: bizType,
	})
}
