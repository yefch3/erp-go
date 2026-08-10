package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// exportMailThread hands over one conversation as a file.
//
// The only route in the mail module that writes raw bytes instead of the JSON
// envelope, because what it returns is a document rather than a record. The
// browser cannot fetch it with a plain link — the mailbox unlock travels in a
// header — so the page fetches it and saves the blob; see EmailsPage.
func (s *Server) exportMailThread(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	op, _ := grpcx.OperatorFromContext(r.Context())
	resp, err := s.Emails.ExportMailThread(r.Context(), &mailv1.ExportMailThreadRequest{
		ThreadKey: q.Get("key"),
		// The reader's own clock and language. Both come from the browser
		// because the server has neither: there is no per-tenant timezone
		// setting and no server-side notion of who reads what.
		Zone: q.Get("tz"),
		Lang: q.Get("lang"),
		// Resolved here, never taken from the caller. A client-supplied
		// address in an audit log is a field the audited party fills in.
		ClientIp: op.IP,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}

	w.Header().Set("Content-Type", resp.GetContentType())
	w.Header().Set("Content-Disposition", attachmentDisposition(resp.GetFileName()))
	w.Header().Set("Content-Length", strconv.Itoa(len(resp.GetContent())))
	// This document is never something to render at our origin, cache in a
	// shared proxy, or index. It is one person's copy of one conversation.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.GetContent())
}

func (s *Server) listMailExports(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	resp, err := s.Emails.ListMailExports(r.Context(), &mailv1.ListMailExportsRequest{
		Page: int32(page), Size: int32(size),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// attachmentDisposition names the file for the browser, twice.
//
// RFC 6266: the quoted `filename` is the fallback for anything that does not
// understand the extended form, and it has to be plain ASCII — a header is
// bytes, and 会话-... in a bare filename= is either mojibake or a rejected
// response depending on the client. `filename*` carries the real name
// percent-encoded as UTF-8, which every browser released this decade prefers.
//
// The name arrives already stripped of control characters and path
// separators (see safeFileStem in the mail service); the quoting here is the
// second line of that defence rather than the first, because a header
// injection through a file name is the sort of thing that gets reintroduced
// by a well-meaning change on the other side of the wire.
func attachmentDisposition(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, name)
	if name == "" {
		name = "conversation.html"
	}
	ascii := asciiFallback(name)
	return `attachment; filename="` + ascii + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// asciiFallback keeps the shape of the name — its extension above all — for
// clients that only read the plain form. A Chinese name reduces to its
// punctuation and extension rather than to nothing, which is enough for the
// file to open in the right application.
//
// Runs of replaced characters collapse to one, because 邮件会话 is four runes
// and "____-buyer@example.com-2026-08-08.html" is a worse name than
// "buyer@example.com-2026-08-08.html" — the leading rubble is what a person
// sees first in a download list.
func asciiFallback(name string) string {
	var b strings.Builder
	dropped := false
	for _, r := range name {
		if r < 0x80 {
			b.WriteRune(r)
			dropped = false
			continue
		}
		if !dropped {
			b.WriteByte('_')
			dropped = true
		}
	}
	out := strings.Trim(b.String(), " _-")
	if out == "" || strings.HasPrefix(out, ".") {
		return "conversation.html"
	}
	return out
}
