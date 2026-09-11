package grpcin

import (
	"context"
	"time"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

func qualityTime(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.Format(time.RFC3339)
}
func qualityTaskProto(q app.QualityTask) *prv1.QualityInspectionTask {
	out := &prv1.QualityInspectionTask{Id: q.ID, PoId: q.POID, TaskNo: q.TaskNo, PoNo: q.PONo, SupplierName: q.SupplierName, BatchNo: q.BatchNo, Status: q.Status, ExpectedDate: q.ExpectedDate, InspectionLocation: q.Location, ContactName: q.ContactName, ContactPhone: q.ContactPhone, Remark: q.Remark, RequestedByName: q.RequestedByName, RequestedAt: q.RequestedAt.Format(time.RFC3339), InspectorName: q.InspectorName, StartedAt: qualityTime(q.StartedAt), CompletedAt: qualityTime(q.CompletedAt)}
	for _, l := range q.Lines {
		out.Lines = append(out.Lines, &prv1.QualityInspectionTaskLine{Id: l.ID, PoItemId: l.POItemID, ProductName: l.ProductName, Spec: l.Spec, UomCode: l.UOM, OrderedQty: l.OrderedQty, RequestedQty: l.RequestedQty, QualifiedQty: l.QualifiedQty, UnresolvedQty: l.UnresolvedQty, FinalResult: l.FinalResult, IssueDescription: l.IssueDescription, HandlingSuggestion: l.HandlingSuggestion, ApprovedReleaseQty: l.ApprovedReleaseQty, ReleaseDecidedByName: l.ReleaseDecidedByName, ReleaseDecidedAt: qualityTime(l.ReleaseDecidedAt)})
	}
	for _, r := range q.Rounds {
		pr := &prv1.QualityInspectionRound{Id: r.ID, RoundNo: r.RoundNo, InspectedAt: r.InspectedAt.Format(time.RFC3339), InspectionLocation: r.Location, InspectorName: r.InspectorName, Remark: r.Remark}
		for _, l := range r.Lines {
			pr.Lines = append(pr.Lines, &prv1.QualityInspectionRoundLine{TaskLineId: l.TaskLineID, Result: l.Result, InspectedQty: l.InspectedQty, QualifiedQty: l.QualifiedQty, UnqualifiedQty: l.UnqualifiedQty, IssueDescription: l.IssueDescription, HandlingSuggestion: l.HandlingSuggestion})
		}
		out.Rounds = append(out.Rounds, pr)
	}
	for _, f := range q.Files {
		out.Files = append(out.Files, &prv1.QualityInspectionFile{Id: f.ID, RoundId: f.RoundID, TaskLineId: f.TaskLineID, Category: f.Category, FileName: f.FileName, ContentType: f.ContentType, SizeBytes: f.SizeBytes, Supplemental: f.Supplemental, UploadedByName: f.UploadedByName, UploadedAt: f.UploadedAt.Format(time.RFC3339), DownloadUrl: f.DownloadURL})
	}
	return out
}
func currentOp(ctx context.Context) app.Operator {
	op, _ := grpcx.OperatorFromContext(ctx)
	return app.Operator{ID: op.EmployeeID, Name: op.Name}
}
func (h *OrderHandler) ApplyQualityInspection(ctx context.Context, req *prv1.ApplyQualityInspectionRequest) (*prv1.ApplyQualityInspectionResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetPoId()); err != nil {
		return nil, err
	}
	in := app.ApplyQualityInput{POID: req.GetPoId(), ExpectedDate: req.GetExpectedDate(), Location: req.GetInspectionLocation(), ContactName: req.GetContactName(), ContactPhone: req.GetContactPhone(), Remark: req.GetRemark()}
	for _, l := range req.GetLines() {
		in.Lines = append(in.Lines, app.ApplyQualityLine{POItemID: l.GetPoItemId(), Qty: l.GetQty()})
	}
	q, err := h.svc.ApplyQualityInspection(ctx, grpcx.TenantID(ctx), in, currentOp(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ApplyQualityInspectionResponse{Task: qualityTaskProto(q)}, nil
}
func (h *OrderHandler) ListQualityInspectionTasks(ctx context.Context, req *prv1.ListQualityInspectionTasksRequest) (*prv1.ListQualityInspectionTasksResponse, error) {
	rows, total, err := h.svc.ListQualityTasks(ctx, grpcx.TenantID(ctx), req.GetTab(), req.GetKeyword(), req.GetPage().GetPage(), req.GetPage().GetPageSize(), currentOp(ctx))
	if err != nil {
		return nil, err
	}
	out := &prv1.ListQualityInspectionTasksResponse{Meta: &commonv1.PageMeta{Total: total}}
	for _, q := range rows {
		out.Tasks = append(out.Tasks, qualityTaskProto(q))
	}
	return out, nil
}
func (h *OrderHandler) GetQualityInspectionTask(ctx context.Context, req *prv1.GetQualityInspectionTaskRequest) (*prv1.GetQualityInspectionTaskResponse, error) {
	if err := h.svc.AuthorizeQualityTask(ctx, grpcx.TenantID(ctx), req.GetId(), currentOp(ctx)); err != nil {
		return nil, err
	}
	q, err := h.svc.GetQualityTask(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetQualityInspectionTaskResponse{Task: qualityTaskProto(q)}, nil
}
func (h *OrderHandler) StartQualityInspection(ctx context.Context, req *prv1.StartQualityInspectionRequest) (*prv1.StartQualityInspectionResponse, error) {
	if err := h.svc.AuthorizeQualityTask(ctx, grpcx.TenantID(ctx), req.GetId(), currentOp(ctx)); err != nil {
		return nil, err
	}
	q, err := h.svc.StartQualityTask(ctx, grpcx.TenantID(ctx), req.GetId(), currentOp(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.StartQualityInspectionResponse{Task: qualityTaskProto(q)}, nil
}
func (h *OrderHandler) SubmitQualityInspectionRound(ctx context.Context, req *prv1.SubmitQualityInspectionRoundRequest) (*prv1.SubmitQualityInspectionRoundResponse, error) {
	if err := h.svc.AuthorizeQualityTask(ctx, grpcx.TenantID(ctx), req.GetId(), currentOp(ctx)); err != nil {
		return nil, err
	}
	in := app.SubmitQualityInput{InspectedAt: req.GetInspectedAt(), Location: req.GetInspectionLocation(), Remark: req.GetRemark()}
	for _, l := range req.GetLines() {
		in.Lines = append(in.Lines, app.SubmitQualityLine{TaskLineID: l.GetTaskLineId(), Result: l.GetResult(), InspectedQty: l.GetInspectedQty(), QualifiedQty: l.GetQualifiedQty(), UnqualifiedQty: l.GetUnqualifiedQty(), IssueDescription: l.GetIssueDescription(), HandlingSuggestion: l.GetHandlingSuggestion()})
	}
	q, err := h.svc.SubmitQualityRound(ctx, grpcx.TenantID(ctx), req.GetId(), in, currentOp(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.SubmitQualityInspectionRoundResponse{Task: qualityTaskProto(q)}, nil
}
func (h *OrderHandler) DecideQualityRelease(ctx context.Context, req *prv1.DecideQualityReleaseRequest) (*prv1.DecideQualityReleaseResponse, error) {
	if err := h.svc.AuthorizeQualityTask(ctx, grpcx.TenantID(ctx), req.GetId(), currentOp(ctx)); err != nil {
		return nil, err
	}
	lines := []app.DecideQualityLine{}
	for _, l := range req.GetLines() {
		lines = append(lines, app.DecideQualityLine{TaskLineID: l.GetTaskLineId(), Qty: l.GetQty()})
	}
	q, err := h.svc.DecideQualityRelease(ctx, grpcx.TenantID(ctx), req.GetId(), lines, currentOp(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.DecideQualityReleaseResponse{Task: qualityTaskProto(q)}, nil
}
func (h *OrderHandler) PresignQualityInspectionFile(ctx context.Context, req *prv1.PresignQualityInspectionFileRequest) (*prv1.PresignQualityInspectionFileResponse, error) {
	if err := h.svc.AuthorizeQualityTask(ctx, grpcx.TenantID(ctx), req.GetId(), currentOp(ctx)); err != nil {
		return nil, err
	}
	key, url, expires, err := h.svc.PresignQualityFile(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &prv1.PresignQualityInspectionFileResponse{FileKey: key, UploadUrl: url, ExpiresSeconds: expires}, nil
}
func (h *OrderHandler) RegisterQualityInspectionFile(ctx context.Context, req *prv1.RegisterQualityInspectionFileRequest) (*prv1.RegisterQualityInspectionFileResponse, error) {
	if err := h.svc.AuthorizeQualityTask(ctx, grpcx.TenantID(ctx), req.GetId(), currentOp(ctx)); err != nil {
		return nil, err
	}
	q, err := h.svc.RegisterQualityFile(ctx, grpcx.TenantID(ctx), req.GetId(), app.RegisterQualityFileInput{RoundID: req.GetRoundId(), TaskLineID: req.GetTaskLineId(), Category: req.GetCategory(), Key: req.GetFileKey(), FileName: req.GetFileName(), ContentType: req.GetContentType(), SizeBytes: req.GetSizeBytes(), Supplemental: req.GetSupplemental()}, currentOp(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.RegisterQualityInspectionFileResponse{Task: qualityTaskProto(q)}, nil
}
