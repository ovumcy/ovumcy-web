package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) GetStatsOverview(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	now := time.Now().In(handler.location)
	stats, _, err := handler.statsService.BuildCycleStatsForRange(user, now.AddDate(-2, 0, 0), now, now, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch stats")
	}

	return c.JSON(stats)
}
