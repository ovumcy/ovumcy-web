package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) buildCalendarViewData(user *models.User, language string, messages map[string]string, now time.Time, monthStart time.Time, selectedDate string) (fiber.Map, string, error) {

	viewData, err := handler.calendarViewService.BuildCalendarPageViewData(user, language, now, monthStart, selectedDate, handler.location)
	if err != nil {
		switch services.ClassifyCalendarViewError(err) {
		case services.CalendarViewErrorLoadLogs:
			return nil, "failed to load calendar", err
		default:
			return nil, "failed to load stats", err
		}
	}

	days := handler.buildCalendarDays(viewData.DayStates)

	data := fiber.Map{
		"Title":        localizedPageTitle(messages, "meta.title.calendar", "Ovumcy | Calendar"),
		"CurrentUser":  user,
		"MonthLabel":   viewData.MonthLabel,
		"MonthValue":   viewData.MonthValue,
		"PrevMonth":    viewData.PrevMonth,
		"NextMonth":    viewData.NextMonth,
		"SelectedDate": viewData.SelectedDate,
		"CalendarDays": days,
		"Today":        viewData.TodayISO,
		"Stats":        viewData.Stats,
		"IsOwner":      viewData.IsOwner,
	}
	return data, "", nil
}
