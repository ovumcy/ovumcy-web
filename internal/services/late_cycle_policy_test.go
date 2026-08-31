package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// lateCycleStats assembles the inputs the late-cycle matrix actually reads: how
// far into the cycle today is, how many completed cycles the account has, and
// the observed range those cycles produced. LastPeriodStart is derived from the
// cycle day so BuildDashboardCycleContext computes the same trigger the
// dashboard computes in production.
func lateCycleStats(today time.Time, cycleDay int, completedCycles int, averageLength float64, minLength int, maxLength int) CycleStats {
	stats := CycleStats{
		CurrentCycleDay:     cycleDay,
		CompletedCycleCount: completedCycles,
		AverageCycleLength:  averageLength,
		MinCycleLength:      minLength,
		MaxCycleLength:      maxLength,
	}
	if cycleDay > 0 {
		stats.LastPeriodStart = today.AddDate(0, 0, -(cycleDay - 1))
	}
	return stats
}

// TestLateCycleNoticeStateMatrix walks the late-cycle states deliberately. The
// contract under test is that the app never states a comparison it cannot
// measure: with fewer completed cycles than the prediction-reliability card
// needs, the copy falls back to the cycle day and claims no "usual range".
func TestLateCycleNoticeStateMatrix(t *testing.T) {
	today := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)

	testCases := []struct {
		name            string
		user            *models.User
		stats           CycleStats
		expectVisible   bool
		expectKey       string
		expectTone      string
		expectForm      string
		expectDays      int
		expectRangeLow  int
		expectRangeHigh int
	}{
		{
			name:          "last day of the expected range stays silent",
			user:          &models.User{Role: models.RoleOwner, CycleLength: 28},
			stats:         lateCycleStats(today, 28, 4, 28, 27, 30),
			expectVisible: false,
		},
		{
			name:          "first day past the expected range stays silent",
			user:          &models.User{Role: models.RoleOwner, CycleLength: 28},
			stats:         lateCycleStats(today, 29, 4, 28, 27, 30),
			expectVisible: false,
		},
		{
			name:          "seven days past the expected range is still inside the grace window",
			user:          &models.User{Role: models.RoleOwner, CycleLength: 28},
			stats:         lateCycleStats(today, 35, 4, 28, 27, 30),
			expectVisible: false,
		},
		{
			name:       "day 35 with four completed cycles states the measured excess",
			user:       &models.User{Role: models.RoleOwner, CycleLength: 27},
			stats:      lateCycleStats(today, 35, 4, 27, 26, 30),
			expectKey:  LateCycleBeyondRangeKey,
			expectTone: LateCycleToneWarning,
			expectForm: LateCycleFormCount,
			expectDays: 5,
		},
		{
			name:       "day 35 on a new account claims no range",
			user:       &models.User{Role: models.RoleOwner, CycleLength: 27},
			stats:      lateCycleStats(today, 35, 0, 0, 0, 0),
			expectKey:  LateCycleNoPersonalRangeKey,
			expectTone: LateCycleToneNeutral,
			expectForm: LateCycleFormPlain,
		},
		{
			name:       "day 35 after exactly one completed cycle claims no range",
			user:       &models.User{Role: models.RoleOwner, CycleLength: 27},
			stats:      lateCycleStats(today, 35, 1, 27, 27, 27),
			expectKey:  LateCycleNoPersonalRangeKey,
			expectTone: LateCycleToneNeutral,
			expectForm: LateCycleFormPlain,
		},
		{
			name:       "two completed cycles reach the reliability threshold and may compare",
			user:       &models.User{Role: models.RoleOwner, CycleLength: 27},
			stats:      lateCycleStats(today, 35, 2, 27, 26, 28),
			expectKey:  LateCycleBeyondRangeKey,
			expectTone: LateCycleToneWarning,
			expectForm: LateCycleFormCount,
			expectDays: 7,
		},
		{
			name:       "irregular mode past the observed range states the excess over it",
			user:       &models.User{Role: models.RoleOwner, CycleLength: 30, IrregularCycle: true},
			stats:      lateCycleStats(today, 46, 3, 32, 24, 40),
			expectKey:  LateCycleBeyondRangeKey,
			expectTone: LateCycleToneWarning,
			expectForm: LateCycleFormCount,
			expectDays: 6,
		},
		{
			name:            "irregular mode still inside the observed range says so instead of inventing a delay",
			user:            &models.User{Role: models.RoleOwner, CycleLength: 30, IrregularCycle: true},
			stats:           lateCycleStats(today, 42, 3, 32, 24, 45),
			expectKey:       LateCycleWithinRangeKey,
			expectTone:      LateCycleToneNeutral,
			expectForm:      LateCycleFormRange,
			expectRangeLow:  24,
			expectRangeHigh: 45,
		},
		{
			name:          "unpredictable mode renders recorded facts only",
			user:          &models.User{Role: models.RoleOwner, CycleLength: 27, UnpredictableCycle: true},
			stats:         lateCycleStats(today, 60, 4, 27, 26, 30),
			expectVisible: false,
		},
		{
			name: "a paused pregnancy renders recorded facts only",
			user: &models.User{Role: models.RoleOwner, CycleLength: 27},
			stats: func() CycleStats {
				stats := lateCycleStats(today, 60, 4, 27, 26, 30)
				stats.PregnancyPaused = true
				return stats
			}(),
			expectVisible: false,
		},
		{
			name:          "an account with no anchor has no cycle day to state",
			user:          &models.User{Role: models.RoleOwner, CycleLength: 27},
			stats:         lateCycleStats(today, 0, 0, 0, 0, 0),
			expectVisible: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			notice := BuildDashboardCycleContext(testCase.user, nil, testCase.stats, today, time.UTC).LateCycle

			expectVisible := testCase.expectVisible || testCase.expectKey != ""
			if notice.Visible != expectVisible {
				t.Fatalf("expected late-cycle notice visible=%t, got %t (key %q)", expectVisible, notice.Visible, notice.MessageKey)
			}
			if !expectVisible {
				return
			}
			if notice.MessageKey != testCase.expectKey {
				t.Errorf("expected message key %q, got %q", testCase.expectKey, notice.MessageKey)
			}
			if notice.Tone != testCase.expectTone {
				t.Errorf("expected tone %q, got %q", testCase.expectTone, notice.Tone)
			}
			if notice.Form != testCase.expectForm {
				t.Errorf("expected form %q, got %q", testCase.expectForm, notice.Form)
			}
			if notice.Days != testCase.expectDays {
				t.Errorf("expected %d day(s) of excess, got %d", testCase.expectDays, notice.Days)
			}
			if notice.RangeLow != testCase.expectRangeLow || notice.RangeHigh != testCase.expectRangeHigh {
				t.Errorf("expected range %d-%d, got %d-%d", testCase.expectRangeLow, testCase.expectRangeHigh, notice.RangeLow, notice.RangeHigh)
			}
		})
	}
}

