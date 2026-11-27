package app

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/sgao19/erp-go/pkg/apierr"
)

const reimbursementBizType = "TRAVEL_REIMBURSEMENT"

type reimbursementDirectoryResolver interface {
	DirectReimbursementDepartment(context.Context, int64) (bool, error)
	ReimbursementApprovers(context.Context) ([]int64, error)
}

func (s *Service) reimbursementApprovers(ctx context.Context) ([]int64, error) {
	dir, ok := s.dir.(reimbursementDirectoryResolver)
	if !ok {
		return nil, apierr.Internal("AP_REIMBURSEMENT_DIRECTORY_UNAVAILABLE", "暂时无法读取报销审批人员")
	}
	return dir.ReimbursementApprovers(ctx)
}

func (s *Service) canApproveReimbursement(ctx context.Context, actorID int64) (bool, error) {
	if _, ok := s.dir.(reimbursementDirectoryResolver); !ok {
		return false, nil
	}
	members, err := s.reimbursementApprovers(ctx)
	return slices.Contains(members, actorID), err
}

func (s *Service) reimbursementCandidates(ctx context.Context, members []int64, submitterID int64) ([]int64, error) {
	if !slices.Contains(members, submitterID) {
		return members, nil
	}
	admin, err := s.isSuperAdmin(ctx, submitterID)
	if err != nil || admin {
		return members, err
	}
	return excludeEmployee(members, submitterID), nil
}

func (s *Service) reimbursementRoute(ctx context.Context, in SubmitInput) (bool, string, []int64, error) {
	if in.BizType != reimbursementBizType {
		return false, in.BizSummary, nil, nil
	}
	var summary map[string]json.RawMessage
	if in.BizSummary != "" {
		if err := json.Unmarshal([]byte(in.BizSummary), &summary); err != nil {
			return false, "", nil, apierr.Invalid("AP_SUMMARY_INVALID", "单据摘要必须是合法 JSON 对象")
		}
	}
	if summary == nil {
		summary = make(map[string]json.RawMessage)
	}
	delete(summary, "direct_finance_approval")
	var direct bool
	var members []int64
	if dir, ok := s.dir.(reimbursementDirectoryResolver); ok {
		var err error
		direct, err = dir.DirectReimbursementDepartment(ctx, in.SubmitterID)
		if err != nil {
			return false, "", nil, err
		}
		if direct {
			members, err = s.reimbursementApprovers(ctx)
			if err != nil {
				return false, "", nil, err
			}
			members, err = s.reimbursementCandidates(ctx, members, in.SubmitterID)
			if err != nil {
				return false, "", nil, err
			}
			if len(members) == 0 {
				return false, "", nil, apierr.Invalid("AP_REIMBURSEMENT_APPROVER_MISSING", "没有可用的报销审批人，请联系超级管理员检查「财务 → 报销审批」权限")
			}
			summary["direct_finance_approval"] = json.RawMessage("true")
		}
	}
	payload, err := json.Marshal(summary)
	return direct, string(payload), members, err
}
