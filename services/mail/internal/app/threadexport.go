package app

import (
	"context"
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// Exporting a conversation.
//
// What leaves the building is a transcript: who wrote to whom, when, what it
// said, and what was attached — assembled here as plain data and rendered by
// exportdoc.go. Three decisions shape the whole thing and are worth stating
// before the code, because each of them is a "why not the obvious way".
//
// **It is text, not the sender's HTML.** The obvious export keeps the original
// markup, and for reading your own mail on your own screen that is right — it
// is what the reader already does, inside a sandboxed frame. A file that
// leaves the company is a different object. Keeping the markup would mean a
// stranger's stylesheet and a stranger's <img> in a document that will be
// opened on the boss's laptop and the customs broker's, where every remote
// image is a "this was opened, at this address, at this hour" beacon back to
// whoever sent the mail — including the tracking pixels we refuse to send
// ourselves. Text costs the layout of the original and buys a document that
// does nothing when opened. The original MIME is still stored and still
// forwardable when fidelity is what is wanted.
//
// **The record is written before the bytes are handed over.** Not after, and
// not in the background. If the log cannot be written the export does not
// happen — an export nobody can prove happened is the exact thing this
// feature would otherwise add to the system.
//
// **It is owner-scoped, like the thread read it mirrors.** A thread key is
// guessable; what it opens must not be.

const (
	// A conversation longer than this is not correspondence any more, it is a
	// mailing list. The cap exists so one request cannot pull an unbounded
	// amount into memory; it is announced in the document rather than
	// applied quietly, because a transcript that stops early without saying
	// so is worse than no transcript.
	exportMaxTurns = 300
	// Roughly fifty pages of prose in one message. Past this the mail is a
	// database dump somebody pasted, and the same "say so" rule applies.
	exportMaxBodyRunes = 50000
	// Attachment names per message. A message with more files than this
	// exists, but listing four hundred of them helps nobody.
	exportMaxFiles = 50
)

// ExportFile is one attachment as the transcript lists it: what it was
// called and how big it was. The bytes stay where they are.
type ExportFile struct {
	Name string
	Size int64
}

// ExportTurn is one message of the conversation.
type ExportTurn struct {
	// OUT = we sent it, IN = it arrived.
	Direction string
	Subject   string
	// The person, as far as we know them: a display name, or empty.
	Who string
	// The other side's address — the recipient of a send, the sender of an
	// arrival.
	Address string
	At      time.Time
	Text    string
	Files   []ExportFile
	// The body was HTML and we converted it. Recorded because it changes what
	// the reader is looking at: tables lose their columns, images vanish. The
	// document says so per message rather than in a disclaimer nobody reads.
	Derived bool
	// The body ran past exportMaxBodyRunes and was cut.
	Clipped bool
	// More files than exportMaxFiles; this many were not listed.
	FilesOmitted int
}

// ThreadExport is the whole conversation, ready to render.
type ThreadExport struct {
	ThreadKey    string
	Subject      string
	Counterparty string
	Turns        []ExportTurn
	// Who asked for it and when — printed in the document, because a
	// transcript with no provenance is an anonymous claim.
	ExportedBy string
	ExportedAt time.Time
	// Turns dropped by exportMaxTurns, zero in every ordinary case.
	TurnsOmitted int
	// The zone every timestamp in the document is printed in.
	Zone *time.Location
	// The language the document's own words are written in — zh, en or es.
	// Not the language of the mail, which is whatever the customer wrote.
	Lang string
}

// ExportDocument is the rendered artifact. Content type and file name travel
// with the bytes because the transport layer must not have to guess either.
type ExportDocument struct {
	FileName    string
	ContentType string
	// The shape it was rendered in, as the log records it.
	Format    string
	Content   []byte
	TurnCount int
}

// ExportMailThread assembles one conversation, records the export, and
// returns the document.
//
// The order is deliberate and is the whole of #96: build, log, hand over. A
// failure at any step means nothing leaves, and in particular a failure to
// log means nothing leaves.
func (s *Service) ExportMailThread(
	ctx context.Context, tenantID int64, op Operator, req ExportRequest,
) (ExportDocument, error) {
	ex, err := s.buildThreadExport(ctx, tenantID, op, req)
	if err != nil {
		return ExportDocument{}, err
	}
	doc := renderTranscript(ex)

	if _, err := s.q.RecordExport(ctx, store.RecordExportParams{
		TenantID:     tenantID,
		EmployeeID:   op.ID,
		EmployeeName: op.Name,
		ThreadKey:    ex.ThreadKey,
		Subject:      ex.Subject,
		Counterparty: ex.Counterparty,
		TurnCount:    int32(len(ex.Turns)),
		ByteSize:     int64(len(doc.Content)),
		Format:       doc.Format,
		ClientIp:     req.ClientIP,
	}); err != nil {
		// Deliberately not degraded to a warning. The permission says who may
		// export; the log is what makes the permission mean anything, and a
		// path that hands over the file when the log is down is a path
		// somebody can arrange to take.
		return ExportDocument{}, err
	}
	return doc, nil
}

// ExportRequest is what the caller controls: which conversation, and the two
// presentation choices that belong to the reader rather than to the server —
// their clock and their language.
type ExportRequest struct {
	ThreadKey string
	// IANA zone name from the browser, e.g. Asia/Shanghai. Empty means UTC.
	Zone string
	// UI locale: zh, en or es. Anything else falls back to zh.
	Lang string
	// Where the request came from, as the gateway resolved it. Recorded, never
	// trusted for a decision.
	ClientIP string
}

// buildThreadExport turns the two halves of a conversation into one ordered
// transcript. Pure assembly beyond the queries: the shaping decisions are in
// exportTurn, which is testable without a database.
func (s *Service) buildThreadExport(
	ctx context.Context, tenantID int64, op Operator, req ExportRequest,
) (ThreadExport, error) {
	threadKey := strings.TrimSpace(req.ThreadKey)
	if threadKey == "" {
		return ThreadExport{}, apierr.Invalid("NT_THREAD_KEY_REQUIRED", "缺少会话标识")
	}
	rows, err := s.q.ListThreadForExport(ctx, store.ListThreadForExportParams{
		TenantID: tenantID, OwnerID: op.ID, ThreadKey: threadKey,
	})
	if err != nil {
		return ThreadExport{}, err
	}
	if len(rows) == 0 {
		// Not found rather than an empty document: an empty transcript of a
		// conversation that does not exist, or is somebody else's, would be a
		// file that looks like an answer.
		return ThreadExport{}, apierr.NotFound("NT_THREAD_NOT_FOUND", "会话不存在或不属于你")
	}

	files, err := s.threadFiles(ctx, tenantID, op.ID, threadKey)
	if err != nil {
		return ThreadExport{}, err
	}

	ex := ThreadExport{
		ThreadKey:  threadKey,
		ExportedBy: op.Name,
		ExportedAt: time.Now(),
		Zone:       loadZone(req.Zone),
		Lang:       req.Lang,
	}
	if len(rows) > exportMaxTurns {
		ex.TurnsOmitted = len(rows) - exportMaxTurns
		rows = rows[:exportMaxTurns]
	}
	for _, r := range rows {
		ex.Turns = append(ex.Turns, exportTurn(r, files[fileKey{r.Direction, r.ID}]))
	}
	ex.Subject, ex.Counterparty = threadHeading(ex.Turns)
	return ex, nil
}

// ListExports reads the record back.
//
// Scoped through the same visibility as the team mail view, so a sales
// manager sees their own team's exports and a super admin sees everyone's.
// Deliberately not self-scoped-by-default the way mail is: a log of your own
// exports, readable only by you, records nothing anybody needed recording.
func (s *Service) ListExports(ctx context.Context, tenantID int64, op Operator, page, size int32) ([]store.ListExportsRow, int64, error) {
	visible, err := s.visibleTo(ctx, op.ID)
	if err != nil {
		return nil, 0, err
	}
	page, size = normalizePage(page, size)
	rows, err := s.q.ListExports(ctx, store.ListExportsParams{
		TenantID: tenantID, VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.q.CountExports(ctx, store.CountExportsParams{
		TenantID: tenantID, VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// fileKey identifies a message across the union: ids are only unique within
// their own table, so a sent message 7 and a received message 7 both exist.
type fileKey struct {
	direction string
	id        int64
}

// threadFiles collects both sides' attachments in one pass, keyed by message.
func (s *Service) threadFiles(ctx context.Context, tenantID, ownerID int64, threadKey string) (map[fileKey][]ExportFile, error) {
	out := map[fileKey][]ExportFile{}
	in, err := s.q.ListThreadInboundFiles(ctx, store.ListThreadInboundFilesParams{
		TenantID: tenantID, OwnerID: ownerID, ThreadKey: threadKey,
	})
	if err != nil {
		return nil, err
	}
	for _, f := range in {
		k := fileKey{"IN", f.MessageID}
		out[k] = append(out[k], ExportFile{Name: f.FileName, Size: f.FileSize})
	}
	sent, err := s.q.ListThreadSentFiles(ctx, store.ListThreadSentFilesParams{
		TenantID: tenantID, OwnerID: ownerID, ThreadKey: threadKey,
	})
	if err != nil {
		return nil, err
	}
	for _, f := range sent {
		k := fileKey{"OUT", f.MessageID}
		out[k] = append(out[k], ExportFile{Name: f.FileName, Size: f.FileSize})
	}
	return out, nil
}

// exportTurn decides what one message contributes to the transcript.
//
// The body choice is the interesting part. A received mail usually arrives as
// multipart/alternative, and its text half was written by the sender's own
// client — which knows where their table's columns were and where the quoted
// history starts. Our HTMLToText knows neither. So the sender's text wins
// whenever there is one, and the conversion is the fallback it was built to
// be. Marked either way, so the reader can tell which they are holding.
func exportTurn(r store.ListThreadForExportRow, files []ExportFile) ExportTurn {
	t := ExportTurn{
		Direction: r.Direction,
		Subject:   strings.TrimSpace(r.Subject),
		Who:       strings.TrimSpace(r.Who),
		Address:   strings.TrimSpace(r.Counterparty),
	}
	if r.At.Valid {
		t.At = r.At.Time
	}

	if text := strings.TrimSpace(r.BodyText); text != "" {
		t.Text = StripInvisible(text)
	} else if html := strings.TrimSpace(r.BodyHtml); html != "" {
		if strings.EqualFold(r.BodyFormat, FormatHTML) {
			t.Text, t.Derived = HTMLToText(html), true
		} else {
			// An outbound mail composed as plain text: the column is called
			// body whatever format it holds, and converting it would eat any
			// literal < the writer typed.
			t.Text = StripInvisible(html)
		}
	}
	if runes := []rune(t.Text); len(runes) > exportMaxBodyRunes {
		t.Text, t.Clipped = string(runes[:exportMaxBodyRunes]), true
	}

	if len(files) > exportMaxFiles {
		t.FilesOmitted = len(files) - exportMaxFiles
		files = files[:exportMaxFiles]
	}
	t.Files = files
	return t
}

// threadHeading names the conversation: the subject it started with, and the
// address on the other side of it.
//
// The earliest subject rather than the latest, because that is the one
// without eight "Re:" in front of it. The most-seen address rather than the
// first, because a thread that begins with a mail to a shared enquiries
// address and then runs for six months with one salesperson is about the
// salesperson.
func threadHeading(turns []ExportTurn) (subject, counterparty string) {
	seen := map[string]int{}
	best := 0
	for _, t := range turns {
		if subject == "" && t.Subject != "" {
			subject = t.Subject
		}
		if t.Address == "" {
			continue
		}
		seen[t.Address]++
		if seen[t.Address] > best {
			best, counterparty = seen[t.Address], t.Address
		}
	}
	return subject, counterparty
}

// loadZone resolves the reader's own zone, falling back to UTC.
//
// The zone comes from the browser because there is nowhere better to get it:
// the server has no per-tenant clock setting, and a transcript that says a
// customer replied at 03:14 when it was a quarter past eleven in the morning
// is worse than useless in the one conversation where the hour matters. The
// rendered timestamps carry their offset, so a wrong or missing zone shows
// itself instead of quietly shifting the record.
func loadZone(name string) *time.Location {
	if name = strings.TrimSpace(name); name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.UTC
}
