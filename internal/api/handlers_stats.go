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

	location := handler.requestLocation(c)
	now := time.Now().In(location)
	stats, err := handler.statsService.BuildOverviewStats(user, now, location)
	if err != nil {
		return handler.respondMappedError(c, statsFetchErrorSpec())
	}

	return c.JSON(stats)
}
