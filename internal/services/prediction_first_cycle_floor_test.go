package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The zero-completed-cycle floor is one policy with five consumers: the
// calendar grid, the .ics feed, the webhook reminder pass, the dashboard
// reminder banner and the implantation-bleeding hint offered when a new cycle
// start is logged. Until the first cycle closes, the fertile window and the
// ovulation date are the onboarding cycle-length slider projected forward, so
// every one of them must withhold the fertility half of the projection — two of
// the five carry it off the instance. The same table drives a one-completed-
// cycle history as the positive anchor: the surfaces must resume there, or a
// guard that simply emptied every projection would read as green.

// implantationHintAgeDays ages the shared history so that the day being logged
// sits inside the closing cycle's implantation window. See
// assertImplantationHint.
const implantationHintAgeDays = 10

// firstCycleFloorLogs seeds a five-day period per start, the first day of each
// flagged as an explicit cycle start.
func firstCycleFloorLogs(starts []time.Time) []models.DailyLog {
	logs := make([]models.DailyLog, 0, len(starts)*5)
	for _, start := range starts {
		for offset := range 5 {
			logs = append(logs, models.DailyLog{
				Date:       start.AddDate(0, 0, offset),
				IsPeriod:   true,
				CycleStart: offset == 0,
			})
		}
	}
	return logs
}

// firstCycleFloorUser is a regular owner on the onboarding defaults: nothing but
// the slider stands behind a fertility claim until a cycle completes.
func firstCycleFloorUser() *models.User {
	return &models.User{
		ID:           7,
		Role:         models.RoleOwner,
		CycleLength:  28,
		PeriodLength: 5,
		LutealPhase:  14,
	}
}

// firstCycleFloorCase is one cycle history plus the completed-cycle count it is
// asserted to produce, so a fixture that stopped meaning what its name says
// fails on the count rather than silently changing what the surfaces are asked.
type firstCycleFloorCase struct {
	name                 string
	startsDaysAgo        []int
	wantCompleted        int
	wantOvulation        bool
	wantFertileDays      bool
	wantImplantationHint bool
}

func firstCycleFloorCases() []firstCycleFloorCase {
	return []firstCycleFloorCase{
		{
			name:          "zero completed cycles: only the slider stands behind the window",
			startsDaysAgo: []int{12},
			wantCompleted: 0,
		},
		{
			name:                 "one completed cycle: the window has an observed length behind it",
			startsDaysAgo:        []int{40, 12},
			wantCompleted:        1,
			wantOvulation:        true,
			wantFertileDays:      true,
			wantImplantationHint: true,
		},
	}
}

func TestFirstCycleFloorSuppressesFertilityOnEverySurface(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 4, 20, 9, 0, 0, 0, location)
	today := DateAtLocation(now, location)

	for _, testCase := range firstCycleFloorCases() {
		t.Run(testCase.name, func(t *testing.T) {
			user := firstCycleFloorUser()
			starts := make([]time.Time, 0, len(testCase.startsDaysAgo))
			for _, daysAgo := range testCase.startsDaysAgo {
				starts = append(starts, today.AddDate(0, 0, -daysAgo))
			}
			logs := firstCycleFloorLogs(starts)
			stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, now, location)

			if stats.CompletedCycleCount != testCase.wantCompleted {
				t.Fatalf("fixture: expected %d completed cycles, got %d", testCase.wantCompleted, stats.CompletedCycleCount)
			}
			if stats.OvulationDate.IsZero() {
				t.Fatalf("fixture: expected the baseline to project an ovulation date, got none")
			}

			assertCalendarFertilityMaps(t, user, logs, stats, now, location, testCase)
			assertFeedOvulationEvents(t, user, logs, now, location, testCase)
			assertWebhookOvulationReminder(t, user, logs, now, location, testCase)
			assertDashboardOvulationBanner(t, user, stats, today, location, testCase)
			assertImplantationHint(t, user, today, location, testCase)
		})
	}
}

// assertCalendarFertilityMaps reads the grid's own prediction maps. The
// predicted-period map is asserted non-empty in both histories: the floor
// withholds the fertility half of the projection, not the next-period estimate
// the dashboard header keeps showing with its qualifier.
func assertCalendarFertilityMaps(t *testing.T, user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location, testCase firstCycleFloorCase) {
	t.Helper()

	gridEnd := DateAtLocation(now, location).AddDate(0, 0, 45)
	maps := buildCalendarPredictionMaps(user, logs, stats, gridEnd, now, location)

	if len(maps.predictedPeriod) == 0 {
		t.Errorf("calendar: expected the predicted-period days to survive the fertility floor")
	}
	if got := len(maps.ovulation) > 0; got != testCase.wantOvulation {
		t.Errorf("calendar: ovulation days present = %v, want %v (%d days)", got, testCase.wantOvulation, len(maps.ovulation))
	}
	fertileDays := len(maps.fertilityPeak) + len(maps.fertilityEdge) + len(maps.preFertile)
	if got := fertileDays > 0; got != testCase.wantFertileDays {
		t.Errorf("calendar: fertility days present = %v, want %v (%d days)", got, testCase.wantFertileDays, fertileDays)
	}
}

