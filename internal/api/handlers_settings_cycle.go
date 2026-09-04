package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

var cycleSettingsMutation = healthMutationKind{action: "settings.cycle_update", target: "cycle_settings"}

func (handler *Handler) UpdateCycleSettings(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, cycleSettingsMutation, unauthorizedErrorSpec())
	}

	// A body carrying the usage goal and nothing else is the dashboard quick
	// switch. It rides this endpoint (and this audit target) rather than a
	// twin route, and the service decides what a goal-only save writes.
	if usageGoal, goalOnly := goalOnlyCycleSettingsRequest(c); goalOnly {
		return handler.updateUsageGoalOnly(c, user, usageGoal)
	}

	input, parseError := handler.parseCycleSettingsInput(c, user)
	if parseError != "" {
		return handler.failMutation(c, cycleSettingsMutation, settingsValidationErrorSpec(parseError))
	}
	if err := handler.settingsService.SaveCycleSettings(c.Context(), user.ID, input); err != nil {
		return handler.failMutation(c, cycleSettingsMutation, settingsCycleUpdateErrorSpec())
	}

	handler.settingsService.ApplyCycleSettings(user, input)
	handler.logMutationSuccess(c, cycleSettingsMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, "cycle_updated", "Cycle settings updated successfully."))
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "cycle_updated"})
	return redirectOrJSON(c, "/settings")
}

func (handler *Handler) updateUsageGoalOnly(c fiber.Ctx, user *models.User, rawUsageGoal string) error {
	usageGoal, err := handler.settingsService.SaveUsageGoal(c.Context(), user.ID, rawUsageGoal)
	if err != nil {
		return handler.failMutation(c, cycleSettingsMutation, settingsCycleUpdateErrorSpec())
	}

	handler.settingsService.ApplyUsageGoal(user, usageGoal)
	handler.logMutationSuccess(c, cycleSettingsMutation)

	if acceptsJSON(c) {
		// The stored goal is not echoed back: OkResponse declares
		// `additionalProperties: false`, so an extra member here is a response a
		// client validating against the published schema rejects.
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		// The mode reframes fertile-window copy, badges and summaries across the
		// whole page, so the answer is a re-render rather than a status swap.
		c.Set("HX-Refresh", "true")
		return c.SendStatus(fiber.StatusNoContent)
	}

	// No flash is written here, unlike every other settings success: the
	// destination is the DASHBOARD, and only the settings page pops the flash
	// cookie. A "cycle_updated" left behind was invisible on arrival and then
	// surfaced on whatever settings page the owner opened next, within the
	// five-minute cookie TTL, as a save they had not just made. The dashboard
	// carries this verdict in its own state instead: the redirect re-renders it
	// in the new mode, chip label included (`data-usage-goal-label-key`).
	// Regression: TestUsageGoalQuickSwitchLeavesNoFlashForTheDashboard.
	return redirectOrJSON(c, "/dashboard")
}
