package services

import (
	"testing"
	"time"
)

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

			banner := BuildDashboardReminderBanner(cycleContext, today)
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
			banner := BuildDashboardReminderBanner(tc.cycleContext, today)
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

			banner := BuildDashboardReminderBanner(cycleContext, today)
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
			banner := BuildDashboardReminderBanner(tc.cycleContext, today)
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

			banner := BuildDashboardReminderBanner(cycleContext, today)
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
