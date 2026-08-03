package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// BizTypeCampaign is the numbering series bulk sends draw from.
const BizTypeCampaign = "CAMPAIGN"

// CampaignInput is a bulk send as composed.
type CampaignInput struct {
	Subject string
	Body    string
	// TEXT or HTML. HTML bodies are sanitised on the way in and always carry
	// a derived plain-text alternative.
	Format      string
	SignatureID int64
	// MARKETING or TRANSACTIONAL — decides what happens to a send whose
	// outcome cannot be determined. See §5.12.3.1.
	Kind       string
	Recipients []Recipient
	// SEPARATE (default): one personalised copy per recipient, invisible to
	// each other. MERGED: one shared mail, everybody on the To and CC lists
	// sees everybody else — the mode for "one letter to the three people at
	// this customer", not for campaigns.
	SendMode string
	// Carbon copies. Meaningful only for MERGED — a CC on a per-recipient
	// send would mean the same person receiving N copies.
	CC []Recipient
	// Set when this send answers a mail in the caller's inbox: the threading
	// headers and thread key are taken from that message, so both sides'
	// clients stack the answer under the question.
	ReplyToInboundID int64
	// Set when forwarding a mail from the caller's inbox: the original's
	// attachments travel along with the new message.
	ForwardInboundID int64
	// Files already in storage, registered inside the same transaction that
	// creates the send so no message can go out before its attachment row.
	Attachments []PendingAttachment
}

// PendingAttachment names a file the browser has already pushed to storage.
type PendingAttachment struct {
	FileName string `json:"fileName"`
	FileKey  string `json:"fileKey"`
}

// CampaignResult reports what was queued and, just as importantly, what was
// not. A caller who asked for 500 and got 487 queued needs to be told why
// rather than left to wonder.
type CampaignResult struct {
	CampaignID  int64
	CampaignNo  string
	Queued      int
	Suppressed  []SkippedRecipient
	NeedsReview []SkippedRecipient
}

// SkippedRecipient is one address that did not make it into the queue.
type SkippedRecipient struct {
	Email  string
	Name   string
	Reason string
}

