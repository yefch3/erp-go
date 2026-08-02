// Package provider holds the sending adapters. The interface takes one
// message at a time by construction, so "recipients never see each other"
// cannot be broken by a later change here.
package provider

import (
	"context"
	"log/slog"
	"strings"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// Dev records what would have been sent instead of sending it.
//
// The provider has not been chosen and merchant onboarding has not happened,
// so nothing can actually go out yet. Everything upstream of this point —
// rendering, the queue, retry classification, the unknown-outcome rule, the
// human queue — is real and testable against this adapter; swapping in SES
// or SendGrid is one file.
//
// It also lets the refusal paths be exercised on demand: an address in a
// magic domain produces a permanent failure, a temporary one, or a timeout,
// which is otherwise very hard to arrange on purpose.
type Dev struct {
	log *slog.Logger
}

func NewDev(log *slog.Logger) *Dev { return &Dev{log: log} }

func (d *Dev) Name() string { return "dev (records, does not send)" }

func (d *Dev) Send(_ context.Context, m app.Outbound) app.SendResult {
	switch {
	case strings.HasSuffix(m.ToEmail, "@bounce.test"):
		return app.SendResult{
			Outcome: app.Permanent,
			Err:     "550 5.1.1 Recipient address rejected: User unknown",
		}
	case strings.HasSuffix(m.ToEmail, "@defer.test"):
		return app.SendResult{
			Outcome: app.Retryable,
			Err:     "451 4.7.1 Greylisted, please try again later",
		}
	case strings.HasSuffix(m.ToEmail, "@timeout.test"):
		return app.SendResult{
			Outcome: app.Unknown,
			Err:     "context deadline exceeded while awaiting response",
		}
	}
	names := make([]string, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		names = append(names, a.FileName)
	}
	d.log.Info("email (not actually sent)",
		"to", m.ToEmail, "subject", m.Subject, "key", m.MessageKey,
		"format", m.Format, "body_chars", len(m.Body),
		"text_chars", len(m.BodyText), "attachments", strings.Join(names, ","))
	return app.SendResult{Outcome: app.Accepted, ProviderID: "dev-" + m.MessageKey}
}
