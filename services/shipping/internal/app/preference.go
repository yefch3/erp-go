package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

var googleHolidayCalendarIDs = map[string]string{
	"CN": "en.china#holiday@group.v.calendar.google.com",
	"US": "en.usa#holiday@group.v.calendar.google.com",
	"GB": "en.uk#holiday@group.v.calendar.google.com",
	"DE": "en.german#holiday@group.v.calendar.google.com",
	"FR": "en.french#holiday@group.v.calendar.google.com",
	"NL": "en.dutch#holiday@group.v.calendar.google.com",
	"IT": "en.italian#holiday@group.v.calendar.google.com",
	"ES": "en.spain#holiday@group.v.calendar.google.com",
	"JP": "en.japanese#holiday@group.v.calendar.google.com",
	"KR": "en.south_korea#holiday@group.v.calendar.google.com",
	"SG": "en.singapore#holiday@group.v.calendar.google.com",
	"HK": "en.hong_kong#holiday@group.v.calendar.google.com",
}

type UserReminderPreference struct {
	LeadDays            []int32
	Timezone            string
	HolidayCountryCodes []string
	CalendarSyncStatus  string
	LastSyncAt          string
	LastSuccessAt       string
	LastError           string
}

type holidayEvent struct {
	Date    string `json:"date"`
	Summary string `json:"summary"`
}

func reminderPreferenceFromStore(row store.ShippingUserReminderPreference) UserReminderPreference {
	result := UserReminderPreference{
		LeadDays: slices.Clone(row.LeadDays), Timezone: row.Timezone,
		HolidayCountryCodes: slices.Clone(row.HolidayCountryCodes),
		CalendarSyncStatus:  row.CalendarSyncStatus, LastError: row.LastError,
	}
	if row.LastSyncAt.Valid {
		result.LastSyncAt = row.LastSyncAt.Time.Format(time.RFC3339)
	}
	if row.LastSuccessAt.Valid {
		result.LastSuccessAt = row.LastSuccessAt.Time.Format(time.RFC3339)
	}
	return result
}

