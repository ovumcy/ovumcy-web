package services

import (
	"testing"
	"time"
)

// TestDashboardReminderBannerDaysUntilDSTSpringForward guards the day count
// against a DST transition between today and the predicted date. In
// Europe/Berlin the 2026-03-29 spring-forward falls between 2026-03-28 and
// 2026-03-30; as location-midnight values those are 2*24-1h apart, so the old
// int(predictedDate.Sub(today).Hours()/24) truncated to 1. CalendarDaysBetween
// re-anchors both to UTC-midnight and reports the true 2 days.
func TestDashboardReminderBannerDaysUntilDSTSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	today := time.Date(2026, time.March, 28, 0, 0, 0, 0, loc)
	predicted := time.Date(2026, time.March, 30, 0, 0, 0, 0, loc)

	daysUntil, ok := dashboardReminderBannerDaysUntil(predicted, today, DashboardReminderBannerWindowDays)
	if !ok {
		t.Fatal("expected the predicted date to fall within the reminder window")
	}
	if daysUntil != 2 {
		t.Fatalf("expected daysUntil=2 across the Berlin DST transition, got %d", daysUntil)
	}
}

func TestBuildDashboardReminderBannerPeriodThresholdBoundaries(t *testing.T) {
	today := mustParseDashboardDay(t, "2026-03-10")

	cases := []struct {
		name            string
		nextPeriodStart string
		wantShow        bool
		wantDaysUntil   int
		wantTitleKey    string
		wantCountable   bool
	}{
		{
			name:            "due today uses the dedicated today copy",
			nextPeriodStart: "2026-03-10",
			wantShow:        true,
			wantDaysUntil:   0,
			wantTitleKey:    DashboardReminderBannerPeriodTodayKey,
			wantCountable:   false,
		},
		{
			name:            "due tomorrow uses the dedicated tomorrow copy",
			nextPeriodStart: "2026-03-11",
			wantShow:        true,
			wantDaysUntil:   1,
			wantTitleKey:    DashboardReminderBannerPeriodTomorrowKey,
			wantCountable:   false,
		},
		{
			name:            "two days away uses the ~N days plural copy",
			nextPeriodStart: "2026-03-12",
			wantShow:        true,
			wantDaysUntil:   2,
			wantTitleKey:    DashboardReminderBannerPeriodKey,
			wantCountable:   true,
		},
		{
			name:            "exactly at the threshold uses the ~N days plural copy",
			nextPeriodStart: "2026-03-13",
			wantShow:        true,
			wantDaysUntil:   DashboardReminderBannerWindowDays,
			wantTitleKey:    DashboardReminderBannerPeriodKey,
			wantCountable:   true,
		},
		{
			name:            "one day beyond the threshold hides the banner",
			nextPeriodStart: "2026-03-14",
			wantShow:        false,
		},
		{
			name:            "far in the future hides the banner",
			nextPeriodStart: "2026-04-10",
			wantShow:        false,
		},
		{
			name:            "yesterday (already past) hides the banner",
			nextPeriodStart: "2026-03-09",
			wantShow:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cycleContext := DashboardCycleContext{
				DisplayNextPeriodStart: mustParseDashboardDay(t, tc.nextPeriodStart),
			}

			// leadDays=0 drives these cases through the fallback path
			// (dashboardReminderBannerWindowDays falls back to
			// DashboardReminderBannerWindowDays), matching the threshold
			// these cases were written against before reminder_lead_days
			// existed. TestBuildDashboardReminderBannerUsesOwnerReminderLeadDays
			// below covers non-zero leadDays values explicitly.
			banner := BuildDashboardReminderBanner(cycleContext, today, 0)
			if banner.Show != tc.wantShow {
				t.Fatalf("expected Show=%v, got Show=%v (banner=%#v)", tc.wantShow, banner.Show, banner)
			}
			if !tc.wantShow {
				return
			}
			if banner.Kind != DashboardReminderBannerKindPeriod {
				t.Fatalf("expected period banner kind, got %q", banner.Kind)
			}
			if banner.TitleKey != tc.wantTitleKey {
				t.Fatalf("expected title key %q, got %q", tc.wantTitleKey, banner.TitleKey)
			}
			if banner.Countable != tc.wantCountable {
				t.Fatalf("expected Countable=%v, got %v", tc.wantCountable, banner.Countable)
			}
			if banner.DaysUntil != tc.wantDaysUntil {
				t.Fatalf("expected DaysUntil=%d, got %d", tc.wantDaysUntil, banner.DaysUntil)
			}
		})
	}
}

