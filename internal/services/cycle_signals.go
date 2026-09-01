package services

import (
	"math"
	"sort"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type cycleBBTPoint struct {
	Date     time.Time
	CycleDay int
	Value    float64
}

// "3-over-6" coverline rule (#249): the sliding coverline is the maximum of the
// bbtCoverlineWindow immediately preceding undisturbed recorded temperatures;
// a shift is bbtElevatedStreakDays calendar-consecutive days strictly above the
// coverline, with the third day at least bbtThirdDayMarginCelsius above it.
// Ovulation is estimated as the calendar day before the first elevated day.
const (
	bbtCoverlineWindow       = 6
	bbtElevatedStreakDays    = 3
	bbtThirdDayMarginCelsius = 0.2
)

// maxPlausibleLutealPhaseDays is the ceiling of the plausibility window the
// observed-luteal inference filters its per-cycle samples through; the floor is
// minLutealPhaseDays. It belongs to this file rather than to the luteal-phase
// const family in cycles.go because it is an outlier-rejection threshold owned
// by the inference, not a property of the cycle model: a length past it means
// the ovulation signal was misread (a stray egg-white day early in the cycle, a
// thermal shift picked up from a disturbed reading), so the cycle is dropped
// from the sample rather than clamped into it. The prediction-time bound is a
// different quantity — maxSupportedLutealPhase (cycles.go), computed from the
// cycle length.
//
// Both ends bound the parameter in CalcOvulationDay's reading of it: the count
// of days that FOLLOW ovulation. They are NOT bounds on the calendar span from
// the ovulation date to the next period start, which is one day longer — a
// sample at this floor is an ovulation on cycle day cycleLength-10, not one
// ten calendar days before the next period.
const maxPlausibleLutealPhaseDays = 20

func InferUserLutealPhase(logs []models.DailyLog, location *time.Location) (int, bool) {
	if location == nil {
		location = time.UTC
	}

	starts := ObservedCycleStarts(logs)
	if len(starts) < 3 {
		return defaultLutealPhaseDays, false
	}

	lutealLengths := make([]int, 0, len(starts)-1)
	for index := 0; index+1 < len(starts); index++ {
		start := CalendarDay(starts[index], location)
		nextStart := CalendarDay(starts[index+1], location)
		ovulationDate := inferObservedOvulationDate(logs, start, nextStart, location)
		if ovulationDate.IsZero() {
			continue
		}

		// Derive the parameter through the inverse of the prediction's own
		// arithmetic rather than measuring the calendar span to nextStart. That
		// span counts the ovulation day itself, so it runs one day longer than
		// the luteal phase CalcOvulationDay consumes, and feeding it back in
		// predicted the day BEFORE the observed ovulation on an identical next
		// cycle — the personalized path shifted ovulation and both fertile-window
		// edges one day early on every surface that renders them.
		cycleLength := CalendarDaysBetween(start, nextStart)
		ovulationCycleDay := CalendarDaysBetween(start, ovulationDate) + 1
		lutealLength := calcLutealPhase(cycleLength, ovulationCycleDay)
		if lutealLength < minLutealPhaseDays || lutealLength > maxPlausibleLutealPhaseDays {
			continue
		}
		lutealLengths = append(lutealLengths, lutealLength)
	}

	if len(lutealLengths) < 2 {
		return defaultLutealPhaseDays, false
	}
	return int(math.Round(averageInts(lutealLengths))), true
}

// deriveUserLutealPhase is the single rule the derived users.luteal_phase cache
// is written by: the personalized inference when the owner's logs support one,
// and defaultLutealPhaseDays when they do not.
//
// All three writers of that column go through it — a day save
// (DayService.refreshDerivedCycleSettings), a bulk restore
// (ImportService.refreshDerivedCycleSettings) and the one-shot boot recompute
// (LutealPhaseRecomputer) — so no two of them can disagree about what the column
// should hold for the same logs. That disagreement is not hypothetical: the boot
// recompute exists because the stored values and the inference that produced
// them had drifted a day apart.
//
// Unexported on purpose. All three writers live in this package, and the column
// has no other legitimate writer: an exported derivation would let a surface
// outside the package compute the value and store it without going through one
// of them, which is the drift this function exists to close. Callers that need
// the value for DISPLAY read it through ApplyUserCycleBaseline, which re-infers
// over the log window it was handed.
//
// The today bound lives HERE, not in each writer's fetch. The column summarizes
// OBSERVED cycles, and manualCycleStartFutureDays lets an owner record a cycle
// start up to two days ahead; ObservedCycleStarts takes such a start as the
// boundary of the last observed cycle, so an unbounded read derives the column
// from a day that has not happened yet. Bounding one writer would be worse than
// bounding none: the boot recompute would correct the column and the next day
// save would put the future-dated value straight back, which is precisely the
// disagreement the paragraph above promises cannot happen. `now` is a required
// parameter for the same reason — a writer cannot omit the bound by forgetting
// it. Same shape as the pregnancy pause (`prediction-display.md`): the bound
// belongs to the derivation, never to the caller's fetch.
func deriveUserLutealPhase(logs []models.DailyLog, now time.Time, location *time.Location) int {
	observed := filterLogsNotAfter(logs, DateAtLocation(now, location))
	if lutealPhase, refined := InferUserLutealPhase(observed, location); refined {
		return lutealPhase
	}
	return defaultLutealPhaseDays
}

// ConfirmedCurrentCycleOvulation reports the day the shared "3-over-6" thermal
// detector CONFIRMS for the owner's CURRENT cycle, and whether it found one.
//
// A detected shift names an ovulation that has already happened; it never
// predicts one. Every surface that may show a confirmed ovulation resolves it
// here — the calendar's solid marker and the dashboard's ovulation line — so the
// two cannot name different days for one shift. That divergence is not
// hypothetical: the grid and the stats chart named two days for one shift until
// the marker was moved onto the detector's own date, and the dashboard line was
// left behind on the model's date by that same change.
//
// The logs are bounded at the owner's today first: a reading recorded against a
// future date must not confirm an ovulation that has not happened. The caller
// supplies today so that a surface which has already resolved it does not
// resolve a second, possibly different one.
func ConfirmedCurrentCycleOvulation(user *models.User, logs []models.DailyLog, stats CycleStats, today time.Time, location *time.Location) (time.Time, bool) {
	if user == nil || !user.TrackBBT || stats.LastPeriodStart.IsZero() || stats.OvulationDate.IsZero() || stats.NextPeriodStart.IsZero() {
		return time.Time{}, false
	}

	// The gate lives HERE rather than at each surface, so the calendar and the
	// dashboard cannot gate the same signal differently. It is the same
	// predicate the calendar already wrapped this pass in
	// (FertilityProjectionSuppressed = unpredictable · pregnancy-paused ·
	// overdue, plus the first-cycle floor), read once instead of restated —
	// prediction-display.md forbids a surface recombining the signals itself.
	if FertilityProjectionSuppressed(user, stats) {
		return time.Time{}, false
	}

	cycleStart := CalendarDay(stats.LastPeriodStart, location)
	if CalendarDaysBetween(cycleStart, today) < 0 {
		return time.Time{}, false
	}

	signal := inferBBTOvulationDate(filterLogsNotAfter(logs, today), cycleStart, CalendarDay(stats.NextPeriodStart, location), location)
	if signal.IsZero() {
		return time.Time{}, false
	}
	return signal, true
}

// ConfirmedOvulationSupersedes reports whether a PROJECTED ovulation day is one
// the owner's temperatures have already answered.
//
// The on-screen surfaces replace such a day with the measured one. The two
// surfaces that leave the instance — the .ics feed and the webhook reminder —
// cannot: both exist to announce a day that is still ahead, and a shift confirms
// a day that is behind. Announcing the projection anyway is how they came to
// name a different day than the dashboard and the grid for one shift, on the day
// the difference is largest: the projected day itself.
//
// The bound is the confirmation's own window. ConfirmedCurrentCycleOvulation
// reads the CURRENT cycle only, so a projection that has already rolled into the
// next cycle is about a day this shift says nothing about, and suppressing it
// would withhold a reminder the account is owed. Callers pass each projected day
// they are about to announce rather than deciding per surface: a rule restated
// twice is a rule that can disagree with itself.
func ConfirmedOvulationSupersedes(user *models.User, logs []models.DailyLog, stats CycleStats, projected time.Time, today time.Time, location *time.Location) bool {
	if projected.IsZero() {
		return false
	}
	if _, confirmed := ConfirmedCurrentCycleOvulation(user, logs, stats, today, location); !confirmed {
		return false
	}
	return CalendarDaysBetween(projected, CalendarDay(stats.NextPeriodStart, location)) > 0
}

func inferObservedOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	bbtDate := inferBBTOvulationDate(logs, cycleStart, nextStart, location)
	if !bbtDate.IsZero() {
		return bbtDate
	}
	return inferEggWhiteOvulationDate(logs, cycleStart, nextStart, location)
}

func inferBBTOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	recordedDays, dayValues := bbtSeriesFromPoints(collectCycleBBTPoints(logs, cycleStart, nextStart, location))
	firstHighDay, _, ok := detectBBTShiftFirstHighDay(recordedDays, dayValues)
	if !ok {
		return time.Time{}
	}

	// Ovulation precedes the sustained thermal shift: basal temperature rises the
	// day after ovulation, so the estimate is the day before the first elevated
	// day (clamped to stay within the cycle).
	ovulationCycleDay := firstHighDay - 1
	// codecov:ignore:start -- defensive floor: firstHighDay is at least the 7th
	// recorded cycle day (the detector requires a full 6-value coverline window
	// before it), so ovulationCycleDay is always >= 6 and this clamp never fires
	// in practice.
	if ovulationCycleDay < 1 {
		ovulationCycleDay = firstHighDay
	}
	// codecov:ignore:end

	// The day step runs over UTC-anchored days rather than from the request-zone
	// anchor cycleStart carries: where a DST jump lands on midnight
	// (America/Santiago 2026-09-06, America/Havana 2026-03-08) local midnight
	// does not exist on that date, and AddDate resolves the missing wall clock
	// BACKWARD into the previous calendar day. Stepped from the zone anchor the
	// estimate names the day BEFORE the one the detector found — and that day is
	// no longer only a luteal sample: it is also the calendar grid's solid
	// ovulation marker. calendarGridBounds leaves the zone for the same reason.
	// Every consumer reads this value as a calendar day only
	// (CalendarDaysBetween in the luteal inference, CalendarDayKey on the grid),
	// so re-anchoring it moves no date on any surface.
	return dateOnly(cycleStart).AddDate(0, 0, ovulationCycleDay-1)
}