// CreateCampaign renders one message per recipient and queues them.
//
// One row per recipient, never one row with a list of addresses. That is what
// makes the four requirements possible at once: recipients cannot see each
// other, each mail can be personalised, each can be retried on its own, and
// each can carry its own delivery status. They are not four features, they
// are one decision.
func (s *Service) CreateCampaign(ctx context.Context, tenantID int64, in CampaignInput, op Operator) (CampaignResult, error) {
	if strings.TrimSpace(in.Subject) == "" {
		return CampaignResult{}, apierr.Invalid("NT_SUBJECT_REQUIRED", "请填写邮件主题")
	}
	if strings.TrimSpace(in.Body) == "" {
		return CampaignResult{}, apierr.Invalid("NT_BODY_REQUIRED", "请填写邮件正文")
	}
	if len(in.Recipients) == 0 {
		return CampaignResult{}, apierr.Invalid("NT_RECIPIENTS_REQUIRED", "请至少选择一个收件人")
	}
	kind := strings.ToUpper(in.Kind)
	if kind != "TRANSACTIONAL" {
		kind = "MARKETING"
	}
	mode := strings.ToUpper(strings.TrimSpace(in.SendMode))
	if mode != "MERGED" {
		mode = "SEPARATE"
	}

	// Reply and forward both borrow from a mail in the caller's own inbox;
	// resolving it here also proves it IS the caller's.
	thread, err := s.composeContext(ctx, tenantID, &in, op)
	if err != nil {
		return CampaignResult{}, err
	}

	sender, err := s.senderOf(ctx, op)
	if err != nil {
		return CampaignResult{}, err
	}
	body, textBody, format, err := s.composeBody(ctx, tenantID, in)
	if err != nil {
		return CampaignResult{}, err
	}

	// One round trip for the whole list rather than a lookup per recipient.
	// CC addresses are checked too: a suppression honoured on the To line and
	// ignored on the CC line is not honoured at all.
	emails := make([]string, 0, len(in.Recipients)+len(in.CC))
	for _, r := range in.Recipients {
		emails = append(emails, strings.ToLower(strings.TrimSpace(r.Email)))
	}
	for _, r := range in.CC {
		emails = append(emails, strings.ToLower(strings.TrimSpace(r.Email)))
	}
	blocked, err := s.q.SuppressedAmong(ctx, store.SuppressedAmongParams{
		TenantID: tenantID, Emails: emails,
	})
	if err != nil {
		return CampaignResult{}, err
	}
	suppressed := make(map[string]string, len(blocked))
	for _, b := range blocked {
		suppressed[b.Email] = b.Reason
	}

	if mode == "MERGED" {
		return s.createMerged(ctx, tenantID, in, kind, sender, body, textBody, format, thread, suppressed)
	}

	result := CampaignResult{}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		no, err := s.number.Next(ctx, BizTypeCampaign)
		if err != nil {
			return err
		}
		campaignID, err := q.CreateCampaign(ctx, store.CreateCampaignParams{
			TenantID: tenantID, CampaignNo: no,
			SubjectTpl: in.Subject, BodyTpl: body, BodyTextTpl: textBody,
			BodyFormat: format, SignatureID: in.SignatureID,
			Kind: kind, SenderID: sender.ID, SenderName: sender.Name,
			SenderEmail: sender.Email,
		})
		if err != nil {
			return err
		}
		result.CampaignID = campaignID
		result.CampaignNo = no

		// Before any message row exists: a recipient claimed by the worker
		// between the two would otherwise go out without its attachment.
		for _, f := range in.Attachments {
			if _, err := s.registerAttachmentTx(ctx, q, tenantID, campaignID, f, op); err != nil {
				return err
			}
		}

		for _, r := range in.Recipients {
			addr := strings.ToLower(strings.TrimSpace(r.Email))
			if addr == "" {
				result.Suppressed = append(result.Suppressed, SkippedRecipient{
					Name: r.Name, Reason: "NO_ADDRESS",
				})
				continue
			}
			// Suppressed addresses never enter the queue. Sending to a dead
			// address repeatedly is the fastest way to ruin a sending domain,
			// and an unsubscribe one campaign honours and the next ignores is
			// worse than not offering one.
			if reason, hit := suppressed[addr]; hit {
				result.Suppressed = append(result.Suppressed, SkippedRecipient{
					Email: addr, Name: r.Name, Reason: reason,
				})
				continue
			}

			// The subject is always plain text, whatever the body is: mail
			// clients render it as text and markup there would be visible.
			subject, missSubject := Render(in.Subject, r, sender)
			rendered, missBody := RenderAs(body, r, sender, format)
			missing := append(missSubject, missBody...)
			renderedText := ""
			if format == FormatHTML {
				// Rendered from the same template rather than derived from
				// the rendered HTML, so the two parts cannot drift.
				renderedText, _ = RenderAs(textBody, r, sender, FormatText)
			}

			status, attention := "QUEUED", ""
			if len(missing) > 0 {
				// Queued as needing a person, not sent. "Dear ," reaching a
				// customer cannot be taken back; a row in a review queue can.
				status = "NEEDS_ATTENTION"
				attention = "取不到变量：" + strings.Join(dedupe(missing), "、")
				result.NeedsReview = append(result.NeedsReview, SkippedRecipient{
					Email: addr, Name: r.Name, Reason: attention,
				})
			} else {
				result.Queued++
			}

			key, err := newMessageKey()
			if err != nil {
				return err
			}
			if _, err := q.QueueMessage(ctx, store.QueueMessageParams{
				TenantID: tenantID, CampaignID: campaignID, MessageKey: key,
				Kind: kind, SenderID: sender.ID, SenderName: sender.Name,
				ToEmail: addr, ToName: r.Name,
				CustomerID: r.CustomerID, CustomerName: r.CustomerName,
				ContactID: r.ContactID,
				Subject:   subject, Body: rendered, BodyText: renderedText,
				BodyFormat: format,
				Status:     status, AttentionReason: attention,
				SendMode:  "SEPARATE",
				ThreadKey: thread.ThreadKey,
				InReplyTo: thread.InReplyTo, ReferencesIds: thread.References,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return CampaignResult{}, err
	}
	s.log.Info("campaign queued", "campaign_no", result.CampaignNo,
		"queued", result.Queued, "suppressed", len(result.Suppressed),
		"needs_review", len(result.NeedsReview))
	return result, nil
}

// Preview renders the mail as one chosen recipient would receive it.
//
// Required before sending, because a variable that cannot resolve is only
// obvious when somebody sees the gap where the name should be.
func (s *Service) Preview(ctx context.Context, tenantID int64, in CampaignInput, r Recipient, op Operator) (PreviewResult, error) {
	sender, err := s.senderOf(ctx, op)
	if err != nil {
		return PreviewResult{}, err
	}
	body, textBody, format, err := s.composeBody(ctx, tenantID, in)
	if err != nil {
		return PreviewResult{}, err
	}
	subject, missSubject := Render(in.Subject, r, sender)
	rendered, missBody := RenderAs(body, r, sender, format)
	out := PreviewResult{Subject: subject, Body: rendered, Format: format,
		Missing: dedupe(append(missSubject, missBody...))}
	if format == FormatHTML {
		out.BodyText, _ = RenderAs(textBody, r, sender, FormatText)
	}
	return out, nil
}

// PreviewResult is what the composer shows before anything is sent. It
// carries the text alternative too, because that is the version a recipient
// with images off will read and nobody would otherwise ever look at it.
type PreviewResult struct {
	Subject  string
	Body     string
	BodyText string
	Format   string
	Missing  []string
}

// composeBody sanitises, appends the signature and derives the text part.
//
// The signature is joined before rendering so its own variables resolve in
// the same pass, and before the text is derived so the sign-off appears in
// both versions.
func (s *Service) composeBody(ctx context.Context, tenantID int64, in CampaignInput) (body, text, format string, err error) {
	format = normalizeFormat(in.Format)
	body = in.Body
	if format == FormatHTML {
		// Every HTML body is sanitised on the way in, not on the way out:
		// the stored value is also what the UI renders back, so cleaning it
		// once here closes both holes.
		body = SanitizeHTML(body)
	}
	sig, sigFormat, err := s.signature(ctx, tenantID, in.SignatureID)
	if err != nil {
		return "", "", "", err
	}
	if sig != "" {
		body = joinSignature(body, sig, format, sigFormat)
	}
	if format == FormatHTML {
		text = HTMLToText(body)
	}
	return body, text, format, nil
}

// joinSignature glues body and signature, converting whichever side needs it.
//
// The two can disagree: a plain-text body with an HTML signature carrying a
// logo is a perfectly ordinary thing to want. Whenever either side is HTML
// the result is HTML, and the text side is escaped rather than injected raw.
func joinSignature(body, sig, bodyFormat, sigFormat string) string {
	switch {
	case bodyFormat == FormatHTML && sigFormat == FormatHTML:
		return body + "<br><br>" + sig
	case bodyFormat == FormatHTML:
		return body + "<br><br>" + textToHTML(sig)
	case sigFormat == FormatHTML:
		// Caller asked for a text body; a signature that needs HTML is
		// flattened rather than silently upgrading the whole mail, because
		// the caller's format choice decides what gets sent.
		return body + "\n\n" + HTMLToText(sig)
	default:
		return body + "\n\n" + sig
	}
}

// textToHTML lifts plain text into the HTML body without letting it inject.
func textToHTML(s string) string {
	return strings.ReplaceAll(escapeForHTML(s), "\n", "<br>")
}

func (s *Service) senderOf(ctx context.Context, op Operator) (Sender, error) {
	sender := Sender{ID: op.ID, Name: op.Name}
	if s.directory == nil {
		return sender, nil
	}
	e, err := s.directory.Get(ctx, op.ID)
	if err != nil {
		// The org lookup failing should not block a send: the operator's own
		// name is already on the token, and the rest are signature niceties.
		s.log.Warn("directory lookup failed, sending with token identity", "err", err)
		return sender, nil
	}
	return Sender{ID: e.ID, Name: orDefault(e.Name, op.Name), Title: e.Title,
		Email: e.Email, Phone: e.Phone}, nil
}

// signature reads the chosen block and the format it was written in.
func (s *Service) signature(ctx context.Context, tenantID, signatureID int64) (string, string, error) {
	if signatureID == 0 {
		return "", FormatText, nil
	}
	sig, err := s.q.GetSignature(ctx, store.GetSignatureParams{TenantID: tenantID, ID: signatureID})
	if err == pgx.ErrNoRows {
		return "", "", apierr.NotFound("NT_SIGNATURE_NOT_FOUND", "签名不存在")
	}
	if err != nil {
		return "", "", err
	}
	return sig.Content, normalizeFormat(sig.BodyFormat), nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Operator is who is acting, carried down from the JWT.
type Operator struct {
	ID   int64
	Name string
}

// mergedRecipientCap bounds one shared To/Cc header. Fifty names is already a
// committee; past that the person wanted a campaign and picked the wrong mode.
const mergedRecipientCap = 50

// asMsgID restores the angle brackets a Message-ID header requires; parsed
// ids are stored without them.
func asMsgID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "<") {
		s = "<" + s
	}
	if !strings.HasSuffix(s, ">") {
		s += ">"
	}
	return s
}

// composeThread is what a reply carries from the mail it answers.
type composeThread struct {
	ThreadKey  string
	InReplyTo  string
	References string
}

// composeContext resolves the reply/forward source, proving on the way that
// it sits in the caller's own inbox — quoting or re-shipping somebody else's
// mail through a send would otherwise be a data-scope hole.
func (s *Service) composeContext(ctx context.Context, tenantID int64, in *CampaignInput, op Operator) (composeThread, error) {
	var t composeThread
	if in.ReplyToInboundID > 0 {
		row, err := s.q.GetInboundForCompose(ctx, store.GetInboundForComposeParams{
			TenantID: tenantID, ID: in.ReplyToInboundID,
		})
		if err != nil {
			return t, apierr.NotFound("NT_INBOUND_NOT_FOUND", "要回复的邮件不存在")
		}
		if row.OwnerID != op.ID {
			return t, apierr.Permission("NT_NOT_YOUR_MAIL", "只能回复自己邮箱里的邮件")
		}
		t.ThreadKey = row.ThreadKey
		// Stored parsed (no angle brackets); the headers require them, and a
		// bracketless In-Reply-To silently breaks threading in strict clients.
		t.InReplyTo = asMsgID(row.MessageID)
		// The chain grows by one: everything the original referenced, then the
		// original itself. This is what lets the customer's client thread our
		// answer even when intermediate mails never passed through us.
		refs := make([]string, 0, 8)
		for _, r := range strings.Fields(row.ReferencesIds) {
			refs = append(refs, asMsgID(r))
		}
		if id := asMsgID(row.MessageID); id != "" {
			refs = append(refs, id)
		}
		t.References = strings.Join(refs, " ")
	}
	if in.ForwardInboundID > 0 {
		row, err := s.q.GetInboundForCompose(ctx, store.GetInboundForComposeParams{
			TenantID: tenantID, ID: in.ForwardInboundID,
		})
		if err != nil {
			return t, apierr.NotFound("NT_INBOUND_NOT_FOUND", "要转发的邮件不存在")
		}
		if row.OwnerID != op.ID {
			return t, apierr.Permission("NT_NOT_YOUR_MAIL", "只能转发自己邮箱里的邮件")
		}
		// A forward starts a new conversation with a new party, so it takes
		// the original's files but not its thread.
		atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
			TenantID: tenantID, InboundID: row.ID,
		})
		if err != nil {
			return t, err
		}
		for _, a := range atts {
			if a.FileKey == "" {
				continue
			}
			in.Attachments = append(in.Attachments, PendingAttachment{
				FileName: a.FileName, FileKey: a.FileKey,
			})
		}
	}
	return t, nil
}

