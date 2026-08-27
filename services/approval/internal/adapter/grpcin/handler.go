// Package grpcin exposes the approval engine over gRPC and maps store rows
// to protobuf. Timestamps travel as RFC3339 strings: the frontend formats
// them, nothing computes with them.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/approval/internal/app"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

type Handler struct {
	apv1.UnimplementedApprovalServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Submit(ctx context.Context, req *apv1.SubmitRequest) (*apv1.SubmitResponse, error) {
	inst, tasks, err := h.svc.Submit(ctx, grpcx.TenantID(ctx), app.SubmitInput{
		BizType: req.GetBizType(), BizID: req.GetBizId(), BizNo: req.GetBizNo(),
		BizSummary: req.GetBizSummary(), SubmitterID: req.GetSubmitterId(),
		SubmitterName: req.GetSubmitterName(), Amount: req.GetAmount(),
	})
	if err != nil {
		return nil, err
	}
	return &apv1.SubmitResponse{Instance: instanceToProto(inst), Tasks: tasksToProto(tasks)}, nil
}

func (h *Handler) Act(ctx context.Context, req *apv1.ActRequest) (*apv1.ActResponse, error) {
	action, err := actionFromProto(req.GetAction())
	if err != nil {
		return nil, err
	}
	// The actor is the caller's identity from the metadata, never a field in
	// the request: a client cannot approve as somebody else.
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	inst, next, err := h.svc.Act(ctx, grpcx.TenantID(ctx), actor, req.GetTaskId(), action, req.GetComment())
	if err != nil {
		return nil, err
	}
	return &apv1.ActResponse{Instance: instanceToProto(inst), NextTasks: tasksToProto(next)}, nil
}

func (h *Handler) MyTodos(ctx context.Context, req *apv1.MyTodosRequest) (*apv1.MyTodosResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.MyTasks(ctx, grpcx.TenantID(ctx), actor,
		req.GetBizType(), req.GetStatus(), req.GetKeyword(), page, size)
	if err != nil {
		return nil, err
	}
	todos := make([]*apv1.TodoItem, 0, len(rows))
	now := time.Now()
	for _, r := range rows {
		dueAt, priority, remainingMinutes := app.ApprovalUrgency(r.CreatedAt.Time, now)
		todos = append(todos, &apv1.TodoItem{
			Task: &apv1.Task{
				Id: r.ID, InstanceId: r.InstanceID, NodeSeq: r.NodeSeq, NodeName: r.NodeName,
				AssigneeId: r.AssigneeID, Status: r.Status, Comment: r.Comment,
				ActedAt: ts(r.ActedAt), CreatedAt: ts(r.CreatedAt),
			},
			Instance: &apv1.Instance{
				Id: r.InstanceID, BizType: r.BizType, BizId: r.BizID, BizNo: r.BizNo,
				BizSummary: string(r.BizSummary), SubmitterId: r.SubmitterID,
				SubmitterName: r.SubmitterName, Status: r.InstanceStatus,
				CurrentSeq: r.CurrentSeq, SubmittedAt: ts(r.SubmittedAt),
			},
			DueAt: dueAt.Format(time.RFC3339), Priority: priority,
			RemainingMinutes: remainingMinutes,
		})
	}
	return &apv1.MyTodosResponse{Todos: todos, Meta: &commonv1.PageMeta{Total: total}}, nil
}

// MySubmitted 只返回认证员工本人发起的审批。发起关系与受理关系含义不同，
// 因此不能与上面的审批人任务查询混为一组。
func (h *Handler) MySubmitted(ctx context.Context, req *apv1.MySubmittedRequest) (*apv1.MySubmittedResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.MySubmitted(ctx, grpcx.TenantID(ctx), actor,
		req.GetBizType(), req.GetStatus(), req.GetKeyword(), page, size)
	if err != nil {
		return nil, err
	}
	instances := make([]*apv1.Instance, 0, len(rows))
	for _, r := range rows {
		instances = append(instances, &apv1.Instance{
			Id: r.ID, BizType: r.BizType, BizId: r.BizID, BizNo: r.BizNo,
			BizSummary: string(r.BizSummary), SubmitterId: r.SubmitterID,
			SubmitterName: r.SubmitterName, Status: r.Status,
			CurrentSeq: r.CurrentSeq, SubmittedAt: ts(r.SubmittedAt),
			FinishedAt: ts(r.FinishedAt),
		})
	}
	return &apv1.MySubmittedResponse{
		Instances: instances, Meta: &commonv1.PageMeta{Total: total},
	}, nil
}

func (h *Handler) ListInstances(ctx context.Context, req *apv1.ListInstancesRequest) (*apv1.ListInstancesResponse, error) {
	rows, err := h.svc.ListInstances(ctx, grpcx.TenantID(ctx), req.GetBizType(), req.GetBizId())
	if err != nil {
		return nil, err
	}
	out := make([]*apv1.Instance, 0, len(rows))
	for _, r := range rows {
		out = append(out, instanceToProto(r))
	}
	return &apv1.ListInstancesResponse{Instances: out}, nil
}

func (h *Handler) GetInstance(ctx context.Context, req *apv1.GetInstanceRequest) (*apv1.GetInstanceResponse, error) {
	inst, tasks, err := h.svc.GetInstance(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &apv1.GetInstanceResponse{Instance: instanceToProto(inst), Tasks: tasksToProto(tasks)}, nil
}

// ---------------------------------------------------------------- mapping

// actorID is the employee behind the call. Todo lists and decisions are
// personal, so an anonymous call is a bug, not an empty result.
func actorID(ctx context.Context) (int64, error) {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.EmployeeID == 0 {
		return 0, apierr.Unauthorized("AP_ACTOR_REQUIRED", "缺少操作人身份")
	}
	return op.EmployeeID, nil
}

func actionFromProto(a apv1.Action) (app.Action, error) {
	switch a {
	case apv1.Action_ACTION_APPROVE:
		return app.ActionApprove, nil
	case apv1.Action_ACTION_REJECT:
		return app.ActionReject, nil
	case apv1.Action_ACTION_RETURN:
		return app.ActionReturn, nil
	default:
		return "", apierr.Invalid("AP_ACTION_REQUIRED", "审批动作必填")
	}
}

func ts(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func instanceToProto(i store.ApprovalInstance) *apv1.Instance {
	return &apv1.Instance{
		Id: i.ID, BizType: i.BizType, BizId: i.BizID, BizNo: i.BizNo,
		BizSummary: string(i.BizSummary), SubmitterId: i.SubmitterID,
		SubmitterName: i.SubmitterName, Status: i.Status, CurrentSeq: i.CurrentSeq,
		SubmittedAt: ts(i.SubmittedAt), FinishedAt: ts(i.FinishedAt),
	}
}

func tasksToProto(tasks []store.ApprovalTask) []*apv1.Task {
	out := make([]*apv1.Task, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, &apv1.Task{
			Id: t.ID, InstanceId: t.InstanceID, NodeSeq: t.NodeSeq, NodeName: t.NodeName,
			AssigneeId: t.AssigneeID, Status: t.Status, Comment: t.Comment,
			ActedAt: ts(t.ActedAt), CreatedAt: ts(t.CreatedAt),
		})
	}
	return out
}

// ---------------------------------------------------------------- flows

func (h *Handler) ListDefinitions(ctx context.Context, req *apv1.ListDefinitionsRequest) (*apv1.ListDefinitionsResponse, error) {
	rows, err := h.svc.ListDefinitions(ctx, grpcx.TenantID(ctx), req.GetBizType())
	if err != nil {
		return nil, err
	}
	out := make([]*apv1.Definition, 0, len(rows))
	for _, r := range rows {
		out = append(out, &apv1.Definition{
			Id: r.ID, BizType: r.BizType, Name: r.Name, Version: r.Version,
			Status: r.Status, CreatedAt: ts(r.CreatedAt),
			NodeCount: r.NodeCount, RunningCount: r.RunningCount,
			MinAmount: r.MinAmount, CreatedBy: r.CreatedBy,
		})
	}
	return &apv1.ListDefinitionsResponse{Definitions: out}, nil
}

func (h *Handler) GetDefinition(ctx context.Context, req *apv1.GetDefinitionRequest) (*apv1.GetDefinitionResponse, error) {
	def, nodes, err := h.svc.GetDefinition(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &apv1.GetDefinitionResponse{
		Definition: &apv1.Definition{
			Id: def.ID, BizType: def.BizType, Name: def.Name, Version: def.Version,
			Status: def.Status, CreatedAt: ts(def.CreatedAt), NodeCount: int32(len(nodes)),
			MinAmount: def.MinAmount, CreatedBy: def.CreatedBy,
		},
		Nodes: nodesToProto(nodes),
	}, nil
}

func (h *Handler) SaveDefinition(ctx context.Context, req *apv1.SaveDefinitionRequest) (*apv1.SaveDefinitionResponse, error) {
	in := make([]app.NodeInput, 0, len(req.GetNodes()))
	for _, n := range req.GetNodes() {
		in = append(in, app.NodeInput{
			Name: n.GetName(), ApproverType: n.GetApproverType(),
			ApproverRef: n.GetApproverRef(), ApproveMode: n.GetApproveMode(),
		})
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	def, nodes, err := h.svc.SaveDefinition(ctx, grpcx.TenantID(ctx), req.GetBizType(),
		req.GetName(), req.GetMinAmount(), in, op.EmployeeID)
	if err != nil {
		return nil, err
	}
	return &apv1.SaveDefinitionResponse{
		Definition: &apv1.Definition{
			Id: def.ID, BizType: def.BizType, Name: def.Name, Version: def.Version,
			Status: def.Status, CreatedAt: ts(def.CreatedAt), NodeCount: int32(len(nodes)),
			MinAmount: def.MinAmount, CreatedBy: def.CreatedBy,
		},
		Nodes: nodesToProto(nodes),
	}, nil
}

func (h *Handler) DeleteBand(ctx context.Context, req *apv1.DeleteBandRequest) (*apv1.DeleteBandResponse, error) {
	removed, err := h.svc.DeleteBand(ctx, grpcx.TenantID(ctx), req.GetBizType(), req.GetMinAmount())
	if err != nil {
		return nil, err
	}
	return &apv1.DeleteBandResponse{Removed: removed}, nil
}

func nodesToProto(nodes []store.ApprovalNode) []*apv1.Node {
	out := make([]*apv1.Node, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, &apv1.Node{
			Seq: n.Seq, Name: n.Name, ApproverType: n.ApproverType,
			ApproverRef: n.ApproverRef, ApproveMode: n.ApproveMode,
		})
	}
	return out
}

func (h *Handler) MyInvolvedDocuments(ctx context.Context, req *apv1.MyInvolvedDocumentsRequest) (*apv1.MyInvolvedDocumentsResponse, error) {
	employeeID := req.GetEmployeeId()
	if employeeID == 0 {
		op, _ := grpcx.OperatorFromContext(ctx)
		employeeID = op.EmployeeID
	}
	ids, err := h.svc.MyInvolvedDocuments(ctx, grpcx.TenantID(ctx), employeeID, req.GetBizType())
	if err != nil {
		return nil, err
	}
	return &apv1.MyInvolvedDocumentsResponse{BizIds: ids}, nil
}
