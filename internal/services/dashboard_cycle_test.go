package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestDashboardCycleReferenceLengthPrefersObservedAverage(t *testing.T) {
	user := &models.User{CycleLength: 29}
	stats := CycleStats{MedianCycleLength: 28, AverageCycleLength: 27}
	if got := DashboardCycleReferenceLength(user, stats); got != 27 {
		t.Fatalf("expected 27, got %d", got)
	}
}

func TestDashboardCycleStaleAnchorPrefersStatsBaseline(t *testing.T) {
	userBaseline := time.Date(2026, time.January, 1, 15, 30, 0, 0, time.UTC)
	statsBaseline := time.Date(2026, time.February, 20, 7, 0, 0, 0, time.UTC)
	user := &models.User{LastPeriodStart: &userBaseline}
	stats := CycleStats{LastPeriodStart: statsBaseline}

	anchor := DashboardCycleStaleAnchor(user, stats, time.UTC)
	if anchor.Format("2006-01-02") != "2026-02-20" {
		t.Fatalf("expected stats baseline date, got %s", anchor.Format("2006-01-02"))
	}
}

func TestCompletedCycleTrendLengths(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseDashboardDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-02-26"), IsPeriod: true},
	}
	now := mustParseDashboardDay(t, "2026-03-10")

	got := CompletedCycleTrendLengths(logs, now, time.UTC)
	if len(got) != 2 || got[0] != 28 || got[1] != 28 {
		t.Fatalf("expected [28 28], got %#v", got)
	}
}

func TestBuildDashboardCycleContext(t *testing.T) {
	userStart := mustParseDashboardDay(t, "2026-02-10")
	user := &models.User{
		CycleLength:     28,
		PeriodLength:    5,
		LastPeriodStart: &userStart,
	}
	stats := CycleStats{
		CurrentCycleDay:     36,
		LastPeriodStart:     mustParseDashboardDay(t, "2026-02-10"),
		MedianCycleLength:   28,
		AveragePeriodLength: 5,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-03-10"),
		OvulationDate:       mustParseDashboardDay(t, "2026-02-24"),
	}
	today := mustParseDashboardDay(t, "2026-03-14")

	context := BuildDashboardCycleContext(user, stats, today, time.UTC)
	if context.CycleDayReference != 28 {
		t.Fatalf("expected cycle day reference 28, got %d", context.CycleDayReference)
	}
	if !context.CycleDayWarning {
		t.Fatalf("expected cycle day warning for long cycle day")
	}
	if !context.CycleDataStale {
		t.Fatalf("expected stale cycle data flag")
	}
	if context.DisplayNextPeriodUseRange {
		t.Fatalf("did not expect stable cycle context to render next period as an uncertainty range")
	}
	if got := context.DisplayNextPeriodStart.Format("2006-01-02"); got != "2026-04-07" {
		t.Fatalf("expected exact next period start 2026-04-07, got %s", got)
	}
	if got := context.DisplayNextPeriodEnd.Format("2006-01-02"); got != "2026-04-11" {
		t.Fatalf("expected exact next period end 2026-04-11, got %s", got)
	}
}

func TestBuildDashboardCycleContextUsesRangeForIrregularMode(t *testing.T) {
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:      mustParseDashboardDay(t, "2026-03-01"),
		AverageCycleLength:   32,
		MinCycleLength:       24,
		MaxCycleLength:       45,
		CurrentCycleDay:      20,
		CompletedCycleCount:  3,
		NextPeriodStart:      mustParseDashboardDay(t, "2026-04-02"),
		FertilityWindowStart: mustParseDashboardDay(t, "2026-03-12"),
		FertilityWindowEnd:   mustParseDashboardDay(t, "2026-03-17"),
		OvulationDate:        mustParseDashboardDay(t, "2026-03-17"),
	}
	today := mustParseDashboardDay(t, "2026-03-20")

	context := BuildDashboardCycleContext(user, stats, today, time.UTC)
	if !context.DisplayNextPeriodUseRange {
		t.Fatalf("expected irregular mode to use prediction range")
	}
	if got := context.DisplayNextPeriodRangeStart.Format("2006-01-02"); got != "2026-03-25" {
		t.Fatalf("expected range start 2026-03-25, got %s", got)
	}
	if got := context.DisplayNextPeriodRangeEnd.Format("2006-01-02"); got != "2026-04-15" {
		t.Fatalf("expected range end 2026-04-15, got %s", got)
	}
	if context.DisplayNextPeriodNeedsData {
		t.Fatalf("expected irregular range to skip the low-data placeholder")
	}
	if !context.DisplayOvulationUseRange {
		t.Fatalf("expected irregular mode to use ovulation range")
	}
	if got := context.DisplayOvulationRangeStart.Format("2006-01-02"); got != "2026-03-11" {
		t.Fatalf("expected ovulation range start 2026-03-11, got %s", got)
	}
	if got := context.DisplayOvulationRangeEnd.Format("2006-01-02"); got != "2026-04-01" {
		t.Fatalf("expected ovulation range end 2026-04-01, got %s", got)
	}
	if !context.DisplayOvulationDate.IsZero() {
		t.Fatalf("expected irregular range to suppress single ovulation date")
	}
}

