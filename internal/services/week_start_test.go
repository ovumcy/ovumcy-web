package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestNormalizeWeekStart(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"monday", models.WeekStartMonday},
		{"Monday", models.WeekStartMonday},
		{"  MONDAY ", models.WeekStartMonday},
		{"sunday", models.WeekStartSunday},
		{"", models.DefaultWeekStart},
		{"garbage", models.DefaultWeekStart},
		{"mon", models.DefaultWeekStart},
	}
	for _, c := range cases {
		if got := NormalizeWeekStart(c.raw); got != c.want {
			t.Errorf("NormalizeWeekStart(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestWeekdayHeaderKeysSundayFirst(t *testing.T) {
	got := WeekdayHeaderKeys(models.WeekStartSunday)
	want := []string{
		"calendar.weekday.sun",
		"calendar.weekday.mon",
		"calendar.weekday.tue",
		"calendar.weekday.wed",
		"calendar.weekday.thu",
		"calendar.weekday.fri",
		"calendar.weekday.sat",
	}
	assertKeysEqual(t, got, want)
}

func TestWeekdayHeaderKeysMondayFirst(t *testing.T) {
	got := WeekdayHeaderKeys(models.WeekStartMonday)
	want := []string{
		"calendar.weekday.mon",
		"calendar.weekday.tue",
		"calendar.weekday.wed",
		"calendar.weekday.thu",
		"calendar.weekday.fri",
		"calendar.weekday.sat",
		"calendar.weekday.sun",
	}
	assertKeysEqual(t, got, want)
}

func TestWeekdayHeaderKeysUnknownFallsBackToSunday(t *testing.T) {
	got := WeekdayHeaderKeys("garbage")
	if got[0] != "calendar.weekday.sun" {
		t.Fatalf("unknown week-start should fall back to Sunday-first, got first key %q", got[0])
	}
}

func assertKeysEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
