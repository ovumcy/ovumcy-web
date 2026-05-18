package services

import "testing"

func TestIsOnboardingPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "exact onboarding path", path: "/onboarding", want: true},
		{name: "nested onboarding path", path: "/onboarding/step1", want: true},
		{name: "trimmed onboarding path", path: " /onboarding/step2 ", want: true},
		{name: "non onboarding path", path: "/dashboard", want: false},
		{name: "similar prefix only", path: "/onboardingx", want: false},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := IsOnboardingPath(testCase.path); got != testCase.want {
				t.Fatalf("IsOnboardingPath(%q) = %v, want %v", testCase.path, got, testCase.want)
			}
		})
	}
}

func TestShouldEnforceOnboardingAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "dashboard requires onboarding gate", path: "/dashboard", want: true},
		{name: "api day endpoint requires onboarding gate", path: "/api/days/2026-03-01", want: true},
		{name: "onboarding page bypasses onboarding gate", path: "/onboarding", want: false},
		{name: "onboarding step bypasses onboarding gate", path: "/onboarding/step1", want: false},
		{name: "logout api bypasses onboarding gate", path: "/api/v1/sessions/current", want: false},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := ShouldEnforceOnboardingAccess(testCase.path); got != testCase.want {
				t.Fatalf("ShouldEnforceOnboardingAccess(%q) = %v, want %v", testCase.path, got, testCase.want)
			}
		})
	}
}