func TestDashboardPredictionRangeUsesObservedStdDevForRegularCycles(t *testing.T) {
	predictedStart := mustParseDashboardDay(t, "2026-04-07")
	cases := []struct {
		name         string
		stats        CycleStats
		wantOK       bool
		wantStartISO string
		wantEndISO   string
	}{
		{
			name:   "fewer than three completed cycles shows no range",
			stats:  CycleStats{CompletedCycleCount: 2, CycleLengthStdDev: 3.0},
			wantOK: false,
		},
		{
			name:   "zero variability shows no range",
			stats:  CycleStats{CompletedCycleCount: 6, CycleLengthStdDev: 0},
			wantOK: false,
		},
		{
			name:         "low variability rounds up to one day",
			stats:        CycleStats{CompletedCycleCount: 4, CycleLengthStdDev: 0.4},
			wantOK:       true,
			wantStartISO: "2026-04-06",
			wantEndISO:   "2026-04-08",
		},
		{
			name:         "moderate variability rounds to nearest day",
			stats:        CycleStats{CompletedCycleCount: 5, CycleLengthStdDev: 2.4},
			wantOK:       true,
			wantStartISO: "2026-04-05",
			wantEndISO:   "2026-04-09",
		},
		{
			name:         "high variability is clamped at five days",
			stats:        CycleStats{CompletedCycleCount: 8, CycleLengthStdDev: 8.7},
			wantOK:       true,
			wantStartISO: "2026-04-02",
			wantEndISO:   "2026-04-12",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rangeStart, rangeEnd, ok := DashboardPredictionRange(&models.User{}, tc.stats, predictedStart, time.UTC)
			if ok != tc.wantOK {
				t.Fatalf("expected ok=%v, got ok=%v", tc.wantOK, ok)
			}
			if !tc.wantOK {
				return
			}
			if got := rangeStart.Format("2006-01-02"); got != tc.wantStartISO {
				t.Fatalf("expected range start %s, got %s", tc.wantStartISO, got)
			}
			if got := rangeEnd.Format("2006-01-02"); got != tc.wantEndISO {
				t.Fatalf("expected range end %s, got %s", tc.wantEndISO, got)
			}
		})
	}
}

// TestDashboardPredictionRangeIgnoresAgeGroup locks in that age, on its
// own, no longer widens the prediction. The previous age_35_plus add-on
// was applied to the cohort with the lowest within-individual variability
// per Gibson et al., npj Digital Medicine 2023 (Apple Women's Health
// Study), so it has been removed in favour of the data-driven span above.
func TestDashboardPredictionRangeIgnoresAgeGroup(t *testing.T) {
	stats := CycleStats{CompletedCycleCount: 5, CycleLengthStdDev: 2.0}
	predictedStart := mustParseDashboardDay(t, "2026-04-07")

	for _, ageGroup := range []string{
		models.AgeGroupUnder40,
		models.AgeGroup40To45,
		models.AgeGroup45Plus,
	} {
		ageGroup := ageGroup
		t.Run(ageGroup, func(t *testing.T) {
			rangeStart, rangeEnd, ok := DashboardPredictionRange(&models.User{AgeGroup: ageGroup}, stats, predictedStart, time.UTC)
			if !ok {
				t.Fatalf("expected prediction range to be calculable for age group %q", ageGroup)
			}
			if got := rangeStart.Format("2006-01-02"); got != "2026-04-05" {
				t.Fatalf("expected range start 2026-04-05 for age group %q, got %s", ageGroup, got)
			}
			if got := rangeEnd.Format("2006-01-02"); got != "2026-04-09" {
				t.Fatalf("expected range end 2026-04-09 for age group %q, got %s", ageGroup, got)
			}
		})
	}
}

func TestBuildDashboardCycleContextShowsDataDrivenRangeForRegularUserWithVariability(t *testing.T) {
	user := &models.User{CycleLength: 28}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-10"),
		AverageCycleLength:  28.4,
		MedianCycleLength:   28,
		AveragePeriodLength: 5,
		CompletedCycleCount: 5,
		CycleLengthStdDev:   2.4,
		CurrentCycleDay:     5,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-04-07"),
		OvulationDate:       mustParseDashboardDay(t, "2026-03-24"),
		OvulationExact:      true,
	}
	today := mustParseDashboardDay(t, "2026-03-14")

	context := BuildDashboardCycleContext(user, stats, today, time.UTC)
	if !context.DisplayNextPeriodUseRange {
		t.Fatalf("expected data-driven prediction range for a regular user with measurable variability")
	}
	if got := context.DisplayNextPeriodRangeStart.Format("2006-01-02"); got != "2026-04-05" {
		t.Fatalf("expected range start 2026-04-05, got %s", got)
	}
	if got := context.DisplayNextPeriodRangeEnd.Format("2006-01-02"); got != "2026-04-09" {
		t.Fatalf("expected range end 2026-04-09, got %s", got)
	}
	if context.DisplayOvulationUseRange {
		t.Fatalf("did not expect ovulation range for a regular user")
	}
	if context.DisplayOvulationDate.IsZero() {
		t.Fatalf("expected ovulation date to remain visible for a regular user")
	}
}

