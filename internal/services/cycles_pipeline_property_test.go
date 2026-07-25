package services

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"pgregory.net/rapid"
)

// System-level property tests for the cycle-prediction pipeline.
//
// Where cycles_property_test.go pins the algebra of the leaf functions
// (ResolveLutealPhase, CalcOvulationDay, PredictCycleWindow), this file drives
// the *whole* pipeline — DetectCycleStarts -> BuildCycleStats -> the predicted
// window — over thousands of synthetic-but-plausible cycle histories, plus the
// day-service auto-fill/clear side effects and the date helpers. A privacy-first
// tracker has no real user data to mine; this generator substitutes for it.
//
// Every invariant is stated as the documented/intended contract from
// docs/cycle-prediction.md and asserts only on RETURNED values, never on
// rendered markup, logs, or error text.
//
// A counterexample rapid finds is triaged, not parked. Earlier revisions of this
// file kept failing invariants as t.Skip carrying the counterexample in their skip
// message, which keeps the package green at the cost of a test that can no longer
// fail — indistinguishable from coverage while asserting nothing. The two
// dispositions are: if the invariant was merely mis-scoped, scope it correctly and
// keep it running (invariant 14 states its scope and why); if the invariant was
// simply false against the model, delete it and move the counterexample to
// docs/cycle-prediction.md, where it documents behaviour instead of contradicting
// it (see the note where invariant 8 used to be).

// ----------------------------------------------------------------------------
// Step 2 — reusable realistic cycle-history generator
// ----------------------------------------------------------------------------

// cycleHistory is the output of drawCycleHistory: the synthetic logs plus the
// ground-truth metadata other tests assert against. logs are sorted ascending
// by date and stored at UTC-midnight (the storage convention enforced by
// DailyLog.BeforeSave and assumed throughout services via dateOnly/CalendarDay).
type cycleHistory struct {
	logs       []models.DailyLog
	startDates []time.Time // first day of each emitted period run (period start)
	periodRuns [][]time.Time
	firstStart time.Time
	now        time.Time // a "now" guaranteed to be on/after the last logged day
	baseCycle  int
	basePeriod int
	numCycles  int
}

// drawCycleHistory draws a plausible []models.DailyLog: a start date, a "true"
// cycle length (~21-45) and period length (~2-8), 0-12 cycles, realistic
// per-cycle variation (a few days either way), with occasional long irregular
// gaps or skipped cycles. Period days are contiguous IsPeriod runs whose first
// day carries CycleStart=true. Dates are stored at UTC-midnight. It also covers
// the degenerate inputs (0 cycles => empty, 1 cycle => a single run) so callers
// exercise the empty/single-day edges for free.
//
// Kept deliberately self-contained and clearly named so any test in the package
// can reuse it.
func drawCycleHistory(t *rapid.T) cycleHistory {
	year := rapid.IntRange(1990, 2090).Draw(t, "year")
	month := rapid.IntRange(1, 12).Draw(t, "month")
	day := rapid.IntRange(1, 28).Draw(t, "day")
	firstStart := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	baseCycle := rapid.IntRange(21, 45).Draw(t, "baseCycle")
	basePeriod := rapid.IntRange(2, 8).Draw(t, "basePeriod")
	numCycles := rapid.IntRange(0, 12).Draw(t, "numCycles")

	hist := cycleHistory{
		firstStart: firstStart,
		baseCycle:  baseCycle,
		basePeriod: basePeriod,
		numCycles:  numCycles,
	}

	cursor := firstStart
	for range numCycles {
		// Per-cycle period length varies by +/-1 around the base, clamped to a
		// sane band so a "true" period is always a real contiguous run.
		periodLen := basePeriod + rapid.IntRange(-1, 1).Draw(t, "periodVar")
		if periodLen < 1 {
			periodLen = 1
		}
		if periodLen > 10 {
			periodLen = 10
		}

		run := make([]time.Time, 0, periodLen)
		for d := range periodLen {
			run = append(run, cursor.AddDate(0, 0, d))
		}
		hist.startDates = append(hist.startDates, cursor)
		hist.periodRuns = append(hist.periodRuns, run)

		// Advance to the next cycle start. Most cycles drift a few days around
		// the base length; occasionally inject a long irregular gap or a fully
		// skipped cycle (a double-length jump) to stress the detector.
		advance := baseCycle + rapid.IntRange(-3, 3).Draw(t, "cycleVar")
		switch rapid.IntRange(0, 9).Draw(t, "irregular") {
		case 0:
			advance += rapid.IntRange(10, 30).Draw(t, "longGap") // irregular long gap
		case 1:
			advance += baseCycle // a skipped cycle (no period logged that month)
		}
		if advance < 15 {
			advance = 15 // never let two starts collapse below the detector's gap
		}
		cursor = cursor.AddDate(0, 0, advance)
	}

	for _, run := range hist.periodRuns {
		for i, d := range run {
			hist.logs = append(hist.logs, models.DailyLog{
				UserID:     1,
				Date:       d,
				IsPeriod:   true,
				CycleStart: i == 0,
				Flow:       models.FlowMedium,
			})
		}
	}
	sort.Slice(hist.logs, func(i, j int) bool {
		return hist.logs[i].Date.Before(hist.logs[j].Date)
	})

	// "now" sits on or after the last logged day so BuildCycleStats does not
	// filter out the tail of the history (filterLogsNotAfter drops future days).
	last := firstStart
	if n := len(hist.logs); n > 0 {
		last = hist.logs[n-1].Date
	}
	hist.now = last.AddDate(0, 0, rapid.IntRange(0, 40).Draw(t, "nowOffset"))

	return hist
}

