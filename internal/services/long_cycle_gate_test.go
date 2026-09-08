package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The long-cycle safety gate, from the history that used to walk straight
// through it.
//
// One missed period log merges two real cycles into a single enormous span, and
// that span lands in the same recent-cycle window every statistic is computed
// over. The MEAN absorbs it — three 28-day cycles beside one 300-day gap average
// 96, four of them average 82 — while the median stays 28. The overdue gate was
// resolved against the average-first reference length, so it asked "is cycle day
// 61 past 103?", answered no, and every surface kept publishing dates produced
// by rolling the 28-day median forward: a projection 33 days past the length that
// produced it, presented as an estimate. That is the estimate-presented-as-fact
// the medical-safety invariant forbids (docs/SECURITY_INVARIANTS.md -> medical
// safety), and the account that hits it is the one asking the most anxious
// question in the product.
//
// The two histories are the ones named in the release plan, with their arithmetic
// asserted rather than assumed: a scenario that stopped producing mean 96 beside
// median 28 would no longer be about this defect at all.

// longCycleGateUser is an ordinary owner with the model defaults, so nothing in
// these cases turns on a setting.
func longCycleGateUser() *models.User {
	return &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
}

// longCycleGateLogs writes one explicit cycle start per offset, counted from
// base. The starts are what DetectCycleStarts reads, so the spans between them
// are the observed cycle lengths.
func longCycleGateLogs(base time.Time, offsets []int) []models.DailyLog {
	logs := make([]models.DailyLog, 0, len(offsets))
	for _, offset := range offsets {
		logs = append(logs, models.DailyLog{
			Date:       base.AddDate(0, 0, offset),
			IsPeriod:   true,
			CycleStart: true,
		})
	}
	return logs
}

type longCycleGateScenario struct {
	name string
	// startOffsets are the explicit cycle starts; the last gap is the merged one.
	startOffsets []int
	// wantRoundedAverage is the inflated reference the mean produces here.
	wantRoundedAverage int
	wantCompleted      int
}

var longCycleGateScenarios = []longCycleGateScenario{
	{
		name:               "three 28-day cycles and a 300-day gap average 96",
		startOffsets:       []int{0, 28, 56, 84, 384},
		wantRoundedAverage: 96,
		wantCompleted:      4,
	},
	{
		name:               "four 28-day cycles and a 300-day gap average 82",
		startOffsets:       []int{0, 28, 56, 84, 112, 412},
		wantRoundedAverage: 82,
		wantCompleted:      5,
	},
}

// longCycleGateSetup returns the stats every surface below is built from, on the
// path the owner surfaces actually take (ApplyUserCycleBaseline over
// BuildCycleStatsFromLogs), plus the day the account is on.
func longCycleGateSetup(t *testing.T, scenario longCycleGateScenario) (*models.User, []models.DailyLog, CycleStats, time.Time) {
	t.Helper()

	loc := time.UTC
	base := time.Date(2025, time.January, 1, 0, 0, 0, 0, loc)
	logs := longCycleGateLogs(base, scenario.startOffsets)
	lastStart := base.AddDate(0, 0, scenario.startOffsets[len(scenario.startOffsets)-1])
	today := lastStart.AddDate(0, 0, 60)

	user := longCycleGateUser()
	stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, today, loc), today, loc)

	if stats.CurrentCycleDay != 61 {
		t.Fatalf("scenario setup: CurrentCycleDay = %d, want 61", stats.CurrentCycleDay)
	}
	if stats.CompletedCycleCount != scenario.wantCompleted {
		t.Fatalf("scenario setup: CompletedCycleCount = %d, want %d", stats.CompletedCycleCount, scenario.wantCompleted)
	}
	if stats.MedianCycleLength != 28 {
		t.Fatalf("scenario setup: MedianCycleLength = %d, want 28", stats.MedianCycleLength)
	}
	if rounded := int(stats.AverageCycleLength + 0.5); rounded != scenario.wantRoundedAverage {
		t.Fatalf("scenario setup: rounded AverageCycleLength = %d, want %d", rounded, scenario.wantRoundedAverage)
	}
	// The bypass itself, pinned as a precondition: the inflated mean lifts the
	// old threshold ABOVE cycle day 61, which is how the projection survived. A
	// scenario where the mean no longer hides the day would pass the assertions
	// below without ever exercising the defect.
	if reference := DashboardCycleReferenceLength(user, stats); stats.CurrentCycleDay > reference+7 {
		t.Fatalf("scenario setup: cycle day %d is already past the average-first reference %d + 7, so this history never bypassed the gate",
			stats.CurrentCycleDay, reference)
	}

	return user, logs, stats, today
}