// TestBuildDashboardReminderBannerUsesOwnerReminderLeadDays covers the switch
// from the fixed DashboardReminderBannerWindowDays constant to the per-owner
// reminder_lead_days column (issue #124 slice 1, wired into the banner in
// this change): the effective threshold must track leadDays for every
// in-range value (1, 3, 7, 14), fall back to the constant for 0-or-unset,
// and clamp an out-of-range value down to the same [0, 14] bound
// NormalizeReminderLeadDays enforces at save — mirroring, not re-deriving,
// that bound so the banner can never operate outside it even if the column
// is ever populated by a path other than SaveWebhookSettings.
func TestBuildDashboardReminderBannerUsesOwnerReminderLeadDays(t *testing.T) {
	today := mustParseDashboardDay(t, "2026-03-10")

	cases := []struct {
		name            string
		leadDays        int
		nextPeriodStart string
		wantShow        bool
		wantDaysUntil   int
	}{
		{
			name:            "leadDays=1: event exactly on the lead boundary shows",
			leadDays:        1,
			nextPeriodStart: "2026-03-11", // 1 day out
			wantShow:        true,
			wantDaysUntil:   1,
		},
		{
			name:            "leadDays=1: one day beyond the lead boundary hides",
			leadDays:        1,
			nextPeriodStart: "2026-03-12", // 2 days out
			wantShow:        false,
		},
		{
			name:            "leadDays=3: matches the legacy constant boundary",
			leadDays:        3,
			nextPeriodStart: "2026-03-13", // 3 days out
			wantShow:        true,
			wantDaysUntil:   3,
		},
		{
			name:            "leadDays=3: one day beyond hides, same as the legacy constant",
			leadDays:        3,
			nextPeriodStart: "2026-03-14", // 4 days out
			wantShow:        false,
		},
		{
			name:            "leadDays=7: event exactly on the lead boundary shows",
			leadDays:        7,
			nextPeriodStart: "2026-03-17", // 7 days out
			wantShow:        true,
			wantDaysUntil:   7,
		},
		{
			name: "leadDays=7: a 6-days-out event shows, unlike the legacy " +
				"constant-3 threshold which would have hidden it",
			leadDays:        7,
			nextPeriodStart: "2026-03-16", // 6 days out
			wantShow:        true,
			wantDaysUntil:   6,
		},
		{
			name:            "leadDays=7: one day beyond the lead boundary hides",
			leadDays:        7,
			nextPeriodStart: "2026-03-18", // 8 days out
			wantShow:        false,
		},
		{
			name:            "leadDays=14 (the maximum save-time bound): event on the boundary shows",
			leadDays:        14,
			nextPeriodStart: "2026-03-24", // 14 days out
			wantShow:        true,
			wantDaysUntil:   14,
		},
		{
			name:            "leadDays=14: one day beyond the maximum bound hides",
			leadDays:        14,
			nextPeriodStart: "2026-03-25", // 15 days out
			wantShow:        false,
		},
		{
			name:            "leadDays=0 (unset/never touched the setting) falls back to the constant",
			leadDays:        0,
			nextPeriodStart: "2026-03-13", // 3 days out: inside the constant's window
			wantShow:        true,
			wantDaysUntil:   DashboardReminderBannerWindowDays,
		},
		{
			name:            "leadDays=0 falls back to the constant, still hides beyond it",
			leadDays:        0,
			nextPeriodStart: "2026-03-14", // 4 days out: outside the constant's window
			wantShow:        false,
		},
		{
			name:            "negative leadDays (defensive) falls back to the constant",
			leadDays:        -5,
			nextPeriodStart: "2026-03-13", // 3 days out
			wantShow:        true,
			wantDaysUntil:   DashboardReminderBannerWindowDays,
		},
		{
			name:            "out-of-range leadDays is clamped to the 14-day save-time maximum, not rejected outright",
			leadDays:        30,
			nextPeriodStart: "2026-03-24", // 14 days out: inside the clamped window
			wantShow:        true,
			wantDaysUntil:   14,
		},
		{
			name:            "out-of-range leadDays clamps to 14, still hides beyond the clamp",
			leadDays:        30,
			nextPeriodStart: "2026-03-25", // 15 days out: outside the clamped window
			wantShow:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cycleContext := DashboardCycleContext{
				DisplayNextPeriodStart: mustParseDashboardDay(t, tc.nextPeriodStart),
			}

			banner := BuildDashboardReminderBanner(cycleContext, today, tc.leadDays)
			if banner.Show != tc.wantShow {
				t.Fatalf("leadDays=%d: expected Show=%v, got Show=%v (banner=%#v)", tc.leadDays, tc.wantShow, banner.Show, banner)
			}
			if !tc.wantShow {
				return
			}
			if banner.Kind != DashboardReminderBannerKindPeriod {
				t.Fatalf("expected period banner kind, got %q", banner.Kind)
			}
			if banner.DaysUntil != tc.wantDaysUntil {
				t.Fatalf("leadDays=%d: expected DaysUntil=%d, got %d", tc.leadDays, tc.wantDaysUntil, banner.DaysUntil)
			}

			// Countable is the banner's own estimate-qualifier signal (see
			// the DashboardReminderBanner doc comment: it tells the caller
			// whether TitleKey is the "~N days" plural copy that needs the
			// day count interpolated). It must keep tracking daysUntil the
			// same way regardless of which leadDays value produced the
			// window, so pin it here across the full lead-day sweep rather
			// than only in the legacy constant-threshold test above.
			wantCountable := tc.wantDaysUntil >= 2
			if banner.Countable != wantCountable {
				t.Fatalf("leadDays=%d, daysUntil=%d: expected Countable=%v, got %v", tc.leadDays, tc.wantDaysUntil, wantCountable, banner.Countable)
			}
		})
	}
}