// detectedStartGaps returns the day gaps between successive detected cycle
// starts — the observed cycle lengths the stats are built from.
func detectedStartGaps(starts []time.Time) []int {
	if len(starts) < 2 {
		return nil
	}
	gaps := make([]int, 0, len(starts)-1)
	for i := 1; i < len(starts); i++ {
		gaps = append(gaps, int(starts[i].Sub(starts[i-1]).Hours()/24))
	}
	return gaps
}

func minMaxOf(values []int) (int, int) {
	lo, hi := values[0], values[0]
	for _, v := range values[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}

func isMidnightUTC(d time.Time) bool {
	h, m, s := d.Clock()
	return d.Location() == time.UTC && h == 0 && m == 0 && s == 0 && d.Nanosecond() == 0
}

// ----------------------------------------------------------------------------
// Step 3 — system-level invariants
// ----------------------------------------------------------------------------

// Invariant 1: every detected start is a period day; starts are strictly
// increasing; the number of starts never exceeds the number of period days.
func TestPipeline_DetectCycleStarts_BasicShape(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		starts := DetectCycleStarts(hist.logs)

		periodDays := make(map[string]bool)
		periodCount := 0
		for _, l := range hist.logs {
			if l.IsPeriod {
				periodDays[dateOnly(l.Date).Format("2006-01-02")] = true
				periodCount++
			}
		}

		for i, s := range starts {
			if !periodDays[s.Format("2006-01-02")] {
				t.Fatalf("start %s is not a period day", s.Format("2006-01-02"))
			}
			if i > 0 && !s.After(starts[i-1]) {
				t.Fatalf("starts not strictly increasing: %s then %s",
					starts[i-1].Format("2006-01-02"), s.Format("2006-01-02"))
			}
		}
		if len(starts) > periodCount {
			t.Fatalf("more starts (%d) than period days (%d)", len(starts), periodCount)
		}
	})
}

// Invariant 2: a single contiguous run of period days yields exactly ONE cycle
// start, which is the first day of the run.
func TestPipeline_ContiguousRunYieldsOneStart(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := drawCycleStartDate(t)
		runLen := rapid.IntRange(1, 12).Draw(t, "runLen")

		logs := make([]models.DailyLog, 0, runLen)
		for d := range runLen {
			logs = append(logs, models.DailyLog{
				Date:     start.AddDate(0, 0, d),
				IsPeriod: true,
			})
		}

		starts := DetectCycleStarts(logs)
		if len(starts) != 1 {
			t.Fatalf("contiguous run of %d days produced %d starts, want 1", runLen, len(starts))
		}
		if !starts[0].Equal(dateOnly(start)) {
			t.Fatalf("start = %s, want first day %s", starts[0], dateOnly(start))
		}
	})
}

