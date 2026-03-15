package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var (
	ErrCalendarViewLoadLogs  = errors.New("calendar view load logs")
	ErrCalendarViewLoadStats = errors.New("calendar view load stats")
)

type CalendarViewDayReader interface {
	FetchLogsForUser(userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error)
}

type CalendarViewStatsProvider interface {
	BuildCycleStatsForRange(user *models.User, from time.Time, to time.Time, now time.Time, location *time.Location) (CycleStats, []models.DailyLog, error)
}

type CalendarPageViewData struct {
	MonthLabel                        string
	MonthValue                        string
	PrevMonth                         string
	NextMonth                         string
	SelectedDate                      string
	DayStates                         []CalendarDayState
	TodayISO                          string
	Stats                             CycleStats
	PredictionExplanationPrimaryKey   string
	PredictionExplanationSecondaryKey string
	HasPredictionExplanationPrimary   bool
	HasPredictionExplanationSecondary bool
	IsOwner                           bool
}

type CalendarViewService struct {
	days  CalendarViewDayReader
	stats CalendarViewStatsProvider
}

func NewCalendarViewService(days CalendarViewDayReader, stats CalendarViewStatsProvider) *CalendarViewService {
	return &CalendarViewService{
		days:  days,
		stats: stats,
	}
}

func (service *CalendarViewService) BuildCalendarPageViewData(user *models.User, language string, now time.Time, monthStart time.Time, selectedDate string, location *time.Location) (CalendarPageViewData, error) {
	logRangeStart, logRangeEnd := CalendarLogRange(monthStart)
	logs, err := service.days.FetchLogsForUser(user.ID, logRangeStart, logRangeEnd, location)
	if err != nil {
		return CalendarPageViewData{}, fmt.Errorf("%w: %v", ErrCalendarViewLoadLogs, err)
	}

	stats, statsLogs, err := service.stats.BuildCycleStatsForRange(user, now.AddDate(-2, 0, 0), now, now, location)
	if err != nil {
		return CalendarPageViewData{}, fmt.Errorf("%w: %v", ErrCalendarViewLoadStats, err)
	}

	minMonth := CalendarMinimumNavigableMonth(user, location)
	prevMonth, nextMonth := CalendarAdjacentMonthValuesWithinBounds(monthStart, minMonth)
	dayStates := BuildCalendarDayStates(user, monthStart, logs, stats, now, location)
	cycleContext := BuildDashboardCycleContext(user, stats, DateAtLocation(now, location), location)
	cycleFactorExplanation, hasCycleFactorExplanation := buildStatsCycleFactorExplanation(user, statsLogs, stats, now, location)
	predictionExplanation := BuildOwnerPredictionExplanation(user, cycleContext, hasCycleFactorExplanation && len(cycleFactorExplanation.HintFactorKeys) > 0)

	return CalendarPageViewData{
		MonthLabel:                        LocalizedMonthYear(language, monthStart),
		MonthValue:                        monthStart.Format("2006-01"),
		PrevMonth:                         prevMonth,
		NextMonth:                         nextMonth,
		SelectedDate:                      selectedDate,
		DayStates:                         dayStates,
		TodayISO:                          DateAtLocation(now, location).Format("2006-01-02"),
		Stats:                             stats,
		PredictionExplanationPrimaryKey:   predictionExplanation.PrimaryKey,
		PredictionExplanationSecondaryKey: predictionExplanation.SecondaryKey,
		HasPredictionExplanationPrimary:   predictionExplanation.PrimaryKey != "",
		HasPredictionExplanationSecondary: predictionExplanation.SecondaryKey != "",
		IsOwner:                           IsOwnerUser(user),
	}, nil
}
