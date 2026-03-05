package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func mapCalendarViewError(err error) APIErrorSpec {
	switch services.ClassifyCalendarViewError(err) {
	case services.CalendarViewErrorLoadLogs:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load calendar")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load stats")
	}
}

func mapDashboardViewError(err error) APIErrorSpec {
	switch services.ClassifyDashboardViewError(err) {
	case services.DashboardViewErrorLoadTodayLog:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load today log")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load logs")
	}
}

func mapDayEditorViewError(err error) APIErrorSpec {
	switch services.ClassifyDashboardViewError(err) {
	case services.DashboardViewErrorLoadDayState:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day state")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day")
	}
}

func mapStatsPageViewError(err error) APIErrorSpec {
	switch services.ClassifyStatsPageViewError(err) {
	case services.StatsPageViewErrorLoadSymptoms:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load symptom stats")
	default:
		return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load stats")
	}
}

func statsFetchErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch stats")
}
