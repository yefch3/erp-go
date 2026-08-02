package provider

import (
	"log/slog"
	"time"

	"github.com/sgao19/erp-go/services/notification/internal/app"
)

// Pick resolves the configured provider name to an adapter.
//
// An unknown name falls back to the recording adapter rather than failing to
// start: a typo in an environment variable should not take the service down,
// and the log line says plainly that nothing will leave the building.
func Pick(name string, accounts Accounts, blobs Blobs, timeout time.Duration, log *slog.Logger) app.Provider {
	switch name {
	case "smtp":
		log.Info("mail will be sent over SMTP as each employee's own mailbox")
		return NewSMTP(accounts, blobs, timeout, log)
	case "dev", "":
		log.Info("mail provider is dev — nothing will actually be sent")
		return NewDev(log)
	default:
		log.Warn("unknown mail provider, falling back to dev — no mail will be sent",
			"requested", name)
		return NewDev(log)
	}
}
