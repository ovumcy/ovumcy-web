package services

import (
	"context"
	"fmt"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// topUpRunningPeriodGap is the period auto-fill's catch-up for a period the
// owner is still logging. The fill behind a logged period start stops at the
// owner's local today, so a later day of the same period is marked by hand and
// finds the days before it empty. Marking such a day D fills the gap between the
// last period day before D and D, and only the days that have no daily_logs row
// at all: a row of any kind — an un-ticked day, a mood-only or notes-only save —
// is the owner's own record and stays as it is.
//
// It runs behind a newly ticked period day under the same setting that gates the
// fill after a period start, inside the same day-write transaction. wasPeriod
// keeps a re-save of a day that already was a period day from re-creating rows
// the owner has since deleted. Reads fail as ErrDayAutoFillCheckFailed and
// writes as ErrDayAutoFillApplyFailed, as the fill after a period start does.
func (service *DayService) topUpRunningPeriodGap(ctx context.Context, userID uint, dayStart time.Time, wasPeriod bool, autoPeriodFillEnabled bool, flow string, now time.Time, location *time.Location) error {
	if !autoPeriodFillEnabled || wasPeriod {
		return nil
	}
	if location == nil {
		location = time.UTC
	}

	gap, err := service.planRunningPeriodGap(ctx, userID, dayStart, now, location)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillCheckFailed, err)
	}
	for _, targetDay := range gap {
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

func (service *DayService) planRunningPeriodGap(ctx context.Context, userID uint, dayStart time.Time, now time.Time, location *time.Location) ([]time.Time, error) {
	userSettings, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	logs, err := service.logs.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	_, periodLength, _ := resolveUserCycleLengths(&userSettings)
	return runningPeriodGapDays(&userSettings, logs, CalendarDay(dayStart, location), periodLength, now, location), nil
}

// runningPeriodGapDays names the calendar days a newly marked period day fills
// behind it: those strictly between the last period day before day and day, none
// of them after the owner's local today. The today bound is the one on logs: a day
// after today is not part of any episode here, so a future-dated day fills nothing
// rather than only its past-today part. It answers nothing unless day belongs to
// the newest bleeding episode up to today and lies within periodLength days of
// that episode's start, so a day of an older period, or one the configured length
// no longer covers, fills nothing. logs must already hold day as a period day.
//
// The start is the episode's first non-spotting day, or its explicit cycle-start
// mark when it carries one. That is deliberately not the CycleBoundaries anchor:
// the anchor does not open a cycle on a lone period day older than yesterday,
// which is exactly the day-one-then-a-later-day shape this top-up serves. The
// result bounds the catch-up window only and is never read as where a cycle
// starts. The returned days still have to be checked for a row at write time.
func runningPeriodGapDays(user *models.User, logs []models.DailyLog, day time.Time, periodLength int, now time.Time, location *time.Location) []time.Time {
	today := DateAtLocation(now, location)
	days := periodDaysOf(filterLogsNotAfter(logs, today))
	if onboarding := OnboardingBoundaryDay(BoundaryContextFor(user, today)); !onboarding.IsZero() {
		days = insertPeriodDay(days, periodDay{day: onboarding, explicit: true})
	}
	clusters := clusterPeriodDays(days)
	if len(clusters) == 0 {
		return nil
	}

	running := clusters[len(clusters)-1]
	if CalendarDaysBetween(running.Start, day) < 0 || CalendarDaysBetween(day, running.End) < 0 {
		return nil
	}
	start, ok := running.firstEpisodeDay()
	if !ok {
		return nil
	}
	offset := CalendarDaysBetween(start, day)
	if offset <= 0 || offset >= periodLength {
		return nil
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
	return gap
}

// firstEpisodeDay is the day the top-up counts the episode from: the first
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
