package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestDayHasData(t *testing.T) {
	tests := []struct {
		name  string
		entry models.DailyLog
		want  bool
	}{
		{
			name:  "period day",
			entry: models.DailyLog{IsPeriod: true},
			want:  true,
		},
		{
			name:  "symptoms present",
			entry: models.DailyLog{SymptomIDs: []uint{1}},
			want:  true,
		},
		{
			name:  "notes present",
			entry: models.DailyLog{Notes: "note"},
			want:  true,
		},
		{
			name:  "cycle factors present",
			entry: models.DailyLog{CycleFactorKeys: []string{models.CycleFactorStress}},
			want:  true,
		},
		{
			name:  "flow present",
			entry: models.DailyLog{Flow: models.FlowLight},
			want:  true,
		},
		{
			name:  "pregnancy test present",
			entry: models.DailyLog{PregnancyTest: models.PregnancyTestPositive},
			want:  true,
		},
		{
			name:  "empty entry",
			entry: models.DailyLog{Flow: models.FlowNone},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DayHasData(tt.entry); got != tt.want {
				t.Fatalf("DayHasData() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAutoFilledPeriodCandidate(t *testing.T) {
	tests := []struct {
		name  string
		entry models.DailyLog
		want  bool
	}{
		{
			name:  "bare period day",
			entry: models.DailyLog{IsPeriod: true},
			want:  true,
		},
		{
			name:  "bare period day with propagated flow",
			entry: models.DailyLog{IsPeriod: true, Flow: models.FlowLight},
			want:  true,
		},
		{
			name:  "non-period day",
			entry: models.DailyLog{IsPeriod: false},
			want:  false,
		},
		{
			name:  "anchor day with cycle start",
			entry: models.DailyLog{IsPeriod: true, CycleStart: true},
			want:  false,
		},
		{
			name:  "period day with pregnancy test",
			entry: models.DailyLog{IsPeriod: true, PregnancyTest: models.PregnancyTestPositive},
			want:  false,
		},
		{
			name:  "uncertain anchor",
			entry: models.DailyLog{IsPeriod: true, IsUncertain: true},
			want:  false,
		},
		{
			name:  "period day with symptoms",
			entry: models.DailyLog{IsPeriod: true, SymptomIDs: []uint{1}},
			want:  false,
		},
		{
			name:  "period day with notes",
			entry: models.DailyLog{IsPeriod: true, Notes: "spotty"},
			want:  false,
		},
		{
			name:  "period day with cycle factors",
			entry: models.DailyLog{IsPeriod: true, CycleFactorKeys: []string{models.CycleFactorStress}},
			want:  false,
		},
		{
			name:  "period day with intimacy logged",
			entry: models.DailyLog{IsPeriod: true, SexActivity: models.SexActivityProtected},
			want:  false,
		},
		{
			name:  "period day with mood",
			entry: models.DailyLog{IsPeriod: true, Mood: MinDayMood},
			want:  false,
		},
		{
			name:  "period day with bbt reading",
			entry: models.DailyLog{IsPeriod: true, BBT: new(36.5)},
			want:  false,
		},
		{
			name:  "period day with cervical mucus",
			entry: models.DailyLog{IsPeriod: true, CervicalMucus: models.CervicalMucusEggWhite},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAutoFilledPeriodCandidate(tt.entry); got != tt.want {
				t.Fatalf("IsAutoFilledPeriodCandidate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDayRangeReturnsUTCBoundsForLocalCalendarDay(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load Europe/Moscow: %v", err)
	}
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("load America/Toronto: %v", err)
	}

	tests := []struct {
		name        string
		input       time.Time
		location    *time.Location
		wantDateKey string
	}{
		{
			name:        "Moscow UTC+3 instant in local 2026-02-01",
			input:       time.Date(2026, time.February, 1, 19, 35, 10, 0, time.UTC),
			location:    moscow,
			wantDateKey: "2026-02-01",
		},
		{
			name:        "Toronto UTC-5 instant past UTC midnight is local 2026-02-09",
			input:       time.Date(2026, time.February, 10, 2, 0, 0, 0, time.UTC),
			location:    toronto,
			wantDateKey: "2026-02-09",
		},
		{
			name:        "Toronto UTC-5 morning instant is local 2026-02-10",
			input:       time.Date(2026, time.February, 10, 14, 0, 0, 0, time.UTC),
			location:    toronto,
			wantDateKey: "2026-02-10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := DayRange(tt.input, tt.location)

			if start.Location() != time.UTC {
				t.Fatalf("expected UTC start, got %s", start.Location())
			}
			if end.Location() != time.UTC {
				t.Fatalf("expected UTC end, got %s", end.Location())
			}
			if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 {
				t.Fatalf("expected UTC-midnight start, got %s", start.Format(time.RFC3339Nano))
			}
			if got := start.Format("2006-01-02"); got != tt.wantDateKey {
				t.Fatalf("expected local calendar day %s rebuilt at UTC, got %s", tt.wantDateKey, got)
			}
			if got := end.Sub(start); got != 24*time.Hour {
				t.Fatalf("expected 24h range, got %s", got)
			}
		})
	}
}

func TestDateAtLocationShiftsToNextLocalDayAcrossUTCBoundary(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	raw := time.Date(2026, time.March, 2, 21, 30, 0, 0, time.UTC)

	day := DateAtLocation(raw, location)
	if day.Format("2006-01-02") != "2026-03-03" {
		t.Fatalf("expected local date 2026-03-03, got %s", day.Format("2006-01-02"))
	}
	if day.Hour() != 0 || day.Minute() != 0 || day.Second() != 0 {
		t.Fatalf("expected normalized local midnight, got %s", day.Format(time.RFC3339))
	}
}

// TestCalendarDayKeyRoundTripsAcrossMidnightDSTTransitions pins the one
// property every date-only surface depends on: the YYYY-MM-DD key a helper is
// handed must be the YYYY-MM-DD key it returns. In a UTC-minus zone whose DST
// jump lands exactly on midnight, local midnight does not exist and time.Date
// normalizes the nonexistent wall clock BACKWARD into the previous calendar
// day, so the spring-forward date could not be entered at all: the onboarding
// anchor, the day entry and the export row all shifted one day earlier.
//
// The list carries three midnight-transition zones on their own transition
// dates, a positive-offset zone (which normalizes forward and was never
// affected), a zone whose jump is at 02:00 (midnight exists), and UTC (no
// transitions at all) so a future change that breaks the unaffected zones is
// caught here too.
func TestCalendarDayKeyRoundTripsAcrossMidnightDSTTransitions(t *testing.T) {
	cases := []struct {
		zone string
		day  string
		note string
	}{
		{zone: "America/Santiago", day: "2026-09-06", note: "UTC-4 -> UTC-3 jump at 00:00"},
		{zone: "America/Havana", day: "2026-03-08", note: "UTC-5 -> UTC-4 jump at 00:00"},
		{zone: "America/Asuncion", day: "2024-10-06", note: "UTC-4 -> UTC-3 jump at 00:00 (last DST year)"},
		{zone: "Asia/Beirut", day: "2026-03-29", note: "control: positive offset, jump at 00:00"},
		{zone: "Europe/Berlin", day: "2026-03-29", note: "control: jump at 02:00, midnight exists"},
		{zone: "UTC", day: "2026-03-08", note: "control: no transitions"},
	}

	for _, tc := range cases {
		t.Run(tc.zone+" "+tc.day, func(t *testing.T) {
			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatalf("load %s: %v", tc.zone, err)
			}
			reference, err := time.Parse("2006-01-02", tc.day)
			if err != nil {
				t.Fatalf("parse reference day %s: %v", tc.day, err)
			}
			year, month, day := reference.Date()

			// The single parse entry point for date inputs.
			parsed, err := ParseDayDate(tc.day, location)
			if err != nil {
				t.Fatalf("ParseDayDate(%s, %s): %v", tc.day, tc.zone, err)
			}
			if got := parsed.Format("2006-01-02"); got != tc.day {
				t.Errorf("ParseDayDate(%s, %s) = %s, want %s (%s)", tc.day, tc.zone, got, tc.day, tc.note)
			}
			// The returned instant is the day's FIRST existing instant: one
			// nanosecond earlier is already the previous calendar day.
			if got := parsed.Add(-time.Nanosecond).Format("2006-01-02"); got == tc.day {
				t.Errorf("ParseDayDate(%s, %s) = %s is not the first instant of the day",
					tc.day, tc.zone, parsed.Format(time.RFC3339))
			}

			// A date-only stored value (persisted at UTC-midnight) rebuilt in
			// the request-local zone.
			stored := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			if got := CalendarDay(stored, location).Format("2006-01-02"); got != tc.day {
				t.Errorf("CalendarDay(%s, %s) = %s, want %s (%s)", tc.day, tc.zone, got, tc.day, tc.note)
			}

			// An instant inside the local calendar day projected back onto it.
			instant := time.Date(year, month, day, 12, 0, 0, 0, location).UTC()
			if got := DateAtLocation(instant, location).Format("2006-01-02"); got != tc.day {
				t.Errorf("DateAtLocation(local noon of %s, %s) = %s, want %s (%s)",
					tc.day, tc.zone, got, tc.day, tc.note)
			}

			// The canonical storage shape: parse in the request zone, persist
			// at UTC-midnight, read the key back.
			if got := CalendarDayKey(CalendarDay(parsed, time.UTC)); got != tc.day {
				t.Errorf("CalendarDayKey(CalendarDay(ParseDayDate(%s, %s), UTC)) = %s, want %s",
					tc.day, tc.zone, got, tc.day)
			}
		})
	}
}

// TestCalendarDayKeepsStdlibResolutionForACalendarDayThatNeverExisted covers
// the one case the transition lookup cannot repair: a zone that skips a whole
// calendar day. Pacific/Apia jumped from 2011-12-29 23:59:59 UTC-10 straight
// to 2011-12-31 00:00 UTC+14, so no instant at all exists on 2011-12-30. The
// helper must not reach for the next transition there (that lands on a
// different day) and keeps time.Date's own resolution instead.
//
// This is the contract for a STORED value being rebuilt, where there is no
// error channel to report the gap on. It is not the contract for user input:
// ParseDayDate refuses the same date with ErrDayDateNonexistent rather than
// returning the previous day as a successful parse
// (TestParseDayDateRefusesACalendarDayTheZoneNeverHad).
func TestCalendarDayKeepsStdlibResolutionForACalendarDayThatNeverExisted(t *testing.T) {
	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Fatalf("load Pacific/Apia: %v", err)
	}

	stored := time.Date(2011, time.December, 30, 0, 0, 0, 0, time.UTC)
	got := CalendarDay(stored, apia)

	if key := got.Format("2006-01-02"); key != "2011-12-29" {
		t.Fatalf("CalendarDay(2011-12-30, Pacific/Apia) = %s, want the stdlib resolution 2011-12-29", key)
	}
	if got.Location() != apia {
		t.Fatalf("expected location Pacific/Apia, got %s", got.Location())
	}
}

func TestSymptomIDSet(t *testing.T) {
	set := SymptomIDSet([]uint{3, 3, 5})
	if len(set) != 2 {
		t.Fatalf("expected unique set size 2, got %d", len(set))
	}
	if !set[3] || !set[5] {
		t.Fatal("expected set to contain ids 3 and 5")
	}
}

// TestCalendarDaysBetween pins the shared calendar-day difference helper: the
// result must be a pure calendar-day count regardless of the midnight shape
// each operand carries (UTC-midnight stored values vs location-midnight
// working values, issue #48 class) and regardless of DST transitions between
// the two days.
func TestCalendarDaysBetween(t *testing.T) {
	tokyo := time.FixedZone("UTC+9", 9*60*60)
	lima := time.FixedZone("UTC-5", -5*60*60)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load Europe/Berlin: %v", err)
	}

	cases := []struct {
		name string
		from time.Time
		to   time.Time
		want int
	}{
		{"same UTC-midnight shape", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC), 4},
		{"UTC-midnight stored vs UTC+9 location-midnight", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 5, 0, 0, 0, 0, tokyo), 4},
		{"UTC-midnight stored vs UTC-5 location-midnight", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 5, 0, 0, 0, 0, lima), 4},
		{"same calendar day across shapes is zero", time.Date(2026, 3, 1, 0, 0, 0, 0, tokyo), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 0},
		{"negative when to precedes from", time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 0, 0, 0, 0, lima), -4},
		{"DST spring-forward span counts whole days", time.Date(2026, 3, 28, 0, 0, 0, 0, berlin), time.Date(2026, 3, 30, 0, 0, 0, 0, berlin), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CalendarDaysBetween(tc.from, tc.to); got != tc.want {
				t.Fatalf("CalendarDaysBetween(%s, %s) = %d, want %d",
					tc.from.Format(time.RFC3339), tc.to.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}