// TestLateCycleNoticeReadsTheSameReliabilitySignalAsStats pins the single gate:
// the dashboard may speak of a "usual range" exactly when the stats page's
// prediction-reliability card is willing to speak of a completed-cycle sample.
// A second threshold for late-cycle copy would let one surface promise a range
// the other still calls a pattern in the making.
func TestLateCycleNoticeReadsTheSameReliabilitySignalAsStats(t *testing.T) {
	today := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	user := &models.User{Role: models.RoleOwner, CycleLength: 27}

	for completedCycles := range 5 {
		stats := lateCycleStats(today, 40, completedCycles, 27, 26, 30)
		notice := BuildLateCycleNotice(user, stats, true)

		_, _, _, _, statsCardVisible := buildStatsPredictionReliability(
			user,
			StatsFlags{CompletedCycleCount: completedCycles},
			stats,
		)
		comparesToARange := notice.MessageKey != LateCycleNoPersonalRangeKey

		if comparesToARange != statsCardVisible {
			t.Errorf(
				"with %d completed cycles the dashboard compares=%t while the stats reliability card shows=%t",
				completedCycles,
				comparesToARange,
				statsCardVisible,
			)
		}
	}
}

// TestLateCycleNoticeCopyExistsInEveryLocale keeps the chosen keys and the
// shipped catalogues in step: a key the policy can select but a catalogue does
// not carry renders as the raw key at the most anxious moment in the product.
func TestLateCycleNoticeCopyExistsInEveryLocale(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}

	languages := manager.SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("expected the i18n manager to report supported languages")
	}

	for _, language := range languages {
		messages := manager.Messages(language)
		for _, key := range []string{
			LateCycleNoPersonalRangeKey,
			"dashboard.late_cycle.actions",
		} {
			if messages[key] == "" {
				t.Errorf("locale %q has no entry for %q: the message would render as the raw key", language, key)
			}
		}
		for _, base := range []string{LateCycleBeyondRangeKey, LateCycleWithinRangeKey} {
			for _, category := range i18n.PluralCategories(language) {
				key := base + "." + category
				if messages[key] == "" {
					t.Errorf("locale %q has no entry for %q: the message would render as the raw key", language, key)
				}
			}
		}
	}
}
