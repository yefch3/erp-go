package provider

import (
	"log/slog"

	"github.com/sgao19/erp-go/services/notification/internal/app"
)

// Pick resolves the configured provider name to an adapter.
//
// An unknown name falls back to the recording adapter rather than failing to
// start: a typo in an environment variable should not take the service down,
// and the log line says plainly that nothing will leave the building.
func Pick(name string, log *slog.Logger) app.Provider {
	switch name {
	case "dev", "":
		return NewDev(log)
	default:
		log.Warn("unknown mail provider, falling back to dev — no mail will be sent",
			"requested", name)
		return NewDev(log)
	}
}