// TestLongCycleGateSuppressesEverySurfaceWhenAMergedCycleInflatesTheAverage is
// the cross-surface regression: dashboard, calendar grid, published stats (the
// one helper /stats, the dashboard and the JSON API all read), the .ics feed and
// the webhook pass must all withhold.
func TestLongCycleGateSuppressesEverySurfaceWhenAMergedCycleInflatesTheAverage(t *testing.T) {
	loc := time.UTC

	for _, scenario := range longCycleGateScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			user, logs, stats, today := longCycleGateSetup(t, scenario)

			if !DashboardCycleOverdue(user, stats) {
				t.Fatalf("cycle day %d against median %d is overdue whatever the mean says: DashboardCycleOverdue = false",
					stats.CurrentCycleDay, stats.MedianCycleLength)
			}
			if !PredictionsSuppressed(user, stats) {
				t.Fatal("PredictionsSuppressed = false, so every surface below is free to publish a projected date")
			}

			// 1. Dashboard hero and header.
			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, loc)
			if !cycleContext.DisplayNextPeriodStart.IsZero() || !cycleContext.DisplayNextPeriodEnd.IsZero() {
				t.Fatalf("dashboard published a next-period window: %s..%s",
					cycleContext.DisplayNextPeriodStart.Format("2006-01-02"), cycleContext.DisplayNextPeriodEnd.Format("2006-01-02"))
			}
			if !cycleContext.DisplayOvulationDate.IsZero() {
				t.Fatalf("dashboard published an ovulation date: %s", cycleContext.DisplayOvulationDate.Format("2006-01-02"))
			}
			if !cycleContext.NextPeriodEstimatePaused {
				t.Fatal("the dashboard cleared the window without saying the estimate is paused, which is a blank slot with no explanation")
			}
			// The notice is what stands where the date was: a surface that
			// withholds silently tells the owner nothing at all.
			//
			// WHICH notice is a separate, open question, and the key is not pinned
			// here on purpose. For this fixture BuildLateCycleNotice compares cycle
			// day 61 against stats.MaxCycleLength — 300, the merged span itself —
			// and lands on LateCycleWithinRangeKey: "still inside your recorded
			// range of 28 to 300 days", beside a withheld date. Answering it means
			// deciding when a recorded span stops counting as a cycle, which is the
			// threshold call this change deliberately does not make; pinning the
			// current copy here would freeze the reassurance as intended.
			if !cycleContext.LateCycle.Visible {
				t.Fatal("no late-cycle notice beside the withheld window")
			}

			// 2. The hero ribbon, which asks its own question rather than reading
			// the gate: an inflated reference kept cycle day 61 inside a 96-day
			// axis, so the ribbon drew a projected cycle map — projected days and
			// a projected ovulation day among them — beside a header saying the
			// estimate is paused.
			hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: loc})
			if hero.Visible || hero.CycleLength > 0 || len(hero.Days) > 0 {
				t.Fatalf("the hero drew a %d-day projected cycle map for a suppressed account: visible=%v, %d day cells",
					hero.CycleLength, hero.Visible, len(hero.Days))
			}

			// 3. The published copy every page and the JSON API read.
			published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, loc)
			if !suppression.PredictionsSuppressed {
				t.Fatal("PublishedOverviewStats reported no suppression")
			}
			if !published.NextPeriodStart.IsZero() {
				t.Fatalf("published NextPeriodStart = %s", published.NextPeriodStart.Format("2006-01-02"))
			}
			if !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
				t.Fatalf("published fertility projection survived: ovulation %s, window %s..%s",
					published.OvulationDate.Format("2006-01-02"),
					published.FertilityWindowStart.Format("2006-01-02"),
					published.FertilityWindowEnd.Format("2006-01-02"))
			}

			// 4. Calendar grid — the month the un-rolled projection falls in and
			// the month the owner is actually looking at.
			for _, month := range []time.Time{
				firstOfMonth(stats.NextPeriodStart, loc),
				firstOfMonth(today, loc),
			} {
				if month.IsZero() {
					continue
				}
				for _, day := range BuildCalendarDayStates(user, month, logs, stats, today, loc) {
					if day.IsPredicted || day.IsPredictedStartWindow || day.IsPreFertile || day.IsFertility || day.IsOvulation || day.IsTentativeOvulation {
						t.Fatalf("calendar grid painted a prediction on %s: %#v", day.Date.Format("2006-01-02"), day)
					}
				}
			}

			// 5. The .ics feed.
			events := calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: today, Location: loc})
			if len(events) > 0 {
				t.Fatalf("the .ics feed announced %d event(s) for a suppressed account: %#v", len(events), events)
			}

			// 6. The webhook reminder pass.
			if reminders := DecideDueReminders(user, enabledWebhookSettings(14), logs, today, loc); len(reminders) > 0 {
				t.Fatalf("the webhook pass queued %d reminder(s) for a suppressed account: %#v", len(reminders), reminders)
			}
		})
	}
}

