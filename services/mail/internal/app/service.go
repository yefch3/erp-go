// Package app holds the mail use cases: what to send, to whom, and
// what happened to it afterwards.
package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// Numbering issues campaign numbers. Notification does not run its own
// sequence: one service owns them all.
type Numbering interface {
	Next(ctx context.Context, bizType string) (string, error)
}

// Directory looks up the person a mail goes out as, so the signature
// variables have something to resolve against.
type Directory interface {
	Get(ctx context.Context, employeeID int64) (Employee, error)
}

type Employee struct {
	ID    int64
	Name  string
	Title string
	Email string
	Phone string
}

// Scopes answers "whose sends may this person read". Notification does not
// decide that itself; the rule belongs with the organisation chart.
type Scopes interface {
	VisibleEmployees(ctx context.Context, employeeID int64, module string) (Visibility, error)
}

type Visibility struct {
	All         bool
	EmployeeIDs []int64
	ScopeType   string
}

// Outcome is what a provider says happened to one message.
type Outcome int

const (
	// Accepted: the provider took it and gave us an id.
	Accepted Outcome = iota
	// Retryable: a temporary refusal — rate limit, greylisting, 5xx, a
	// network error that never reached the provider.
	Retryable
	// Permanent: it will fail the same way for ever — unknown recipient,
	// content rejected by policy.
	Permanent
	// Unknown: the request went out and no answer came back. Whether it was
	// sent cannot be determined from here; see §5.12.3.1.
	Unknown
)

// SendResult is one attempt.
type SendResult struct {
	Outcome    Outcome
	ProviderID string
	// FromEmail is the address this message actually left from — whatever the
	// adapter put in the From header, reported back rather than inferred.
	// Only the adapter knows it: it resolves the sender's mailbox at the
	// moment of sending, and that binding can change afterwards. Set on
	// Accepted; empty otherwise, because nothing left.
	FromEmail string
	Err       string
	// Recipients the host refused individually while accepting the
	// transaction as a whole. Only ever set for merged sends.
	Rejected []RecipientReject
	// Raw is the message exactly as it went out, so a copy of it can be filed
	// in the mailbox's own 已发送 folder. Set on Accepted only; there is
	// nothing to keep a copy of otherwise.
	//
	// Carried back rather than rebuilt later because these are the bytes the
	// recipient received, boundaries and Message-ID included. A rebuild would
	// be a second rendering of the same message, and the copy in 已发送 would
	// slowly stop being the thing that was actually sent.
	Raw []byte
}

// Outbound is one rendered message on its way out.
type Outbound struct {
	MessageKey string
	// Who this is being sent as. Everybody sends through their own mailbox,
	// so the adapter has to authenticate as this person rather than as one
	// shared account — these two fields are what it resolves credentials by.
	TenantID int64
	SenderID int64
	// AccountID 是这封信从哪个信箱发。入队时定死（00047），发信时按它取
	// 凭据——不是发信那一刻才拿 SenderID 反查默认箱。
	//
	// 0 = 这次改动之前入队的行，那些退回反查。
	AccountID int64
	FromName  string
	ToEmail   string
	ToName    string
	Subject   string
	// Body is the HTML when Format is HTML, otherwise the whole message.
	Body string
	// BodyText is the plain-text alternative, set only for HTML mail. Both
	// parts go out together as multipart/alternative: HTML alone is a
	// measurable spam signal, and some recipients genuinely read in text.
	BodyText string
	Format   string
	// Files travelling with this message. The same set for every recipient
	// of a send, which is why they are fetched once per campaign.
	Attachments []Attachment
	// Pictures the body references by Content-ID rather than by URL. Carried
	// with their bytes already read, because whether a picture can be carried
	// is decided where it can be decided safely: one that cannot be read is
	// left as a link instead of failing the send. See InlineMailImages.
	InlineImages []InlineImage
	// A merged send: when ToList is non-empty it replaces ToEmail/ToName
	// entirely — the To header carries the whole list, everybody sees each
	// other, and the envelope covers every entry plus CCList. Both stay
	// empty for the normal one-recipient send, which keeps the isolation
	// guarantee the comment on Provider describes.
	ToList []NamedAddress
	CCList []NamedAddress
	// Blind copies. The one list that must reach the envelope and never the
	// headers: a Bcc header in the delivered message would tell every other
	// recipient exactly who was copied in secret, which is the opposite of
	// what the field means. Kept separate from CCList so the two can never be
	// confused by a later edit.
	BCCList []NamedAddress
	// Reply threading: the Message-ID being answered and the chain above
	// it. Empty for a fresh mail.
	InReplyTo  string
	References string
}

// NamedAddress is one person on a mail header.
type NamedAddress struct {
	Name  string
	Email string
}

// RecipientReject is one RCPT TO the host refused inside an otherwise
// accepted merged transaction — the mail went to the others, not to them.
type RecipientReject struct {
	Email  string
	Detail string
}

// Provider is the sending service. One message per call by construction —
// for a SEPARATE send there is no recipient list in this call, which is what
// makes "recipients never see each other" impossible to get wrong later. A
// MERGED send passes its list openly in ToList/CCList, because being seen
// together is that mode's declared meaning.
type Provider interface {
	Send(ctx context.Context, m Outbound) SendResult
	Name() string
}

