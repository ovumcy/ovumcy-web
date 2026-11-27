package services

// CycleStartMissing reports an account with no cycle start at all: the
// onboarding date was skipped, or clear-data removed every period day and the
// stored start with it. No prediction can be anchored there, so the owner pages
// show one shared notice asking for the date instead of an empty or broken
// surface. stats.LastPeriodStart is read rather than the stored onboarding
// start, so the notice goes away as soon as a period day is logged anywhere.
func CycleStartMissing(stats CycleStats) bool {
	return stats.LastPeriodStart.IsZero()
}