// TestLongCycleGateMeasuresEveryHistoryAgainstItsOwnProjection is the control
// table, and it deliberately includes the histories the gate's answer CHANGED
// for, not only the ones it left alone. An earlier draft asserted only on
// histories where the mean equals the median — where the change is a no-op by
// construction — and the whole affected cohort was untestable in it.
//
// The two boundaries the gate must respect:
//   - it may not fire while the account is inside the length its own projection
//     was computed from, plus the week of grace — including the left-skewed
//     history, whose median EXCEEDS its mean and whose projected date would
//     otherwise be withheld four days before it fell due;
//   - it must fire once past that, for a right-skewed history too, where the mean
//     used to buy days of grace the projection had already spent.
func TestLongCycleGateMeasuresEveryHistoryAgainstItsOwnProjection(t *testing.T) {
	loc := time.UTC
	base := time.Date(2025, time.January, 1, 0, 0, 0, 0, loc)

	cases := []struct {
		name         string
		startOffsets []int
		cycleDay     int
		// wantProjectionLength is the median the account's dates are rolled
		// forward from, written out so a scenario that stopped being about this
		// arithmetic reds here rather than passing quietly.
		wantProjectionLength int
		wantOverdue          bool
	}{
		{"four 28-day cycles, cycle day 30", []int{0, 28, 56, 84, 112}, 30, 28, false},
		{"four 28-day cycles, cycle day 36", []int{0, 28, 56, 84, 112}, 36, 28, true},
		{"three real 50-day cycles, cycle day 55", []int{0, 50, 100, 150}, 55, 50, false},
		{"three real 50-day cycles, cycle day 58", []int{0, 50, 100, 150}, 58, 50, true},
		// 25/28/28/45 — no merged span, mean 32 against median 28. The mean used
		// to grant grace to cycle day 39; the projection it publishes ran out on
		// day 35. Both sides of that boundary are pinned.
		{"a variable history inside its projection, cycle day 34", []int{0, 25, 53, 81, 126}, 34, 28, false},
		{"a variable history past its projection, cycle day 36", []int{0, 25, 53, 81, 126}, 36, 28, true},
		// 28/60/60 — median 60 above mean 49. The projected next period is day 61,
		// so a gate resolved against the smaller statistic would have suppressed
		// it on day 57, before the date was due.
		{"a history whose median exceeds its mean, cycle day 60", []int{0, 28, 88, 148}, 60, 60, false},
		{"a history whose median exceeds its mean, cycle day 68", []int{0, 28, 88, 148}, 68, 60, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := longCycleGateLogs(base, tc.startOffsets)
			lastStart := base.AddDate(0, 0, tc.startOffsets[len(tc.startOffsets)-1])
			today := lastStart.AddDate(0, 0, tc.cycleDay-1)

			user := longCycleGateUser()
			stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, today, loc), today, loc)
			if stats.CurrentCycleDay != tc.cycleDay {
				t.Fatalf("scenario setup: CurrentCycleDay = %d, want %d", stats.CurrentCycleDay, tc.cycleDay)
			}
			if got := DashboardProjectionCycleLength(user, stats); got != tc.wantProjectionLength {
				t.Fatalf("scenario setup: projection length = %d, want %d (median %d, mean %.1f)",
					got, tc.wantProjectionLength, stats.MedianCycleLength, stats.AverageCycleLength)
			}

			if got := DashboardCycleOverdue(user, stats); got != tc.wantOverdue {
				t.Fatalf("DashboardCycleOverdue = %v, want %v (cycle day %d, median %d, mean %.1f)",
					got, tc.wantOverdue, stats.CurrentCycleDay, stats.MedianCycleLength, stats.AverageCycleLength)
			}

			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, loc)
			if tc.wantOverdue {
				if !cycleContext.DisplayNextPeriodStart.IsZero() {
					t.Fatalf("an overdue cycle still published %s", cycleContext.DisplayNextPeriodStart.Format("2006-01-02"))
				}
				return
			}
			if cycleContext.DisplayNextPeriodStart.IsZero() {
				t.Fatal("a cycle inside its own length lost its next-period estimate — the gate is over-suppressing")
			}
		})
	}
}

// firstOfMonth is the month a date falls in, or the zero time for a zero date.
func firstOfMonth(day time.Time, loc *time.Location) time.Time {
	if day.IsZero() {
		return time.Time{}
	}
	return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, loc)
}
