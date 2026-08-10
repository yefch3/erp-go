package app

import (
	"fmt"
	"strings"
	"time"
)

// Rendering a conversation into something a person outside the ERP can read.
//
// The output is one self-contained HTML file: no stylesheet, no script, no
// image, nothing fetched from anywhere. That is the property that makes it
// safe to hand over — the file does exactly the same thing on the customs
// broker's laptop as it does on ours, which is nothing.
//
// It is also the print master. The browser's own print engine turns it into a
// PDF, and that is not a shortcut around writing a PDF writer, it is the
// better answer for this document: the text is Chinese, English and Spanish,
// and the browser already has the fonts and — the part that is genuinely hard
// — the line-breaking rules for all three. A Go PDF library would need a
// ten-megabyte CJK font compiled into the binary and a hand-written line
// breaker that knows Chinese wraps between any two characters and English
// does not. Gmail's "print all" for a conversation is the same choice.

// exportLabels is the handful of words the document itself says. Trilingual
// like the rest of the product: the person exporting is about to give this to
// a colleague or a broker who reads what they read.
type exportLabels struct {
	docTitle     string
	conversation string
	with         string
	messages     string
	exportedBy   string
	exportedAt   string
	sent         string
	received     string
	attachments  string
	emptyBody    string
	converted    string
	clipped      string
	turnsOmitted string
	filesOmitted string
	printHint    string
	unknownTime  string
}

var exportLocales = map[string]exportLabels{
	"zh": {
		docTitle: "邮件会话", conversation: "会话主题", with: "往来对象",
		messages: "邮件数", exportedBy: "导出人", exportedAt: "导出时间",
		sent: "发出", received: "收到", attachments: "附件",
		emptyBody:    "（正文为空）",
		converted:    "由 HTML 正文转换为文本，表格与图片已丢失",
		clipped:      "正文过长，已截断",
		turnsOmitted: "另有 %d 封邮件未包含在本文件中",
		filesOmitted: "另有 %d 个附件未列出",
		printHint:    "在浏览器中按 Ctrl+P（Mac 为 ⌘P），目标选择「另存为 PDF」。",
		unknownTime:  "时间未知",
	},
	"en": {
		docTitle: "Mail conversation", conversation: "Subject", with: "With",
		messages: "Messages", exportedBy: "Exported by", exportedAt: "Exported at",
		sent: "Sent", received: "Received", attachments: "Attachments",
		emptyBody:    "(no body)",
		converted:    "converted from an HTML body; tables and images are lost",
		clipped:      "body truncated",
		turnsOmitted: "%d further messages are not included in this file",
		filesOmitted: "%d further attachments not listed",
		printHint:    "Press Ctrl+P (⌘P on a Mac) and choose “Save as PDF”.",
		unknownTime:  "time unknown",
	},
	"es": {
		docTitle: "Conversación de correo", conversation: "Asunto", with: "Con",
		messages: "Mensajes", exportedBy: "Exportado por", exportedAt: "Exportado el",
		sent: "Enviado", received: "Recibido", attachments: "Adjuntos",
		emptyBody:    "(sin contenido)",
		converted:    "convertido desde un cuerpo HTML; se pierden tablas e imágenes",
		clipped:      "contenido truncado",
		turnsOmitted: "hay %d mensajes más que no se incluyen en este archivo",
		filesOmitted: "hay %d adjuntos más sin listar",
		printHint:    "Pulsa Ctrl+P (⌘P en Mac) y elige «Guardar como PDF».",
		unknownTime:  "hora desconocida",
	},
}

// labelsFor picks the document's language. Chinese is the fallback because it
// is this company's working language, and a document in a language nobody at
// the desk reads is worse than one in the wrong one.
func labelsFor(lang string) exportLabels {
	if l, ok := exportLocales[strings.ToLower(strings.TrimSpace(lang))]; ok {
		return l
	}
	return exportLocales["zh"]
}

