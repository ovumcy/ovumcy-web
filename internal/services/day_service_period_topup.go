package services

import (
	"context"
	"fmt"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// runningPeriodWindow is what a newly ticked period day D means for the period
// the owner is still logging. matched is false for a day that starts a period of
// its own (the fill behind it is the ordinary one), and for every day the
// catch-up does not cover. When it is true, D is a continuation: lastDay is the
// last day of that period by the configured length, which the forward fill from D
// may not pass, and gap is the run of days behind D the catch-up may fill.
type runningPeriodWindow struct {
	matched bool
	lastDay time.Time
	gap     []time.Time
}

// planRunningPeriod resolves the window for a newly ticked day, and reads
// nothing unless the fill is on, the day was not already a period day and the
// configured length leaves room for a second day. Only the stored onboarding
// start is read from the settings: the length is the caller's, already resolved.
func (service *DayService) planRunningPeriod(ctx context.Context, userID uint, dayStart time.Time, wasPeriod bool, autoPeriodFillEnabled bool, periodLength int, now time.Time, location *time.Location) (runningPeriodWindow, error) {
	if !autoPeriodFillEnabled || wasPeriod || periodLength <= 1 {
		return runningPeriodWindow{}, nil
	}
	userSettings, logs, err := service.loadEpisodeHistory(ctx, userID, dayStart, periodLength, now, location)
	if err != nil {
		return runningPeriodWindow{}, err
	}
	return runningPeriodWindowFor(&userSettings, logs, CalendarDay(dayStart, location), periodLength, now, location), nil
}

// loadEpisodeHistory reads the stored onboarding start and the logs that can
// decide where the period around dayStart began. The read is bounded to the days
// from periodLength plus the cluster gap before dayStart up to today: a period
// that began earlier than that has its first period day inside the span within
// one cluster gap of the cut, so it still reads as past periodLength days from
// dayStart.
func (service *DayService) loadEpisodeHistory(ctx context.Context, userID uint, dayStart time.Time, periodLength int, now time.Time, location *time.Location) (models.User, []models.DailyLog, error) {
	userSettings, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return models.User{}, nil, err
	}
	from := AddCalendarDays(CalendarDay(dayStart, location), -(periodLength + periodClusterGapDays), location)
	logs, err := service.FetchLogsForUser(ctx, userID, from, DateAtLocation(now, location), location)
	if err != nil {
		return models.User{}, nil, err
	}
	return userSettings, logs, nil
}

// undoRunningPeriodGap is the un-tick's half of the catch-up: when the owner
// un-ticks a period day D that continues the running period, the rows directly
// behind it that are still bare auto-filled rows go with it, back to (not
// including) the period's first day. The run stops at the first day that is
// absent, not a period day, or carries anything of the owner's: a mood, a note, a
// symptom, a flow other than D's. Those rows are removed, not un-ticked, because
// the catch-up created them and they hold nothing else. Nothing is recorded as to
// which rows a tick created, so a bare same-flow day the owner ticked by hand in
// that run is taken for one; that is the same reading the forward clear makes of
// the days after an un-ticked period start.
func (service *DayService) undoRunningPeriodGap(ctx context.Context, userID uint, dayStart time.Time, periodLength int, propagatedFlow string, now time.Time, location *time.Location) error {
	if periodLength <= 1 {
		return nil
	}
	if location == nil {
		location = time.UTC
	}
	userSettings, logs, err := service.loadEpisodeHistory(ctx, userID, dayStart, periodLength, now, location)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillCheckFailed, err)
	}
	day := CalendarDay(dayStart, location)
	start, ok := episodeStartBehind(&userSettings, logs, day, now, location)
	if !ok || CalendarDaysBetween(start, day) >= periodLength {
		return nil
	}
	for target := AddCalendarDays(day, -1, location); CalendarDaysBetween(start, target) > 0; target = AddCalendarDays(target, -1, location) {
		rangeStart, rangeEnd := DayRange(target, location)
		entry, found, err := service.logs.FindByUserAndDayRangeForUpdate(ctx, userID, rangeStart, rangeEnd)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
		}
		if !found || !IsAutoFilledPeriodCandidate(entry, propagatedFlow) {
			return nil
		}
		if err := service.logs.DeleteByUserAndDayRange(ctx, userID, rangeStart, rangeEnd); err != nil {
			return fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
		}
	}
	return nil
}

// episodeClusters groups the owner's period days up to today, the stored
// onboarding start included as an explicit day, the way the cycle rule does.
func episodeClusters(user *models.User, logs []models.DailyLog, now time.Time, location *time.Location) []periodCluster {
	today := DateAtLocation(now, location)
	days := periodDaysOf(filterLogsNotAfter(logs, today))
	if onboarding := OnboardingBoundaryDay(BoundaryContextFor(user, today)); !onboarding.IsZero() {
		days = insertPeriodDay(days, periodDay{day: onboarding, explicit: true})
	}
	return clusterPeriodDays(days)
}

