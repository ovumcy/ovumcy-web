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
}
