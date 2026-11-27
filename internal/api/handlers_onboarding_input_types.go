package api

import "time"

type onboardingStep1Input struct {
	LastPeriodStart string `json:"last_period_start" form:"last_period_start"`
}

type onboardingStep1Values struct {
	Start time.Time
	// Skip is set when the owner skipped the date; Start is then the zero time
	// and must not be read.
	Skip bool
}

// onboardingSkipFieldName answers step 1 with "I'll add it later". Step 1 reads
// it as the owner's answer (it outranks a date sent beside it), and the step-2
// submit and the completion endpoint read it again, because the skip itself is
// not stored.
const onboardingSkipFieldName = "skip_last_period"

type onboardingSkipInput struct {
	SkipLastPeriod bool `json:"skip_last_period"`
}

type onboardingStep2Input struct {
	CycleLength    int    `json:"cycle_length" form:"cycle_length"`
	PeriodLength   int    `json:"period_length" form:"period_length"`
	AutoPeriodFill bool   `json:"auto_period_fill" form:"auto_period_fill"`
	IrregularCycle bool   `json:"irregular_cycle" form:"irregular_cycle"`
	UsageGoal      string `json:"usage_goal" form:"usage_goal"`
}