type Deps struct {
	Numbering Numbering
	Directory Directory
	Scopes    Scopes
	Provider  Provider
	Files     Files
	// Tables extracts arbitrary tabular content from selected mail text or an
	// attachment. Nil keeps the rest of mail fully operational and makes only
	// the explicit Excel action report that it is not configured.
	Tables TableExtractor
	// Pricing 把 token 折成钱，只为给人看。零值表示没配单价：用量照记，
	// 金额留空——空和 0 是两回事，一个是「不知道」，一个是「不要钱」。
	Pricing ModelPricing
	// Secrets decrypts stored mailbox credentials. Nil in tests and in any
	// deployment that has not been given a key; every path that needs it
	// checks and fails loudly rather than proceeding without encryption.
	Secrets *SecretBox
	// Live pushes "go and re-read" hints to open browser tabs. Nil disables
	// pushing; the page still refreshes on the next manual or timed load.
	Live *livefeed.Publisher
}

type Service struct {
	// One bounded fleet for every mailbox sync in the process. See
	// syncfleet.go; built on first use because its size comes from SyncConfig.
	fleetOnce sync.Once
	syncFleet *syncFleet

	pool      *pgxpool.Pool
	q         *store.Queries
	number    Numbering
	directory Directory
	scopes    Scopes
	provider  Provider
	files     Files
	tables    TableExtractor
	// 模型单价，只用来把 token 折成钱给人看。零值就不折——不猜价格。
	pricing ModelPricing
	mailbox Mailbox
	secrets *SecretBox
	live    *livefeed.Publisher
	oauth   OAuthConfig
	// Access tokens by account id. They live an hour; caching them keeps the
	// sender and the sync from asking Google once per message.
	tokenCache sync.Map
	// Where each account's special folders live, keyed "sent:<id>" and
	// "junk:<id>"; found once, never changes.
	sentFolders sync.Map
	// 我们自己的公网主机名。读信时用来认出自家的追踪像素并拆掉它——不拆，
	// 本公司的人打开自己发出的信就会把对方标成已读。空表示没配公网地址，
	// 那种情况下在外面也不存在我们的像素。见 ownpixel.go。
	selfHost string

	// Address classification for the tracking pixel, built on first use.
	originsOnce sync.Once
	originClass *originClassifier
	log         *slog.Logger
}

// UsePublicBaseURL tells the read path what our own address looks like.
//
// 与 SyncConfig.PublicBaseURL 同一个值，但读路径拿不到那份配置，而它恰恰是
// 需要认出自家像素的那一侧。
func (s *Service) UsePublicBaseURL(base string) { s.selfHost = publicHostOf(base) }

func New(pool *pgxpool.Pool, d Deps, log *slog.Logger) *Service {
	return &Service{
		pool: pool, q: store.New(pool),
		number: d.Numbering, directory: d.Directory, scopes: d.Scopes,
		provider: d.Provider, files: d.Files, tables: d.Tables,
		pricing: d.Pricing,
		secrets: d.Secrets, live: d.Live, log: log,
	}
}

// origins returns the address classifier, building it on first use.
//
// Lazy rather than constructed in New because it is only ever touched by the
// tracking pixel route, and every other caller of New — every test that builds
// a Service — would otherwise pay for a field it never reads.
func (s *Service) origins() *originClassifier {
	s.originsOnce.Do(func() { s.originClass = newOriginClassifier(nil) })
	return s.originClass
}

// UseProvider installs the sending adapter after construction.
//
// The SMTP adapter needs the service to resolve each employee's credentials,
// and the service needs an adapter to send — so one of the two has to be
// wired in a second step. Doing it explicitly here beats a lazy indirection
// that hides the cycle.
func (s *Service) UseProvider(p Provider) { s.provider = p }

// ExcelAvailable is configuration state, not account data. It is exposed on
// the existing lightweight mailbox-capability read so the browser can avoid
// offering an action that can only fail.
func (s *Service) ExcelAvailable() bool { return s.tables != nil }

// scopeModule is the key mail is scoped under; it must match
// role_data_scopes.module in iam.
const scopeModule = "mail"

// visibleTo resolves whose sends this person may read. A failure to reach
// iam denies rather than widens: a visible error beats a silent leak of
// somebody else's correspondence.
func (s *Service) visibleTo(ctx context.Context, employeeID int64) (Visibility, error) {
	if s.scopes == nil {
		return Visibility{All: true, ScopeType: "ALL"}, nil
	}
	return s.scopes.VisibleEmployees(ctx, employeeID, scopeModule)
}

// mayRead decides whether this person may see correspondence sent by that
// one. The list queries filter in SQL; the single-record reads cannot, so
// they ask here — otherwise anyone who guessed an id would walk straight past
// the scope the list view enforces.
func (s *Service) mayRead(ctx context.Context, readerID, senderID int64) error {
	if readerID == senderID {
		return nil
	}
	visible, err := s.visibleTo(ctx, readerID)
	if err != nil {
		return err
	}
	if visible.All {
		return nil
	}
	for _, id := range visible.EmployeeIDs {
		if id == senderID {
			return nil
		}
	}
	// Not found rather than forbidden: whether a particular mail exists is
	// itself something the reader is not entitled to learn.
	return apierr.NotFound("NT_MESSAGE_NOT_FOUND", "邮件记录不存在")
}

// newMessageKey is generated before the provider is called and travels with
// the request as a tag. Mail providers do not offer request-level idempotency
// the way payment processors do, so this identifies a send after the fact
// rather than deduplicating it.
func newMessageKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func normalizePage(page, size int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}
