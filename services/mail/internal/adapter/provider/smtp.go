package provider

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/adapter/xoauth2"
	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// Accounts resolves the mailbox a message is being sent as.
//
// Behind this interface sits the decryption of a stored credential, which is
// why it is an interface at all: the adapter gets a secret to use for one
// dial and has no way to enumerate, log or return them.
type Accounts interface {
	// ForAccount 是正路：出站队列上记着这封信从哪个信箱发（00047），
	// 直接按那个信箱取凭据。
	ForAccount(ctx context.Context, tenantID, accountID int64) (app.MailAccount, error)
	// ForSender 是退路，给 account_id 为 0 的行用——这次改动之前入队、
	// 还没发出去的那些。它答的是「这个人的默认箱」，也就是改动之前的行为。
	ForSender(ctx context.Context, tenantID, senderID int64) (app.MailAccount, error)
	// RecordFailure surfaces a connection or authentication problem on the
	// account itself. A wrong authorisation code otherwise shows up only as
	// every message failing, with nothing on screen saying why.
	RecordFailure(ctx context.Context, tenantID, accountID int64, msg string)
}

// Blobs fetches attachment bytes.
type Blobs interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// SMTP sends through the tenant's own mail host, authenticating as the
// employee the message is from.
//
// 263 — like any mail host, as opposed to an API service — gives us no
// request-level idempotency. There is no key we can send twice and have the
// second one ignored, which is why the Unknown outcome exists and why
// classify() below is careful about where in the conversation a failure
// happened.
type SMTP struct {
	accounts Accounts
	blobs    Blobs
	log      *slog.Logger
	timeout  time.Duration
	// Injected so the message-building tests do not depend on the clock.
	now func() time.Time
}

func NewSMTP(accounts Accounts, blobs Blobs, timeout time.Duration, log *slog.Logger) *SMTP {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &SMTP{accounts: accounts, blobs: blobs, log: log, timeout: timeout, now: time.Now}
}

func (s *SMTP) Name() string { return "smtp" }

func (s *SMTP) Send(ctx context.Context, m app.Outbound) app.SendResult {
	// 这封信记着自己从哪个信箱发（00047），按它取凭据。
	//
	// **不再按 sender_id 反查默认箱。** 反查有个不报错的坏法：一封排队中的
	// 信重试时才查，而这期间这个人可能换过默认箱——同一封信，两次尝试两个
	// 发件人，客户那边的会话就断了。
	//
	// 0 = 这次改动之前入队、还没发出去的行。那些退回老办法，行为和改动之前
	// 一模一样，所以部署那一刻队列里积着的信不会卡住。
	var acct app.MailAccount
	var err error
	if m.AccountID > 0 {
		acct, err = s.accounts.ForAccount(ctx, m.TenantID, m.AccountID)
	} else {
		acct, err = s.accounts.ForSender(ctx, m.TenantID, m.SenderID)
	}
	if err != nil {
		// Not the recipient's fault and not permanent: somebody has to enter
		// a code. Retryable means the queue drains itself once they do,
		// instead of a permanent failure nobody can undo.
		return app.SendResult{Outcome: app.Retryable, Err: "no usable mailbox for the sender: " + err.Error()}
	}

	files, err := s.fetch(ctx, m.Attachments)
	if err != nil {
		return app.SendResult{Outcome: app.Retryable, Err: "could not read attachment: " + err.Error()}
	}

	domain := acct.Domain
	if domain == "" {
		domain = domainOf(acct.Email)
	}
	raw, messageID, err := buildMessage(m, acct.Email, domain, files, s.now())
	if err != nil {
		// Rendering cannot succeed on a retry if it failed now.
		return app.SendResult{Outcome: app.Permanent, Err: "could not build the message: " + err.Error()}
	}

	// The envelope. A merged send covers its whole cast in one transaction;
	// the normal send has exactly one name on it.
	rcpts := []string{m.ToEmail}
	if len(m.ToList) > 0 {
		rcpts = rcpts[:0]
		for _, a := range m.ToList {
			rcpts = append(rcpts, a.Email)
		}
		for _, a := range m.CCList {
			rcpts = append(rcpts, a.Email)
		}
		// The envelope is the only place a blind copy exists. buildMessage
		// deliberately writes no Bcc header, so this loop is what actually
		// delivers to them — drop it and the address silently receives
		// nothing while the composer reports a successful send.
		for _, a := range m.BCCList {
			rcpts = append(rcpts, a.Email)
		}
	}

	res := s.dialAndSend(ctx, acct, rcpts, raw)
	if res.Outcome == app.Accepted {
		res.ProviderID = messageID
		// The same acct.Email that went into the From header a few lines up.
		// Reported rather than looked up again later: by the time anybody
		// reads this message back, the sender may be bound to a different
		// mailbox, and then the lookup answers about a mailbox this mail
		// never touched.
		res.FromEmail = acct.Email
		// And the message itself, for filing a copy in the sender's own Sent
		// folder. Only the adapter has these bytes.
		res.Raw = raw
	}
	if res.Outcome != app.Accepted && res.authProblem {
		s.accounts.RecordFailure(ctx, m.TenantID, acct.AccountID, res.Err)
	}
	return res.SendResult
}

