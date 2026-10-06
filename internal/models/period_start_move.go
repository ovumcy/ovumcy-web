package models

import "time"

// PeriodStartMove is the day-log half of moving users.last_period_start in
// Settings, written in the same transaction as the settings columns. The
// service decides it; the repository only carries it out.
//
// ClearDays are the days the old start's auto-fill wrote: on each, a row is
// deleted only when Clearable reports it untouched, so a day the owner edited
// survives. FillDays get a bare period row each, the shape onboarding writes,
// only where no row exists. A non-period row on MarkDay (the new start) becomes
// a period day; zero means none.
type PeriodStartMove struct {
	ClearDays []time.Time
	Clearable func(DailyLog) bool
	FillDays  []time.Time
	MarkDay   time.Time
}
