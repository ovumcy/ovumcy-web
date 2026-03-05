package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) GetStatsOverview(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	now := time.Now().In(handler.location)
	stats, err := handler.statsService.BuildOverviewStats(user, now, handler.location)
	if err != nil {
		return handler.respondMappedError(c, statsFetchErrorSpec())
	}

	return c.JSON(stats)
}
