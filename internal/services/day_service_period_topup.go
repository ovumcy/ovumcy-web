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
// The history read is bounded to the days that can decide the answer: a cluster
// reaching further back than periodLength plus the cluster gap cannot start
// within periodLength days of dayStart, so the first period day inside that span
// already is past the window whenever the real start lies before it.
func (service *DayService) planRunningPeriod(ctx context.Context, userID uint, dayStart time.Time, wasPeriod bool, autoPeriodFillEnabled bool, periodLength int, now time.Time, location *time.Location) (runningPeriodWindow, error) {
	if !autoPeriodFillEnabled || wasPeriod || periodLength <= 1 {
		return runningPeriodWindow{}, nil
	}
	userSettings, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return runningPeriodWindow{}, err
	}
	day := CalendarDay(dayStart, location)
	today := DateAtLocation(now, location)
	from := AddCalendarDays(day, -(periodLength + periodClusterGapDays), location)
	logs, err := service.FetchLogsForUser(ctx, userID, from, today, location)
	if err != nil {
		return runningPeriodWindow{}, err
	}
	return runningPeriodWindowFor(&userSettings, logs, day, periodLength, now, location), nil
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
	today := DateAtLocation(now, location)
	days := periodDaysOf(filterLogsNotAfter(logs, today))
	if onboarding := OnboardingBoundaryDay(BoundaryContextFor(user, today)); !onboarding.IsZero() {
		days = insertPeriodDay(days, periodDay{day: onboarding, explicit: true})
	}
	clusters := clusterPeriodDays(days)
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
