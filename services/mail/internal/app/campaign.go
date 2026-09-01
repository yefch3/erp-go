package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	// Blind copies. Same mode restriction, and one extra property that is the
	// whole point: these addresses reach the envelope and never a header, so
	// nobody on To or Cc learns they were included.
	BCC []Recipient
	// 从**哪个信箱**发。0 = 由服务端挑（回复继承收到那封信的箱，否则用默认箱）。
	//
	// 一个人可以绑多个信箱，而「你在哪个箱里」是浏览器才知道的事。不传的话
	// 服务端只能猜，而它以前猜的永远是默认箱——于是你在 Gmail 里点回复，
	// 信从 263 发出去，客户看到的发件人不是刚才跟他写信的那个人。
	AccountID int64
	// Set when this send answers a mail in the caller's inbox: the threading
	// headers and thread key are taken from that message, so both sides'
	// clients stack the answer under the question.
	ReplyToInboundID int64
	// Set when forwarding a mail from the caller's inbox: the original's
	// attachments travel along with the new message.
	ForwardInboundID int64
	// Forward the original as a .eml attachment rather than quoting it into
	// the body. What the recipient receives is then the message itself —
	// every header, every part, byte for byte as it arrived — instead of our
	// rendering of it. That difference is the whole point: a quoted forward
	// is a retelling, and a retelling cannot be used to prove where a mail
	// came from.
	ForwardAsAttachment bool
	// True switches the open-tracking pixel off for this send. Inverted so
	// the zero value keeps the historical behaviour — see the proto note.
	DisableTracking bool
	// Files already in storage, registered inside the same transaction that
	// creates the send so no message can go out before its attachment row.
	Attachments []PendingAttachment
	// When to deliver. Zero means now. An absolute instant, resolved by the
	// composer from whatever zone the person was thinking in.
	ScheduledAt time.Time
}

// PendingAttachment names a file the browser has already pushed to storage.
type PendingAttachment struct {
	FileName string `json:"fileName"`
	FileKey  string `json:"fileKey"`
	// serverDerived marks a key this package produced itself, from a row
	// whose owner was already checked — not one a caller sent us.
	//
	// Unexported on purpose. It is the difference between "the browser says
	// this object is mine" and "we looked up the object on a message we
	// proved belongs to this person", and no amount of JSON, protobuf or
	// gateway translation can set it, because nothing outside this package
	// can name the field. The guard in registerAttachmentTx is what keeps
	// one employee from attaching another's file by pasting its key; this
	// flag is how the trusted path gets past that guard without widening it
	// into a prefix anybody could imitate.
	serverDerived bool
}

// fromInbox names a file this package resolved off a message the caller was
// already proven to own.
func fromInbox(fileName, fileKey string) PendingAttachment {
	return PendingAttachment{FileName: fileName, FileKey: fileKey, serverDerived: true}
}

// allowedFor reports whether this object may be attached to a send by this
// tenant. It is the whole trust boundary for attachment keys.
//
// A key the client sent us has to sit under the prefix PresignAttachment
// hands out, or anybody could attach anybody's file by pasting its key. A key
// this package resolved itself does not, because it was read off a row whose
// owner was checked first — and it could not pass the prefix test anyway,
// since received mail is stored under mail/inbound/<tenant>/<account>/.
//
// The prefix is deliberately not widened to admit mail/inbound/: that path is
// keyed by account, so a caller able to name it could name a colleague's
// account and attach their mail's files.
func (f PendingAttachment) allowedFor(tenantID int64) bool {
	if f.serverDerived {
		return true
	}
	return strings.HasPrefix(f.FileKey, "mail-attachments/"+itoa(int(tenantID))+"/")
}