// Invariant 3: when there is at least one completed cycle, the median and the
// average cycle length each lie within [min, max] of the observed gaps between
// detected starts. (Min/max in stats come from the full set of gaps; median and
// average come from the most recent <=6, a subset, so they must still fall in
// the full-set band.)
func TestPipeline_CycleLengthCentralWithinObservedRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		stats := BuildCycleStats(hist.logs, hist.now)

		if stats.CompletedCycleCount < 1 {
			return
		}
		// The stats are built from ObservedCycleStarts; with explicit CycleStart
		// flags on each run's first day this equals DetectCycleStarts here. Use
		// the same source the stats use to define the band.
		observed := ObservedCycleStarts(sortDailyLogs(filterLogsNotAfter(hist.logs, dateOnly(hist.now))))
		gaps := detectedStartGaps(observed)
		if len(gaps) == 0 {
			return
		}
		lo, hi := minMaxOf(gaps)

		if stats.MedianCycleLength < lo || stats.MedianCycleLength > hi {
			t.Fatalf("median %d outside observed gap range [%d,%d] (gaps=%v)",
				stats.MedianCycleLength, lo, hi, gaps)
		}
		avgRounded := int(stats.AverageCycleLength + 0.5)
		if avgRounded < lo || avgRounded > hi {
			t.Fatalf("average %.3f (rounds to %d) outside observed gap range [%d,%d] (gaps=%v)",
				stats.AverageCycleLength, avgRounded, lo, hi, gaps)
		}
	})
}

// Invariant 4: when a period length is computed it is >= 1 and <= the cycle
// length used for the prediction.
func TestPipeline_PeriodLengthWithinCycle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		stats := BuildCycleStats(hist.logs, hist.now)

		if stats.LastPeriodLength <= 0 {
			return
		}
		cycleLen := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
		if stats.LastPeriodLength < 1 {
			t.Fatalf("last period length %d < 1", stats.LastPeriodLength)
		}
		if stats.LastPeriodLength > cycleLen {
			t.Fatalf("last period length %d exceeds cycle length %d", stats.LastPeriodLength, cycleLen)
		}
	})
}

// Invariant 5: determinism — the same logs and the same `now` produce identical
// CycleStats on repeated calls.
func TestPipeline_BuildCycleStatsDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		a := BuildCycleStats(hist.logs, hist.now)
		b := BuildCycleStats(hist.logs, hist.now)
		if a != b {
			t.Fatalf("BuildCycleStats not deterministic:\n a=%+v\n b=%+v", a, b)
		}
	})
}

// Invariant 6: the predicted next-period start is strictly AFTER the last
// observed period start.
func TestPipeline_NextPeriodAfterLastStart(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		stats := BuildCycleStats(hist.logs, hist.now)
		if stats.LastPeriodStart.IsZero() || stats.NextPeriodStart.IsZero() {
			return
		}
		if !stats.NextPeriodStart.After(stats.LastPeriodStart) {
			t.Fatalf("next period %s not after last start %s",
				stats.NextPeriodStart.Format("2006-01-02"), stats.LastPeriodStart.Format("2006-01-02"))
		}
	})
}

// Invariant 7: the predicted next period equals lastPeriodStart + the resolved
// prediction cycle length (formula consistency).
func TestPipeline_NextPeriodMatchesFormula(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		stats := BuildCycleStats(hist.logs, hist.now)
		if stats.LastPeriodStart.IsZero() || stats.NextPeriodStart.IsZero() {
			return
		}
		cycleLen := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
		want := dateOnly(stats.LastPeriodStart.AddDate(0, 0, cycleLen))
		if !stats.NextPeriodStart.Equal(want) {
			t.Fatalf("next period %s != lastStart+%d=%s",
				stats.NextPeriodStart.Format("2006-01-02"), cycleLen, want.Format("2006-01-02"))
		}
	})
}

// Invariant 8 used to live here: "appending a period start later than every
// existing one never moves the predicted next-period date earlier". It was FALSE
// against the documented model and was removed rather than left as a permanent
// skip. Logging a new start advances the anchor and simultaneously re-estimates
// the cycle length from the new observation; when the fresh cycle is much shorter
// than the running estimate, the shrinking estimate outweighs the advancing
// anchor and the absolute date moves slightly earlier. The counterexample and the
// reasoning now live in docs/cycle-prediction.md ("Assumptions and limitations"),
// where they describe product behaviour instead of asserting its opposite. The
// contract that IS true — the projection equals anchor + predicted length — is
// pinned by TestPipeline_NextPeriodMatchesFormula above.