func normalizeHolidayCountries(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		code := strings.ToUpper(strings.TrimSpace(value))
		if code == "" {
			continue
		}
		if _, ok := googleHolidayCalendarIDs[code]; !ok {
			return nil, apierr.Invalid("SHIPPING_HOLIDAY_COUNTRY_UNSUPPORTED", "暂不支持国家或地区："+code)
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	slices.Sort(out)
	return out, nil
}

func (s *Service) GetUserReminderPreference(ctx context.Context, tenantID int64, op Operator) (UserReminderPreference, error) {
	if op.ID <= 0 {
		return UserReminderPreference{}, apierr.Permission("SHIPPING_OPERATOR_REQUIRED", "无法识别当前用户")
	}
	row, err := s.q.GetUserReminderPreference(ctx, store.GetUserReminderPreferenceParams{TenantID: tenantID, EmployeeID: op.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return UserReminderPreference{LeadDays: []int32{7}, Timezone: "UTC", CalendarSyncStatus: "NOT_CONFIGURED"}, nil
	}
	if err != nil {
		return UserReminderPreference{}, err
	}
	return reminderPreferenceFromStore(row), nil
}

func (s *Service) UpdateUserReminderPreference(ctx context.Context, tenantID int64, days []int32, timezone string, countries []string, op Operator) (UserReminderPreference, error) {
	if op.ID <= 0 {
		return UserReminderPreference{}, apierr.Permission("SHIPPING_OPERATOR_REQUIRED", "无法识别当前用户")
	}
	normalizedDays, err := normalizeArrivalReminderDays(days)
	if err != nil {
		return UserReminderPreference{}, err
	}
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err = time.LoadLocation(timezone); err != nil {
		return UserReminderPreference{}, apierr.Invalid("SHIPPING_TIMEZONE_INVALID", "业务时区无效")
	}
	normalizedCountries, err := normalizeHolidayCountries(countries)
	if err != nil {
		return UserReminderPreference{}, err
	}
	row, err := s.q.UpsertUserReminderPreference(ctx, store.UpsertUserReminderPreferenceParams{
		TenantID: tenantID, EmployeeID: op.ID, LeadDays: normalizedDays, Timezone: timezone, HolidayCountryCodes: normalizedCountries,
	})
	if err != nil {
		return UserReminderPreference{}, err
	}
	if len(normalizedCountries) == 0 {
		row, err = s.q.UpdateUserReminderCalendarSync(ctx, store.UpdateUserReminderCalendarSyncParams{
			TenantID: tenantID, EmployeeID: op.ID, CalendarSyncStatus: "NOT_CONFIGURED", LastError: "", CachedHolidays: []byte("{}"),
		})
		if err != nil {
			return UserReminderPreference{}, err
		}
		return reminderPreferenceFromStore(row), nil
	}
	return s.syncUserHolidayCalendars(ctx, tenantID, op.ID, row)
}

func (s *Service) SyncUserHolidayCalendars(ctx context.Context, tenantID int64, op Operator) (UserReminderPreference, error) {
	if op.ID <= 0 {
		return UserReminderPreference{}, apierr.Permission("SHIPPING_OPERATOR_REQUIRED", "无法识别当前用户")
	}
	row, err := s.q.GetUserReminderPreference(ctx, store.GetUserReminderPreferenceParams{TenantID: tenantID, EmployeeID: op.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return UserReminderPreference{LeadDays: []int32{7}, Timezone: "UTC", CalendarSyncStatus: "NOT_CONFIGURED"}, nil
	}
	if err != nil {
		return UserReminderPreference{}, err
	}
	if len(row.HolidayCountryCodes) == 0 {
		return reminderPreferenceFromStore(row), nil
	}
	return s.syncUserHolidayCalendars(ctx, tenantID, op.ID, row)
}

func (s *Service) syncUserHolidayCalendars(ctx context.Context, tenantID, employeeID int64, current store.ShippingUserReminderPreference) (UserReminderPreference, error) {
	cache := make(map[string][]holidayEvent, len(current.HolidayCountryCodes))
	errorsByCountry := make([]string, 0)
	type syncResult struct {
		code   string
		events []holidayEvent
		err    error
	}
	results := make(chan syncResult, len(current.HolidayCountryCodes))
	for _, code := range current.HolidayCountryCodes {
		go func(country string) {
			events, err := s.fetchGoogleHolidayCalendar(ctx, googleHolidayCalendarIDs[country])
			results <- syncResult{country, events, err}
		}(code)
	}
	for range current.HolidayCountryCodes {
		result := <-results
		if result.err != nil {
			errorsByCountry = append(errorsByCountry, result.code+": "+result.err.Error())
		} else {
			cache[result.code] = result.events
		}
	}
	status := "SYNCED"
	lastError := ""
	cacheJSON := current.CachedHolidays
	if len(errorsByCountry) > 0 {
		lastError = strings.Join(errorsByCountry, "; ")
		if current.LastSuccessAt.Valid && len(current.CachedHolidays) > 2 {
			status = "STALE"
		} else {
			status = "FAILED"
		}
	} else {
		cacheJSON, _ = json.Marshal(cache)
	}
	row, err := s.q.UpdateUserReminderCalendarSync(ctx, store.UpdateUserReminderCalendarSyncParams{
		TenantID: tenantID, EmployeeID: employeeID, CalendarSyncStatus: status, LastError: lastError, CachedHolidays: cacheJSON,
	})
	if err != nil {
		return UserReminderPreference{}, err
	}
	return reminderPreferenceFromStore(row), nil
}

func (s *Service) fetchGoogleHolidayCalendar(ctx context.Context, calendarID string) ([]holidayEvent, error) {
	endpoint := strings.TrimRight(s.holidayBaseURL, "/") + "/" + url.PathEscape(calendarID) + "/public/basic.ics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.holidayClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Calendar 暂时不可用")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Calendar 返回 %d", resp.StatusCode)
	}
	return parseHolidayICS(io.LimitReader(resp.Body, 4<<20))
}

func parseHolidayICS(reader io.Reader) ([]holidayEvent, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	events := make([]holidayEvent, 0)
	var current holidayEvent
	inEvent := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "BEGIN:VEVENT":
			inEvent = true
			current = holidayEvent{}
		case "END:VEVENT":
			if inEvent && current.Date != "" {
				events = append(events, current)
			}
			inEvent = false
		default:
			if !inEvent {
				continue
			}
			if strings.HasPrefix(line, "DTSTART") {
				if index := strings.IndexByte(line, ':'); index >= 0 {
					value := line[index+1:]
					if len(value) >= 8 {
						if parsed, err := time.Parse("20060102", value[:8]); err == nil {
							current.Date = parsed.Format("2006-01-02")
						}
					}
				}
			} else if strings.HasPrefix(line, "SUMMARY:") {
				current.Summary = strings.TrimPrefix(line, "SUMMARY:")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Service) UseHolidayCalendarClient(client *http.Client, baseURL string) {
	if client != nil {
		s.holidayClient = client
	}
	if strings.TrimSpace(baseURL) != "" {
		s.holidayBaseURL = baseURL
	}
}