type sendOutcome struct {
	app.SendResult
	// Set when the failure was about our credentials rather than the
	// recipient, so it can be shown on the settings page.
	authProblem bool
}

func (s *SMTP) dialAndSend(ctx context.Context, acct app.MailAccount, rcpts []string, raw []byte) sendOutcome {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	addr := net.JoinHostPort(acct.Host, fmt.Sprint(acct.Port))
	d := net.Dialer{Timeout: s.timeout}

	var conn net.Conn
	var err error
	if acct.Security == "SSL" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: acct.Host})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		// Never reached the host, so nothing was sent. Retryable, not
		// Unknown: there is no message in flight to duplicate.
		return sendOutcome{SendResult: app.SendResult{Outcome: app.Retryable, Err: "connect: " + err.Error()}}
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	c, err := smtp.NewClient(conn, acct.Host)
	if err != nil {
		return sendOutcome{SendResult: app.SendResult{Outcome: app.Retryable, Err: "greeting: " + err.Error()}}
	}
	defer c.Close()

	if acct.Security == "STARTTLS" {
		if err := c.StartTLS(&tls.Config{ServerName: acct.Host}); err != nil {
			return sendOutcome{SendResult: app.SendResult{Outcome: app.Retryable, Err: "starttls: " + err.Error()}}
		}
	}

	if acct.Secret != "" {
		var auth smtp.Auth
		if acct.AuthKind == "OAUTH" {
			auth = xoauth2.NewSMTP(acct.Email, acct.Secret)
		} else {
			user := acct.Username
			if user == "" {
				user = acct.Email
			}
			// PlainAuth refuses to send credentials over an unencrypted link,
			// so a misconfigured NONE port fails here rather than putting
			// somebody's authorisation code on the wire in clear.
			auth = smtp.PlainAuth("", user, acct.Secret, acct.Host)
		}
		if err := c.Auth(auth); err != nil {
			return sendOutcome{
				SendResult:  app.SendResult{Outcome: app.Retryable, Err: "auth: " + err.Error()},
				authProblem: true,
			}
		}
	}

	if err := c.Mail(acct.Email); err != nil {
		out := classify(err, "MAIL FROM")
		// A host refusing our own address is about us, not the recipient.
		out.authProblem = true
		return out
	}
	// Each RCPT TO gets its own verdict: hosts routinely accept some
	// recipients and refuse others inside one transaction. With one recipient
	// a refusal fails the send; with several, the refused are recorded and
	// the mail still goes to everyone the host accepted — which is what a
	// person expects of "one letter to five people, one address was wrong".
	var rejected []app.RecipientReject
	var lastRcptErr error
	acceptedRcpts := 0
	for _, to := range rcpts {
		if err := c.Rcpt(to); err != nil {
			if len(rcpts) == 1 {
				return classify(err, "RCPT TO")
			}
			lastRcptErr = err
			rejected = append(rejected, app.RecipientReject{Email: to, Detail: err.Error()})
			continue
		}
		acceptedRcpts++
	}
	if acceptedRcpts == 0 {
		// Everybody refused: no transaction to finish, classify by the last
		// answer so retry/permanent follows the host's own verdict.
		return classify(lastRcptErr, "RCPT TO")
	}

	w, err := c.Data()
	if err != nil {
		return classify(err, "DATA")
	}
	if _, err := w.Write(raw); err != nil {
		// The host accepted DATA and then the write failed. Whether it kept
		// what it had received is not knowable from here.
		return sendOutcome{SendResult: app.SendResult{Outcome: app.Unknown, Err: "writing message: " + err.Error()}}
	}
	if err := w.Close(); err != nil {
		// Close sends the terminating dot and reads the verdict. This is the
		// one place where a timeout genuinely means "we do not know": the
		// host may have accepted and queued it before the answer was lost.
		if isTimeout(err) {
			return sendOutcome{SendResult: app.SendResult{Outcome: app.Unknown, Err: "no answer after sending: " + err.Error()}}
		}
		return classify(err, "end of DATA")
	}
	// Anything after this point cannot un-send the message, so a failure to
	// say QUIT politely is logged and otherwise ignored.
	if err := c.Quit(); err != nil {
		s.log.Debug("smtp quit failed after a successful send", "err", err)
	}
	return sendOutcome{SendResult: app.SendResult{Outcome: app.Accepted, Rejected: rejected}}
}