// renderTranscript writes the whole document.
func renderTranscript(ex ThreadExport) ExportDocument {
	l := labelsFor(ex.Lang)
	title := ex.Subject
	if title == "" {
		title = l.docTitle
	}

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"" + escapeForHTML(documentLang(ex.Lang)) + "\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	// No referrer and no outbound requests are possible from this document
	// anyway; the meta says so to anything that opens it and reads policy
	// before it reads content.
	b.WriteString("<meta name=\"referrer\" content=\"no-referrer\">\n")
	// The title is what a browser offers as the PDF's file name when this is
	// printed, so it is the file name rather than a heading.
	b.WriteString("<title>" + escapeForHTML(transcriptStem(ex)) + "</title>\n")
	b.WriteString("<style>\n" + transcriptCSS + "</style>\n</head>\n<body>\n")

	b.WriteString("<p class=\"hint\">" + escapeForHTML(l.printHint) + "</p>\n")

	b.WriteString("<header>\n<h1>" + escapeForHTML(title) + "</h1>\n<dl>\n")
	writeField(&b, l.with, ex.Counterparty)
	writeField(&b, l.messages, fmt.Sprintf("%d", len(ex.Turns)))
	writeField(&b, l.exportedBy, ex.ExportedBy)
	writeField(&b, l.exportedAt, stamp(ex.ExportedAt, ex.Zone, l))
	b.WriteString("</dl>\n</header>\n")

	for _, t := range ex.Turns {
		writeTurn(&b, t, ex.Zone, l)
	}

	if ex.TurnsOmitted > 0 {
		b.WriteString("<p class=\"omitted\">" +
			escapeForHTML(fmt.Sprintf(l.turnsOmitted, ex.TurnsOmitted)) + "</p>\n")
	}
	b.WriteString("</body>\n</html>\n")

	return ExportDocument{
		FileName:    transcriptStem(ex) + ".html",
		ContentType: "text/html; charset=utf-8",
		Format:      "HTML",
		Content:     []byte(b.String()),
		TurnCount:   len(ex.Turns),
	}
}

// writeTurn is one message: a header line that never separates from at least
// the start of its body, then the body as it was written.
func writeTurn(b *strings.Builder, t ExportTurn, zone *time.Location, l exportLabels) {
	dir, cls := l.received, "in"
	if t.Direction == "OUT" {
		dir, cls = l.sent, "out"
	}
	b.WriteString("<article class=\"turn " + cls + "\">\n<h2>")
	b.WriteString("<span class=\"dir\">" + escapeForHTML(dir) + "</span> ")
	if t.Who != "" {
		b.WriteString("<span class=\"who\">" + escapeForHTML(t.Who) + "</span> ")
	}
	if t.Address != "" {
		b.WriteString("<span class=\"addr\">&lt;" + escapeForHTML(t.Address) + "&gt;</span>")
	}
	b.WriteString("<span class=\"when\">" + escapeForHTML(stamp(t.At, zone, l)) + "</span>")
	b.WriteString("</h2>\n")
	if t.Subject != "" {
		b.WriteString("<p class=\"subj\">" + escapeForHTML(t.Subject) + "</p>\n")
	}

	var notes []string
	if t.Derived {
		notes = append(notes, l.converted)
	}
	if t.Clipped {
		notes = append(notes, l.clipped)
	}
	if len(notes) > 0 {
		b.WriteString("<p class=\"note\">" + escapeForHTML(strings.Join(notes, " · ")) + "</p>\n")
	}

	if t.Text == "" {
		b.WriteString("<p class=\"empty\">" + escapeForHTML(l.emptyBody) + "</p>\n")
	} else {
		b.WriteString("<pre class=\"body\">" + escapeForHTML(t.Text) + "</pre>\n")
	}

	if len(t.Files) > 0 || t.FilesOmitted > 0 {
		b.WriteString("<p class=\"files\"><span class=\"flabel\">" +
			escapeForHTML(l.attachments) + "</span> ")
		names := make([]string, 0, len(t.Files))
		for _, f := range t.Files {
			names = append(names, escapeForHTML(f.Name)+" <span class=\"fsize\">("+humanBytes(f.Size)+")</span>")
		}
		b.WriteString(strings.Join(names, "， "))
		if t.FilesOmitted > 0 {
			b.WriteString(" <span class=\"omitted\">" +
				escapeForHTML(fmt.Sprintf(l.filesOmitted, t.FilesOmitted)) + "</span>")
		}
		b.WriteString("</p>\n")
	}
	b.WriteString("</article>\n")
}

func writeField(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString("<dt>" + escapeForHTML(label) + "</dt><dd>" + escapeForHTML(value) + "</dd>\n")
}

// stamp prints an instant with its offset spelled out.
//
// The offset is not decoration. A transcript read six months later in another
// country is asked "when did they actually reply", and "14:32" alone cannot
// answer it. Printing +08:00 next to it means a wrong zone is visible rather
// than silently shifting the record.
func stamp(at time.Time, zone *time.Location, l exportLabels) string {
	if at.IsZero() {
		return l.unknownTime
	}
	if zone == nil {
		zone = time.UTC
	}
	return at.In(zone).Format("2006-01-02 15:04 -07:00")
}

// documentLang maps our locale keys onto the lang attribute. zh alone leaves
// a reader agent guessing between simplified and traditional, and the product
// is simplified.
func documentLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "en":
		return "en"
	case "es":
		return "es"
	default:
		return "zh-Hans"
	}
}

