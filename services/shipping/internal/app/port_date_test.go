package app

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestPortDateRoundTrip(t *testing.T) {
	d := pgtype.Date{Time: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), Valid: true}
	for _, zone := range []string{"Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin"} {
		t.Run(zone, func(t *testing.T) {
			got := timestampEventDate(dateTimestamp(d, zone), zone)
			if !got.Valid || dateText(got) != dateText(d) {
				t.Fatalf("port date changed from %s to %s", dateText(d), dateText(got))
			}
		})
	}
}
