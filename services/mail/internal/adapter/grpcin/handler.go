// Package grpcin exposes correspondence over gRPC.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/mail/internal/app"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

type Handler struct {
	mailv1.UnimplementedEmailServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func operator(ctx context.Context) app.Operator {
	op, _ := grpcx.OperatorFromContext(ctx)
	return app.Operator{ID: op.EmployeeID, Name: op.Name}
}

func ts(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func page(p *commonv1.PageRequest) (int32, int32) {
	if p == nil {
		return 1, 20
	}
	return p.GetPage(), p.GetPageSize()
}

func meta(total int64, p *commonv1.PageRequest) *commonv1.PageMeta {
	pg, size := page(p)
	if pg < 1 {
		pg = 1
	}
	if size < 1 {
		size = 20
	}
	return &commonv1.PageMeta{Total: total, Page: pg, PageSize: size}
}

// ---------------------------------------------------------------- campaigns

func (h *Handler) ListCampaigns(ctx context.Context, req *mailv1.ListCampaignsRequest) (*mailv1.ListCampaignsResponse, error) {
	pg, size := page(req.GetPage())
	rows, total, err := h.svc.ListCampaigns(ctx, grpcx.TenantID(ctx), app.CampaignQuery{
		Keyword: req.GetKeyword(), SenderID: req.GetSenderId(), Page: pg, Size: size,
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.Campaign, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mailv1.Campaign{
			Id: r.ID, CampaignNo: r.CampaignNo, Subject: r.SubjectTpl, Kind: r.Kind,
			SenderId: r.SenderID, SenderName: r.SenderName, Status: r.Status,
			CreatedAt: ts(r.CreatedAt), FinishedAt: ts(r.FinishedAt),
			ToNames:    r.ToNames,
			TotalCount: r.TotalCount, SentCount: r.SentCount,
			DeliveredCount: r.DeliveredCount, FailedCount: r.FailedCount,
			PendingCount: r.PendingCount,
		})
	}
	return &mailv1.ListCampaignsResponse{Campaigns: out, Meta: meta(total, req.GetPage())}, nil
}

func (h *Handler) GetCampaign(ctx context.Context, req *mailv1.GetCampaignRequest) (*mailv1.GetCampaignResponse, error) {
	c, err := h.svc.GetCampaign(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	files, err := h.svc.SignedAttachments(ctx, grpcx.TenantID(ctx), c.ID)
	if err != nil {
		return nil, err
	}
	return &mailv1.GetCampaignResponse{Campaign: &mailv1.CampaignDetail{
		Id: c.ID, CampaignNo: c.CampaignNo, SubjectTpl: c.SubjectTpl, BodyTpl: c.BodyTpl,
		BodyTextTpl: c.BodyTextTpl, BodyFormat: c.BodyFormat,
		Attachments: signedAttachmentsToProto(files),
		SignatureId: c.SignatureID, Kind: c.Kind, SenderId: c.SenderID,
		SenderName: c.SenderName, SenderEmail: c.SenderEmail, Status: c.Status,
		CreatedAt: ts(c.CreatedAt), FinishedAt: ts(c.FinishedAt),
	}}, nil
}

func signedAttachmentsToProto(in []app.Attachment) []*mailv1.EmailAttachment {
	out := make([]*mailv1.EmailAttachment, 0, len(in))
	for _, a := range in {
		out = append(out, &mailv1.EmailAttachment{
			Id: a.ID, FileName: a.FileName, FileSize: a.FileSize,
			ContentType: a.ContentType, DownloadUrl: a.DownloadURL,
			PreviewUrl: a.PreviewURL,
		})
	}
	return out
}

func (h *Handler) PreviewCampaign(ctx context.Context, req *mailv1.PreviewCampaignRequest) (*mailv1.PreviewCampaignResponse, error) {
	p, err := h.svc.Preview(ctx, grpcx.TenantID(ctx), app.CampaignInput{
		Subject: req.GetSubject(), Body: req.GetBody(),
		Format: req.GetBodyFormat(), SignatureID: req.GetSignatureId(),
	}, recipientFromProto(req.GetRecipient()), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.PreviewCampaignResponse{
		Subject: p.Subject, Body: p.Body, BodyText: p.BodyText,
		BodyFormat: p.Format, MissingVariables: p.Missing,
	}, nil
}

func (h *Handler) CreateCampaign(ctx context.Context, req *mailv1.CreateCampaignRequest) (*mailv1.CreateCampaignResponse, error) {
	recipients := make([]app.Recipient, 0, len(req.GetRecipients()))
	for _, r := range req.GetRecipients() {
		recipients = append(recipients, recipientFromProto(r))
	}
	cc := make([]app.Recipient, 0, len(req.GetCc()))
	for _, r := range req.GetCc() {
		cc = append(cc, recipientFromProto(r))
	}
	at, err := scheduleAt(req.GetScheduledAt())
	if err != nil {
		return nil, err
	}
	res, err := h.svc.CreateCampaign(ctx, grpcx.TenantID(ctx), app.CampaignInput{
		Subject: req.GetSubject(), Body: req.GetBody(),
		SignatureID: req.GetSignatureId(), Kind: req.GetKind(),
		Format: req.GetBodyFormat(), Recipients: recipients,
		SendMode: req.GetSendMode(), CC: cc, BCC: recipientsFromProto(req.GetBcc()),
		ReplyToInboundID:    req.GetReplyToInboundId(),
		ForwardInboundID:    req.GetForwardInboundId(),
		ForwardAsAttachment: req.GetForwardAsAttachment(),
		Attachments:         pendingFromProto(req.GetAttachments()),
		ScheduledAt:         at,
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.CreateCampaignResponse{
		CampaignId: res.CampaignID, CampaignNo: res.CampaignNo,
		Queued:      int32(res.Queued),
		Suppressed:  skippedToProto(res.Suppressed),
		NeedsReview: skippedToProto(res.NeedsReview),
	}, nil
}

func (h *Handler) SendProcurementRfq(ctx context.Context, req *mailv1.SendProcurementRfqRequest) (*mailv1.SendProcurementRfqResponse, error) {
	res, err := h.svc.SendProcurementRFQ(ctx, grpcx.TenantID(ctx), req.GetSenderEmployeeId(), req.GetRecipientName(),
		req.GetRecipientEmail(), req.GetSubject(), req.GetBody(), req.GetFileName(), req.GetFileData())
	if err != nil {
		return nil, err
	}
	return &mailv1.SendProcurementRfqResponse{CampaignId: res.CampaignID, CampaignNo: res.CampaignNo, Queued: int32(res.Queued)}, nil
}

func (h *Handler) SendProcurementOrder(ctx context.Context, req *mailv1.SendProcurementOrderRequest) (*mailv1.SendProcurementOrderResponse, error) {
	files := make([]app.ProcurementOrderAttachment, 0, len(req.GetAttachments()))
	for _, file := range req.GetAttachments() {
		files = append(files, app.ProcurementOrderAttachment{FileName: file.GetFileName(), ContentType: file.GetContentType(), Data: file.GetFileData()})
	}
	res, err := h.svc.SendProcurementOrder(ctx, grpcx.TenantID(ctx), req.GetSenderEmployeeId(), req.GetRecipientName(), req.GetRecipientEmail(), req.GetSubject(), req.GetBody(), files)
	if err != nil {
		return nil, err
	}
	return &mailv1.SendProcurementOrderResponse{CampaignId: res.CampaignID, CampaignNo: res.CampaignNo, Queued: int32(res.Queued)}, nil
}

func pendingFromProto(in []*mailv1.PendingAttachment) []app.PendingAttachment {
	out := make([]app.PendingAttachment, 0, len(in))
	for _, f := range in {
		out = append(out, app.PendingAttachment{
			FileName: f.GetFileName(), FileKey: f.GetFileKey(),
		})
	}
	return out
}

func recipientFromProto(r *mailv1.Recipient) app.Recipient {
	if r == nil {
		return app.Recipient{}
	}
	return app.Recipient{
		ContactID: r.GetContactId(), Name: r.GetName(), Email: r.GetEmail(),
		CustomerID: r.GetCustomerId(), CustomerName: r.GetCustomerName(),
		Extra: r.GetExtra(),
	}
}

func skippedToProto(in []app.SkippedRecipient) []*mailv1.SkippedRecipient {
	out := make([]*mailv1.SkippedRecipient, 0, len(in))
	for _, s := range in {
		out = append(out, &mailv1.SkippedRecipient{Email: s.Email, Name: s.Name, Reason: s.Reason})
	}
	return out
}

// ---------------------------------------------------------------- messages

func (h *Handler) ListMessages(ctx context.Context, req *mailv1.ListMessagesRequest) (*mailv1.ListMessagesResponse, error) {
	_, size := page(req.GetPage())
	rows, total, next, err := h.svc.ListMessages(ctx, grpcx.TenantID(ctx), app.MessageQuery{
		CampaignID: req.GetCampaignId(), SenderID: req.GetSenderId(),
		Status:        req.GetStatus(),
		AttentionOnly: req.GetAttentionOnly(), Keyword: req.GetKeyword(),
		Cursor: req.GetCursor(), Size: size,
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.EmailMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mailv1.EmailMessage{
			Id: r.ID, CampaignId: r.CampaignID, Kind: r.Kind,
			SenderId: r.SenderID, SenderName: r.SenderName,
			ToEmail: r.ToEmail, ToName: r.ToName, CustomerName: r.CustomerName,
			Subject: r.Subject, Status: r.Status, AttemptCount: r.AttemptCount,
			LastError: r.LastError, AttentionReason: r.AttentionReason,
			QueuedAt: ts(r.QueuedAt), SentAt: ts(r.SentAt),
			DeliveredAt: ts(r.DeliveredAt), OpenedAt: ts(r.OpenedAt),
		})
	}
	return &mailv1.ListMessagesResponse{
		Messages: out, Meta: meta(total, req.GetPage()), NextCursor: next,
	}, nil
}

func (h *Handler) GetMessage(ctx context.Context, req *mailv1.GetMessageRequest) (*mailv1.GetMessageResponse, error) {
	v, err := h.svc.GetMessage(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	m := v.Message
	// The files are the campaign's, so a message that was not part of one
	// simply has none.
	var files []app.Attachment
	if m.CampaignID != 0 {
		if files, err = h.svc.SignedAttachments(ctx, grpcx.TenantID(ctx), m.CampaignID); err != nil {
			return nil, err
		}
	}
	return &mailv1.GetMessageResponse{Message: &mailv1.MessageDetail{
		Id: m.ID, CampaignId: m.CampaignID, MessageKey: m.MessageKey, Kind: m.Kind,
		SenderId: m.SenderID, SenderName: m.SenderName, SenderEmail: m.SenderEmail,
		ToEmail: m.ToEmail, ToName: m.ToName, CustomerName: m.CustomerName,
		ContactId: m.ContactID, Subject: m.Subject, Body: m.Body,
		BodyText: m.BodyText, BodyFormat: m.BodyFormat,
		Attachments: signedAttachmentsToProto(files),
		Status:      m.Status, AttemptCount: m.AttemptCount, ProviderId: m.ProviderID,
		LastError: m.LastError, AttentionReason: m.AttentionReason,
		QueuedAt: ts(m.QueuedAt), SentAt: ts(m.SentAt),
		DeliveredAt: ts(m.DeliveredAt), OpenedAt: ts(m.OpenedAt),
		ClickedAt: ts(m.ClickedAt), Events: eventsToProto(v.Events),
		TrackingEnabled: v.TrackingEnabled,
	}}, nil
}

func eventsToProto(in []store.ListEventsOfMessageRow) []*mailv1.DeliveryEvent {
	out := make([]*mailv1.DeliveryEvent, 0, len(in))
	for _, e := range in {
		out = append(out, &mailv1.DeliveryEvent{
			Id: e.ID, Kind: e.Kind, OccurredAt: ts(e.OccurredAt),
			Ip: e.Ip, UserAgent: e.UserAgent, IsProxy: e.IsProxy,
			TargetUrl: e.TargetUrl, Detail: e.Detail,
		})
	}
	return out
}

func (h *Handler) RequeueMessage(ctx context.Context, req *mailv1.RequeueMessageRequest) (*mailv1.RequeueMessageResponse, error) {
	if err := h.svc.Requeue(ctx, grpcx.TenantID(ctx), req.GetId(),
		req.GetNewEmail(), operator(ctx)); err != nil {
		return nil, err
	}
	return &mailv1.RequeueMessageResponse{Ok: true}, nil
}

func (h *Handler) AbandonMessage(ctx context.Context, req *mailv1.AbandonMessageRequest) (*mailv1.AbandonMessageResponse, error) {
	if err := h.svc.Abandon(ctx, grpcx.TenantID(ctx), req.GetId(),
		req.GetReason(), operator(ctx)); err != nil {
		return nil, err
	}
	return &mailv1.AbandonMessageResponse{Ok: true}, nil
}

// ---------------------------------------------------------------- signatures

func (h *Handler) ListSignatures(ctx context.Context, _ *mailv1.ListSignaturesRequest) (*mailv1.ListSignaturesResponse, error) {
	rows, err := h.svc.ListSignatures(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.Signature, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mailv1.Signature{
			Id: r.ID, OwnerType: r.OwnerType, OwnerId: r.OwnerID,
			Name: r.Name, Content: r.Content, BodyFormat: r.BodyFormat,
			IsDefault: r.IsDefault,
		})
	}
	return &mailv1.ListSignaturesResponse{Signatures: out}, nil
}

func (h *Handler) CreateSignature(ctx context.Context, req *mailv1.CreateSignatureRequest) (*mailv1.CreateSignatureResponse, error) {
	id, err := h.svc.CreateSignature(ctx, grpcx.TenantID(ctx), app.SignatureInput{
		OwnerType: req.GetOwnerType(), Name: req.GetName(),
		Content: req.GetContent(), Format: req.GetBodyFormat(),
		IsDefault: req.GetIsDefault(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.CreateSignatureResponse{Id: id}, nil
}

func (h *Handler) UpdateSignature(ctx context.Context, req *mailv1.UpdateSignatureRequest) (*mailv1.UpdateSignatureResponse, error) {
	if err := h.svc.UpdateSignature(ctx, grpcx.TenantID(ctx), req.GetId(), app.SignatureInput{
		OwnerType: req.GetOwnerType(), Name: req.GetName(),
		Content: req.GetContent(), Format: req.GetBodyFormat(),
		IsDefault: req.GetIsDefault(),
	}, operator(ctx)); err != nil {
		return nil, err
	}
	return &mailv1.UpdateSignatureResponse{Ok: true}, nil
}

func (h *Handler) DeleteSignature(ctx context.Context, req *mailv1.DeleteSignatureRequest) (*mailv1.DeleteSignatureResponse, error) {
	if err := h.svc.DeleteSignature(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx)); err != nil {
		return nil, err
	}
	return &mailv1.DeleteSignatureResponse{Ok: true}, nil
}

// ---------------------------------------------------------------- templates

func (h *Handler) ListEmailTemplates(ctx context.Context, _ *mailv1.ListEmailTemplatesRequest) (*mailv1.ListEmailTemplatesResponse, error) {
	rows, err := h.svc.ListTemplates(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.EmailTemplate, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mailv1.EmailTemplate{
			Id: r.ID, OwnerType: r.OwnerType, OwnerId: r.OwnerID,
			Name: r.Name, Lang: r.Lang, Subject: r.Subject,
			Content: r.Content, BodyFormat: r.BodyFormat,
		})
	}
	return &mailv1.ListEmailTemplatesResponse{Templates: out}, nil
}

func (h *Handler) CreateEmailTemplate(ctx context.Context, req *mailv1.CreateEmailTemplateRequest) (*mailv1.CreateEmailTemplateResponse, error) {
	id, err := h.svc.CreateTemplate(ctx, grpcx.TenantID(ctx), app.TemplateInput{
		OwnerType: req.GetOwnerType(), Name: req.GetName(), Lang: req.GetLang(),
		Subject: req.GetSubject(), Content: req.GetContent(),
		Format: req.GetBodyFormat(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.CreateEmailTemplateResponse{Id: id}, nil
}

func (h *Handler) UpdateEmailTemplate(ctx context.Context, req *mailv1.UpdateEmailTemplateRequest) (*mailv1.UpdateEmailTemplateResponse, error) {
	if err := h.svc.UpdateTemplate(ctx, grpcx.TenantID(ctx), req.GetId(), app.TemplateInput{
		OwnerType: req.GetOwnerType(), Name: req.GetName(), Lang: req.GetLang(),
		Subject: req.GetSubject(), Content: req.GetContent(),
		Format: req.GetBodyFormat(),
	}, operator(ctx)); err != nil {
		return nil, err
	}
	return &mailv1.UpdateEmailTemplateResponse{Ok: true}, nil
}

func (h *Handler) DeleteEmailTemplate(ctx context.Context, req *mailv1.DeleteEmailTemplateRequest) (*mailv1.DeleteEmailTemplateResponse, error) {
	if err := h.svc.DeleteTemplate(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx)); err != nil {
		return nil, err
	}
	return &mailv1.DeleteEmailTemplateResponse{Ok: true}, nil
}

// ---------------------------------------------------------------- suppression

func (h *Handler) ListSuppressions(ctx context.Context, req *mailv1.ListSuppressionsRequest) (*mailv1.ListSuppressionsResponse, error) {
	rows, err := h.svc.ListSuppressions(ctx, grpcx.TenantID(ctx), req.GetKeyword())
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.Suppression, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mailv1.Suppression{
			Id: r.ID, Email: r.Email, Reason: r.Reason,
			Detail: r.Detail, CreatedAt: ts(r.CreatedAt),
		})
	}
	return &mailv1.ListSuppressionsResponse{Suppressions: out}, nil
}

func (h *Handler) AddSuppression(ctx context.Context, req *mailv1.AddSuppressionRequest) (*mailv1.AddSuppressionResponse, error) {
	if err := h.svc.Suppress(ctx, grpcx.TenantID(ctx),
		req.GetEmail(), req.GetReason(), req.GetDetail()); err != nil {
		return nil, err
	}
	return &mailv1.AddSuppressionResponse{Ok: true}, nil
}

func (h *Handler) RemoveSuppression(ctx context.Context, req *mailv1.RemoveSuppressionRequest) (*mailv1.RemoveSuppressionResponse, error) {
	if err := h.svc.Unsuppress(ctx, grpcx.TenantID(ctx), req.GetEmail()); err != nil {
		return nil, err
	}
	return &mailv1.RemoveSuppressionResponse{Ok: true}, nil
}

// ------------------------------------------------------ attachments & images

func (h *Handler) ListAttachments(ctx context.Context, req *mailv1.ListAttachmentsRequest) (*mailv1.ListAttachmentsResponse, error) {
	rows, err := h.svc.SignedAttachments(ctx, grpcx.TenantID(ctx), req.GetCampaignId())
	if err != nil {
		return nil, err
	}
	var total int64
	for _, a := range rows {
		total += a.FileSize
	}
	return &mailv1.ListAttachmentsResponse{
		Attachments: signedAttachmentsToProto(rows), TotalBytes: total,
	}, nil
}

func (h *Handler) ListImages(ctx context.Context, _ *mailv1.ListImagesRequest) (*mailv1.ListImagesResponse, error) {
	rows, err := h.svc.ListImages(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.EmailImage, 0, len(rows))
	for _, i := range rows {
		out = append(out, &mailv1.EmailImage{
			Id: i.ID, Token: i.Token, FileName: i.FileName, FileSize: i.FileSize,
			ContentType: i.ContentType, UploadedAt: ts(i.UploadedAt),
		})
	}
	return &mailv1.ListImagesResponse{Images: out}, nil
}

func (h *Handler) WithdrawImage(ctx context.Context, req *mailv1.WithdrawImageRequest) (*mailv1.WithdrawImageResponse, error) {
	if err := h.svc.WithdrawImage(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &mailv1.WithdrawImageResponse{Ok: true}, nil
}

func (h *Handler) PresignAttachment(ctx context.Context, req *mailv1.PresignAttachmentRequest) (*mailv1.PresignAttachmentResponse, error) {
	key, url, expires, err := h.svc.PresignAttachment(ctx, grpcx.TenantID(ctx), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &mailv1.PresignAttachmentResponse{
		FileKey: key, UploadUrl: url, ExpiresInSeconds: expires,
	}, nil
}

func (h *Handler) RegisterAttachment(ctx context.Context, req *mailv1.RegisterAttachmentRequest) (*mailv1.RegisterAttachmentResponse, error) {
	a, err := h.svc.RegisterAttachment(ctx, grpcx.TenantID(ctx),
		req.GetCampaignId(), req.GetFileName(), req.GetFileKey(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.RegisterAttachmentResponse{Attachment: &mailv1.EmailAttachment{
		Id: a.ID, FileName: a.FileName, FileSize: a.FileSize, ContentType: a.ContentType,
	}}, nil
}

func (h *Handler) PresignImage(ctx context.Context, req *mailv1.PresignImageRequest) (*mailv1.PresignImageResponse, error) {
	key, url, expires, err := h.svc.PresignImage(ctx, grpcx.TenantID(ctx), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &mailv1.PresignImageResponse{
		FileKey: key, UploadUrl: url, ExpiresInSeconds: expires,
	}, nil
}

func (h *Handler) RegisterImage(ctx context.Context, req *mailv1.RegisterImageRequest) (*mailv1.RegisterImageResponse, error) {
	i, err := h.svc.RegisterImage(ctx, grpcx.TenantID(ctx),
		req.GetFileName(), req.GetFileKey(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.RegisterImageResponse{Image: &mailv1.EmailImage{
		Id: i.ID, Token: i.Token, FileName: i.FileName,
		FileSize: i.FileSize, ContentType: i.ContentType,
	}}, nil
}

func (h *Handler) ImportImage(ctx context.Context, req *mailv1.ImportImageRequest) (*mailv1.ImportImageResponse, error) {
	i, err := h.svc.ImportImageFromURL(ctx, grpcx.TenantID(ctx), req.GetUrl(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.ImportImageResponse{Image: &mailv1.EmailImage{
		Id: i.ID, Token: i.Token, FileName: i.FileName,
		FileSize: i.FileSize, ContentType: i.ContentType,
	}}, nil
}

// FetchImage is reached from the public route, so it carries no tenant and no
// operator: the token is the whole credential.
func (h *Handler) FetchImage(ctx context.Context, req *mailv1.FetchImageRequest) (*mailv1.FetchImageResponse, error) {
	b, contentType, err := h.svc.OpenImage(ctx, req.GetToken())
	if err != nil {
		return nil, err
	}
	return &mailv1.FetchImageResponse{Content: b, ContentType: contentType}, nil
}

func (h *Handler) ListSenders(ctx context.Context, _ *mailv1.ListSendersRequest) (*mailv1.ListSendersResponse, error) {
	rows, err := h.svc.ListSenders(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.Sender, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mailv1.Sender{
			EmployeeId: r.SenderID, Name: r.SenderName,
			MessageCount: r.MessageCount, FailedCount: r.FailedCount,
			LastSentAt: ts(r.LastSentAt),
		})
	}
	return &mailv1.ListSendersResponse{Senders: out}, nil
}

// ---------------------------------------------------------------- drafts

func (h *Handler) SaveDraft(ctx context.Context, req *mailv1.SaveDraftRequest) (*mailv1.SaveDraftResponse, error) {
	id, err := h.svc.SaveDraft(ctx, grpcx.TenantID(ctx), app.DraftInput{
		ID: req.GetId(), Subject: req.GetSubject(), Body: req.GetBody(),
		Format: req.GetBodyFormat(), SignatureID: req.GetSignatureId(),
		Kind:                req.GetKind(),
		Recipients:          recipientsFromProto(req.GetRecipients()),
		Attachments:         pendingFromProto(req.GetAttachments()),
		SendMode:            req.GetSendMode(),
		CC:                  recipientsFromProto(req.GetCc()),
		BCC:                 recipientsFromProto(req.GetBcc()),
		ReplyToInboundID:    req.GetReplyToInboundId(),
		ForwardInboundID:    req.GetForwardInboundId(),
		ForwardAsAttachment: req.GetForwardAsAttachment(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.SaveDraftResponse{Id: id}, nil
}

func (h *Handler) ListDrafts(ctx context.Context, _ *mailv1.ListDraftsRequest) (*mailv1.ListDraftsResponse, error) {
	rows, err := h.svc.ListDrafts(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.Draft, 0, len(rows))
	for _, d := range rows {
		out = append(out, &mailv1.Draft{
			Id: d.ID, Subject: d.Subject, BodyFormat: d.BodyFormat, Kind: d.Kind,
			RecipientCount: d.RecipientCount, UpdatedAt: ts(d.UpdatedAt),
		})
	}
	return &mailv1.ListDraftsResponse{Drafts: out}, nil
}

func (h *Handler) GetDraft(ctx context.Context, req *mailv1.GetDraftRequest) (*mailv1.GetDraftResponse, error) {
	d, err := h.svc.GetDraft(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.GetDraftResponse{Draft: &mailv1.Draft{
		Id: d.ID, Subject: d.Subject, Body: d.Body, BodyFormat: d.Format,
		SignatureId: d.SignatureID, Kind: d.Kind, UpdatedAt: d.UpdatedAt,
		Recipients:          recipientsToProto(d.Recipients),
		Attachments:         pendingToProto(d.Attachments),
		SendMode:            d.SendMode,
		Cc:                  recipientsToProto(d.CC),
		Bcc:                 recipientsToProto(d.BCC),
		ReplyToInboundId:    d.ReplyToInboundID,
		ForwardInboundId:    d.ForwardInboundID,
		ForwardAsAttachment: d.ForwardAsAttachment,
	}}, nil
}

func (h *Handler) DeleteDraft(ctx context.Context, req *mailv1.DeleteDraftRequest) (*mailv1.DeleteDraftResponse, error) {
	if err := h.svc.DeleteDraft(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx)); err != nil {
		return nil, err
	}
	return &mailv1.DeleteDraftResponse{Ok: true}, nil
}

func (h *Handler) SendDraft(ctx context.Context, req *mailv1.SendDraftRequest) (*mailv1.SendDraftResponse, error) {
	at, err := scheduleAt(req.GetScheduledAt())
	if err != nil {
		return nil, err
	}
	res, err := h.svc.SendDraft(ctx, grpcx.TenantID(ctx), req.GetId(), at, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.SendDraftResponse{Result: &mailv1.CreateCampaignResponse{
		CampaignId: res.CampaignID, CampaignNo: res.CampaignNo,
		Queued:      int32(res.Queued),
		Suppressed:  skippedToProto(res.Suppressed),
		NeedsReview: skippedToProto(res.NeedsReview),
	}}, nil
}

// scheduleAt parses the requested delivery moment. Empty means now.
func scheduleAt(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, apierr.Invalid("NT_SCHEDULE_BAD", "定时发送时间格式不正确")
	}
	return at, nil
}

func (h *Handler) ListScheduled(ctx context.Context, req *mailv1.ListScheduledRequest) (*mailv1.ListScheduledResponse, error) {
	// Only the size is read; pages are reached by cursor.
	_, size := page(req.GetPage())
	if size < 1 {
		size = 20
	}
	sends, total, next, err := h.svc.ListScheduled(ctx, grpcx.TenantID(ctx), operator(ctx), size, req.GetCursor())
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.ScheduledSend, 0, len(sends))
	for _, s := range sends {
		out = append(out, &mailv1.ScheduledSend{
			CampaignId: s.CampaignID, CampaignNo: s.CampaignNo,
			Subject: s.Subject, PendingCount: s.PendingCount,
			ToNames:     s.ToNames,
			ScheduledAt: s.ScheduledAt.Format(time.RFC3339),
			SendMode:    s.SendMode, BodyFormat: s.BodyFormat,
		})
	}
	return &mailv1.ListScheduledResponse{
		Sends: out, Meta: meta(total, req.GetPage()), NextCursor: next,
	}, nil
}

func (h *Handler) SendScheduledNow(ctx context.Context, req *mailv1.SendScheduledNowRequest) (*mailv1.SendScheduledNowResponse, error) {
	n, err := h.svc.SendScheduledNow(ctx, grpcx.TenantID(ctx), req.GetCampaignId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.SendScheduledNowResponse{Released: int32(n)}, nil
}

func (h *Handler) CancelScheduled(ctx context.Context, req *mailv1.CancelScheduledRequest) (*mailv1.CancelScheduledResponse, error) {
	n, draft, err := h.svc.CancelScheduled(ctx, grpcx.TenantID(ctx), req.GetCampaignId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.CancelScheduledResponse{Cancelled: int32(n), DraftId: draft}, nil
}

func recipientsFromProto(in []*mailv1.Recipient) []app.Recipient {
	out := make([]app.Recipient, 0, len(in))
	for _, r := range in {
		out = append(out, recipientFromProto(r))
	}
	return out
}

func recipientsToProto(in []app.Recipient) []*mailv1.Recipient {
	out := make([]*mailv1.Recipient, 0, len(in))
	for _, r := range in {
		out = append(out, &mailv1.Recipient{
			ContactId: r.ContactID, Name: r.Name, Email: r.Email,
			CustomerId: r.CustomerID, CustomerName: r.CustomerName, Extra: r.Extra,
		})
	}
	return out
}

func pendingToProto(in []app.PendingAttachment) []*mailv1.PendingAttachment {
	out := make([]*mailv1.PendingAttachment, 0, len(in))
	for _, f := range in {
		out = append(out, &mailv1.PendingAttachment{FileName: f.FileName, FileKey: f.FileKey})
	}
	return out
}

func (h *Handler) GetMailHost(ctx context.Context, _ *mailv1.GetMailHostRequest) (*mailv1.GetMailHostResponse, error) {
	s, err := h.svc.GetMailHost(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	return &mailv1.GetMailHostResponse{Host: &mailv1.MailHost{
		Domain: s.Domain, SmtpHost: s.SMTPHost, SmtpPort: s.SMTPPort,
		SmtpSecurity: s.SMTPSecurity, ImapHost: s.IMAPHost, ImapPort: s.IMAPPort,
		ImapSecurity: s.IMAPSecurity, HourlyQuota: s.HourlyQuota, DailyQuota: s.DailyQuota,
	}}, nil
}

func (h *Handler) SaveMailHost(ctx context.Context, req *mailv1.SaveMailHostRequest) (*mailv1.SaveMailHostResponse, error) {
	in := req.GetHost()
	err := h.svc.SaveMailHost(ctx, grpcx.TenantID(ctx), app.MailHostSettings{
		Domain: in.GetDomain(), SMTPHost: in.GetSmtpHost(), SMTPPort: in.GetSmtpPort(),
		SMTPSecurity: in.GetSmtpSecurity(), IMAPHost: in.GetImapHost(), IMAPPort: in.GetImapPort(),
		IMAPSecurity: in.GetImapSecurity(), HourlyQuota: in.GetHourlyQuota(), DailyQuota: in.GetDailyQuota(),
	})
	if err != nil {
		return nil, err
	}
	return &mailv1.SaveMailHostResponse{Ok: true}, nil
}

func (h *Handler) GetMyMailAccount(ctx context.Context, _ *mailv1.GetMyMailAccountRequest) (*mailv1.GetMyMailAccountResponse, error) {
	op := operator(ctx)
	v, err := h.svc.GetMyMailAccount(ctx, grpcx.TenantID(ctx), op.ID)
	if err != nil {
		return nil, err
	}
	return &mailv1.GetMyMailAccountResponse{Account: &mailv1.MailAccount{
		Email: v.Email, Username: v.Username, HasSecret: v.HasSecret,
		VerifiedAt: v.VerifiedAt, LastError: v.LastError, IsActive: v.IsActive,
		AuthKind: v.AuthKind,
	}, ExcelAvailable: h.svc.ExcelAvailable()}, nil
}

func (h *Handler) RecordOpen(ctx context.Context, req *mailv1.RecordOpenRequest) (*mailv1.RecordOpenResponse, error) {
	// Never reports whether the key was real. The gateway serves the same
	// image either way, so telling it apart here would only create a way to
	// probe which sends exist.
	h.svc.RecordOpen(ctx, req.GetMessageKey(), req.GetUserAgent(), req.GetIp())
	return &mailv1.RecordOpenResponse{Ok: true}, nil
}

func inboundToProto(v app.InboundView) *mailv1.InboundMail {
	m := &mailv1.InboundMail{
		Id: v.ID, FromEmail: v.FromEmail, FromName: v.FromName,
		Subject: v.Subject, Snippet: v.Snippet, ThreadKey: v.ThreadKey,
		IsRead: v.IsRead, IsStarred: v.IsStarred, HasAttachments: v.HasAttachments,
		BodyHtml: v.BodyHTML, QuotedHtml: v.QuotedHTML,
		BodyText: v.BodyText, ToEmail: v.ToEmail,
		ThreadCount: v.ThreadCount,
		Kind:        v.Kind,
		ToName:      v.ToName,
		Status:      v.Status,
		HasRaw:      v.HasRaw,
	}
	if !v.OpenedAt.IsZero() {
		m.OpenedAt = v.OpenedAt.Format(time.RFC3339)
	}
	if !v.ReceivedAt.IsZero() {
		m.ReceivedAt = v.ReceivedAt.Format(time.RFC3339)
	}
	if !v.SentAt.IsZero() {
		m.SentAt = v.SentAt.Format(time.RFC3339)
	}
	for _, a := range v.Attachments {
		m.Attachments = append(m.Attachments, &mailv1.InboundAttachment{
			Id: a.ID, FileName: a.FileName, ContentType: a.ContentType,
			FileSize: a.FileSize, DownloadUrl: a.DownloadURL,
			PreviewUrl: a.PreviewURL, Stored: a.FileKey != "",
		})
	}
	return m
}

func (h *Handler) ListInbound(ctx context.Context, req *mailv1.ListInboundRequest) (*mailv1.ListInboundResponse, error) {
	op := operator(ctx)
	p, err := h.svc.ListInbound(ctx, grpcx.TenantID(ctx), op.ID,
		req.GetKeyword(), req.GetView(), req.GetCursor(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.InboundMail, 0, len(p.Mails))
	for _, r := range p.Mails {
		out = append(out, inboundToProto(r))
	}
	return &mailv1.ListInboundResponse{
		Mails: out, UnreadCount: p.Unread, NextCursor: p.NextCursor,
		Meta: &commonv1.PageMeta{Total: p.Total},
	}, nil
}

func (h *Handler) SearchMail(ctx context.Context, req *mailv1.SearchMailRequest) (*mailv1.SearchMailResponse, error) {
	op := operator(ctx)
	p, err := h.svc.SearchMail(ctx, grpcx.TenantID(ctx), op.ID,
		req.GetKeyword(), req.GetCursor(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	hits := make([]*mailv1.SearchHit, 0, len(p.Hits))
	for _, h := range p.Hits {
		hits = append(hits, &mailv1.SearchHit{
			Mail:         inboundToProto(h.InboundView),
			Folder:       h.Folder,
			MatchSnippet: h.MatchSnippet,
		})
	}
	return &mailv1.SearchMailResponse{
		Hits: hits, NextCursor: p.NextCursor,
		Meta: &commonv1.PageMeta{Total: p.Total},
	}, nil
}

func (h *Handler) GetInbound(ctx context.Context, req *mailv1.GetInboundRequest) (*mailv1.GetInboundResponse, error) {
	op := operator(ctx)
	v, err := h.svc.GetInbound(ctx, grpcx.TenantID(ctx), op.ID, req.GetId())
	if err != nil {
		return nil, err
	}
	return &mailv1.GetInboundResponse{Mail: inboundToProto(v)}, nil
}

func (h *Handler) ConvertInboundToExcel(ctx context.Context, req *mailv1.ConvertInboundToExcelRequest) (*mailv1.ConvertInboundToExcelResponse, error) {
	op := operator(ctx)
	result, err := h.svc.ConvertInboundToExcel(
		ctx, grpcx.TenantID(ctx), op.ID, req.GetId(),
		req.AttachmentId, req.SelectedText, req.GetLocale(), nil,
	)
	if err != nil {
		return nil, err
	}
	return excelResultToProto(result), nil
}

func (h *Handler) StartInboundExcelConversion(ctx context.Context, req *mailv1.StartInboundExcelConversionRequest) (*mailv1.StartInboundExcelConversionResponse, error) {
	op := operator(ctx)
	columns := make([]app.InquiryColumn, 0, len(req.GetTemplateColumns()))
	for _, column := range req.GetTemplateColumns() {
		columns = append(columns, app.InquiryColumn{
			FieldKey: column.GetFieldKey(), DisplayName: column.GetDisplayName(),
			DataType: column.GetDataType(), IsRequired: column.GetIsRequired(),
			DefaultValue: column.GetDefaultValue(),
		})
	}
	job, err := h.svc.StartExcelJob(
		ctx, grpcx.TenantID(ctx), op.ID, req.GetId(),
		req.AttachmentId, req.SelectedText, req.GetLocale(), columns,
	)
	if err != nil {
		return nil, err
	}
	return &mailv1.StartInboundExcelConversionResponse{Job: excelJobToProto(job)}, nil
}

func (h *Handler) GetInboundExcelConversionJob(ctx context.Context, req *mailv1.GetInboundExcelConversionJobRequest) (*mailv1.GetInboundExcelConversionJobResponse, error) {
	op := operator(ctx)
	job, err := h.svc.GetExcelJob(ctx, grpcx.TenantID(ctx), op.ID, req.GetId())
	if err != nil {
		return nil, err
	}
	return &mailv1.GetInboundExcelConversionJobResponse{Job: excelJobToProto(job)}, nil
}

func excelJobToProto(job app.ExcelJob) *mailv1.ExcelConversionJob {
	out := &mailv1.ExcelConversionJob{
		Id: job.ID, Status: job.Status, ErrorCode: job.ErrorCode,
		ErrorMessage: job.ErrorMessage,
	}
	if !job.CreatedAt.IsZero() {
		out.CreatedAt = job.CreatedAt.Format(time.RFC3339)
	}
	if !job.CompletedAt.IsZero() {
		out.CompletedAt = job.CompletedAt.Format(time.RFC3339)
	}
	if job.Status == "COMPLETED" {
		out.Result = excelResultToProto(job.Result)
	}
	return out
}

func excelResultToProto(result app.ExcelResult) *mailv1.ConvertInboundToExcelResponse {
	resp := &mailv1.ConvertInboundToExcelResponse{
		FileName: result.FileName, FileData: result.Data, Model: result.Model,
	}
	for _, sheet := range result.Workbook.Sheets {
		preview := &mailv1.ExcelSheetPreview{
			Name: sheet.Name, Summary: sheet.Summary, Columns: sheet.Columns,
			TotalRows: int64(len(sheet.Rows)), ColumnKeys: sheet.ColumnKeys,
		}
		rows := sheet.Rows
		if len(rows) > 200 {
			rows = rows[:200]
		}
		for _, row := range rows {
			preview.Rows = append(preview.Rows, &mailv1.ExcelSheetPreview_Row{Cells: row})
		}
		resp.Sheets = append(resp.Sheets, preview)
	}
	return resp
}

func (h *Handler) GetMailThread(ctx context.Context, req *mailv1.GetMailThreadRequest) (*mailv1.GetMailThreadResponse, error) {
	op := operator(ctx)
	items, err := h.svc.GetMailThread(ctx, grpcx.TenantID(ctx), op.ID, req.GetThreadKey())
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.ThreadItem, 0, len(items))
	for _, v := range items {
		it := &mailv1.ThreadItem{
			Direction: v.Direction, Id: v.ID, Subject: v.Subject,
			Body: v.Body, Quoted: v.Quoted, BodyFormat: v.BodyFormat,
			Counterparty: v.Counterparty, Who: v.Who,
		}
		if !v.At.IsZero() {
			it.At = v.At.Format(time.RFC3339)
		}
		out = append(out, it)
	}
	return &mailv1.GetMailThreadResponse{Items: out}, nil
}

func (h *Handler) ExportMailThread(ctx context.Context, req *mailv1.ExportMailThreadRequest) (*mailv1.ExportMailThreadResponse, error) {
	doc, err := h.svc.ExportMailThread(ctx, grpcx.TenantID(ctx), operator(ctx), app.ExportRequest{
		ThreadKey: req.GetThreadKey(),
		Zone:      req.GetZone(),
		Lang:      req.GetLang(),
		ClientIP:  req.GetClientIp(),
	})
	if err != nil {
		return nil, err
	}
	return &mailv1.ExportMailThreadResponse{
		Content:     doc.Content,
		ContentType: doc.ContentType,
		FileName:    doc.FileName,
		TurnCount:   int32(doc.TurnCount),
	}, nil
}

func (h *Handler) ListMailExports(ctx context.Context, req *mailv1.ListMailExportsRequest) (*mailv1.ListMailExportsResponse, error) {
	rows, total, err := h.svc.ListExports(ctx, grpcx.TenantID(ctx), operator(ctx), req.GetPage(), req.GetSize())
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.ExportRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mailv1.ExportRecord{
			Id: r.ID, EmployeeId: r.EmployeeID, EmployeeName: r.EmployeeName,
			ThreadKey: r.ThreadKey, Subject: r.Subject, Counterparty: r.Counterparty,
			TurnCount: r.TurnCount, ByteSize: r.ByteSize, Format: r.Format,
			ClientIp: r.ClientIp, ExportedAt: ts(r.ExportedAt),
		})
	}
	return &mailv1.ListMailExportsResponse{Items: out, Total: total}, nil
}

func (h *Handler) MarkInbound(ctx context.Context, req *mailv1.MarkInboundRequest) (*mailv1.MarkInboundResponse, error) {
	op := operator(ctx)
	err := h.svc.MarkInbound(ctx, grpcx.TenantID(ctx), op.ID, req.GetId(),
		req.Read, req.Starred, req.Archived, req.Deleted, req.NotJunk,
		req.GetWholeThread())
	if err != nil {
		return nil, err
	}
	return &mailv1.MarkInboundResponse{Ok: true}, nil
}

func (h *Handler) PurgeInbound(ctx context.Context, req *mailv1.PurgeInboundRequest) (*mailv1.PurgeInboundResponse, error) {
	op := operator(ctx)
	if err := h.svc.PurgeInbound(ctx, grpcx.TenantID(ctx), op.ID, req.GetId(), req.GetWholeThread()); err != nil {
		return nil, err
	}
	return &mailv1.PurgeInboundResponse{Ok: true}, nil
}

func (h *Handler) MarkViewRead(ctx context.Context, req *mailv1.MarkViewReadRequest) (*mailv1.MarkViewReadResponse, error) {
	op := operator(ctx)
	n, err := h.svc.MarkViewRead(ctx, grpcx.TenantID(ctx), op.ID, req.GetView())
	if err != nil {
		return nil, err
	}
	return &mailv1.MarkViewReadResponse{Marked: int32(n)}, nil
}

func (h *Handler) EmptyTrash(ctx context.Context, _ *mailv1.EmptyTrashRequest) (*mailv1.EmptyTrashResponse, error) {
	op := operator(ctx)
	n, err := h.svc.EmptyTrash(ctx, grpcx.TenantID(ctx), op.ID)
	if err != nil {
		return nil, err
	}
	return &mailv1.EmptyTrashResponse{Deleted: int32(n)}, nil
}

func (h *Handler) EmptyJunk(ctx context.Context, _ *mailv1.EmptyJunkRequest) (*mailv1.EmptyJunkResponse, error) {
	op := operator(ctx)
	n, err := h.svc.EmptyJunk(ctx, grpcx.TenantID(ctx), op.ID)
	if err != nil {
		return nil, err
	}
	return &mailv1.EmptyJunkResponse{Deleted: int32(n)}, nil
}

func (h *Handler) SyncMailbox(ctx context.Context, _ *mailv1.SyncMailboxRequest) (*mailv1.SyncMailboxResponse, error) {
	op := operator(ctx)
	n, pending, err := h.svc.SyncNow(ctx, grpcx.TenantID(ctx), op.ID)
	if err != nil {
		return &mailv1.SyncMailboxResponse{Fetched: 0, Detail: err.Error()}, nil
	}
	// pending 不填 Detail：Detail 是给错误用的，前端见到它就弹红字。还在收
	// 不是错误。
	return &mailv1.SyncMailboxResponse{Fetched: int32(n), Pending: pending}, nil
}

func (h *Handler) ListMailboxSent(ctx context.Context, req *mailv1.ListMailboxSentRequest) (*mailv1.ListMailboxSentResponse, error) {
	op := operator(ctx)
	page, err := h.svc.ListMailboxSent(ctx, grpcx.TenantID(ctx), op.ID,
		req.GetKeyword(), req.GetCursor(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*mailv1.InboundMail, 0, len(page.Mails))
	for _, r := range page.Mails {
		out = append(out, inboundToProto(r))
	}
	return &mailv1.ListMailboxSentResponse{
		Mails: out, Meta: &commonv1.PageMeta{Total: page.Total},
		NextCursor: page.NextCursor,
	}, nil
}

func (h *Handler) VerifyMailAccess(ctx context.Context, req *mailv1.VerifyMailAccessRequest) (*mailv1.VerifyMailAccessResponse, error) {
	op := operator(ctx)
	detail, err := h.svc.VerifyMailSecret(ctx, grpcx.TenantID(ctx), op.ID, req.GetEmail(), req.GetSecret())
	if err != nil {
		// HostRejected separates "your code is wrong" from "we could not even
		// try". Only the first cost a real login against the mail host, and
		// only the first should cost the caller an attempt.
		return &mailv1.VerifyMailAccessResponse{
			Ok: false, Detail: err.Error(), HostRejected: app.FromMailHost(err),
		}, nil
	}
	return &mailv1.VerifyMailAccessResponse{Ok: true, Detail: detail}, nil
}

func (h *Handler) CompleteGoogleOAuth(ctx context.Context, req *mailv1.CompleteGoogleOAuthRequest) (*mailv1.CompleteGoogleOAuthResponse, error) {
	op := operator(ctx)
	email, err := h.svc.CompleteGoogleOAuth(ctx, grpcx.TenantID(ctx), op.ID,
		req.GetCode(), req.GetRedirectUri(), req.GetExpectEmail())
	if err != nil {
		return nil, err
	}
	return &mailv1.CompleteGoogleOAuthResponse{Email: email}, nil
}
