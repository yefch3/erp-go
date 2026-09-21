package grpcin

import (
	"context"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
)

func supplierDocumentsResponse(rows []app.SupplierDocument) []*mdv1.SupplierDocument {
	out := make([]*mdv1.SupplierDocument, 0, len(rows))
	for _, d := range rows {
		out = append(out, &mdv1.SupplierDocument{Id: d.ID, SupplierId: d.SupplierID, DocumentId: d.DocumentID, Version: d.Version, Title: d.Title, Remark: d.Remark, FileName: d.FileName, ContentType: d.ContentType, SizeBytes: d.SizeBytes, ExpiresOn: d.ExpiresOn, RemindDays: d.RemindDays, ReminderEnabled: d.ReminderEnabled, UploadedByName: d.UploadedByName, CreatedAt: d.CreatedAt, Current: d.Current, SupplierName: d.SupplierName, Unread: d.Unread})
	}
	return out
}
func (h *Handler) ListSupplierDocuments(ctx context.Context, r *mdv1.ListSupplierDocumentsRequest) (*mdv1.ListSupplierDocumentsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.ListSupplierDocuments(ctx, op.TenantID, r.SupplierId, op.EmployeeID, false)
	return &mdv1.ListSupplierDocumentsResponse{Documents: supplierDocumentsResponse(rows)}, err
}
func (h *Handler) ListSupplierDocumentReminders(ctx context.Context, r *mdv1.ListSupplierDocumentRemindersRequest) (*mdv1.ListSupplierDocumentRemindersResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.ListSupplierDocuments(ctx, op.TenantID, 0, op.EmployeeID, true)
	return &mdv1.ListSupplierDocumentRemindersResponse{Documents: supplierDocumentsResponse(rows)}, err
}
func (h *Handler) SaveSupplierDocument(ctx context.Context, r *mdv1.SaveSupplierDocumentRequest) (*mdv1.SaveSupplierDocumentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	id, err := h.svc.SaveSupplierDocument(ctx, op.TenantID, app.SupplierDocumentInput{SupplierID: r.SupplierId, ReplacesID: r.ReplacesId, OperatorID: op.EmployeeID, OperatorName: op.Name, Title: r.Title, Remark: r.Remark, FileName: r.FileName, Content: r.Content, ExpiresOn: r.ExpiresOn, RemindDays: r.RemindDays, ReminderEnabled: r.ReminderEnabled})
	return &mdv1.SaveSupplierDocumentResponse{Id: id}, err
}
func (h *Handler) GetSupplierDocumentFile(ctx context.Context, r *mdv1.GetSupplierDocumentFileRequest) (*mdv1.GetSupplierDocumentFileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	name, kind, data, err := h.svc.GetSupplierDocumentFile(ctx, op.TenantID, r.SupplierId, r.RevisionId)
	return &mdv1.GetSupplierDocumentFileResponse{FileName: name, ContentType: kind, Content: data}, err
}
func (h *Handler) DeleteSupplierDocument(ctx context.Context, r *mdv1.DeleteSupplierDocumentRequest) (*mdv1.DeleteSupplierDocumentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	err := h.svc.DeleteSupplierDocument(ctx, op.TenantID, r.SupplierId, r.RevisionId)
	return &mdv1.DeleteSupplierDocumentResponse{}, err
}
func (h *Handler) MarkSupplierDocumentRemindersRead(ctx context.Context, r *mdv1.MarkSupplierDocumentRemindersReadRequest) (*mdv1.MarkSupplierDocumentRemindersReadResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	n, err := h.svc.MarkSupplierDocumentRemindersRead(ctx, op.TenantID, op.EmployeeID, r.Ids)
	return &mdv1.MarkSupplierDocumentRemindersReadResponse{Marked: n}, err
}
