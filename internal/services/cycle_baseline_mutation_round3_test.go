package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestMR3Cycles_DetectCurrentPhaseNilLocation targets
// cycle_baseline.go:121 `if location == nil` (NEGATION) in DetectCurrentPhase.
// A nil location must fall back to UTC without panic and still classify the
// phase. With period logged today the phase is "menstrual". Under the NEGATION
// mutation the nil location is left nil and downstream CalendarDay calls would
// be reached with nil.
func TestMR3Cycles_DetectCurrentPhaseNilLocation(t *testing.T) {
	today := mr3cycDay(2026, time.March, 10)
	logs := []models.DailyLog{
		mr3cycPeriodLog(today, true, false),
	}
	stats := CycleStats{
		LastPeriodStart:     today,
		AveragePeriodLength: 5,
	}

	var phase string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nil location must not panic: %v", r)
			}
		}()
		phase = DetectCurrentPhase(stats, logs, today, nil)
	}()

	if phase != "menstrual" {
		t.Fatalf("expected menstrual phase with period logged today, got %q", phase)
	}
}

// TestMR3Cycles_DetectCurrentPhaseHonorsLocation also targets
// cycle_baseline.go:121. The NEGATION mutation (`location != nil`) clobbers a
// non-nil location to UTC, which shifts the location-rebuilt period-end and
// fertility-window dates relative to the UTC-midnight `today`. At the
// period-end boundary (LastPeriodStart Mar 1 + 5-day period = Mar 5) under a
// far-east UTC+14 location, Mar 5 (UTC midnight) falls just AFTER the
// location-midnight period end, so the phase is follicular. Clobbering the
// location to UTC pulls the period end back to UTC midnight, making Mar 5 land
// inside the menstrual window — a wrong "menstrual" classification.
func TestMR3Cycles_DetectCurrentPhaseHonorsLocation(t *testing.T) {
	loc := time.FixedZone("UTCplus14", 14*60*60)
	stats := CycleStats{
		LastPeriodStart:      mr3cycDay(2026, time.March, 1),
		AveragePeriodLength:  5,
		OvulationDate:        mr3cycDay(2026, time.March, 14),
		FertilityWindowStart: mr3cycDay(2026, time.March, 9),
		FertilityWindowEnd:   mr3cycDay(2026, time.March, 14),
	}
	// today = Mar 5 (UTC midnight), the day after the 5-day menstrual window
	// ends in a far-east locale; no period is logged on this day.
	today := mr3cycDay(2026, time.March, 5)

	phase := DetectCurrentPhase(stats, nil, today, loc)
	if phase != "follicular" {
		t.Fatalf("expected follicular phase honoring UTC+14, got %q", phase)
	}
}
