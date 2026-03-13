package api

import (
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestBuildCalendarDaysMapsStateToTemplateClasses(t *testing.T) {
	handler := &Handler{}
	states := []services.CalendarDayState{
		{
			Date:        time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
			DateString:  "2026-02-17",
			Day:         17,
			InMonth:     true,
			IsToday:     false,
			IsPeriod:    true,
			IsPredicted: false,
			IsFertility: false,
			IsOvulation: false,
		},
		{
			Date:        time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
			DateString:  "2026-03-01",
			Day:         1,
			InMonth:     false,
			IsToday:     true,
			IsPeriod:    false,
			IsPredicted: false,
			IsFertility: false,
			IsOvulation: true,
		},
		{
			Date:         time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC),
			DateString:   "2026-03-02",
			Day:          2,
			InMonth:      true,
			IsToday:      false,
			IsPeriod:     false,
			IsPredicted:  false,
			IsPreFertile: true,
			IsFertility:  false,
			IsOvulation:  false,
			HasData:      true,
		},
	}

	days := handler.buildCalendarDays(states)
	if len(days) != 3 {
		t.Fatalf("expected three mapped calendar days, got %d", len(days))
	}

	if !strings.Contains(days[0].CellClass, "calendar-cell-period") {
		t.Fatalf("expected period class for first day, got %q", days[0].CellClass)
	}
	if days[0].BadgeClass != "calendar-tag calendar-tag-period" {
		t.Fatalf("expected period badge class, got %q", days[0].BadgeClass)
	}

	if !strings.Contains(days[1].CellClass, "calendar-cell-fertile") {
		t.Fatalf("expected fertile class for ovulation day, got %q", days[1].CellClass)
	}
	if !strings.Contains(days[1].CellClass, "calendar-cell-out") {
		t.Fatalf("expected out-of-month class for second day, got %q", days[1].CellClass)
	}
	if !strings.Contains(days[1].CellClass, "calendar-cell-today") {
		t.Fatalf("expected today class for second day, got %q", days[1].CellClass)
	}
	if !strings.Contains(days[1].TextClass, "calendar-day-out") {
		t.Fatalf("expected out-of-month text class, got %q", days[1].TextClass)
	}
	if days[1].BadgeClass != "calendar-tag calendar-tag-ovulation" {
		t.Fatalf("expected ovulation badge class, got %q", days[1].BadgeClass)
	}
	if !days[1].OvulationDot {
		t.Fatalf("expected ovulation dot for second day")
	}

	if !strings.Contains(days[2].CellClass, "calendar-cell-pre-fertile") {
		t.Fatalf("expected pre-fertile class for third day, got %q", days[2].CellClass)
	}
	if days[2].StateKey != "pre-fertile" {
		t.Fatalf("expected pre-fertile state key, got %q", days[2].StateKey)
	}
	if !days[2].HasData {
		t.Fatalf("expected third day to preserve logged-data marker state")
	}
}