// detectBBTShiftFirstHighDay is the one shared "3-over-6" detector: luteal
// inference, the calendar tentative-ovulation signal, and the stats chart
// marker/coverline must all route through it so they never disagree.
//
// The sliding coverline for a candidate first elevated day is the MAX of the 6
// immediately preceding recorded temperatures (max, not mean, so ordinary
// follicular noise cannot slip past). A shift is three consecutive calendar
// days (cycle days n, n+1, n+2), all recorded, the first two strictly above
// the coverline and the third at least bbtThirdDayMarginCelsius above it.
// recordedDays must be sorted ascending; dayValues maps each recorded cycle
// day to its temperature. Returns the first elevated cycle day and the
// coverline in effect.
func detectBBTShiftFirstHighDay(recordedDays []int, dayValues map[int]float64) (int, float64, bool) {
	for index := bbtCoverlineWindow; index+bbtElevatedStreakDays-1 < len(recordedDays); index++ {
		dayOne := recordedDays[index]
		dayTwo := recordedDays[index+1]
		dayThree := recordedDays[index+2]
		if dayTwo != dayOne+1 || dayThree != dayTwo+1 {
			continue
		}

		coverline := dayValues[recordedDays[index-bbtCoverlineWindow]]
		for windowIndex := index - bbtCoverlineWindow + 1; windowIndex < index; windowIndex++ {
			if value := dayValues[recordedDays[windowIndex]]; value > coverline {
				coverline = value
			}
		}

		if dayValues[dayOne] <= coverline || dayValues[dayTwo] <= coverline {
			continue
		}
		if dayValues[dayThree] < coverline+bbtThirdDayMarginCelsius {
			continue
		}
		return dayOne, coverline, true
	}
	return 0, 0, false
}

