package api

import "time"

type onboardingStep1Input struct {
	LastPeriodStart string `json:"last_period_start" form:"last_period_start"`
}

type onboardingStep1Values struct {
	Start time.Time
}

type onboardingStep2Input struct {
	CycleLength    int    `json:"cycle_length" form:"cycle_length"`
	PeriodLength   int    `json:"period_length" form:"period_length"`
	AutoPeriodFill bool   `json:"auto_period_fill" form:"auto_period_fill"`
	IrregularCycle bool   `json:"irregular_cycle" form:"irregular_cycle"`
	UsageGoal      string `json:"usage_goal" form:"usage_goal"`
}