func assertFeedOvulationEvents(t *testing.T, user *models.User, logs []models.DailyLog, now time.Time, location *time.Location, testCase firstCycleFloorCase) {
	t.Helper()

	events := calendarFeedEvents(CalendarFeedICSInput{
		User:     user,
		Logs:     logs,
		Now:      now,
		Location: location,
	})

	ovulationEvents := 0
	periodEvents := 0
	for _, event := range events {
		switch event.kind {
		case "ovulation":
			ovulationEvents++
		case "period":
			periodEvents++
		}
	}

	if periodEvents == 0 {
		t.Errorf("feed: expected the projected period events to survive the fertility floor")
	}
	if got := ovulationEvents > 0; got != testCase.wantOvulation {
		t.Errorf("feed: ovulation VEVENTs present = %v, want %v (%d events)", got, testCase.wantOvulation, ovulationEvents)
	}
}

func assertWebhookOvulationReminder(t *testing.T, user *models.User, logs []models.DailyLog, now time.Time, location *time.Location, testCase firstCycleFloorCase) {
	t.Helper()

	reminders := DecideDueReminders(user, enabledWebhookSettings(3), logs, now, location)
	_, hasOvulation := findDueReminder(reminders, DueReminderTypeOvulation)
	if hasOvulation != testCase.wantOvulation {
		t.Errorf("webhook: ovulation reminder due = %v, want %v", hasOvulation, testCase.wantOvulation)
	}
}

// assertDashboardOvulationBanner also pins the banner to the SHARED predicate,
// not merely to the tier: the banner reads a decision the cycle context
// resolved, so the case asserts that the carried decision is the predicate's
// own answer. Without that link the banner row would keep passing if the floor
// were dropped from FertilityProjectionSuppressed — the banner would go on
// reading AwaitingFirstCycle while the calendar, the feed and the webhook moved.
func assertDashboardOvulationBanner(t *testing.T, user *models.User, stats CycleStats, today time.Time, location *time.Location, testCase firstCycleFloorCase) {
	t.Helper()

	cycleContext := BuildDashboardCycleContext(user, nil, stats, today, location)
	if got, want := cycleContext.FertilitySuppressed, FertilityProjectionSuppressed(user, stats); got != want {
		t.Errorf("banner: cycle context carries FertilitySuppressed = %v, want the shared predicate's %v", got, want)
	}
	if got, want := cycleContext.FertilitySuppressed, !testCase.wantOvulation; got != want {
		t.Errorf("banner: cycle context carries FertilitySuppressed = %v, want %v", got, want)
	}

	banner := BuildDashboardReminderBanner(cycleContext, today, 3)
	isOvulationBanner := banner.Show && banner.Kind == DashboardReminderBannerKindOvulation
	if isOvulationBanner != testCase.wantOvulation {
		t.Errorf("banner: ovulation banner shown = %v, want %v (kind %q, show %v)", isOvulationBanner, testCase.wantOvulation, banner.Kind, banner.Show)
	}
}

// assertImplantationHint is the fifth consumer. Logging a cycle start a week or
// so after the closing cycle's PROJECTED ovulation offers the
// implantation-bleeding caution, and the caution names the gap counted from
// that projection. With no completed cycle behind it the projection is the
// onboarding cycle-length slider rolled forward, so the caution presents an
// inferred number as a grounded one and the floor withholds it.
//
// The hint is read on the day the bleed is logged rather than on today's
// dashboard, so this row ages the case's history by implantationHintAgeDays.
// The gaps BETWEEN the starts are untouched, so the completed-cycle count and
// the observed median are the ones the case names; what moves is where today
// falls, and after the shift it is cycle day 23 — nine days past the projected
// ovulation of a 28-day cycle, inside the 6..12-day window the policy looks
// for. Both rows project the same ovulation date (the default length and the
// observed median are both 28 here), so the only thing that differs between
// them is the completed-cycle count, and the positive row shows the window is
// reachable at all.
func assertImplantationHint(t *testing.T, user *models.User, today time.Time, location *time.Location, testCase firstCycleFloorCase) {
	t.Helper()

	starts := make([]time.Time, 0, len(testCase.startsDaysAgo))
	for _, daysAgo := range testCase.startsDaysAgo {
		starts = append(starts, today.AddDate(0, 0, -(daysAgo + implantationHintAgeDays)))
	}
	logs := firstCycleFloorLogs(starts)

	policy := ResolveManualCycleStartPolicy(user, logs, today, today, location)
	if got := policy.PotentialImplantation; got != testCase.wantImplantationHint {
		t.Errorf("cycle start: implantation hint offered = %v, want %v (gap %d days past the projected ovulation)",
			got, testCase.wantImplantationHint, policy.ImplantationGapDays)
	}
}
