package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestCycleSignals_InferUserLutealPhase_UnchangedByDSTTransitionInCycle(t *testing.T) {
	// America/Toronto springs forward on 2025-03-09. The first cycle's
	// ovulation (Mar 6, the day before the BBT rise) and next period start (Mar 20) bracket that
	// transition. Because CalendarDaysBetween re-anchors both operands to
	// UTC-midnight, the luteal length is the true calendar-day count (14) and
	// is immune to the DST transition inside the cycle: a DST-observing local
	// zone must yield the same value as UTC. Before the fix, the raw
	// `nextStart.Sub(ovulationDate)/24` truncated the 14*24-1h span down to 13
	// in Toronto, dragging the inferred phase to 13 in a DST-observing zone
	// while UTC saw 14 — a location-dependent skew this test now guards against.
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		// Three observed starts: Mar 1, Mar 20, Apr 8.
		{Date: day("2025-03-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-03-20"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-04-08"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle A (Mar1->Mar20): coverline window Mar1-6, rise Mar7-9 ->
		// ovulation Mar6 (day before the first high day).
		// Cycle A spans the Mar 9 DST boundary: luteal = 14 (UTC) / 13 (Toronto).
		{Date: day("2025-03-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-06"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-07"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-08"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-09"), BBT: models.NewBBT(36.50)},

		// Cycle B (Mar20->Apr8): coverline window Mar20-25, rise Mar27-29 ->
		// ovulation Mar26. No DST boundary in the luteal span: luteal = 13.
		{Date: day("2025-03-20"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-21"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-22"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-23"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-24"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-25"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-27"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-28"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-29"), BBT: models.NewBBT(36.50)},
	}

	// Cycle A (Mar6->Mar20) = 14 true calendar days, cycle B (Mar26->Apr8) = 13.
	// lens = [14, 13] -> round(13.5) = 14, regardless of DST, in every zone.
	phaseLocal, ok := InferUserLutealPhase(logs, loc)
	if !ok {
		t.Fatalf("expected ok=true with two BBT-confirmed cycles")
	}
	if phaseLocal != 14 {
		t.Fatalf("expected inferred luteal phase 14 in America/Toronto (DST-immune calendar count); got %d. A phase of 13 means a DST transition inside the cycle truncated the luteal span.", phaseLocal)
	}

	// DST-immunity: the same calendar dates evaluated in UTC (no DST) must
	// produce the identical phase — the location must not change the result.
	phaseUTC, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatalf("expected ok=true in UTC as well")
	}
	if phaseUTC != phaseLocal {
		t.Fatalf("expected DST-observing zone (%d) and UTC (%d) to agree", phaseLocal, phaseUTC)
	}
}

func TestCycleSignals_InferUserLutealPhase_LutealLengthExactlyMinIsKept(t *testing.T) {
	// A cycle whose BBT-inferred luteal length is EXACTLY minLutealPhaseDays (10)
	// must be counted (the filter is `< min`, inclusive of the boundary). Paired
	// with one more valid cycle (luteal 14) this yields two valid lengths and a
	// successful inference. If line 37 used `<=` instead of `<`, the length-10
	// cycle would be dropped, leaving a single valid length and flipping the
	// result to (default, false).
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 (Jan1->Jan29): coverline window Jan1-6, rise Jan20-22 ->
		// ovulation Jan19. luteal = Jan29 - Jan19 = 10 (exactly minLutealPhaseDays).
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-06"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-20"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-21"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-22"), BBT: models.NewBBT(36.50)},

		// Cycle 2 (Jan29->Feb26): coverline window Jan29-Feb3, rise Feb13-15 ->
		// ovulation Feb12. luteal = Feb26 - Feb12 = 14 (valid).
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-15"), BBT: models.NewBBT(36.50)},
	}

	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatalf("expected ok=true: a luteal length of exactly %d must be kept, giving two valid lengths", minLutealPhaseDays)
	}
	// lens = [10, 14] -> round((10+14)/2) = round(12.0) = 12.
	if phase != 12 {
		t.Fatalf("expected inferred luteal phase 12 (avg of 10 and 14); got %d. A value of %d means the boundary length %d was wrongly dropped.", phase, defaultLutealPhaseDays, minLutealPhaseDays)
	}
}

func TestCycleSignals_InferUserLutealPhase_LutealLengthExactlyTwentyIsKept(t *testing.T) {
	// A cycle whose BBT-inferred luteal length is EXACTLY 20 must be counted
	// (the upper filter is `> 20`, which keeps 20). Paired with one more valid
	// cycle (luteal 14) this yields two valid lengths and a successful
	// inference. If line 37 used `>= 20` instead of `> 20`, the length-20 cycle
	// would be dropped, leaving a single valid length and flipping the result
	// to (default, false).
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 (Jan1->Jan29): coverline window Jan1-6, rise Jan10-12 ->
		// ovulation Jan9. luteal = Jan29 - Jan9 = 20 (exactly the upper boundary).
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-06"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-10"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-11"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-12"), BBT: models.NewBBT(36.50)},

		// Cycle 2 (Jan29->Feb26): coverline window Jan29-Feb3, rise Feb13-15 ->
		// ovulation Feb12. luteal = Feb26 - Feb12 = 14 (valid).
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-15"), BBT: models.NewBBT(36.50)},
	}

	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatalf("expected ok=true: a luteal length of exactly 20 must be kept, giving two valid lengths")
	}
	// lens = [20, 14] -> round((20+14)/2) = round(17.0) = 17.
	if phase != 17 {
		t.Fatalf("expected inferred luteal phase 17 (avg of 20 and 14); got %d. A value of %d means the boundary length 20 was wrongly dropped.", phase, defaultLutealPhaseDays)
	}
}