func TestBuildDashboardReminderBannerNotYetCalculableAndInsufficientDataCases(t *testing.T) {
	today := mustParseDashboardDay(t, "2026-03-10")
	withinWindow := mustParseDashboardDay(t, "2026-03-12")

	cases := []struct {
		name         string
		cycleContext DashboardCycleContext
	}{
		{
			name:         "zero next period date is not yet calculable",
			cycleContext: DashboardCycleContext{DisplayNextPeriodStart: time.Time{}},
		},
		{
			name: "next period needs more cycles of data",
			cycleContext: DashboardCycleContext{
				DisplayNextPeriodStart:     withinWindow,
				DisplayNextPeriodNeedsData: true,
			},
		},
		{
			name: "next period is still awaiting the first cycle start",
			cycleContext: DashboardCycleContext{
				DisplayNextPeriodStart:  withinWindow,
				DisplayNextPeriodPrompt: true,
			},
		},
		{
			name: "next period is shown as an uncertainty range, not a single date",
			cycleContext: DashboardCycleContext{
				DisplayNextPeriodStart:    withinWindow,
				DisplayNextPeriodUseRange: true,
			},
		},
		{
			name: "predictions are disabled for unpredictable-cycle mode",
			cycleContext: DashboardCycleContext{
				DisplayNextPeriodStart: withinWindow,
				PredictionDisabled:     true,
			},
		},
		{
			name: "predictions are paused for pregnancy",
			cycleContext: DashboardCycleContext{
				DisplayNextPeriodStart: withinWindow,
				PregnancyPaused:        true,
				PredictionDisabled:     true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			banner := BuildDashboardReminderBanner(tc.cycleContext, today, 0)
			if banner.Show {
				t.Fatalf("expected no banner, got %#v", banner)
			}
		})
	}
}

