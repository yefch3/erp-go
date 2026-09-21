package grpcin

import (
	"context"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
)

func documentsResponse(rows []app.CustomerDocument) *mdv1.ListCustomerDocumentsResponse {
	out := &mdv1.ListCustomerDocumentsResponse{}
	for _, d := range rows {
		out.Documents = append(out.Documents, &mdv1.CustomerDocument{Id: d.ID, CustomerId: d.CustomerID, DocumentId: d.DocumentID, Version: d.Version, Title: d.Title, Remark: d.Remark, FileName: d.FileName, ContentType: d.ContentType, SizeBytes: d.SizeBytes, ExpiresOn: d.ExpiresOn, RemindDays: d.RemindDays, ReminderEnabled: d.ReminderEnabled, UploadedByName: d.UploadedByName, CreatedAt: d.CreatedAt, Current: d.Current, CustomerName: d.CustomerName, Unread: d.Unread})
	}
	return out
}
func (h *Handler) ListCustomerDocuments(ctx context.Context, r *mdv1.ListCustomerDocumentsRequest) (*mdv1.ListCustomerDocumentsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.ListCustomerDocuments(ctx, op.TenantID, r.CustomerId, op.EmployeeID, false)
	return documentsResponse(rows), err
}
func (h *Handler) ListCustomerDocumentReminders(ctx context.Context, r *mdv1.ListCustomerDocumentRemindersRequest) (*mdv1.ListCustomerDocumentRemindersResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.ListCustomerDocuments(ctx, op.TenantID, 0, op.EmployeeID, true)
	return &mdv1.ListCustomerDocumentRemindersResponse{Documents: documentsResponse(rows).Documents}, err
}
func (h *Handler) SaveCustomerDocument(ctx context.Context, r *mdv1.SaveCustomerDocumentRequest) (*mdv1.SaveCustomerDocumentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	id, err := h.svc.SaveCustomerDocument(ctx, op.TenantID, app.CustomerDocumentInput{CustomerID: r.CustomerId, ReplacesID: r.ReplacesId, OperatorID: op.EmployeeID, OperatorName: op.Name, Title: r.Title, Remark: r.Remark, FileName: r.FileName, Content: r.Content, ExpiresOn: r.ExpiresOn, RemindDays: r.RemindDays, ReminderEnabled: r.ReminderEnabled})
	return &mdv1.SaveCustomerDocumentResponse{Id: id}, err
}
func (h *Handler) GetCustomerDocumentFile(ctx context.Context, r *mdv1.GetCustomerDocumentFileRequest) (*mdv1.GetCustomerDocumentFileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	name, kind, data, err := h.svc.GetCustomerDocumentFile(ctx, op.TenantID, r.CustomerId, r.RevisionId)
	return &mdv1.GetCustomerDocumentFileResponse{FileName: name, ContentType: kind, Content: data}, err
}
func (h *Handler) MarkCustomerDocumentRemindersRead(ctx context.Context, r *mdv1.MarkCustomerDocumentRemindersReadRequest) (*mdv1.MarkCustomerDocumentRemindersReadResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	ids := r.Ids
	if ids == nil {
		ids = []int64{}
	}
	n, err := h.svc.MarkCustomerDocumentRemindersRead(ctx, op.TenantID, op.EmployeeID, ids)
	return &mdv1.MarkCustomerDocumentRemindersReadResponse{Marked: n}, err
}

func (h *Handler) DeleteCustomerDocument(ctx context.Context, r *mdv1.DeleteCustomerDocumentRequest) (*mdv1.DeleteCustomerDocumentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	err := h.svc.DeleteCustomerDocument(ctx, op.TenantID, r.CustomerId, r.RevisionId)
	return &mdv1.DeleteCustomerDocumentResponse{}, err
}