// Invariant 9: ovulation falls strictly between lastPeriodStart and the
// predicted next-period start.
func TestPipeline_OvulationBetweenStartAndNextPeriod(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		stats := BuildCycleStats(hist.logs, hist.now)
		if stats.OvulationImpossible || stats.OvulationDate.IsZero() {
			return
		}
		if !stats.OvulationDate.After(stats.LastPeriodStart) {
			t.Fatalf("ovulation %s not after last start %s",
				stats.OvulationDate.Format("2006-01-02"), stats.LastPeriodStart.Format("2006-01-02"))
		}
		if !stats.OvulationDate.Before(stats.NextPeriodStart) {
			t.Fatalf("ovulation %s not before next period %s",
				stats.OvulationDate.Format("2006-01-02"), stats.NextPeriodStart.Format("2006-01-02"))
		}
	})
}

// Invariant 10: AutoFillFollowingPeriodDays fills at most periodLength-1
// following days and never a day after the caller-local "today".
func TestPipeline_AutoFillBoundedAndNotFuture(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := drawCycleStartDate(t)
		periodLength := rapid.IntRange(1, 10).Draw(t, "periodLength")
		// nowOffset relative to start: may land before, inside, or after the
		// fill window so the "not after today" clamp is exercised.
		nowOffset := rapid.IntRange(-2, 14).Draw(t, "nowOffset")
		now := start.AddDate(0, 0, nowOffset)

		logs := newDayLogRepositoryStub()
		users := &dayUserRepositoryStub{settings: models.User{PeriodLength: periodLength, AutoPeriodFill: true}}
		service := NewDayService(logs, users)

		if err := service.AutoFillFollowingPeriodDays(context.Background(), 1, start, periodLength, models.FlowLight, now, time.UTC); err != nil {
			t.Fatalf("AutoFillFollowingPeriodDays: %v", err)
		}

		today := DateAtLocation(now, time.UTC)
		filled := 0
		for key, entry := range logs.entries {
			if !entry.IsPeriod {
				continue
			}
			filled++
			if entry.Date.After(today) {
				t.Fatalf("filled future day %s (today=%s)", key, today.Format("2006-01-02"))
			}
			// Filled days must be within (start, start+periodLength-1].
			if !entry.Date.After(start) || entry.Date.After(start.AddDate(0, 0, periodLength-1)) {
				t.Fatalf("filled day %s outside (start, start+%d]", key, periodLength-1)
			}
		}
		if filled > periodLength-1 {
			t.Fatalf("filled %d days, exceeds periodLength-1=%d", filled, periodLength-1)
		}
	})
}

// Invariant 11: AutoFillFollowingPeriodDays is a no-op when periodLength <= 1.
func TestPipeline_AutoFillNoOpForShortPeriod(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := drawCycleStartDate(t)
		periodLength := rapid.IntRange(-3, 1).Draw(t, "periodLength")
		now := start.AddDate(0, 0, 30)

		logs := newDayLogRepositoryStub()
		users := &dayUserRepositoryStub{settings: models.User{AutoPeriodFill: true}}
		service := NewDayService(logs, users)

		if err := service.AutoFillFollowingPeriodDays(context.Background(), 1, start, periodLength, models.FlowLight, now, time.UTC); err != nil {
			t.Fatalf("AutoFillFollowingPeriodDays: %v", err)
		}
		if len(logs.entries) != 0 {
			t.Fatalf("expected no writes for periodLength=%d, got %d entries", periodLength, len(logs.entries))
		}
	})
}

