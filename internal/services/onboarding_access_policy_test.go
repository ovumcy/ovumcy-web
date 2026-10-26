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
		//   - a traversal segment still reads as the onboarding prefix — the
		//     one row here that fails OPEN. It is not an access bypass because
		//     ShouldEnforceOnboardingAccess refuses any path that is not
		//     canonical, or that the router rewrote, before this prefix is
		//     consulted; a router that removes the segment would otherwise
		//     dispatch the request to a gated route.
		//
		// That last row is half an argument: the gate's half is pinned by
		// TestShouldEnforceOnboardingAccess below and, through the real
		// middleware and router, by
		// TestOnboardingGateRefusesATraversalSegmentWhateverTheRouterMakesOfIt
		// (internal/api).
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

	// wire is the path the request arrived with and path the one the router
	// dispatched on; an empty wire means the router left the path unchanged.
	tests := []struct {
		name string
		wire string
		path string
		want bool
	}{
		{name: "dashboard requires onboarding gate", path: "/dashboard", want: true},
		{name: "api day endpoint requires onboarding gate", path: "/api/v1/days/2026-03-01", want: true},
		{name: "onboarding page bypasses onboarding gate", path: "/onboarding", want: false},
		{name: "onboarding step bypasses onboarding gate", path: "/api/v1/onboarding/steps/1", want: false},
		{name: "logout api bypasses onboarding gate", path: "/api/v1/sessions/current", want: false},
		{name: "logout page route bypasses onboarding gate", path: "/logout", want: false},
		{name: "logout lookalike requires onboarding gate", path: "/logoutx", want: true},
		{name: "logout reached through a rewritten path requires onboarding gate", wire: "/./logout", path: "/logout", want: true},
		{name: "root path requires onboarding gate", path: "/", want: true},
		// The same adversarial set, read as the gate decision it drives: every
		// row fails closed. The traversal row is the one IsOnboardingPath still
		// reads as an onboarding path, and the gate refuses it before the prefix
		// is ever consulted.
		{name: "bare collection path requires onboarding gate", path: "/api/v1/onboarding", want: true},
		{name: "upper cased prefix requires onboarding gate", path: "/API/v1/onboarding/steps/1", want: true},
		{name: "traversal segment requires onboarding gate", path: "/api/v1/onboarding/../days/2026-03-01", want: true},
		{name: "trailing traversal segment requires onboarding gate", path: "/api/v1/onboarding/steps/1/..", want: true},
		{name: "dot segment requires onboarding gate", path: "/api/v1/onboarding/./steps/1", want: true},
		{name: "empty segment requires onboarding gate", path: "/api/v1/onboarding//steps/1", want: true},
		{name: "relative path requires onboarding gate", path: "api/v1/onboarding/steps/1", want: true},
		{name: "empty path requires onboarding gate", path: "", want: true},
		// A router that normalizes before matching hands the gate a clean
		// routed path. Clean or not, a routed path that is not the wire path is
		// refused: these rows land on exempt routes and are still gated.
		{name: "normalized traversal onto a gated route requires onboarding gate", wire: "/api/v1/onboarding/../days/2026-03-01", path: "/api/v1/days/2026-03-01", want: true},
		{name: "decoded dot segments onto a gated route require onboarding gate", wire: "/api/v1/onboarding/%2e%2e/days/2026-03-01", path: "/api/v1/days/2026-03-01", want: true},
		{name: "normalized dot segment onto an exempt route requires onboarding gate", wire: "/api/v1/onboarding/./steps/1", path: "/api/v1/onboarding/steps/1", want: true},
		{name: "normalized dot segment onto the onboarding page requires onboarding gate", wire: "/./onboarding", path: "/onboarding", want: true},
	}

	for _, testCase := range tests {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wire := testCase.wire
			if wire == "" {
				wire = testCase.path
			}
			if got := ShouldEnforceOnboardingAccess(wire, testCase.path); got != testCase.want {
				t.Fatalf("ShouldEnforceOnboardingAccess(%q, %q) = %v, want %v", wire, testCase.path, got, testCase.want)
			}
		})
	}
}
