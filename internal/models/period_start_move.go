package models

import "time"

// PeriodStartMove is the day-log half of moving users.last_period_start in
// Settings, written in the same transaction as the settings columns. The
// service decides it; the repository only carries it out.
//
// The rows dated from ClearFrom up to (not including) ClearTo are read inside
// the transaction, ascending by date, and handed to ClearRows; every row it
// returns is deleted. Judging the rows as they stand in the transaction keeps a
// day edited after the service planned the move out of the delete. A nil
// ClearRows or a zero ClearFrom clears nothing. FillDays get a bare period row
// each, the shape onboarding writes, only where no row exists, all stamped with
// one creation time. A non-period row on MarkDay (the new start) becomes a
// period day carrying the cycle-start mark, and a missing row is created as
// one; zero means none (a start after the owner's today writes no day).
//
// ReleaseStart is the old start's date: after the clear above, the rows dated
// that day lose their cycle-start (and uncertain) mark, and a row ReleaseIsBare
// then judges to carry nothing else is deleted. A nil ReleaseIsBare deletes
// nothing; a zero ReleaseStart releases nothing.
type PeriodStartMove struct {
	ClearFrom     time.Time
	ClearTo       time.Time
	ClearRows     func([]DailyLog) []DailyLog
	FillDays      []time.Time
	MarkDay       time.Time
	ReleaseStart  time.Time
	ReleaseIsBare func(DailyLog) bool
}
