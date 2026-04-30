package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	daySaveMessageSelfCare = "dashboard.save_message_self_care"
	daySaveMessageFertile  = "dashboard.save_message_fertile"
	daySaveMessageNeutral  = "dashboard.save_message_neutral"
)

type DayFeedbackState struct {
	MessageKey               string
	ShowSpottingCycleWarning bool
	ShowLongPeriodWarning    bool
	LongPeriodCycleStart     time.Time
}

func (service *DayService) ResolveDayFeedback(user *models.User, day time.Time, now time.Time, location *time.Location) (DayFeedbackState, error) {
	if location == nil {
		location = time.UTC
	}

	logs, err := service.logs.ListByUser(user.ID)
	if err != nil {
		return DayFeedbackState{}, err
	}

	day = DateAtLocation(day, location)
	today := DateAtLocation(now, location)
	stats := BuildCycleStats(logs, today)
	entry, err := service.FetchLogByDate(user.ID, day, location)
	if err != nil {
		return DayFeedbackState{}, err
	}

	state := DayFeedbackState{
		MessageKey: resolveDaySaveMessageKey(user, day, stats),
	}

	if shouldShowSpottingCycleWarning(logs, entry, day, location) {
		state.ShowSpottingCycleWarning = true
	}

	streakLength, cycleStart, ok := currentPeriodStreakAtDay(logs, day, location)
	if ok && streakLength > 8 && (user == nil || user.LongPeriodWarnedAt == nil || !sameCalendarDay(CalendarDay(*user.LongPeriodWarnedAt, location), cycleStart)) {
		state.ShowLongPeriodWarning = true
		state.LongPeriodCycleStart = cycleStart
	}

	return state, nil
}

func (service *DayService) AcknowledgeLongPeriodWarning(userID uint, cycleStart time.Time, location *time.Location) error {
	if service == nil || service.users == nil || cycleStart.IsZero() {
		return nil
	}
	if location != nil {
		cycleStart = CalendarDay(cycleStart, location)
	}
	return service.users.UpdateByID(userID, map[string]any{
		"long_period_warning_cycle_start": cycleStart,
	})
}

func resolveDaySaveMessageKey(user *models.User, day time.Time, stats CycleStats) string {
	if user != nil && user.UnpredictableCycle {
		return daySaveMessageNeutral
	}
	if !stats.LastPeriodStart.IsZero() {
		cycleDay := cycleDayAt(stats.LastPeriodStart, day)
		if cycleDay >= 1 && cycleDay <= 3 {
			return daySaveMessageSelfCare
		}
	}
	if !stats.FertilityWindowStart.IsZero() && !day.Before(stats.FertilityWindowStart) && !day.After(stats.FertilityWindowEnd) {
		return daySaveMessageFertile
	}
	return daySaveMessageNeutral
}

func shouldShowSpottingCycleWarning(logs []models.DailyLog, entry models.DailyLog, day time.Time, location *time.Location) bool {
	if !entry.IsPeriod || NormalizeDayFlow(entry.Flow) != models.FlowSpotting {
		return false
	}

	_, cycleStart, ok := currentPeriodStreakAtDay(logs, day, location)
	if !ok {
		return false
	}

	return sameCalendarDay(cycleStart, DateAtLocation(day, location))
}

func currentPeriodStreakAtDay(logs []models.DailyLog, day time.Time, location *time.Location) (int, time.Time, bool) {
	if len(logs) == 0 {
		return 0, time.Time{}, false
	}

	targetDay := DateAtLocation(day, location)
	logByDay := make(map[string]models.DailyLog, len(logs))
	for _, logEntry := range sortDailyLogs(logs) {
		logByDay[CalendarDay(logEntry.Date, location).Format("2006-01-02")] = logEntry
	}

	current, ok := logByDay[targetDay.Format("2006-01-02")]
	if !ok || !current.IsPeriod {
		return 0, time.Time{}, false
	}

	streak := 0
	cycleStart := targetDay
	for cursor := targetDay; ; cursor = cursor.AddDate(0, 0, -1) {
		logEntry, exists := logByDay[cursor.Format("2006-01-02")]
		if !exists || !logEntry.IsPeriod {
			break
		}
		streak++
		cycleStart = cursor
	}
	return streak, cycleStart, true
}
