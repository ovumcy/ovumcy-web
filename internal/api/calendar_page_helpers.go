package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func (handler *Handler) buildCalendarViewData(ctx context.Context, user *models.User, language string, messages map[string]string, now time.Time, monthStart time.Time, selectedDate string, location *time.Location) (fiber.Map, error) {
	viewData, err := handler.calendarViewService.BuildCalendarPageViewData(ctx, user, language, now, monthStart, selectedDate, location)
	if err != nil {
		return nil, err
	}

	days := handler.buildCalendarDays(viewData.DayStates)
	formAccount, err := handler.dayFormAccountBinding(user)
	if err != nil {
		return nil, err // codecov:ignore -- NewHandler refuses an empty secret and a signed-in user has a non-zero id
	}

	data := fiber.Map{
		"Title":                             localizedPageTitle(messages, "meta.title.calendar", "Ovumcy | Calendar"),
		"CurrentUser":                       user,
		"MonthLabel":                        viewData.MonthLabel,
		"MonthValue":                        viewData.MonthValue,
		"PrevMonth":                         viewData.PrevMonth,
		"NextMonth":                         viewData.NextMonth,
		"PrevHref":                          calendarMonthHref(viewData.PrevMonth, viewData.SelectedDate),
		"NextHref":                          calendarMonthHref(viewData.NextMonth, viewData.SelectedDate),
		"TodayHref":                         calendarTodayHref(viewData.TodayISO, viewData.SelectedDate),
		"TodayMonth":                        calendarMonthOf(viewData.TodayISO),
		"SelectedDate":                      viewData.SelectedDate,
		"CalendarDays":                      days,
		"WeekdayKeys":                       viewData.WeekdayKeys,
		"Today":                             viewData.TodayISO,
		"Stats":                             viewData.Stats,
		"PredictionExplanationPrimaryKey":   viewData.PredictionExplanationPrimaryKey,
		"PredictionExplanationPrimaryCount": viewData.PredictionExplanationPrimaryCount,
		"PredictionExplanationSecondaryKey": viewData.PredictionExplanationSecondaryKey,
		"HasPredictionExplanationPrimary":   viewData.HasPredictionExplanationPrimary,
		"HasPredictionExplanationSecondary": viewData.HasPredictionExplanationSecondary,
		"LateCycle":                         viewData.LateCycle,
		"IsOwner":                           viewData.IsOwner,
		"CalendarLegend":                    viewData.Legend,
		"DayFormAccount":                    formAccount,
	}
	return data, nil
}

// Month navigation keeps the selected day open, so stepping to a neighbouring
// month does not close the editor. The calendar script rebuilds the same links
// when a day is chosen without a page load; the two must agree.
func calendarMonthHref(month string, selectedDate string) string {
	if month == "" {
		return ""
	}
	if selectedDate == "" {
		return "/calendar?month=" + month
	}
	return "/calendar?month=" + month + "&day=" + selectedDate
}

// A bare /calendar opens the current month with today selected, which is
// already what Today means when nothing else, or today itself, is selected.
func calendarTodayHref(todayISO string, selectedDate string) string {
	if selectedDate == "" || selectedDate == todayISO {
		return "/calendar"
	}
	return calendarMonthHref(calendarMonthOf(todayISO), selectedDate)
}

func calendarMonthOf(isoDate string) string {
	if len(isoDate) < len("2006-01") {
		return ""
	}
	return isoDate[:len("2006-01")]
}
