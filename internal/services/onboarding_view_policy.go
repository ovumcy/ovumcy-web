package services

import (
	"strconv"
	"strings"
)

func ResolveOnboardingStep(raw string) int {
	step, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	if step < 0 {
		return 0
	}
	if step > 3 {
		return 3
	}
	return step
}