// classify turns an SMTP error into a retry decision.
//
// The rule is the reply code's first digit: 4xx is the host telling us to
// come back, 5xx is the host telling us not to bother. Getting this backwards
// is expensive in both directions — treating 5xx as retryable hammers a host
// that has already refused, and treating 4xx as permanent silently drops mail
// that greylisting would have accepted thirty seconds later.
func classify(err error, stage string) sendOutcome {
	msg := stage + ": " + err.Error()

	if isTimeout(err) {
		return sendOutcome{SendResult: app.SendResult{Outcome: app.Retryable, Err: msg}}
	}

	switch code := replyCode(err); {
	case code >= 500:
		return sendOutcome{SendResult: app.SendResult{Outcome: app.Permanent, Err: msg}}
	case code >= 400:
		return sendOutcome{SendResult: app.SendResult{Outcome: app.Retryable, Err: msg}}
	default:
		// No code at all means the connection broke rather than the host
		// answering. Nothing was acknowledged, so it is safe to try again.
		return sendOutcome{SendResult: app.SendResult{Outcome: app.Retryable, Err: msg}}
	}
}

// replyCode digs the three-digit status out of an SMTP error string.
//
// net/smtp returns *textproto.Error for a coded reply, but wraps some of them
// in plain errors, so the string is the reliable surface. A reply always
// starts with the code.
func replyCode(err error) int {
	s := strings.TrimSpace(err.Error())
	if len(s) < 3 {
		return 0
	}
	n := 0
	for i := 0; i < 3; i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func (s *SMTP) fetch(ctx context.Context, atts []app.Attachment) ([]fileBlob, error) {
	out := make([]fileBlob, 0, len(atts))
	for _, a := range atts {
		rc, err := s.blobs.Get(ctx, a.FileKey)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.FileName, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.FileName, err)
		}
		out = append(out, fileBlob{FileName: a.FileName, ContentType: a.ContentType, Data: data})
	}
	return out, nil
}

func domainOf(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 && i+1 < len(email) {
		return email[i+1:]
	}
	return "localhost"
}