// episodeStartBehind is the first day of the period episode that holds the day
// before day; false when that day is no period day, or the episode is spotting
// only.
func episodeStartBehind(user *models.User, logs []models.DailyLog, day time.Time, now time.Time, location *time.Location) (time.Time, bool) {
	before := AddCalendarDays(day, -1, location)
	for _, cluster := range episodeClusters(user, logs, now, location) {
		if CalendarDaysBetween(cluster.Start, before) >= 0 && CalendarDaysBetween(before, cluster.End) >= 0 {
			return cluster.firstEpisodeDay()
		}
	}
	return time.Time{}, false
}

// fillRunningPeriodGap is the period auto-fill's catch-up for a period the
// owner is still logging. The fill behind a logged period start stops at the
// owner's local today, so a later day of the same period is marked by hand and
// finds the days before it empty. Marking such a day D fills the gap between the
// last period day before D and D, and only the days that have no daily_logs row
// at all: a row of any kind — an un-ticked day, a mood-only or notes-only save —
// is the owner's own record and stays as it is.
//
// It runs behind a newly ticked period day under the same setting that gates the
// fill after a period start, inside the same day-write transaction. Writes fail
// as ErrDayAutoFillApplyFailed, as the fill after a period start does.
func (service *DayService) fillRunningPeriodGap(ctx context.Context, userID uint, window runningPeriodWindow, flow string, location *time.Location) error {
	for _, targetDay := range window.gap {
		existing, err := service.fetchLogByDateForUpdate(ctx, userID, targetDay, location)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
		}
		if existing.ID != 0 {
			continue
		}
		newEntry := newAutoFilledPeriodEntry(userID, targetDay, flow)
		if err := service.logs.Create(ctx, &newEntry); err != nil {
			return fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
		}
	}
	return nil
}

// runningPeriodWindowFor reads the window off the owner's logs. The gap is the
// calendar days strictly between the last period day before day and day, none of
// them after the owner's local today. The today bound is the one on logs: a day
// after today is not part of any episode here, so a future-dated day matches
// nothing rather than only its past-today part. It matches only when day belongs
// to the newest bleeding episode up to today and lies within periodLength days of
// that episode's start, so a day of an older period, or one the configured length
// no longer covers, matches nothing. logs must already hold day as a period day.
//
// The start is the episode's first non-spotting day, or its explicit cycle-start
// mark when it carries one. That is deliberately not the CycleBoundaries anchor:
// the anchor does not open a cycle on a lone period day older than yesterday,
// which is exactly the day-one-then-a-later-day shape this serves. The result
// bounds the window only and is never read as where a cycle starts. The returned
// days still have to be checked for a row at write time.
func runningPeriodWindowFor(user *models.User, logs []models.DailyLog, day time.Time, periodLength int, now time.Time, location *time.Location) runningPeriodWindow {
	clusters := episodeClusters(user, logs, now, location)
	if len(clusters) == 0 {
		return runningPeriodWindow{}
	}

	running := clusters[len(clusters)-1]
	if CalendarDaysBetween(running.Start, day) < 0 || CalendarDaysBetween(day, running.End) < 0 {
		return runningPeriodWindow{}
	}
	start, ok := running.firstEpisodeDay()
	if !ok {
		return runningPeriodWindow{}
	}
	offset := CalendarDaysBetween(start, day)
	if offset <= 0 || offset >= periodLength {
		return runningPeriodWindow{}
	}

	previous := start
	for _, entry := range running.days {
		if CalendarDaysBetween(entry.day, day) > 0 {
			previous = entry.day
		}
	}

	gap := make([]time.Time, 0, offset)
	for step := 1; CalendarDaysBetween(previous, day) > step; step++ {
		gap = append(gap, AddCalendarDays(previous, step, location))
	}
	return runningPeriodWindow{
		matched: true,
		lastDay: AddCalendarDays(start, periodLength-1, location),
		gap:     gap,
	}
}

// firstEpisodeDay is the day the window counts the episode from: the first
// explicit non-spotting cycle-start mark, else the first non-spotting period day.
// It is false for an episode of spotting only, which never opens a cycle.
func (cluster periodCluster) firstEpisodeDay() (time.Time, bool) {
	first := time.Time{}
	for _, entry := range cluster.days {
		if entry.spotting {
			continue
		}
		if entry.explicit {
			return entry.day, true
		}
		if first.IsZero() {
			first = entry.day
		}
	}
	return first, !first.IsZero()
}

// newAutoFilledPeriodEntry is the bare period row both period fills create: no
// field beyond the period and its propagated flow, which is what lets the
// un-tick path recognise an auto-filled neighbour.
func newAutoFilledPeriodEntry(userID uint, day time.Time, flow string) models.DailyLog {
	return models.DailyLog{
		UserID:          userID,
		Date:            day,
		IsPeriod:        true,
		Flow:            flow,
		SexActivity:     models.SexActivityNone,
		CervicalMucus:   models.CervicalMucusNone,
		PregnancyTest:   models.PregnancyTestNone,
		CycleFactorKeys: []string{},
		SymptomIDs:      []uint{},
	}
}
