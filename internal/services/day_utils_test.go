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
			name:  "lh test present",
			entry: models.DailyLog{LHTest: models.LHTestPeak},
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
			entry: models.DailyLog{IsPeriod: true, BBT: 36.5},
			want:  false,
		},
		{
			name:  "period day with cervical mucus",
			entry: models.DailyLog{IsPeriod: true, CervicalMucus: models.CervicalMucusEggWhite},
			want:  false,
		},
		{
			name:  "period day with lh test",
			entry: models.DailyLog{IsPeriod: true, LHTest: models.LHTestPeak},
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
