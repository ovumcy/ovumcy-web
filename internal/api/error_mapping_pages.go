package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func mapCalendarViewError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrCalendarViewLoadLogs):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load calendar")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load stats")
	}
}

func mapDashboardViewError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrDashboardViewLoadTodayLog):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load today log")
	case errors.Is(err, services.ErrDashboardViewLoadLogs):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load symptom history")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load logs")
	}
}

func mapDayEditorViewError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrDashboardViewLoadDayState):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day state")
	case errors.Is(err, services.ErrDashboardViewLoadDayLog):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day log")
	case errors.Is(err, services.ErrDashboardViewLoadLogs):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load symptom history")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day")
	}
}

func mapStatsPageViewError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrStatsPageViewLoadSymptoms):
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load symptom stats")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load stats")
	}
}

func statsFetchErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch stats")
}

func respondNotFoundMappedError(c *fiber.Ctx) error {
	spec := notFoundErrorSpec()
	if isHTMX(c) {
		message := translateMessage(currentMessages(c), "not_found.title")
		if message == "not_found.title" {
			message = "Page not found"
		}
		return c.Status(spec.Status).SendString(httpx.StatusErrorMarkup(message, "not_found.title"))
	}
	return respondGlobalMappedError(c, spec)
}