// humanBytes is the size as a person reads it. Two significant places under
// ten, none above: "1.4 MB" and "312 KB" are both the right precision.
func humanBytes(n int64) string {
	switch {
	case n < 0:
		return "?"
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n) / 1024
	for _, u := range units {
		if v < 1024 || u == "TB" {
			if v < 10 {
				return fmt.Sprintf("%.1f %s", v, u)
			}
			return fmt.Sprintf("%.0f %s", v, u)
		}
		v /= 1024
	}
	return fmt.Sprintf("%d B", n)
}

// transcriptStem is the file name without its extension, and the print
// title. Built from what the reader would call this conversation rather than
// from ids: a folder of "export-4471.html" is a folder nobody can search.
func transcriptStem(ex ThreadExport) string {
	l := labelsFor(ex.Lang)
	parts := []string{l.docTitle}
	if ex.Counterparty != "" {
		parts = append(parts, ex.Counterparty)
	}
	when := ex.ExportedAt
	if when.IsZero() {
		when = time.Now()
	}
	zone := ex.Zone
	if zone == nil {
		zone = time.UTC
	}
	parts = append(parts, when.In(zone).Format("2006-01-02"))
	return safeFileStem(strings.Join(parts, "-"))
}

// safeFileStem removes what a file name may not contain and bounds what it
// may. The counterparty address is attacker-influenced in the only sense that
// matters here — anybody can mail us from an address of their choosing — so
// path separators, quotes and control characters go before this reaches a
// Content-Disposition header.
func safeFileStem(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			// control characters: a newline here is a header injection
		case strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
		if b.Len() > 120 {
			break
		}
	}
	out := strings.Trim(b.String(), " .-")
	if out == "" {
		return "mail-conversation"
	}
	return out
}

// transcriptCSS is deliberately small and deliberately inline. An external
// stylesheet would be a request this document must never make.
const transcriptCSS = `
:root { --ink:#1a1a1a; --muted:#6b7280; --rule:#d7dbe0; --tint:#f6f8fa; }
* { box-sizing: border-box; }
body {
  margin: 0 auto; padding: 24px 28px 48px; max-width: 820px;
  color: var(--ink); background: #fff;
  font: 14px/1.65 -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei",
        "Noto Sans CJK SC", "Source Han Sans SC", sans-serif;
}
.hint {
  margin: 0 0 20px; padding: 8px 12px; border-radius: 6px;
  background: var(--tint); color: var(--muted); font-size: 12px;
}
header { border-bottom: 2px solid var(--ink); padding-bottom: 14px; margin-bottom: 8px; }
h1 { margin: 0 0 10px; font-size: 21px; line-height: 1.35; }
dl { display: grid; grid-template-columns: max-content 1fr; gap: 2px 14px; margin: 0; font-size: 12.5px; }
dt { color: var(--muted); }
dd { margin: 0; }
.turn { border-bottom: 1px solid var(--rule); padding: 16px 0; }
.turn:last-of-type { border-bottom: 0; }
h2 {
  margin: 0; font-size: 13px; font-weight: 600; line-height: 1.5;
  display: flex; flex-wrap: wrap; align-items: baseline; gap: 6px;
}
.dir {
  font-size: 11px; font-weight: 700; letter-spacing: .04em;
  padding: 1px 7px; border-radius: 3px; border: 1px solid var(--rule);
  color: var(--muted);
}
.out .dir { background: var(--ink); border-color: var(--ink); color: #fff; }
.addr { color: var(--muted); font-weight: 400; }
.when { margin-left: auto; color: var(--muted); font-weight: 400; font-variant-numeric: tabular-nums; }
.subj { margin: 6px 0 0; font-size: 13px; color: var(--muted); }
.note, .omitted { margin: 6px 0 0; font-size: 11.5px; color: var(--muted); font-style: italic; }
.empty { margin: 10px 0 0; color: var(--muted); }
.body {
  margin: 10px 0 0; white-space: pre-wrap; overflow-wrap: anywhere;
  tab-size: 8; font: inherit;
}
.files { margin: 12px 0 0; font-size: 12.5px; }
.flabel { color: var(--muted); }
.fsize { color: var(--muted); }

@page { size: A4; margin: 16mm 14mm 18mm; }
@media print {
  body { max-width: none; padding: 0; font-size: 11pt; }
  .hint { display: none; }
  /* A header stranded at the foot of a page reads as a message with no
     content; the body may break freely, its heading may not leave it. */
  h2, .subj { break-after: avoid-page; }
  .turn { break-inside: auto; }
  header { break-after: avoid-page; }
}
`