// bbtSeriesFromPoints converts ordered points into the recordedDays/dayValues
// pair the shared detector consumes.
func bbtSeriesFromPoints(points []cycleBBTPoint) ([]int, map[int]float64) {
	recordedDays := make([]int, len(points))
	dayValues := make(map[int]float64, len(points))
	for index, point := range points {
		recordedDays[index] = point.CycleDay
		dayValues[point.CycleDay] = point.Value
	}
	return recordedDays, dayValues
}

// collectCycleBBTPoints builds the detection series: one undisturbed reading
// per calendar day (the latest same-day reading wins, matching the chart
// series). Days tagged illness or sleep_disruption are excluded entirely — a
// fever must neither inflate the coverline nor confirm an elevated streak.
func collectCycleBBTPoints(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) []cycleBBTPoint {
	pointByDay := make(map[int]cycleBBTPoint)
	for _, logEntry := range sortDailyLogs(logs) {
		if logEntry.BBT == nil || !IsValidDayBBT(logEntry.BBT) || isBBTDisturbedLog(logEntry) {
			continue
		}

		day := CalendarDay(logEntry.Date, location)
		if day.Before(cycleStart) || !day.Before(nextStart) {
			continue
		}

		cycleDay := CalendarDaysBetween(cycleStart, day) + 1
		pointByDay[cycleDay] = cycleBBTPoint{
			Date:     day,
			CycleDay: cycleDay,
			Value:    *logEntry.BBT,
		}
	}

	points := make([]cycleBBTPoint, 0, len(pointByDay))
	for _, point := range pointByDay {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].CycleDay < points[j].CycleDay
	})
	return points
}

// isBBTDisturbedLog reports whether the day carries a factor that distorts
// basal temperature independently of ovulation (#249 disturbance rejection).
func isBBTDisturbedLog(logEntry models.DailyLog) bool {
	for _, factorKey := range logEntry.CycleFactorKeys {
		if factorKey == models.CycleFactorIllness || factorKey == models.CycleFactorSleepDisruption {
			return true
		}
	}
	return false
}

func inferEggWhiteOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	lastEggWhite := time.Time{}
	for _, logEntry := range sortDailyLogs(logs) {
		day := CalendarDay(logEntry.Date, location)
		if day.Before(cycleStart) || !day.Before(nextStart) {
			continue
		}
		if NormalizeDayCervicalMucus(logEntry.CervicalMucus) != models.CervicalMucusEggWhite {
			continue
		}
		lastEggWhite = day
	}
	if lastEggWhite.IsZero() {
		return lastEggWhite
	}

	// Peak-day rule: the last day of fertile-quality (egg-white) mucus is the peak
	// fertility signal, and ovulation most commonly follows it by about a day.
	// Estimate ovulation as the day after the peak, clamped to stay before the
	// next cycle start (a peak on the final cycle day keeps the peak day itself).
	//
	// Step and clamp both leave the request zone, for the reason spelled out in
	// inferBBTOvulationDate: stepping from the zone anchor lands on the previous
	// calendar day whenever the peak's successor is a day whose local midnight a
	// DST jump skips. Once the step is UTC-anchored the clamp has to leave the
	// zone too — compared as instants, a UTC-anchored day reads as EARLIER than
	// the same day anchored in a UTC-minus zone, so the clamp would let the
	// estimate land on the next cycle start itself.
	peakDay := dateOnly(lastEggWhite)
	estimated := peakDay.AddDate(0, 0, 1)
	if CalendarDaysBetween(estimated, nextStart) <= 0 {
		return peakDay
	}
	return estimated
}
