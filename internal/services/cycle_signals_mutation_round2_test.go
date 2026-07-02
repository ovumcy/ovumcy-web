package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestCyclesignalsCov_InferUserLutealPhase_RespectsNonUTCLocationAcrossDST(t *testing.T) {
	// America/Toronto springs forward on 2025-03-09. The first cycle's
	// ovulation (Mar 6, BBT rise) and next period start (Mar 20) bracket that
	// transition, so its luteal length is one day SHORTER when computed in a
	// DST-observing local zone than in UTC. The function genuinely uses the
	// passed location; if line 18 were negated to `location != nil` the
	// caller's Toronto location would be discarded in favour of UTC and the
	// inferred phase would shift from 13 to 14.
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

		// Cycle A (Mar1->Mar20): baseline Mar1-5, rise Mar6-8 -> ovulation Mar6.
		// Cycle A spans the Mar 9 DST boundary: luteal = 14 (UTC) / 13 (Toronto).
		{Date: day("2025-03-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-06"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-07"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-08"), BBT: models.NewBBT(36.50)},

		// Cycle B (Mar20->Apr8): baseline Mar20-24, rise Mar26-28 -> ovulation Mar26.
		// No DST boundary in this window: luteal = 13 in both UTC and Toronto.
		{Date: day("2025-03-20"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-21"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-22"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-23"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-24"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-03-26"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-27"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-03-28"), BBT: models.NewBBT(36.50)},
	}

	phase, ok := InferUserLutealPhase(logs, loc)
	if !ok {
		t.Fatalf("expected ok=true with two BBT-confirmed cycles")
	}
	// Toronto: lens = [13, 13] -> round(13) = 13.
	// If location is forced to UTC (mutation), lens = [14, 13] -> round(13.5) = 14.
	if phase != 13 {
		t.Fatalf("expected inferred luteal phase 13 in America/Toronto (DST-spanning cycle); got %d. A phase of 14 means the caller location was discarded for UTC.", phase)
	}
}

func TestCyclesignalsCov_InferUserLutealPhase_LutealLengthExactlyMinIsKept(t *testing.T) {
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

		// Cycle 1 (Jan1->Jan29): baseline Jan1-5, rise Jan19-21 -> ovulation Jan19.
		// luteal = Jan29 - Jan19 = 10 (exactly minLutealPhaseDays).
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-19"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-20"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-21"), BBT: models.NewBBT(36.50)},

		// Cycle 2 (Jan29->Feb26): baseline Jan29-Feb2, rise Feb12-14 -> ovulation Feb12.
		// luteal = Feb26 - Feb12 = 14 (valid).
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-12"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
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

func TestCyclesignalsCov_InferUserLutealPhase_LutealLengthExactlyTwentyIsKept(t *testing.T) {
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

		// Cycle 1 (Jan1->Jan29): baseline Jan1-5, rise Jan9-11 -> ovulation Jan9.
		// luteal = Jan29 - Jan9 = 20 (exactly the upper boundary).
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-09"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-10"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-11"), BBT: models.NewBBT(36.50)},

		// Cycle 2 (Jan29->Feb26): baseline Jan29-Feb2, rise Feb12-14 -> ovulation Feb12.
		// luteal = Feb26 - Feb12 = 14 (valid).
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-12"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
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
