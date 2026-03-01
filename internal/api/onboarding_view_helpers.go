package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) buildOnboardingViewData(c *fiber.Ctx, user *models.User, now time.Time) fiber.Map {
	messages := currentMessages(c)
	state := services.BuildOnboardingViewState(user, c.Query("step"), now, handler.location)

	lastPeriodStart := ""
	if state.LastPeriodStart != nil {
		lastPeriodStart = state.LastPeriodStart.Format("2006-01-02")
	}

	return fiber.Map{
		"Title":           localizedPageTitle(messages, "meta.title.onboarding", "Ovumcy | Onboarding"),
		"CurrentUser":     user,
		"HideNavigation":  true,
		"OnboardingStep":  state.Step,
		"MinDate":         state.MinDate.Format("2006-01-02"),
		"MaxDate":         state.MaxDate.Format("2006-01-02"),
		"LastPeriodStart": lastPeriodStart,
		"CycleLength":     state.CycleLength,
		"PeriodLength":    state.PeriodLength,
		"AutoPeriodFill":  state.AutoPeriodFill,
	}
}
