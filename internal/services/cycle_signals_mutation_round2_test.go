package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/testenv"
)

func TestCycleSignals_InferUserLutealPhase_UnchangedByDSTTransitionInCycle(t *testing.T) {
	// America/Toronto springs forward on 2025-03-09. The first cycle's start
	// (Mar 1) and next period start (Mar 20) bracket that transition. Because
	// CalendarDaysBetween re-anchors both operands to UTC-midnight, both spans
	// the inference measures — the cycle length and the ovulation's offset from
	// the cycle start — are true calendar-day counts and immune to a DST
	// transition inside the cycle: a DST-observing local zone must yield the
	// same value as UTC. A raw `nextStart.Sub(start)/24` would truncate the
	// 19*24-1h cycle span down to 18 in Toronto and drag the inferred phase to
	// 12 there while UTC saw 13 — a location-dependent skew this test guards
	// against.
	loc := testenv.RequireTimeZone(t, "America/Toronto")
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		// Three observed starts: Mar 1, Mar 20, Apr 8.
		{Date: day("2025-03-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-03-20"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-04-08"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle A (Mar1->Mar20, 19 days): coverline window Mar1-6, rise Mar7-9 ->
		// ovulation Mar6 (day before the first high day) = cycle day 6, so
		// luteal = 19-6 = 13. The cycle spans the Mar 9 DST boundary.
		{Date: day("2025-03-01"), BBT: new(36.20)},
		{Date: day("2025-03-02"), BBT: new(36.20)},
		{Date: day("2025-03-03"), BBT: new(36.20)},
		{Date: day("2025-03-04"), BBT: new(36.20)},
		{Date: day("2025-03-05"), BBT: new(36.20)},
		{Date: day("2025-03-06"), BBT: new(36.20)},
		{Date: day("2025-03-07"), BBT: new(36.50)},
		{Date: day("2025-03-08"), BBT: new(36.50)},
		{Date: day("2025-03-09"), BBT: new(36.50)},

		// Cycle B (Mar20->Apr8, 19 days): coverline window Mar20-25, rise Mar27-29
		// -> ovulation Mar26 = cycle day 7, so luteal = 19-7 = 12. No DST boundary
		// inside this cycle.
		{Date: day("2025-03-20"), BBT: new(36.20)},
		{Date: day("2025-03-21"), BBT: new(36.20)},
		{Date: day("2025-03-22"), BBT: new(36.20)},
		{Date: day("2025-03-23"), BBT: new(36.20)},
		{Date: day("2025-03-24"), BBT: new(36.20)},
		{Date: day("2025-03-25"), BBT: new(36.20)},
		{Date: day("2025-03-27"), BBT: new(36.50)},
		{Date: day("2025-03-28"), BBT: new(36.50)},
		{Date: day("2025-03-29"), BBT: new(36.50)},
	}

	// lens = [13, 12] -> round(12.5) = 13, regardless of DST, in every zone.
	phaseLocal, ok := InferUserLutealPhase(logs, loc)
	if !ok {
		t.Fatalf("expected ok=true with two BBT-confirmed cycles")
	}
	if phaseLocal != 13 {
		t.Fatalf("expected inferred luteal phase 13 in America/Toronto (DST-immune calendar count); got %d. A phase of 12 means a DST transition inside the cycle truncated one of the two spans.", phaseLocal)
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

		// Cycle 1 (Jan1->Jan29, 28 days): coverline window Jan1-6, rise Jan19-21 ->
		// ovulation Jan18 = cycle day 18. luteal = 28-18 = 10 (exactly
		// minLutealPhaseDays).
		{Date: day("2025-01-01"), BBT: new(36.20)},
		{Date: day("2025-01-02"), BBT: new(36.20)},
		{Date: day("2025-01-03"), BBT: new(36.20)},
		{Date: day("2025-01-04"), BBT: new(36.20)},
		{Date: day("2025-01-05"), BBT: new(36.20)},
		{Date: day("2025-01-06"), BBT: new(36.20)},
		{Date: day("2025-01-19"), BBT: new(36.50)},
		{Date: day("2025-01-20"), BBT: new(36.50)},
		{Date: day("2025-01-21"), BBT: new(36.50)},

		// Cycle 2 (Jan29->Feb26, 28 days): coverline window Jan29-Feb3, rise
		// Feb12-14 -> ovulation Feb11 = cycle day 14. luteal = 28-14 = 14 (valid).
		{Date: day("2025-01-29"), BBT: new(36.20)},
		{Date: day("2025-01-30"), BBT: new(36.20)},
		{Date: day("2025-01-31"), BBT: new(36.20)},
		{Date: day("2025-02-01"), BBT: new(36.20)},
		{Date: day("2025-02-02"), BBT: new(36.20)},
		{Date: day("2025-02-03"), BBT: new(36.20)},
		{Date: day("2025-02-13"), BBT: new(36.50)},
		{Date: day("2025-02-14"), BBT: new(36.50)},
		{Date: day("2025-02-15"), BBT: new(36.50)},
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

		// Cycle 1 (Jan1->Jan29, 28 days): coverline window Jan1-6, rise Jan9-11 ->
		// ovulation Jan8 = cycle day 8. luteal = 28-8 = 20 (exactly the upper
		// boundary).
		{Date: day("2025-01-01"), BBT: new(36.20)},
		{Date: day("2025-01-02"), BBT: new(36.20)},
		{Date: day("2025-01-03"), BBT: new(36.20)},
		{Date: day("2025-01-04"), BBT: new(36.20)},
		{Date: day("2025-01-05"), BBT: new(36.20)},
		{Date: day("2025-01-06"), BBT: new(36.20)},
		{Date: day("2025-01-09"), BBT: new(36.50)},
		{Date: day("2025-01-10"), BBT: new(36.50)},
		{Date: day("2025-01-11"), BBT: new(36.50)},

		// Cycle 2 (Jan29->Feb26, 28 days): coverline window Jan29-Feb3, rise
		// Feb12-14 -> ovulation Feb11 = cycle day 14. luteal = 28-14 = 14 (valid).
		{Date: day("2025-01-29"), BBT: new(36.20)},
		{Date: day("2025-01-30"), BBT: new(36.20)},
		{Date: day("2025-01-31"), BBT: new(36.20)},
		{Date: day("2025-02-01"), BBT: new(36.20)},
		{Date: day("2025-02-02"), BBT: new(36.20)},
		{Date: day("2025-02-03"), BBT: new(36.20)},
		{Date: day("2025-02-13"), BBT: new(36.50)},
		{Date: day("2025-02-14"), BBT: new(36.50)},
		{Date: day("2025-02-15"), BBT: new(36.50)},
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