// emlFileName is what a forwarded original is called in the recipient's
// client. The subject, because that is what the person forwarding it was
// looking at; a fallback rather than a bare ".eml", because a file whose whole
// name is an extension reads as a broken attachment.
func emlFileName(subject string) string {
	name := safeName(strings.TrimSpace(subject))
	if name == "" {
		name = "forwarded-message"
	}
	return name + ".eml"
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
	due, err := validSchedule(in.ScheduledAt)
	if err != nil {
		return CampaignResult{}, err
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
	// CC and BCC are checked too: a suppression honoured on the To line and
	// ignored on the other two is not honoured at all — least of all on BCC,
	// where nobody would see the address that got written to.
	emails := make([]string, 0, len(in.Recipients)+len(in.CC)+len(in.BCC))
	for _, list := range [][]Recipient{in.Recipients, in.CC, in.BCC} {
		for _, r := range list {
			emails = append(emails, strings.ToLower(strings.TrimSpace(r.Email)))
		}
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

	// 从哪个信箱发，**在入队之前定死**。发信时才反查的那一版有个不报错的
	// 坏法：一封排队中的信重试时，如果这期间换过默认箱，重试会从另一个
	// 地址发出去——同一封信，两次尝试两个发件人。
	accountID, err := s.sendingMailbox(ctx, tenantID, op.ID, in.AccountID, in.ReplyToInboundID)
	if err != nil {
		return CampaignResult{}, err
	}

	if mode == "MERGED" {
		return s.createMerged(ctx, tenantID, in, kind, sender, accountID, body, textBody, format, thread, suppressed, due)
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
			ScheduledAt: due, ReplyToInboundID: in.ReplyToInboundID,
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
				Kind: kind, SenderID: sender.ID, AccountID: accountID,
				SenderName: sender.Name,
				ToEmail:    addr, ToName: r.Name,
				CustomerID: r.CustomerID, CustomerName: r.CustomerName,
				ContactID: r.ContactID,
				Subject:   subject, Body: rendered, BodyText: renderedText,
				BodyFormat: format,
				Status:     status, AttentionReason: attention,
				SendMode:  "SEPARATE",
				ThreadKey: thread.ThreadKey,
				InReplyTo: thread.InReplyTo, ReferencesIds: thread.References,
				ScheduledAt: due,
				TrackOpens:  !in.DisableTracking,
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

// sendingMailbox 决定这封信从哪个信箱发出去。
//
// 三层，从最明确到最兜底：
//
//  1. 调用方点名了（写信框里的发件人下拉）——但必须是**他自己的**箱。
//     不验的话，任何人填一个别人的 account_id 就能以别人的地址发信。
//  2. 这是一封回复：用收到原信的那个箱。这几乎总是对的——对方写到哪个
//     地址，回信就该从哪个地址出去，否则会话在客户那边会断成两条。
//  3. 都没有：默认箱。和改动之前一样。
//
// 返回 0 表示这个人一个箱都没绑。那种情况照旧交给发信那边处理（它会答
// 「没有可用的信箱」并让这封信可重试），这里不提前报错——入队本身没问题，
// 绑好箱之后队列会自己排出去。
func (s *Service) sendingMailbox(ctx context.Context, tenantID, employeeID, requested, replyToInboundID int64) (int64, error) {
	if requested > 0 {
		return s.mailboxOfMine(ctx, tenantID, employeeID, requested)
	}
	if replyToInboundID > 0 {
		var acct, owner int64
		if err := s.pool.QueryRow(ctx, `
			SELECT account_id, owner_id FROM email_inbound
			 WHERE tenant_id=$1 AND id=$2`, tenantID, replyToInboundID).Scan(&acct, &owner); err == nil &&
			owner == employeeID && acct > 0 {
			return acct, nil
		}
	}
	id, err := s.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		// 一个箱都没绑。入队不拦——绑好之后队列会自己排出去。
		return 0, nil
	}
	return id, nil
}

// mailboxOfMine 把「调用方点名的那个信箱」翻成 id，顺便确认它确实在他名下。
//
// **这条必须在服务层判，不能只靠调用方。** 网关传过来的是解锁令牌里那个箱
// （验过的），但服务层是最后一道；这里放过去的后果，在发信是「以同事的地址
// 给客户写信」，在收信是「点一下就把同事的信箱拉了一遍」——花的是同事的
// 配额，动的是同事的已读状态。
func (s *Service) mailboxOfMine(ctx context.Context, tenantID, employeeID, id int64) (int64, error) {
	row, err := s.q.GetMailAccountByID(ctx, store.GetMailAccountByIDParams{
		TenantID: tenantID, ID: id,
	})
	if err != nil || row.EmployeeID != employeeID {
		// 措辞含糊：说「这个信箱不是你的」而不是「它属于张三」，
		// 否则这里就成了一个拿 id 探测别人信箱的口子。
		return 0, apierr.Invalid("MAIL_NOT_YOUR_MAILBOX", "这个邮箱不在你名下")
	}
	// 解绑了的箱能读历史，但不能收发——凭据已经清掉了。这里说清楚是哪一种
	// 拒绝：「不在你名下」会让人去找管理员，而该做的是重新填一次授权码。
	if row.UnboundAt.Valid {
		return 0, apierr.Invalid("MAIL_MAILBOX_UNBOUND",
			"这个邮箱已解绑，只能查看历史邮件。要重新收发，请重新登录这个邮箱")
	}
	return id, nil
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
		if in.ForwardAsAttachment {
			// One file instead of many: the whole original message, headers
			// and all. Its own attachments are inside it, so adding them
			// separately would ship every file twice.
			if row.RawKey == "" {
				return t, apierr.Invalid("NT_RAW_NOT_KEPT",
					"这封邮件的原始内容没有留档，无法作为附件转发")
			}
			in.Attachments = append(in.Attachments, fromInbox(emlFileName(row.Subject), row.RawKey))
			return t, nil
		}
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
			in.Attachments = append(in.Attachments, fromInbox(a.FileName, a.FileKey))
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
	sender Sender, accountID int64, body, textBody, format string, thread composeThread,
	suppressed map[string]string, due pgtype.Timestamptz,
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
	bccs := keep(in.BCC)
	if len(tos) == 0 {
		return CampaignResult{}, apierr.Invalid("NT_RECIPIENTS_REQUIRED",
			"收件人都被跳过了，没有可发送的地址")
	}
	if len(tos)+len(ccs)+len(bccs) > mergedRecipientCap {
		return CampaignResult{}, apierr.Invalid("NT_TOO_MANY_MERGED",
			"合并发送最多 50 个收件人（含抄送、密送）；更多人请改用分别发送")
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
			ScheduledAt: due, ReplyToInboundID: in.ReplyToInboundID,
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
			Kind: kind, SenderID: sender.ID, AccountID: accountID,
			SenderName: sender.Name,
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
			ScheduledAt: due,
			TrackOpens:  !in.DisableTracking,
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
		if err := add(bccs, "BCC"); err != nil {
			return err
		}
		result.Queued = 1
		return nil
	})
	if err != nil {
		return CampaignResult{}, err
	}
	s.log.Info("merged mail queued", "campaign_no", result.CampaignNo,
		"to", len(tos), "cc", len(ccs), "bcc", len(bccs),
		"suppressed", len(result.Suppressed))
	return result, nil
}
