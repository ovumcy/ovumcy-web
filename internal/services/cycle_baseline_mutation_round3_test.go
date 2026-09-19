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

// TestMR3Cycles_DetectCurrentPhaseHonorsLocation targets the location handling
// in DetectCurrentPhase (cycle_baseline.go). Every value is built the way
// ApplyUserCycleBaseline hands it over — stats dates and `today` all at
// midnight of their calendar day in the owner's zone — and every expected
// phase comes from calendar arithmetic written out below, never from a
// production helper, so the table is the same in every zone:
//
//	period starts 2026-03-01, average period 5 days -> Mar 1..Mar 5 menstrual
//	28-day cycle, luteal 14 -> ovulation on cycle day 28-14 = 14 -> Mar 1 + 13 = Mar 14
//	Mar 6..Mar 13 follicular, Mar 14 ovulation, Mar 15 onward luteal
//
// UTC is the control. UTC+14 is where reading today's calendar date off its
// UTC wall clock loses a whole day (Mar 14 00:00 +14 is Mar 13 10:00 UTC), so
// the ovulation row reddens there. UTC-10 is where clobbering the location to
// UTC (the NEGATION `location != nil` mutant) pulls the period end back to
// Mar 4 14:00 local, so the last menstrual day reads follicular; east of UTC
// that clobber moves the bound later and stays invisible.
func TestMR3Cycles_DetectCurrentPhaseHonorsLocation(t *testing.T) {
	zones := []*time.Location{
		time.UTC,
		time.FixedZone("UTC+14", 14*60*60),
		time.FixedZone("UTC-10", -10*60*60),
	}
	probes := []struct {
		day  int // day of March 2026
		want string
	}{
		{1, "menstrual"},
		{5, "menstrual"},
		{6, "follicular"},
		{13, "follicular"},
		{14, "ovulation"},
		{15, "luteal"},
		{28, "luteal"},
	}
	for _, loc := range zones {
		march := func(day int) time.Time { return time.Date(2026, time.March, day, 0, 0, 0, 0, loc) }
		stats := CycleStats{
			LastPeriodStart:      march(1),
			AveragePeriodLength:  5,
			OvulationDate:        march(14),
			FertilityWindowStart: march(9),
			FertilityWindowEnd:   march(14),
		}
		for _, probe := range probes {
			if got := DetectCurrentPhase(stats, nil, march(probe.day), loc); got != probe.want {
				t.Errorf("%s: 2026-03-%02d phase = %q, want %q", loc, probe.day, got, probe.want)
			}
		}
	}
}
