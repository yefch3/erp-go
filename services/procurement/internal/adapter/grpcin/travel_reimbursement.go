package grpcin

import (
	"context"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

func travelOp(ctx context.Context) app.Operator {
	op, _ := grpcx.OperatorFromContext(ctx)
	return app.Operator{ID: op.EmployeeID, Name: op.Name}
}
func travelInput(a, b, c, d, e, f, g, h, i string) app.TravelReimbursementInput {
	return app.TravelReimbursementInput{TripStart: a, TripEnd: b, Origin: c, Destination: d, Purpose: e, Amount: f, Currency: g, PaymentAccount: h, Note: i}
}
func travelProto(v app.TravelReimbursement) *prv1.TravelReimbursement {
	out := &prv1.TravelReimbursement{Id: v.ID, ClaimNo: v.ClaimNo, ClaimantId: v.ClaimantID, ClaimantName: v.ClaimantName, DepartmentName: v.DepartmentName, TripStart: v.TripStart, TripEnd: v.TripEnd, Origin: v.Origin, Destination: v.Destination, Purpose: v.Purpose, Amount: v.Amount, Currency: v.Currency, PaymentAccount: v.PaymentAccount, Note: v.Note, Status: v.Status, ApprovalInstanceId: v.ApprovalInstanceID, RejectionReason: v.RejectionReason, PaidAt: v.PaidAt, PaidBy: v.PaidBy, PaidByName: v.PaidByName, PaymentReference: v.PaymentReference, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	for _, f := range v.Files {
		out.Files = append(out.Files, &prv1.TravelReimbursementFile{Id: f.ID, Category: f.Category, FileName: f.FileName, ObjectKey: f.ObjectKey, Url: f.URL, UploadedByName: f.UploadedByName, UploadedAt: f.UploadedAt})
	}
	for _, x := range v.History {
		out.History = append(out.History, &prv1.TravelReimbursementHistory{Id: x.ID, Action: x.Action, FromStatus: x.FromStatus, ToStatus: x.ToStatus, Detail: x.Detail, ActorName: x.ActorName, CreatedAt: x.CreatedAt})
	}
	return out
}
func (h *OrderHandler) CreateTravelReimbursement(ctx context.Context, r *prv1.CreateTravelReimbursementRequest) (*prv1.CreateTravelReimbursementResponse, error) {
	v, e := h.svc.CreateTravelReimbursement(ctx, grpcx.TenantID(ctx), travelInput(r.GetTripStart(), r.GetTripEnd(), r.GetOrigin(), r.GetDestination(), r.GetPurpose(), r.GetAmount(), r.GetCurrency(), r.GetPaymentAccount(), r.GetNote()), travelOp(ctx))
	return &prv1.CreateTravelReimbursementResponse{Reimbursement: travelProto(v)}, e
}
func (h *OrderHandler) UpdateTravelReimbursement(ctx context.Context, r *prv1.UpdateTravelReimbursementRequest) (*prv1.UpdateTravelReimbursementResponse, error) {
	v, e := h.svc.UpdateTravelReimbursement(ctx, grpcx.TenantID(ctx), r.GetId(), travelInput(r.GetTripStart(), r.GetTripEnd(), r.GetOrigin(), r.GetDestination(), r.GetPurpose(), r.GetAmount(), r.GetCurrency(), r.GetPaymentAccount(), r.GetNote()), travelOp(ctx))
	return &prv1.UpdateTravelReimbursementResponse{Reimbursement: travelProto(v)}, e
}
func (h *OrderHandler) ListTravelReimbursements(ctx context.Context, r *prv1.ListTravelReimbursementsRequest) (*prv1.ListTravelReimbursementsResponse, error) {
	op := travelOp(ctx)
	rows, e := h.svc.ListTravelReimbursements(ctx, grpcx.TenantID(ctx), op.ID, r.GetIncludeAll(), r.GetStatus())
	out := &prv1.ListTravelReimbursementsResponse{}
	for _, v := range rows {
		out.Items = append(out.Items, travelProto(v))
	}
	return out, e
}
func (h *OrderHandler) SubmitTravelReimbursement(ctx context.Context, r *prv1.SubmitTravelReimbursementRequest) (*prv1.SubmitTravelReimbursementResponse, error) {
	v, e := h.svc.SubmitTravelReimbursement(ctx, grpcx.TenantID(ctx), r.GetId(), travelOp(ctx))
	return &prv1.SubmitTravelReimbursementResponse{Reimbursement: travelProto(v)}, e
}
func (h *OrderHandler) PresignTravelReimbursementFile(ctx context.Context, r *prv1.PresignTravelReimbursementFileRequest) (*prv1.PresignTravelReimbursementFileResponse, error) {
	key, url, expires, e := h.svc.PresignTravelFile(ctx, grpcx.TenantID(ctx), r.GetId(), r.GetFileName(), r.GetCategory(), travelOp(ctx))
	return &prv1.PresignTravelReimbursementFileResponse{ObjectKey: key, UploadUrl: url, ExpiresSeconds: expires}, e
}
func (h *OrderHandler) AttachTravelReimbursementFile(ctx context.Context, r *prv1.AttachTravelReimbursementFileRequest) (*prv1.AttachTravelReimbursementFileResponse, error) {
	v, e := h.svc.AttachTravelFile(ctx, grpcx.TenantID(ctx), r.GetId(), r.GetObjectKey(), r.GetFileName(), r.GetCategory(), travelOp(ctx))
	return &prv1.AttachTravelReimbursementFileResponse{Reimbursement: travelProto(v)}, e
}
func (h *OrderHandler) MarkTravelReimbursementPaid(ctx context.Context, r *prv1.MarkTravelReimbursementPaidRequest) (*prv1.MarkTravelReimbursementPaidResponse, error) {
	v, e := h.svc.MarkTravelPaid(ctx, grpcx.TenantID(ctx), r.GetId(), r.GetPaidAt(), r.GetPaymentAccount(), r.GetPaymentReference(), travelOp(ctx))
	return &prv1.MarkTravelReimbursementPaidResponse{Reimbursement: travelProto(v)}, e
}
func (h *OrderHandler) ReverseTravelReimbursementPayment(ctx context.Context, r *prv1.ReverseTravelReimbursementPaymentRequest) (*prv1.ReverseTravelReimbursementPaymentResponse, error) {
	v, e := h.svc.ReverseTravelPayment(ctx, grpcx.TenantID(ctx), r.GetId(), r.GetReason(), travelOp(ctx))
	return &prv1.ReverseTravelReimbursementPaymentResponse{Reimbursement: travelProto(v)}, e
}
