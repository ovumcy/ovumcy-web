package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestBuildOwnerPredictionExplanation(t *testing.T) {
	t.Run("unsupported role sees no owner-only explanation", func(t *testing.T) {
		explanation := BuildOwnerPredictionExplanation(&models.User{Role: "legacy_viewer"}, DashboardCycleContext{}, true)
		if explanation.PrimaryKey != "" || explanation.SecondaryKey != "" {
			t.Fatalf("expected no prediction explanation for unsupported role, got %#v", explanation)
		}
	})

	t.Run("unpredictable mode shows facts-only explanation", func(t *testing.T) {
		explanation := BuildOwnerPredictionExplanation(
			&models.User{Role: models.RoleOwner, UnpredictableCycle: true},
			DashboardCycleContext{PredictionDisabled: true},
			true,
		)
		if explanation.PrimaryKey != "prediction.explainer.unpredictable" {
			t.Fatalf("expected unpredictable primary key, got %#v", explanation)
		}
		if explanation.SecondaryKey != "" {
			t.Fatalf("expected no factor secondary hint in unpredictable mode, got %#v", explanation)
		}
	})

	t.Run("irregular sparse data explains range threshold", func(t *testing.T) {
		explanation := BuildOwnerPredictionExplanation(
			&models.User{Role: models.RoleOwner, IrregularCycle: true},
			DashboardCycleContext{
				DisplayNextPeriodNeedsData: true,
				DisplayOvulationNeedsData:  true,
			},
			false,
		)
		if explanation.PrimaryKey != "prediction.explainer.irregular_sparse" {
			t.Fatalf("expected sparse irregular key, got %#v", explanation)
		}
	})

	t.Run("irregular range state explains range mode", func(t *testing.T) {
		explanation := BuildOwnerPredictionExplanation(
			&models.User{Role: models.RoleOwner, IrregularCycle: true},
			DashboardCycleContext{
				DisplayNextPeriodUseRange: true,
				DisplayOvulationUseRange:  true,
			},
			false,
		)
		if explanation.PrimaryKey != "prediction.explainer.irregular_ranges" {
			t.Fatalf("expected irregular range key, got %#v", explanation)
		}
	})

	t.Run("variable patterns keep observational factor hint", func(t *testing.T) {
		explanation := BuildOwnerPredictionExplanation(
			&models.User{Role: models.RoleOwner},
			DashboardCycleContext{},
			true,
		)
		if explanation.PrimaryKey != "" {
			t.Fatalf("did not expect primary key for regular variable pattern, got %#v", explanation)
		}
		if explanation.SecondaryKey != "prediction.explainer.factor_context" {
			t.Fatalf("expected factor context secondary key, got %#v", explanation)
		}
	})

	// A regular owner whose prediction renders as a range no longer gets an
	// explainer sentence: the range itself, plus the next-period line naming
	// the start window, is the affordance. Only an irregular owner keeps a
	// range explainer, which the subtest above pins.
	t.Run("regular user with data-driven range gets no explainer", func(t *testing.T) {
		explanation := BuildOwnerPredictionExplanation(
			&models.User{Role: models.RoleOwner},
			DashboardCycleContext{DisplayNextPeriodUseRange: true},
			false,
		)
		if explanation.PrimaryKey != "" {
			t.Fatalf("expected no primary key for a regular owner in range mode, got %#v", explanation)
		}
	})

	t.Run("pregnancy pause outranks other explainer states", func(t *testing.T) {
		explanation := BuildOwnerPredictionExplanation(
			&models.User{Role: models.RoleOwner, UnpredictableCycle: true},
			DashboardCycleContext{PregnancyPaused: true, PredictionDisabled: true},
			true,
		)
		if explanation.PrimaryKey != "prediction.explainer.pregnancy_paused" {
			t.Fatalf("expected pregnancy paused primary key, got %#v", explanation)
		}
		if explanation.SecondaryKey != "" {
			t.Fatalf("expected no factor secondary hint while paused, got %#v", explanation)
		}
	})
}

// TestBuildOwnerPredictionExplanationNamesTheFirstCycleFloor pins the line the
// cohort with no completed cycle had none of. It also pins the ORDER: an
// irregular account in this tier gets the first-cycle sentence rather than the
// irregular one, because the missing first cycle is why the fertility half is
// absent entirely, while the irregular branches only describe how a projection
// that does exist is presented.
func TestBuildOwnerPredictionExplanationNamesTheFirstCycleFloor(t *testing.T) {
	owner := &models.User{Role: models.RoleOwner}

	explanation := BuildOwnerPredictionExplanation(owner, DashboardCycleContext{AwaitingFirstCycle: true}, false)
	if explanation.PrimaryKey != "prediction.explainer.awaiting_first_cycle" {
		t.Fatalf("expected the first-cycle explainer, got %#v", explanation)
	}

	irregular := &models.User{Role: models.RoleOwner, IrregularCycle: true}
	withRanges := BuildOwnerPredictionExplanation(
		irregular,
		DashboardCycleContext{AwaitingFirstCycle: true, DisplayNextPeriodUseRange: true},
		false,
	)
	if withRanges.PrimaryKey != "prediction.explainer.awaiting_first_cycle" {
		t.Fatalf("expected the first-cycle floor to outrank the irregular-range branch, got %#v", withRanges)
	}

	// Negative control: without the floor the same account keeps the branch it
	// had before, so the new case is not swallowing the ones under it.
	paused := BuildOwnerPredictionExplanation(owner, DashboardCycleContext{PregnancyPaused: true, AwaitingFirstCycle: true}, false)
	if paused.PrimaryKey != "prediction.explainer.pregnancy_paused" {
		t.Fatalf("expected the pregnancy pause to still outrank the first-cycle floor, got %#v", paused)
	}
	settled := BuildOwnerPredictionExplanation(irregular, DashboardCycleContext{DisplayNextPeriodUseRange: true}, false)
	if settled.PrimaryKey != "prediction.explainer.irregular_ranges" {
		t.Fatalf("expected the irregular-range branch once the first cycle is behind, got %#v", settled)
	}
}
