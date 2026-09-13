package app

import (
	"strings"
	"testing"
)

func TestNormalizeHolidayCountries(t *testing.T) {
	got, err := normalizeHolidayCountries([]string{"us", " CN ", "US", ""})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "CN,US" {
		t.Fatalf("unexpected countries: %v", got)
	}
	if _, err = normalizeHolidayCountries([]string{"ZZ"}); err == nil {
		t.Fatal("unsupported country accepted")
	}
}

func TestParseHolidayICS(t *testing.T) {
	input := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nDTSTART;VALUE=DATE:20261001\r\nSUMMARY:National Day\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	events, err := parseHolidayICS(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Date != "2026-10-01" || events[0].Summary != "National Day" {
		t.Fatalf("unexpected events: %#v", events)
	}
}
