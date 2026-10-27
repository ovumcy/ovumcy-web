package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrCalendarViewLoadLogs  = errors.New("calendar view load logs")
	ErrCalendarViewLoadStats = errors.New("calendar view load stats")
)

type CalendarViewDayReader interface {
	FetchLogsForUser(ctx context.Context, userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error)
	EarliestLogDayForUser(ctx context.Context, userID uint) (time.Time, bool, error)
}

// CalendarMonthBounds is the navigable month range of one owner's calendar; a
// zero side is unbounded.
type CalendarMonthBounds struct {
	Min time.Time
	Max time.Time
}

type CalendarViewStatsProvider interface {
	BuildCycleStatsForRange(ctx context.Context, user *models.User, from time.Time, to time.Time, now time.Time, location *time.Location) (CycleStats, []models.DailyLog, error)
}

type CalendarPageViewData struct {
	MonthLabel                        string
	MonthValue                        string
	PrevMonth                         string
	NextMonth                         string
	SelectedDate                      string
	DayStates                         []CalendarDayState
	WeekdayKeys                       []string
	TodayISO                          string
	Stats                             CycleStats
	PredictionExplanationPrimaryKey   string
	PredictionExplanationPrimaryCount int
	PredictionExplanationSecondaryKey string
	HasPredictionExplanationPrimary   bool
	HasPredictionExplanationSecondary bool
	LateCycle                         LateCycleNotice
	IsOwner                           bool
	Legend                            CalendarLegend
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

// NavigableMonthBounds reads the owner's earliest stored day once per request,
// so the handler's clamp and the page's prev/next links share one lower bound.
func (service *CalendarViewService) NavigableMonthBounds(ctx context.Context, user *models.User, now time.Time, location *time.Location) (CalendarMonthBounds, error) {
	earliest, _, err := service.days.EarliestLogDayForUser(ctx, user.ID)
	if err != nil {
		return CalendarMonthBounds{}, fmt.Errorf("%w: %v", ErrCalendarViewLoadLogs, err)
	}
	return CalendarMonthBounds{
		Min: CalendarMinimumNavigableMonth(user, earliest, location),
		Max: CalendarMaximumNavigableMonth(now, location),
	}, nil
}

func (service *CalendarViewService) BuildCalendarPageViewData(ctx context.Context, user *models.User, language string, now time.Time, monthStart time.Time, selectedDate string, bounds CalendarMonthBounds, location *time.Location) (CalendarPageViewData, error) {
	logRangeStart, logRangeEnd := CalendarLogRange(monthStart)
	logs, err := service.days.FetchLogsForUser(ctx, user.ID, logRangeStart, logRangeEnd, location)
	if err != nil {
		return CalendarPageViewData{}, fmt.Errorf("%w: %v", ErrCalendarViewLoadLogs, err)
	}

	statsFrom, statsTo := StatsOverviewRange(now)
	stats, statsLogs, err := service.stats.BuildCycleStatsForRange(ctx, user, statsFrom, statsTo, now, location)
	if err != nil {
		return CalendarPageViewData{}, fmt.Errorf("%w: %v", ErrCalendarViewLoadStats, err)
	}

	prevMonth, nextMonth := CalendarAdjacentMonthValuesWithinBounds(monthStart, bounds.Min, bounds.Max)
	dayStates := BuildCalendarDayStates(user, monthStart, logs, stats, now, location)
	// statsLogs, not the grid's set: `logs` is the window around the MONTH being
	// viewed (CalendarLogRange), so a past month excludes the current cycle
	// entirely and the confirmation would answer "no shift" purely because of
	// where the owner navigated. The cycle context describes the cycle the owner
	// is in, and the stats window is anchored on now.
	cycleContext := BuildDashboardCycleContext(user, statsLogs, stats, DateAtLocation(now, location), location)
	cycleFactorExplanation, hasCycleFactorExplanation := buildStatsCycleFactorExplanation(user, statsLogs, stats, now, location)
	predictionExplanation := BuildOwnerPredictionExplanation(user, cycleContext, hasCycleFactorExplanation && len(cycleFactorExplanation.HintFactorKeys) > 0)

	return CalendarPageViewData{
		MonthLabel:                        LocalizedMonthYear(language, monthStart),
		MonthValue:                        monthStart.Format("2006-01"),
		PrevMonth:                         prevMonth,
		NextMonth:                         nextMonth,
		SelectedDate:                      selectedDate,
		DayStates:                         dayStates,
		WeekdayKeys:                       WeekdayHeaderKeys(NormalizeWeekStart(user.WeekStartsOn)),
		TodayISO:                          DateAtLocation(now, location).Format("2006-01-02"),
		Stats:                             stats,
		PredictionExplanationPrimaryKey:   predictionExplanation.PrimaryKey,
		PredictionExplanationPrimaryCount: predictionExplanation.PrimaryCount,
		PredictionExplanationSecondaryKey: predictionExplanation.SecondaryKey,
		HasPredictionExplanationPrimary:   predictionExplanation.PrimaryKey != "",
		HasPredictionExplanationSecondary: predictionExplanation.SecondaryKey != "",
		IsOwner:                           IsOwnerUser(user),
		// The dashboard's late-cycle notice, read off the same cycle context:
		// the three owner pages state a late cycle in one set of words.
		LateCycle: cycleContext.LateCycle,
		Legend:    BuildCalendarLegend(dayStates),
	}, nil
}
