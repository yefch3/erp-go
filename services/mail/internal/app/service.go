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
	Err        string
	// Recipients the host refused individually while accepting the
	// transaction as a whole. Only ever set for merged sends.
	Rejected []RecipientReject
}

// Outbound is one rendered message on its way out.
type Outbound struct {
	MessageKey string
	// Who this is being sent as. Everybody sends through their own mailbox,
	// so the adapter has to authenticate as this person rather than as one
	// shared account — these two fields are what it resolves credentials by.
	TenantID int64
	SenderID int64
	FromName string
	ToEmail  string
	ToName   string
	Subject  string
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
	// A merged send: when ToList is non-empty it replaces ToEmail/ToName
	// entirely — the To header carries the whole list, everybody sees each
	// other, and the envelope covers every entry plus CCList. Both stay
	// empty for the normal one-recipient send, which keeps the isolation
	// guarantee the comment on Provider describes.
	ToList []NamedAddress
	CCList []NamedAddress
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
	// Secrets decrypts stored mailbox credentials. Nil in tests and in any
	// deployment that has not been given a key; every path that needs it
	// checks and fails loudly rather than proceeding without encryption.
	Secrets *SecretBox
	// Live pushes "go and re-read" hints to open browser tabs. Nil disables
	// pushing; the page still refreshes on the next manual or timed load.
	Live *livefeed.Publisher
}

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	number    Numbering
	directory Directory
	scopes    Scopes
	provider  Provider
	files     Files
	mailbox   Mailbox
	secrets   *SecretBox
	live      *livefeed.Publisher
	oauth     OAuthConfig
	// Access tokens by account id. They live an hour; caching them keeps the
	// sender and the sync from asking Google once per message.
	tokenCache sync.Map
	// Where each account's special folders live, keyed "sent:<id>" and
	// "junk:<id>"; found once, never changes.
	sentFolders sync.Map
	log         *slog.Logger
}

func New(pool *pgxpool.Pool, d Deps, log *slog.Logger) *Service {
	return &Service{
		pool: pool, q: store.New(pool),
		number: d.Numbering, directory: d.Directory, scopes: d.Scopes,
		provider: d.Provider, files: d.Files, secrets: d.Secrets, live: d.Live, log: log,
	}
}

// UseProvider installs the sending adapter after construction.
//
// The SMTP adapter needs the service to resolve each employee's credentials,
// and the service needs an adapter to send — so one of the two has to be
// wired in a second step. Doing it explicitly here beats a lazy indirection
// that hides the cycle.
func (s *Service) UseProvider(p Provider) { s.provider = p }

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
