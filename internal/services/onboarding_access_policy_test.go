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
		{name: "nested onboarding path", path: "/api/v1/onboarding/steps/1", want: true},
		{name: "trimmed onboarding path", path: " /api/v1/onboarding/steps/2 ", want: true},
		{name: "non onboarding path", path: "/dashboard", want: false},
		{name: "similar prefix only", path: "/onboardingx", want: false},
		// Adversarial rows. The prefix is matched on the exact string the
		// router matches, so each of these decides an access gate:
		//   - the bare collection path has no trailing slash and is not the
		//     page path, so it is NOT an onboarding path and stays gated;
		//   - the match is case-sensitive, so an upper-cased variant stays
		//     gated too — the fail-closed direction in both cases;
		//   - a traversal segment still reads as the onboarding prefix. It is
		//     not an access bypass, because Fiber routes on this same
		//     unnormalized value and no other route matches it, but it is the
		//     one row here that fails OPEN. If path cleaning is ever introduced
		//     upstream, this row is the decision to revisit.
		//
		// That last row is half an argument: it pins what this string function
		// answers, and the safety rests on what the ROUTER does with the same
		// string — which lives outside this repository and cannot be seen from
		// here. The other half is
		// TestRouterDoesNotNormalizeATraversalSegmentIntoAGatedRoute
		// (internal/api), which fails the release the assumption stops holding.
		// Change either row without the other and the pair stops meaning
		// anything.
		{name: "bare collection path", path: "/api/v1/onboarding", want: false},
		{name: "upper cased prefix", path: "/API/v1/onboarding/steps/1", want: false},
		{name: "traversal segment under the prefix", path: "/api/v1/onboarding/../days/2026-03-01", want: true},
	}

	for _, testCase := range tests {

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
		{name: "api day endpoint requires onboarding gate", path: "/api/v1/days/2026-03-01", want: true},
		{name: "onboarding page bypasses onboarding gate", path: "/onboarding", want: false},
		{name: "onboarding step bypasses onboarding gate", path: "/api/v1/onboarding/steps/1", want: false},
		{name: "logout api bypasses onboarding gate", path: "/api/v1/sessions/current", want: false},
		// The same adversarial set, read as the gate decision it drives: the
		// two fail-closed rows must still be gated, and the traversal row is
		// the one that is not.
		{name: "bare collection path requires onboarding gate", path: "/api/v1/onboarding", want: true},
		{name: "upper cased prefix requires onboarding gate", path: "/API/v1/onboarding/steps/1", want: true},
		{name: "traversal segment bypasses onboarding gate", path: "/api/v1/onboarding/../days/2026-03-01", want: false},
	}

	for _, testCase := range tests {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := ShouldEnforceOnboardingAccess(testCase.path); got != testCase.want {
				t.Fatalf("ShouldEnforceOnboardingAccess(%q) = %v, want %v", testCase.path, got, testCase.want)
			}
		})
	}
}
