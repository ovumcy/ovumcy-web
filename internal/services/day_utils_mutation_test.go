package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestCalendarDayPreservesLocationAndHandlesNil(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load Europe/Moscow: %v", err)
	}

	value := time.Date(2026, time.February, 17, 9, 30, 0, 0, time.UTC)

	// Non-nil location must be preserved on the returned value. Under the
	// `==`->`!=` mutation the guard reassigns location to UTC for non-nil
	// inputs, so the returned Location would be UTC instead of Moscow.
	got := CalendarDay(value, moscow)
	if got.Location() != moscow {
		t.Fatalf("expected returned location %s, got %s", moscow, got.Location())
	}
	if got.Year() != 2026 || got.Month() != time.February || got.Day() != 17 {
		t.Fatalf("expected calendar day 2026-02-17, got %s", got.Format("2006-01-02"))
	}
	if got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
		t.Fatalf("expected midnight, got %s", got.Format(time.RFC3339Nano))
	}

	// Nil location must fall back to UTC midnight. Under the mutation the
	// guard no longer substitutes UTC for a nil location, so time.Date would
	// receive nil and panic; this call recovers and fails the test instead.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("CalendarDay panicked for nil location: %v", r)
			}
		}()
		nilLoc := CalendarDay(value, nil)
		if nilLoc.Location() != time.UTC {
			t.Fatalf("expected UTC fallback location, got %s", nilLoc.Location())
		}
		if nilLoc.Format("2006-01-02") != "2026-02-17" {
			t.Fatalf("expected 2026-02-17 at UTC, got %s", nilLoc.Format("2006-01-02"))
		}
	}()
}

func TestDayHasDataMaxMoodCountsAsData(t *testing.T) {
	// Mood at the inclusive upper bound (MaxDayMood) is real logged data.
	// The CONDITIONALS_BOUNDARY mutation at line 97 turns `<= MaxDayMood`
	// into `< MaxDayMood`, which would make a max-mood-only entry report no
	// data. Flow defaults to FlowNone so no other branch sets the result.
	entry := models.DailyLog{Mood: MaxDayMood, Flow: models.FlowNone}
	if got := DayHasData(entry); !got {
		t.Fatalf("DayHasData() with Mood=MaxDayMood = %v, want true", got)
	}
}