func TestDashboardUpcomingPredictionsClampsShortCycleOvulationAwayFromCycleStart(t *testing.T) {
	stats := CycleStats{
		LastPeriodStart: mustParseDashboardDay(t, "2026-02-10"),
		LutealPhase:     14,
	}
	today := mustParseDashboardDay(t, "2026-02-10")

	nextPeriodStart, ovulationDate, ovulationExact, ovulationImpossible := DashboardUpcomingPredictions(stats, &models.User{}, today, 15)
	if got := nextPeriodStart.Format("2006-01-02"); got != "2026-02-25" {
		t.Fatalf("expected next period start 2026-02-25, got %s", got)
	}
	if got := ovulationDate.Format("2006-01-02"); got != "2026-02-14" {
		t.Fatalf("expected clamped ovulation date 2026-02-14, got %s", got)
	}
	if ovulationExact {
		t.Fatalf("expected short-cycle ovulation prediction to be approximate")
	}
	if ovulationImpossible {
		t.Fatalf("expected short-cycle ovulation prediction to remain calculable")
	}
}

func TestDashboardUpcomingPredictionsKeepsNearestUpcomingPeriodWhenCurrentOvulationAlreadyPassed(t *testing.T) {
	stats := CycleStats{
		LastPeriodStart: mustParseDashboardDay(t, "2026-04-01"),
		LutealPhase:     14,
	}
	today := mustParseDashboardDay(t, "2026-04-15")

	nextPeriodStart, ovulationDate, ovulationExact, ovulationImpossible := DashboardUpcomingPredictions(stats, &models.User{}, today, 28)
	if got := nextPeriodStart.Format("2006-01-02"); got != "2026-04-29" {
		t.Fatalf("expected nearest next period start 2026-04-29, got %s", got)
	}
	if got := ovulationDate.Format("2006-01-02"); got != "2026-05-12" {
		t.Fatalf("expected next upcoming ovulation date 2026-05-12, got %s", got)
	}
	if !ovulationExact {
		t.Fatalf("expected standard-cycle ovulation prediction to stay exact")
	}
	if ovulationImpossible {
		t.Fatalf("expected standard-cycle ovulation prediction to remain calculable")
	}
}

func TestDashboardNextPeriodEndUsesPredictedPeriodLength(t *testing.T) {
	stats := CycleStats{
		AveragePeriodLength: 5,
	}

	end := dashboardNextPeriodEnd(mustParseDashboardDay(t, "2026-04-29"), stats, time.UTC)
	if got := end.Format("2006-01-02"); got != "2026-05-03" {
		t.Fatalf("expected next period end 2026-05-03, got %s", got)
	}
}

func TestBuildDashboardCycleContextDisablesPredictionsForUnpredictableMode(t *testing.T) {
	user := &models.User{UnpredictableCycle: true, CycleLength: 28}
	stats := CycleStats{
		LastPeriodStart:   mustParseDashboardDay(t, "2026-03-01"),
		NextPeriodStart:   mustParseDashboardDay(t, "2026-03-29"),
		OvulationDate:     mustParseDashboardDay(t, "2026-03-15"),
		CurrentCycleDay:   13,
		MedianCycleLength: 28,
	}

	context := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-13"), time.UTC)
	if !context.PredictionDisabled {
		t.Fatalf("expected unpredictable mode to disable dashboard predictions")
	}
	if !context.DisplayNextPeriodStart.IsZero() || !context.DisplayOvulationDate.IsZero() {
		t.Fatalf("expected unpredictable mode to clear projected dates")
	}
}

func TestBuildDashboardCycleContextNeedsOvulationDataForIrregularModeWithFewerThanThreeCycles(t *testing.T) {
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		AverageCycleLength:  32,
		CompletedCycleCount: 2,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-04-02"),
		OvulationDate:       mustParseDashboardDay(t, "2026-03-19"),
	}

	context := BuildDashboardCycleContext(user, stats, mustParseDashboardDay(t, "2026-03-10"), time.UTC)
	if !context.DisplayOvulationNeedsData {
		t.Fatalf("expected irregular mode with sparse data to defer ovulation estimate")
	}
	if context.DisplayOvulationUseRange {
		t.Fatalf("did not expect ovulation range without enough completed cycles")
	}
	if !context.DisplayOvulationDate.IsZero() {
		t.Fatalf("expected sparse irregular mode to suppress single ovulation date")
	}
}

func mustParseDashboardDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
