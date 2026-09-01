package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) GetStatsOverview(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	now := time.Now().In(location)
	stats, err := handler.statsService.BuildOverviewStats(c.Context(), user, now, location)
	if err != nil {
		return handler.respondMappedError(c, statsFetchErrorSpec())
	}

	// The same adapter /stats and the dashboard publish through: this endpoint
	// used to serialize the domain struct, so every date those pages withhold
	// left the instance as JSON (medical safety — suppression is the floor).
	published, suppression := services.PublishedStats(user, stats)

	return c.JSON(newStatsOverviewResponse(published, suppression, handler.medicalDisclaimer(c)))
}