// Invariant 12: ClearAutoFilledPeriodNeighbors never clears a manually-edited
// day (only bare auto-fill candidates) and clears at most periodLength-1 days.
func TestPipeline_ClearNeighborsPreservesManualAndBounded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := dateOnly(drawCycleStartDate(t))
		periodLength := rapid.IntRange(2, 10).Draw(t, "periodLength")

		logs := newDayLogRepositoryStub()
		users := &dayUserRepositoryStub{settings: models.User{PeriodLength: periodLength, AutoPeriodFill: true}}
		service := NewDayService(logs, users)

		// Seed the anchor + a contiguous run of following period days. Randomly
		// mark some following days as manually edited (mood set) so they are NOT
		// bare auto-fill candidates and must be preserved.
		manual := make(map[string]bool)
		logs.entries[start.Format("2006-01-02")] = models.DailyLog{
			ID: 1, UserID: 1, Date: start, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium,
		}
		id := uint(2)
		for offset := 1; offset < periodLength; offset++ {
			d := start.AddDate(0, 0, offset)
			key := d.Format("2006-01-02")
			entry := models.DailyLog{ID: id, UserID: 1, Date: d, IsPeriod: true, Flow: models.FlowMedium}
			if rapid.Bool().Draw(t, "manual") {
				entry.Mood = MinDayMood
				manual[key] = true
			}
			logs.entries[key] = entry
			id++
		}

		// Snapshot which days were period-true before clearing.
		before := make(map[string]bool)
		for k, e := range logs.entries {
			before[k] = e.IsPeriod
		}

		if err := service.ClearAutoFilledPeriodNeighbors(context.Background(), 1, start, periodLength, time.UTC); err != nil {
			t.Fatalf("ClearAutoFilledPeriodNeighbors: %v", err)
		}

		clearedCount := 0
		for k, wasPeriod := range before {
			nowPeriod := logs.entries[k].IsPeriod
			if wasPeriod && !nowPeriod {
				clearedCount++
				if manual[k] {
					t.Fatalf("manually edited day %s was cleared", k)
				}
				if k == start.Format("2006-01-02") {
					t.Fatalf("the anchor/start day %s was cleared", k)
				}
			}
		}
		if clearedCount > periodLength-1 {
			t.Fatalf("cleared %d days, exceeds periodLength-1=%d", clearedCount, periodLength-1)
		}
	})
}

// Invariant 13: DateAtLocation is idempotent and returns midnight in the given
// location.
func TestPipeline_DateAtLocationIdempotentMidnight(t *testing.T) {
	locations := []*time.Location{
		time.UTC,
		time.FixedZone("UTC+9", 9*3600),
		time.FixedZone("UTC-8", -8*3600),
		time.FixedZone("UTC+5:30", 5*3600+30*60),
	}
	rapid.Check(t, func(t *rapid.T) {
		// Draw an arbitrary instant (any time of day, any UTC offset on input).
		unix := rapid.Int64Range(0, 4102444800).Draw(t, "unix") // 1970..2100
		offset := rapid.IntRange(-12*3600, 12*3600).Draw(t, "inputOffset")
		instant := time.Unix(unix, 0).In(time.FixedZone("in", offset))

		loc := locations[rapid.IntRange(0, len(locations)-1).Draw(t, "loc")]

		once := DateAtLocation(instant, loc)
		twice := DateAtLocation(once, loc)

		if !once.Equal(twice) {
			t.Fatalf("DateAtLocation not idempotent: once=%s twice=%s", once, twice)
		}
		h, m, s := once.Clock()
		if h != 0 || m != 0 || s != 0 || once.Nanosecond() != 0 {
			t.Fatalf("DateAtLocation did not return midnight: %s", once)
		}
		if once.Location().String() != loc.String() {
			t.Fatalf("DateAtLocation location = %s, want %s", once.Location(), loc)
		}
	})
}