func TestBuildDashboardReminderBannerOvulationThresholdBoundaries(t *testing.T) {
	today := mustParseDashboardDay(t, "2026-03-10")

	cases := []struct {
		name          string
		ovulationDate string
		exact         bool
		wantShow      bool
		wantDaysUntil int
		wantTitleKey  string
		wantCountable bool
		wantApprox    bool
	}{
		{
			name:          "ovulation today uses the dedicated today copy",
			ovulationDate: "2026-03-10",
			exact:         true,
			wantShow:      true,
			wantDaysUntil: 0,
			wantTitleKey:  DashboardReminderBannerOvulationTodayKey,
			wantCountable: false,
		},
		{
			name:          "exact ovulation tomorrow uses the dedicated tomorrow copy",
			ovulationDate: "2026-03-11",
			exact:         true,
			wantShow:      true,
			wantDaysUntil: 1,
			wantTitleKey:  DashboardReminderBannerOvulationTomorrowKey,
			wantCountable: false,
			wantApprox:    false,
		},
		{
			name:          "approximate ovulation tomorrow is flagged approximate",
			ovulationDate: "2026-03-11",
			exact:         false,
			wantShow:      true,
			wantDaysUntil: 1,
			wantTitleKey:  DashboardReminderBannerOvulationTomorrowKey,
			wantCountable: false,
			wantApprox:    true,
		},
		{
			name:          "two days away uses the ~N days plural copy",
			ovulationDate: "2026-03-12",
			exact:         true,
			wantShow:      true,
			wantDaysUntil: 2,
			wantTitleKey:  DashboardReminderBannerOvulationKey,
			wantCountable: true,
		},
		{
			name:          "exactly at the threshold uses the ~N days plural copy",
			ovulationDate: "2026-03-13",
			exact:         true,
			wantShow:      true,
			wantDaysUntil: DashboardReminderBannerWindowDays,
			wantTitleKey:  DashboardReminderBannerOvulationKey,
			wantCountable: true,
		},
		{
			name:          "beyond the threshold hides the banner",
			ovulationDate: "2026-03-14",
			exact:         true,
			wantShow:      false,
		},
		{
			name:          "already past hides the banner",
			ovulationDate: "2026-03-01",
			exact:         true,
			wantShow:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cycleContext := DashboardCycleContext{
				// No next-period estimate available, so the ovulation branch
				// is exercised in isolation.
				DisplayOvulationDate:  mustParseDashboardDay(t, tc.ovulationDate),
				DisplayOvulationExact: tc.exact,
			}

			banner := BuildDashboardReminderBanner(cycleContext, today, 0)
			if banner.Show != tc.wantShow {
				t.Fatalf("expected Show=%v, got Show=%v (banner=%#v)", tc.wantShow, banner.Show, banner)
			}
			if !tc.wantShow {
				return
			}
			if banner.Kind != DashboardReminderBannerKindOvulation {
				t.Fatalf("expected ovulation banner kind, got %q", banner.Kind)
			}
			if banner.TitleKey != tc.wantTitleKey {
				t.Fatalf("expected title key %q, got %q", tc.wantTitleKey, banner.TitleKey)
			}
			if banner.Countable != tc.wantCountable {
				t.Fatalf("expected Countable=%v, got %v", tc.wantCountable, banner.Countable)
			}
			if banner.DaysUntil != tc.wantDaysUntil {
				t.Fatalf("expected DaysUntil=%d, got %d", tc.wantDaysUntil, banner.DaysUntil)
			}
			if banner.Approximate != tc.wantApprox {
				t.Fatalf("expected Approximate=%v, got %v", tc.wantApprox, banner.Approximate)
			}
		})
	}
}