// createMerged queues ONE message whose recipients see each other.
//
// The isolation promises of the per-recipient path do not apply — being seen
// together is this mode's declared meaning — but two of its rules survive:
// suppressed addresses still never enter a send, and nothing goes out with a
// variable that cannot resolve. A variable that depends on the recipient
// cannot resolve when there are many recipients and one body, so it is
// refused rather than rendered against an arbitrary person.
func (s *Service) createMerged(
	ctx context.Context, tenantID int64, in CampaignInput, kind string,
	sender Sender, body, textBody, format string, thread composeThread,
	suppressed map[string]string,
) (CampaignResult, error) {
	result := CampaignResult{}
	keep := func(list []Recipient) []Recipient {
		out := make([]Recipient, 0, len(list))
		for _, r := range list {
			addr := strings.ToLower(strings.TrimSpace(r.Email))
			if addr == "" {
				result.Suppressed = append(result.Suppressed, SkippedRecipient{
					Name: r.Name, Reason: "NO_ADDRESS",
				})
				continue
			}
			if reason, hit := suppressed[addr]; hit {
				result.Suppressed = append(result.Suppressed, SkippedRecipient{
					Email: addr, Name: r.Name, Reason: reason,
				})
				continue
			}
			r.Email = addr
			out = append(out, r)
		}
		return out
	}
	tos := keep(in.Recipients)
	ccs := keep(in.CC)
	if len(tos) == 0 {
		return CampaignResult{}, apierr.Invalid("NT_RECIPIENTS_REQUIRED",
			"收件人都被跳过了，没有可发送的地址")
	}
	if len(tos)+len(ccs) > mergedRecipientCap {
		return CampaignResult{}, apierr.Invalid("NT_TOO_MANY_MERGED",
			"合并发送最多 50 个收件人（含抄送）；更多人请改用分别发送")
	}

	// Rendered once against nobody: what fails to resolve is exactly the set
	// of per-recipient variables, which one shared body cannot carry. Sender
	// variables (my_name, my_title …) resolve normally.
	subject, missSubject := Render(in.Subject, Recipient{}, sender)
	rendered, missBody := RenderAs(body, Recipient{}, sender, format)
	if missing := dedupe(append(missSubject, missBody...)); len(missing) > 0 {
		return CampaignResult{}, apierr.Invalid("NT_MERGED_VARS",
			"合并发送对所有人是同一封信，不能使用按收件人变化的变量："+strings.Join(missing, "、"))
	}
	renderedText := ""
	if format == FormatHTML {
		renderedText, _ = RenderAs(textBody, Recipient{}, sender, FormatText)
	}

	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		no, err := s.number.Next(ctx, BizTypeCampaign)
		if err != nil {
			return err
		}
		campaignID, err := q.CreateCampaign(ctx, store.CreateCampaignParams{
			TenantID: tenantID, CampaignNo: no,
			SubjectTpl: in.Subject, BodyTpl: body, BodyTextTpl: textBody,
			BodyFormat: format, SignatureID: in.SignatureID,
			Kind: kind, SenderID: sender.ID, SenderName: sender.Name,
			SenderEmail: sender.Email,
		})
		if err != nil {
			return err
		}
		result.CampaignID = campaignID
		result.CampaignNo = no

		for _, f := range in.Attachments {
			if _, err := s.registerAttachmentTx(ctx, q, tenantID, campaignID, f, Operator{ID: sender.ID, Name: sender.Name}); err != nil {
				return err
			}
		}

		key, err := newMessageKey()
		if err != nil {
			return err
		}
		msgID, err := q.QueueMessage(ctx, store.QueueMessageParams{
			TenantID: tenantID, CampaignID: campaignID, MessageKey: key,
			Kind: kind, SenderID: sender.ID, SenderName: sender.Name,
			// The first To recipient stands for the message in list views;
			// the full cast lives in the recipients table.
			ToEmail: tos[0].Email, ToName: tos[0].Name,
			CustomerID: tos[0].CustomerID, CustomerName: tos[0].CustomerName,
			ContactID: tos[0].ContactID,
			Subject:   subject, Body: rendered, BodyText: renderedText,
			BodyFormat: format, Status: "QUEUED",
			SendMode:  "MERGED",
			ThreadKey: thread.ThreadKey,
			InReplyTo: thread.InReplyTo, ReferencesIds: thread.References,
		})
		if err != nil {
			return err
		}
		add := func(list []Recipient, rkind string) error {
			for _, r := range list {
				if err := q.AddMessageRecipient(ctx, store.AddMessageRecipientParams{
					TenantID: tenantID, MessageID: msgID, Kind: rkind,
					Email: r.Email, Name: r.Name, CustomerID: r.CustomerID,
				}); err != nil {
					return err
				}
			}
			return nil
		}
		if err := add(tos, "TO"); err != nil {
			return err
		}
		if err := add(ccs, "CC"); err != nil {
			return err
		}
		result.Queued = 1
		return nil
	})
	if err != nil {
		return CampaignResult{}, err
	}
	s.log.Info("merged mail queued", "campaign_no", result.CampaignNo,
		"to", len(tos), "cc", len(ccs), "suppressed", len(result.Suppressed))
	return result, nil
}