// Invariant 14: for an instant that is unambiguously past the history in every
// display location, ApplyUserCycleBaseline derives the same CALENDAR dates (last
// period start, next period, ovulation, fertility window) regardless of that
// location, because the stored history is calendar-only and CalendarDay must not
// shift it.
//
// The margin below is what makes the invariant true rather than a way to dodge a
// bug. Only ONE of the compared fields is "today"-dependent: LastPeriodStart is
// gated on `anchor <= local today`, and local today for a fixed instant legitimately
// differs by a day across zones. At the UTC-midnight edge that gate is the whole
// behaviour of DateAtLocation, not a defect — with a single period day on
// 1990-01-01 and now = 1990-01-01T00:00Z, UTC reads today as 1990-01-01 and
// anchors on it, while UTC-8 reads 1989-12-31 and correctly treats the period as
// still in that user's future. Two calendar days of margin puts the anchor behind
// local today in every zone (the widest zone here shifts by 13h), so what remains
// under test is the calendar stability of the derived dates. The projected half
// (applyProjectedBaseline) never reads today at all, so it is fully covered.
// Invariant 13 covers the midnight-boundary semantics themselves.
func TestPipeline_BaselinePredictionCalendarStableAcrossLocations(t *testing.T) {
	locations := []*time.Location{
		time.FixedZone("UTC+9", 9*3600),
		time.FixedZone("UTC-8", -8*3600),
		time.FixedZone("UTC+13", 13*3600),
		time.FixedZone("UTC-11", -11*3600),
	}
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		user := &models.User{
			Role:         models.RoleOwner,
			CycleLength:  hist.baseCycle,
			PeriodLength: hist.basePeriod,
			LutealPhase:  defaultLutealPhaseDays,
		}

		// drawCycleHistory may put `now` exactly on the last logged day at
		// UTC-midnight; push it past the zone spread so "today" is the same
		// calendar day or later everywhere.
		now := hist.now.AddDate(0, 0, 2)

		// Baseline computed in UTC is the reference.
		baseStats := BuildCycleStats(hist.logs, now)
		utc := ApplyUserCycleBaseline(user, hist.logs, baseStats, now, time.UTC)

		key := func(d time.Time) string {
			if d.IsZero() {
				return "<zero>"
			}
			return d.Format("2006-01-02")
		}

		for _, loc := range locations {
			locStats := ApplyUserCycleBaseline(user, hist.logs, BuildCycleStats(hist.logs, now), now, loc)
			for _, pair := range []struct {
				name string
				a, b time.Time
			}{
				{"LastPeriodStart", utc.LastPeriodStart, locStats.LastPeriodStart},
				{"NextPeriodStart", utc.NextPeriodStart, locStats.NextPeriodStart},
				{"OvulationDate", utc.OvulationDate, locStats.OvulationDate},
				{"FertilityWindowStart", utc.FertilityWindowStart, locStats.FertilityWindowStart},
				{"FertilityWindowEnd", utc.FertilityWindowEnd, locStats.FertilityWindowEnd},
			} {
				if key(pair.a) != key(pair.b) {
					t.Fatalf("%s calendar date shifted under %s: UTC=%s loc=%s",
						pair.name, loc, key(pair.a), key(pair.b))
				}
			}
		}
	})
}

// Invariant 15: the full pipeline never panics and never returns a
// negative/zero-nonsensical cycle or period length for ANY generated history,
// including the empty and single-day edges.
func TestPipeline_NeverPanicsOrReturnsNonsense(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hist := drawCycleHistory(t)
		stats := BuildCycleStats(hist.logs, hist.now)

		// Lengths are either unset (0, meaning "not enough data") or strictly
		// positive. Never negative.
		for name, v := range map[string]int{
			"MedianCycleLength":   stats.MedianCycleLength,
			"MinCycleLength":      stats.MinCycleLength,
			"MaxCycleLength":      stats.MaxCycleLength,
			"LastCycleLength":     stats.LastCycleLength,
			"LastPeriodLength":    stats.LastPeriodLength,
			"CompletedCycleCount": stats.CompletedCycleCount,
			"CurrentCycleDay":     stats.CurrentCycleDay,
		} {
			if v < 0 {
				t.Fatalf("%s = %d is negative", name, v)
			}
		}
		if stats.AverageCycleLength < 0 || stats.AveragePeriodLength < 0 {
			t.Fatalf("negative average: cycle=%.3f period=%.3f", stats.AverageCycleLength, stats.AveragePeriodLength)
		}
		// The cycle length actually used for prediction must always be positive.
		if cl := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength); cl <= 0 {
			t.Fatalf("predicted cycle length %d is non-positive", cl)
		}
		// Min/max coherence when both are set.
		if stats.MinCycleLength > 0 && stats.MaxCycleLength > 0 && stats.MaxCycleLength < stats.MinCycleLength {
			t.Fatalf("max %d < min %d", stats.MaxCycleLength, stats.MinCycleLength)
		}
		// Derived prediction dates, when present, are well-formed midnight-UTC.
		for _, d := range []time.Time{stats.NextPeriodStart, stats.OvulationDate, stats.FertilityWindowStart, stats.FertilityWindowEnd} {
			if !d.IsZero() && !isMidnightUTC(d) {
				t.Fatalf("non-midnight-UTC derived date: %s", d)
			}
		}
	})
}