func TestBuildDashboardReminderBannerOvulationSuppressedCases(t *testing.T) {
	today := mustParseDashboardDay(t, "2026-03-10")
	withinWindow := mustParseDashboardDay(t, "2026-03-12")

	cases := []struct {
		name         string
		cycleContext DashboardCycleContext
	}{
		{
			name:         "zero ovulation date is not yet calculable",
			cycleContext: DashboardCycleContext{DisplayOvulationDate: time.Time{}},
		},
		{
			name: "ovulation is flagged impossible for this cycle",
			cycleContext: DashboardCycleContext{
				DisplayOvulationDate:       withinWindow,
				DisplayOvulationImpossible: true,
			},
		},
		{
			name: "ovulation needs more cycles of data",
			cycleContext: DashboardCycleContext{
				DisplayOvulationDate:      withinWindow,
				DisplayOvulationNeedsData: true,
			},
		},
		{
			name: "ovulation is shown as an uncertainty range, not a single date",
			cycleContext: DashboardCycleContext{
				DisplayOvulationDate:     withinWindow,
				DisplayOvulationUseRange: true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			banner := BuildDashboardReminderBanner(tc.cycleContext, today, 0)
			if banner.Show {
				t.Fatalf("expected no banner, got %#v", banner)
			}
		})
	}
}

// TestBuildDashboardReminderBannerPeriodTakesPriorityOverOvulation locks in
// that when both the next period and ovulation predictions fall inside the
// reminder window on the same request, the period reminder wins — it is the
// more actionable of the two for the owner, and showing both would clutter
// the dashboard with two banners for one underlying cycle prediction. The
// period's own day count drives which copy variant is chosen, independent of
// how far away the (suppressed) ovulation date is.
func TestBuildDashboardReminderBannerPeriodTakesPriorityOverOvulation(t *testing.T) {
	today := mustParseDashboardDay(t, "2026-03-10")

	cases := []struct {
		name            string
		nextPeriodStart string
		ovulationDate   string
		wantTitleKey    string
		wantCountable   bool
		wantDaysUntil   int
	}{
		{
			name:            "period two days out wins with the plural copy over a nearer ovulation",
			nextPeriodStart: "2026-03-12",
			ovulationDate:   "2026-03-11",
			wantTitleKey:    DashboardReminderBannerPeriodKey,
			wantCountable:   true,
			wantDaysUntil:   2,
		},
		{
			name:            "period today wins with the today copy over a later ovulation",
			nextPeriodStart: "2026-03-10",
			ovulationDate:   "2026-03-12",
			wantTitleKey:    DashboardReminderBannerPeriodTodayKey,
			wantCountable:   false,
			wantDaysUntil:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cycleContext := DashboardCycleContext{
				DisplayNextPeriodStart: mustParseDashboardDay(t, tc.nextPeriodStart),
				DisplayOvulationDate:   mustParseDashboardDay(t, tc.ovulationDate),
				DisplayOvulationExact:  true,
			}

			banner := BuildDashboardReminderBanner(cycleContext, today, 0)
			if !banner.Show {
				t.Fatalf("expected a banner to show")
			}
			if banner.Kind != DashboardReminderBannerKindPeriod {
				t.Fatalf("expected period banner to take priority, got kind %q", banner.Kind)
			}
			if banner.TitleKey != tc.wantTitleKey {
				t.Fatalf("expected title key %q, got %q", tc.wantTitleKey, banner.TitleKey)
			}
			if banner.Countable != tc.wantCountable {
				t.Fatalf("expected Countable=%v, got %v", tc.wantCountable, banner.Countable)
			}
			if banner.DaysUntil != tc.wantDaysUntil {
				t.Fatalf("expected DaysUntil=%d, got %d", tc.wantDaysUntil, banner.DaysUntil)
			}
		})
	}
}
