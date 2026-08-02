// Package grpcin exposes correspondence over gRPC.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	ntv1 "github.com/sgao19/erp-go/gen/go/erp/notification/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/notification/internal/app"
	"github.com/sgao19/erp-go/services/notification/internal/store"
)

type Handler struct {
	ntv1.UnimplementedEmailServiceServer
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

func (h *Handler) ListCampaigns(ctx context.Context, req *ntv1.ListCampaignsRequest) (*ntv1.ListCampaignsResponse, error) {
	pg, size := page(req.GetPage())
	rows, total, err := h.svc.ListCampaigns(ctx, grpcx.TenantID(ctx), app.CampaignQuery{
		Keyword: req.GetKeyword(), SenderID: req.GetSenderId(), Page: pg, Size: size,
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.Campaign, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ntv1.Campaign{
			Id: r.ID, CampaignNo: r.CampaignNo, Subject: r.SubjectTpl, Kind: r.Kind,
			SenderId: r.SenderID, SenderName: r.SenderName, Status: r.Status,
			CreatedAt: ts(r.CreatedAt), FinishedAt: ts(r.FinishedAt),
			ToNames:    r.ToNames,
			TotalCount: r.TotalCount, SentCount: r.SentCount,
			DeliveredCount: r.DeliveredCount, FailedCount: r.FailedCount,
			PendingCount: r.PendingCount,
		})
	}
	return &ntv1.ListCampaignsResponse{Campaigns: out, Meta: meta(total, req.GetPage())}, nil
}

func (h *Handler) GetCampaign(ctx context.Context, req *ntv1.GetCampaignRequest) (*ntv1.GetCampaignResponse, error) {
	c, err := h.svc.GetCampaign(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	files, err := h.svc.ListAttachments(ctx, grpcx.TenantID(ctx), c.ID)
	if err != nil {
		return nil, err
	}
	return &ntv1.GetCampaignResponse{Campaign: &ntv1.CampaignDetail{
		Id: c.ID, CampaignNo: c.CampaignNo, SubjectTpl: c.SubjectTpl, BodyTpl: c.BodyTpl,
		BodyTextTpl: c.BodyTextTpl, BodyFormat: c.BodyFormat,
		Attachments: attachmentsToProto(files),
		SignatureId: c.SignatureID, Kind: c.Kind, SenderId: c.SenderID,
		SenderName: c.SenderName, SenderEmail: c.SenderEmail, Status: c.Status,
		CreatedAt: ts(c.CreatedAt), FinishedAt: ts(c.FinishedAt),
	}}, nil
}

func attachmentsToProto(in []store.ListAttachmentsRow) []*ntv1.EmailAttachment {
	out := make([]*ntv1.EmailAttachment, 0, len(in))
	for _, a := range in {
		out = append(out, &ntv1.EmailAttachment{
			Id: a.ID, FileName: a.FileName, FileSize: a.FileSize,
			ContentType: a.ContentType, UploadedAt: ts(a.UploadedAt),
		})
	}
	return out
}

func (h *Handler) PreviewCampaign(ctx context.Context, req *ntv1.PreviewCampaignRequest) (*ntv1.PreviewCampaignResponse, error) {
	p, err := h.svc.Preview(ctx, grpcx.TenantID(ctx), app.CampaignInput{
		Subject: req.GetSubject(), Body: req.GetBody(),
		Format: req.GetBodyFormat(), SignatureID: req.GetSignatureId(),
	}, recipientFromProto(req.GetRecipient()), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.PreviewCampaignResponse{
		Subject: p.Subject, Body: p.Body, BodyText: p.BodyText,
		BodyFormat: p.Format, MissingVariables: p.Missing,
	}, nil
}

func (h *Handler) CreateCampaign(ctx context.Context, req *ntv1.CreateCampaignRequest) (*ntv1.CreateCampaignResponse, error) {
	recipients := make([]app.Recipient, 0, len(req.GetRecipients()))
	for _, r := range req.GetRecipients() {
		recipients = append(recipients, recipientFromProto(r))
	}
	cc := make([]app.Recipient, 0, len(req.GetCc()))
	for _, r := range req.GetCc() {
		cc = append(cc, recipientFromProto(r))
	}
	res, err := h.svc.CreateCampaign(ctx, grpcx.TenantID(ctx), app.CampaignInput{
		Subject: req.GetSubject(), Body: req.GetBody(),
		SignatureID: req.GetSignatureId(), Kind: req.GetKind(),
		Format: req.GetBodyFormat(), Recipients: recipients,
		SendMode: req.GetSendMode(), CC: cc,
		ReplyToInboundID: req.GetReplyToInboundId(),
		ForwardInboundID: req.GetForwardInboundId(),
		Attachments:      pendingFromProto(req.GetAttachments()),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.CreateCampaignResponse{
		CampaignId: res.CampaignID, CampaignNo: res.CampaignNo,
		Queued:      int32(res.Queued),
		Suppressed:  skippedToProto(res.Suppressed),
		NeedsReview: skippedToProto(res.NeedsReview),
	}, nil
}

func pendingFromProto(in []*ntv1.PendingAttachment) []app.PendingAttachment {
	out := make([]app.PendingAttachment, 0, len(in))
	for _, f := range in {
		out = append(out, app.PendingAttachment{
			FileName: f.GetFileName(), FileKey: f.GetFileKey(),
		})
	}
	return out
}

func recipientFromProto(r *ntv1.Recipient) app.Recipient {
	if r == nil {
		return app.Recipient{}
	}
	return app.Recipient{
		ContactID: r.GetContactId(), Name: r.GetName(), Email: r.GetEmail(),
		CustomerID: r.GetCustomerId(), CustomerName: r.GetCustomerName(),
		Extra: r.GetExtra(),
	}
}

func skippedToProto(in []app.SkippedRecipient) []*ntv1.SkippedRecipient {
	out := make([]*ntv1.SkippedRecipient, 0, len(in))
	for _, s := range in {
		out = append(out, &ntv1.SkippedRecipient{Email: s.Email, Name: s.Name, Reason: s.Reason})
	}
	return out
}

// ---------------------------------------------------------------- messages

func (h *Handler) ListMessages(ctx context.Context, req *ntv1.ListMessagesRequest) (*ntv1.ListMessagesResponse, error) {
	pg, size := page(req.GetPage())
	rows, total, err := h.svc.ListMessages(ctx, grpcx.TenantID(ctx), app.MessageQuery{
		CampaignID: req.GetCampaignId(), SenderID: req.GetSenderId(),
		Status:        req.GetStatus(),
		AttentionOnly: req.GetAttentionOnly(), Keyword: req.GetKeyword(),
		Page: pg, Size: size,
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.EmailMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ntv1.EmailMessage{
			Id: r.ID, CampaignId: r.CampaignID, Kind: r.Kind,
			SenderId: r.SenderID, SenderName: r.SenderName,
			ToEmail: r.ToEmail, ToName: r.ToName, CustomerName: r.CustomerName,
			Subject: r.Subject, Status: r.Status, AttemptCount: r.AttemptCount,
			LastError: r.LastError, AttentionReason: r.AttentionReason,
			QueuedAt: ts(r.QueuedAt), SentAt: ts(r.SentAt),
			DeliveredAt: ts(r.DeliveredAt), OpenedAt: ts(r.OpenedAt),
		})
	}
	return &ntv1.ListMessagesResponse{Messages: out, Meta: meta(total, req.GetPage())}, nil
}

func (h *Handler) GetMessage(ctx context.Context, req *ntv1.GetMessageRequest) (*ntv1.GetMessageResponse, error) {
	v, err := h.svc.GetMessage(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	m := v.Message
	// The files are the campaign's, so a message that was not part of one
	// simply has none.
	var files []store.ListAttachmentsRow
	if m.CampaignID != 0 {
		if files, err = h.svc.ListAttachments(ctx, grpcx.TenantID(ctx), m.CampaignID); err != nil {
			return nil, err
		}
	}
	return &ntv1.GetMessageResponse{Message: &ntv1.MessageDetail{
		Id: m.ID, CampaignId: m.CampaignID, MessageKey: m.MessageKey, Kind: m.Kind,
		SenderId: m.SenderID, SenderName: m.SenderName,
		ToEmail: m.ToEmail, ToName: m.ToName, CustomerName: m.CustomerName,
		ContactId: m.ContactID, Subject: m.Subject, Body: m.Body,
		BodyText: m.BodyText, BodyFormat: m.BodyFormat,
		Attachments: attachmentsToProto(files),
		Status:      m.Status, AttemptCount: m.AttemptCount, ProviderId: m.ProviderID,
		LastError: m.LastError, AttentionReason: m.AttentionReason,
		QueuedAt: ts(m.QueuedAt), SentAt: ts(m.SentAt),
		DeliveredAt: ts(m.DeliveredAt), OpenedAt: ts(m.OpenedAt),
		ClickedAt: ts(m.ClickedAt), Events: eventsToProto(v.Events),
	}}, nil
}

func eventsToProto(in []store.ListEventsOfMessageRow) []*ntv1.DeliveryEvent {
	out := make([]*ntv1.DeliveryEvent, 0, len(in))
	for _, e := range in {
		out = append(out, &ntv1.DeliveryEvent{
			Id: e.ID, Kind: e.Kind, OccurredAt: ts(e.OccurredAt),
			Ip: e.Ip, UserAgent: e.UserAgent, IsProxy: e.IsProxy,
			TargetUrl: e.TargetUrl, Detail: e.Detail,
		})
	}
	return out
}

func (h *Handler) RequeueMessage(ctx context.Context, req *ntv1.RequeueMessageRequest) (*ntv1.RequeueMessageResponse, error) {
	if err := h.svc.Requeue(ctx, grpcx.TenantID(ctx), req.GetId(),
		req.GetNewEmail(), operator(ctx)); err != nil {
		return nil, err
	}
	return &ntv1.RequeueMessageResponse{Ok: true}, nil
}

func (h *Handler) AbandonMessage(ctx context.Context, req *ntv1.AbandonMessageRequest) (*ntv1.AbandonMessageResponse, error) {
	if err := h.svc.Abandon(ctx, grpcx.TenantID(ctx), req.GetId(),
		req.GetReason(), operator(ctx)); err != nil {
		return nil, err
	}
	return &ntv1.AbandonMessageResponse{Ok: true}, nil
}

// ---------------------------------------------------------------- signatures

func (h *Handler) ListSignatures(ctx context.Context, _ *ntv1.ListSignaturesRequest) (*ntv1.ListSignaturesResponse, error) {
	rows, err := h.svc.ListSignatures(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.Signature, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ntv1.Signature{
			Id: r.ID, OwnerType: r.OwnerType, OwnerId: r.OwnerID,
			Name: r.Name, Content: r.Content, BodyFormat: r.BodyFormat,
			IsDefault: r.IsDefault,
		})
	}
	return &ntv1.ListSignaturesResponse{Signatures: out}, nil
}

func (h *Handler) CreateSignature(ctx context.Context, req *ntv1.CreateSignatureRequest) (*ntv1.CreateSignatureResponse, error) {
	id, err := h.svc.CreateSignature(ctx, grpcx.TenantID(ctx), app.SignatureInput{
		OwnerType: req.GetOwnerType(), Name: req.GetName(),
		Content: req.GetContent(), Format: req.GetBodyFormat(),
		IsDefault: req.GetIsDefault(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.CreateSignatureResponse{Id: id}, nil
}

func (h *Handler) DeleteSignature(ctx context.Context, req *ntv1.DeleteSignatureRequest) (*ntv1.DeleteSignatureResponse, error) {
	if err := h.svc.DeleteSignature(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &ntv1.DeleteSignatureResponse{Ok: true}, nil
}

// ---------------------------------------------------------------- suppression

func (h *Handler) ListSuppressions(ctx context.Context, req *ntv1.ListSuppressionsRequest) (*ntv1.ListSuppressionsResponse, error) {
	rows, err := h.svc.ListSuppressions(ctx, grpcx.TenantID(ctx), req.GetKeyword())
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.Suppression, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ntv1.Suppression{
			Id: r.ID, Email: r.Email, Reason: r.Reason,
			Detail: r.Detail, CreatedAt: ts(r.CreatedAt),
		})
	}
	return &ntv1.ListSuppressionsResponse{Suppressions: out}, nil
}

func (h *Handler) AddSuppression(ctx context.Context, req *ntv1.AddSuppressionRequest) (*ntv1.AddSuppressionResponse, error) {
	if err := h.svc.Suppress(ctx, grpcx.TenantID(ctx),
		req.GetEmail(), req.GetReason(), req.GetDetail()); err != nil {
		return nil, err
	}
	return &ntv1.AddSuppressionResponse{Ok: true}, nil
}

func (h *Handler) RemoveSuppression(ctx context.Context, req *ntv1.RemoveSuppressionRequest) (*ntv1.RemoveSuppressionResponse, error) {
	if err := h.svc.Unsuppress(ctx, grpcx.TenantID(ctx), req.GetEmail()); err != nil {
		return nil, err
	}
	return &ntv1.RemoveSuppressionResponse{Ok: true}, nil
}

// ------------------------------------------------------ attachments & images

func (h *Handler) ListAttachments(ctx context.Context, req *ntv1.ListAttachmentsRequest) (*ntv1.ListAttachmentsResponse, error) {
	rows, err := h.svc.ListAttachments(ctx, grpcx.TenantID(ctx), req.GetCampaignId())
	if err != nil {
		return nil, err
	}
	var total int64
	for _, a := range rows {
		total += a.FileSize
	}
	return &ntv1.ListAttachmentsResponse{
		Attachments: attachmentsToProto(rows), TotalBytes: total,
	}, nil
}

func (h *Handler) ListImages(ctx context.Context, _ *ntv1.ListImagesRequest) (*ntv1.ListImagesResponse, error) {
	rows, err := h.svc.ListImages(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.EmailImage, 0, len(rows))
	for _, i := range rows {
		out = append(out, &ntv1.EmailImage{
			Id: i.ID, Token: i.Token, FileName: i.FileName, FileSize: i.FileSize,
			ContentType: i.ContentType, UploadedAt: ts(i.UploadedAt),
		})
	}
	return &ntv1.ListImagesResponse{Images: out}, nil
}

func (h *Handler) WithdrawImage(ctx context.Context, req *ntv1.WithdrawImageRequest) (*ntv1.WithdrawImageResponse, error) {
	if err := h.svc.WithdrawImage(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &ntv1.WithdrawImageResponse{Ok: true}, nil
}

func (h *Handler) PresignAttachment(ctx context.Context, req *ntv1.PresignAttachmentRequest) (*ntv1.PresignAttachmentResponse, error) {
	key, url, expires, err := h.svc.PresignAttachment(ctx, grpcx.TenantID(ctx), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &ntv1.PresignAttachmentResponse{
		FileKey: key, UploadUrl: url, ExpiresInSeconds: expires,
	}, nil
}

func (h *Handler) RegisterAttachment(ctx context.Context, req *ntv1.RegisterAttachmentRequest) (*ntv1.RegisterAttachmentResponse, error) {
	a, err := h.svc.RegisterAttachment(ctx, grpcx.TenantID(ctx),
		req.GetCampaignId(), req.GetFileName(), req.GetFileKey(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.RegisterAttachmentResponse{Attachment: &ntv1.EmailAttachment{
		Id: a.ID, FileName: a.FileName, FileSize: a.FileSize, ContentType: a.ContentType,
	}}, nil
}

func (h *Handler) PresignImage(ctx context.Context, req *ntv1.PresignImageRequest) (*ntv1.PresignImageResponse, error) {
	key, url, expires, err := h.svc.PresignImage(ctx, grpcx.TenantID(ctx), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &ntv1.PresignImageResponse{
		FileKey: key, UploadUrl: url, ExpiresInSeconds: expires,
	}, nil
}

func (h *Handler) RegisterImage(ctx context.Context, req *ntv1.RegisterImageRequest) (*ntv1.RegisterImageResponse, error) {
	i, err := h.svc.RegisterImage(ctx, grpcx.TenantID(ctx),
		req.GetFileName(), req.GetFileKey(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.RegisterImageResponse{Image: &ntv1.EmailImage{
		Id: i.ID, Token: i.Token, FileName: i.FileName,
		FileSize: i.FileSize, ContentType: i.ContentType,
	}}, nil
}

// FetchImage is reached from the public route, so it carries no tenant and no
// operator: the token is the whole credential.
func (h *Handler) FetchImage(ctx context.Context, req *ntv1.FetchImageRequest) (*ntv1.FetchImageResponse, error) {
	b, contentType, err := h.svc.OpenImage(ctx, req.GetToken())
	if err != nil {
		return nil, err
	}
	return &ntv1.FetchImageResponse{Content: b, ContentType: contentType}, nil
}

func (h *Handler) ListSenders(ctx context.Context, _ *ntv1.ListSendersRequest) (*ntv1.ListSendersResponse, error) {
	rows, err := h.svc.ListSenders(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.Sender, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ntv1.Sender{
			EmployeeId: r.SenderID, Name: r.SenderName,
			MessageCount: r.MessageCount, FailedCount: r.FailedCount,
			LastSentAt: ts(r.LastSentAt),
		})
	}
	return &ntv1.ListSendersResponse{Senders: out}, nil
}

// ---------------------------------------------------------------- drafts

func (h *Handler) SaveDraft(ctx context.Context, req *ntv1.SaveDraftRequest) (*ntv1.SaveDraftResponse, error) {
	id, err := h.svc.SaveDraft(ctx, grpcx.TenantID(ctx), app.DraftInput{
		ID: req.GetId(), Subject: req.GetSubject(), Body: req.GetBody(),
		Format: req.GetBodyFormat(), SignatureID: req.GetSignatureId(),
		Kind:        req.GetKind(),
		Recipients:  recipientsFromProto(req.GetRecipients()),
		Attachments: pendingFromProto(req.GetAttachments()),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.SaveDraftResponse{Id: id}, nil
}

func (h *Handler) ListDrafts(ctx context.Context, _ *ntv1.ListDraftsRequest) (*ntv1.ListDraftsResponse, error) {
	rows, err := h.svc.ListDrafts(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.Draft, 0, len(rows))
	for _, d := range rows {
		out = append(out, &ntv1.Draft{
			Id: d.ID, Subject: d.Subject, BodyFormat: d.BodyFormat, Kind: d.Kind,
			RecipientCount: d.RecipientCount, UpdatedAt: ts(d.UpdatedAt),
		})
	}
	return &ntv1.ListDraftsResponse{Drafts: out}, nil
}

func (h *Handler) GetDraft(ctx context.Context, req *ntv1.GetDraftRequest) (*ntv1.GetDraftResponse, error) {
	d, err := h.svc.GetDraft(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.GetDraftResponse{Draft: &ntv1.Draft{
		Id: d.ID, Subject: d.Subject, Body: d.Body, BodyFormat: d.Format,
		SignatureId: d.SignatureID, Kind: d.Kind, UpdatedAt: d.UpdatedAt,
		Recipients:  recipientsToProto(d.Recipients),
		Attachments: pendingToProto(d.Attachments),
	}}, nil
}

func (h *Handler) DeleteDraft(ctx context.Context, req *ntv1.DeleteDraftRequest) (*ntv1.DeleteDraftResponse, error) {
	if err := h.svc.DeleteDraft(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx)); err != nil {
		return nil, err
	}
	return &ntv1.DeleteDraftResponse{Ok: true}, nil
}

func (h *Handler) SendDraft(ctx context.Context, req *ntv1.SendDraftRequest) (*ntv1.SendDraftResponse, error) {
	res, err := h.svc.SendDraft(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.SendDraftResponse{Result: &ntv1.CreateCampaignResponse{
		CampaignId: res.CampaignID, CampaignNo: res.CampaignNo,
		Queued:      int32(res.Queued),
		Suppressed:  skippedToProto(res.Suppressed),
		NeedsReview: skippedToProto(res.NeedsReview),
	}}, nil
}

func recipientsFromProto(in []*ntv1.Recipient) []app.Recipient {
	out := make([]app.Recipient, 0, len(in))
	for _, r := range in {
		out = append(out, recipientFromProto(r))
	}
	return out
}

func recipientsToProto(in []app.Recipient) []*ntv1.Recipient {
	out := make([]*ntv1.Recipient, 0, len(in))
	for _, r := range in {
		out = append(out, &ntv1.Recipient{
			ContactId: r.ContactID, Name: r.Name, Email: r.Email,
			CustomerId: r.CustomerID, CustomerName: r.CustomerName, Extra: r.Extra,
		})
	}
	return out
}

func pendingToProto(in []app.PendingAttachment) []*ntv1.PendingAttachment {
	out := make([]*ntv1.PendingAttachment, 0, len(in))
	for _, f := range in {
		out = append(out, &ntv1.PendingAttachment{FileName: f.FileName, FileKey: f.FileKey})
	}
	return out
}

func (h *Handler) GetMailHost(ctx context.Context, _ *ntv1.GetMailHostRequest) (*ntv1.GetMailHostResponse, error) {
	s, err := h.svc.GetMailHost(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	return &ntv1.GetMailHostResponse{Host: &ntv1.MailHost{
		Domain: s.Domain, SmtpHost: s.SMTPHost, SmtpPort: s.SMTPPort,
		SmtpSecurity: s.SMTPSecurity, ImapHost: s.IMAPHost, ImapPort: s.IMAPPort,
		ImapSecurity: s.IMAPSecurity, HourlyQuota: s.HourlyQuota, DailyQuota: s.DailyQuota,
	}}, nil
}

func (h *Handler) SaveMailHost(ctx context.Context, req *ntv1.SaveMailHostRequest) (*ntv1.SaveMailHostResponse, error) {
	in := req.GetHost()
	err := h.svc.SaveMailHost(ctx, grpcx.TenantID(ctx), app.MailHostSettings{
		Domain: in.GetDomain(), SMTPHost: in.GetSmtpHost(), SMTPPort: in.GetSmtpPort(),
		SMTPSecurity: in.GetSmtpSecurity(), IMAPHost: in.GetImapHost(), IMAPPort: in.GetImapPort(),
		IMAPSecurity: in.GetImapSecurity(), HourlyQuota: in.GetHourlyQuota(), DailyQuota: in.GetDailyQuota(),
	})
	if err != nil {
		return nil, err
	}
	return &ntv1.SaveMailHostResponse{Ok: true}, nil
}

func (h *Handler) GetMyMailAccount(ctx context.Context, _ *ntv1.GetMyMailAccountRequest) (*ntv1.GetMyMailAccountResponse, error) {
	op := operator(ctx)
	v, err := h.svc.GetMyMailAccount(ctx, grpcx.TenantID(ctx), op.ID)
	if err != nil {
		return nil, err
	}
	return &ntv1.GetMyMailAccountResponse{Account: &ntv1.MailAccount{
		Email: v.Email, Username: v.Username, HasSecret: v.HasSecret,
		VerifiedAt: v.VerifiedAt, LastError: v.LastError, IsActive: v.IsActive,
		AuthKind: v.AuthKind,
	}}, nil
}

func (h *Handler) RecordOpen(ctx context.Context, req *ntv1.RecordOpenRequest) (*ntv1.RecordOpenResponse, error) {
	// Never reports whether the key was real. The gateway serves the same
	// image either way, so telling it apart here would only create a way to
	// probe which sends exist.
	h.svc.RecordOpen(ctx, req.GetMessageKey(), req.GetUserAgent(), req.GetIp())
	return &ntv1.RecordOpenResponse{Ok: true}, nil
}

func inboundToProto(v app.InboundView) *ntv1.InboundMail {
	m := &ntv1.InboundMail{
		Id: v.ID, FromEmail: v.FromEmail, FromName: v.FromName,
		Subject: v.Subject, Snippet: v.Snippet, ThreadKey: v.ThreadKey,
		IsRead: v.IsRead, IsStarred: v.IsStarred, HasAttachments: v.HasAttachments,
		BodyHtml: v.BodyHTML, BodyText: v.BodyText, ToEmail: v.ToEmail,
	}
	if !v.ReceivedAt.IsZero() {
		m.ReceivedAt = v.ReceivedAt.Format(time.RFC3339)
	}
	if !v.SentAt.IsZero() {
		m.SentAt = v.SentAt.Format(time.RFC3339)
	}
	for _, a := range v.Attachments {
		m.Attachments = append(m.Attachments, &ntv1.InboundAttachment{
			Id: a.ID, FileName: a.FileName, ContentType: a.ContentType, FileSize: a.FileSize,
		})
	}
	return m
}

func (h *Handler) ListInbound(ctx context.Context, req *ntv1.ListInboundRequest) (*ntv1.ListInboundResponse, error) {
	op := operator(ctx)
	rows, total, unread, err := h.svc.ListInbound(ctx, grpcx.TenantID(ctx), op.ID,
		req.GetKeyword(), req.GetView(), req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.InboundMail, 0, len(rows))
	for _, r := range rows {
		out = append(out, inboundToProto(r))
	}
	return &ntv1.ListInboundResponse{
		Mails: out, UnreadCount: unread,
		Meta: &commonv1.PageMeta{Total: total},
	}, nil
}

func (h *Handler) GetInbound(ctx context.Context, req *ntv1.GetInboundRequest) (*ntv1.GetInboundResponse, error) {
	op := operator(ctx)
	v, err := h.svc.GetInbound(ctx, grpcx.TenantID(ctx), op.ID, req.GetId())
	if err != nil {
		return nil, err
	}
	return &ntv1.GetInboundResponse{Mail: inboundToProto(v)}, nil
}

func (h *Handler) GetMailThread(ctx context.Context, req *ntv1.GetMailThreadRequest) (*ntv1.GetMailThreadResponse, error) {
	op := operator(ctx)
	items, err := h.svc.GetMailThread(ctx, grpcx.TenantID(ctx), op.ID, req.GetThreadKey())
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.ThreadItem, 0, len(items))
	for _, v := range items {
		it := &ntv1.ThreadItem{
			Direction: v.Direction, Id: v.ID, Subject: v.Subject,
			Body: v.Body, BodyFormat: v.BodyFormat,
			Counterparty: v.Counterparty, Who: v.Who,
		}
		if !v.At.IsZero() {
			it.At = v.At.Format(time.RFC3339)
		}
		out = append(out, it)
	}
	return &ntv1.GetMailThreadResponse{Items: out}, nil
}

func (h *Handler) MarkInbound(ctx context.Context, req *ntv1.MarkInboundRequest) (*ntv1.MarkInboundResponse, error) {
	op := operator(ctx)
	err := h.svc.MarkInbound(ctx, grpcx.TenantID(ctx), op.ID, req.GetId(),
		req.Read, req.Starred, req.Archived, req.Deleted)
	if err != nil {
		return nil, err
	}
	return &ntv1.MarkInboundResponse{Ok: true}, nil
}

func (h *Handler) SyncMailbox(ctx context.Context, _ *ntv1.SyncMailboxRequest) (*ntv1.SyncMailboxResponse, error) {
	op := operator(ctx)
	n, err := h.svc.SyncNow(ctx, grpcx.TenantID(ctx), op.ID)
	if err != nil {
		return &ntv1.SyncMailboxResponse{Fetched: 0, Detail: err.Error()}, nil
	}
	return &ntv1.SyncMailboxResponse{Fetched: int32(n)}, nil
}

func (h *Handler) ListMailboxSent(ctx context.Context, req *ntv1.ListMailboxSentRequest) (*ntv1.ListMailboxSentResponse, error) {
	op := operator(ctx)
	rows, total, err := h.svc.ListMailboxSent(ctx, grpcx.TenantID(ctx), op.ID,
		req.GetKeyword(), req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*ntv1.InboundMail, 0, len(rows))
	for _, r := range rows {
		out = append(out, inboundToProto(r))
	}
	return &ntv1.ListMailboxSentResponse{Mails: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *Handler) VerifyMailAccess(ctx context.Context, req *ntv1.VerifyMailAccessRequest) (*ntv1.VerifyMailAccessResponse, error) {
	op := operator(ctx)
	detail, err := h.svc.VerifyMailSecret(ctx, grpcx.TenantID(ctx), op.ID, req.GetEmail(), req.GetSecret())
	if err != nil {
		return &ntv1.VerifyMailAccessResponse{Ok: false, Detail: err.Error()}, nil
	}
	return &ntv1.VerifyMailAccessResponse{Ok: true, Detail: detail}, nil
}

func (h *Handler) CompleteGoogleOAuth(ctx context.Context, req *ntv1.CompleteGoogleOAuthRequest) (*ntv1.CompleteGoogleOAuthResponse, error) {
	op := operator(ctx)
	email, err := h.svc.CompleteGoogleOAuth(ctx, grpcx.TenantID(ctx), op.ID,
		req.GetCode(), req.GetRedirectUri())
	if err != nil {
		return nil, err
	}
	return &ntv1.CompleteGoogleOAuthResponse{Email: email}, nil
}
