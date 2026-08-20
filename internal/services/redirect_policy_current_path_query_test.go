package services

import "testing"

// TestSanitizeCurrentPathQuery pins the allowlist that decides which query
// parameters may be echoed back into a rendered page's own address. The path
// half is returned untouched — that half belongs to SanitizeRedirectPath — and
// everything the pages do not read is dropped whatever its value, because the
// rendered path reaches the markup of every page and the outgoing /privacy link.
func TestSanitizeCurrentPathQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "no query is returned unchanged", raw: "/dashboard", want: "/dashboard"},
		{name: "empty query drops the separator", raw: "/dashboard?", want: "/dashboard"},
		{name: "calendar month is allowlisted", raw: "/calendar?month=2026-02", want: "/calendar?month=2026-02"},
		{
			name: "every calendar parameter survives together",
			raw:  "/calendar?month=2026-02&day=2026-02-17&selected=2026-02-17&edit=1",
			want: "/calendar?day=2026-02-17&edit=1&month=2026-02&selected=2026-02-17",
		},
		{name: "onboarding step is allowlisted", raw: "/onboarding?step=2", want: "/onboarding?step=2"},
		{name: "privacy back is allowlisted", raw: "/privacy?back=%2Fdashboard", want: "/privacy?back=%2Fdashboard"},
		{name: "email is dropped", raw: "/login?email=victim%40example.com", want: "/login"},
		{name: "unencoded email is dropped", raw: "/login?email=victim@example.com", want: "/login"},
		{name: "error code is dropped", raw: "/login?error=invalid_credentials", want: "/login"},
		{name: "token is dropped", raw: "/reset-password?token=abcdef", want: "/reset-password"},
		{
			name: "an allowlisted parameter does not carry its neighbours",
			raw:  "/calendar?email=victim%40example.com&month=2026-02&error=invalid",
			want: "/calendar?month=2026-02",
		},
		// An empty value matches no consumer's shape, and the consumer reads an
		// absent month exactly as it reads an empty one, so it is dropped.
		{name: "an allowlisted key present with an empty value is dropped", raw: "/calendar?month=", want: "/calendar"},
		{name: "a dropped key present with an empty value is dropped", raw: "/login?email=", want: "/login"},
		{name: "a valueless dropped key is dropped", raw: "/login?email", want: "/login"},
		{
			name: "a repeated allowlisted key keeps every occurrence in order",
			raw:  "/calendar?month=2026-02&month=2026-03",
			want: "/calendar?month=2026-02&month=2026-03",
		},
		{name: "a repeated dropped key is dropped", raw: "/login?email=a%40b.c&email=d%40e.f", want: "/login"},
		// One good occurrence must not rescue a crafted one: keeping the half
		// that matches would leave the caller choosing which survives.
		{
			name: "a repeated allowlisted key with one hostile occurrence is dropped whole",
			raw:  "/calendar?month=2026-08&month=victim%40example.com",
			want: "/calendar",
		},
		// Fail closed: a query Go's parser rejects is never echoed back raw,
		// or an unparsable parameter would pass through unfiltered.
		{name: "malformed escape falls back to the bare path", raw: "/login?email=%zz", want: "/login"},
		{name: "malformed escape beside an allowlisted key still fails closed", raw: "/calendar?month=2026-02&email=%zz", want: "/calendar"},
		{name: "semicolon separator falls back to the bare path", raw: "/login?month=2026-02;email=victim%40example.com", want: "/login"},
		{name: "an empty input stays empty", raw: "", want: ""},
		// `back` carries a path of its own, so a name-only allowlist would wave
		// a smuggled query through inside an allowed value.
		{
			name: "an allowlisted parameter inside back survives",
			raw:  "/privacy?back=%2Fcalendar%3Fmonth%3D2026-08",
			want: "/privacy?back=%2Fcalendar%3Fmonth%3D2026-08",
		},
		{
			name: "a smuggled email inside back is dropped while back survives",
			raw:  "/privacy?back=%2Flogin%3Femail%3Dvictim%40example.com",
			want: "/privacy?back=%2Flogin",
		},
		{
			name: "an unparsable inner query drops back entirely",
			raw:  "/privacy?back=%2Flogin%3Femail%3D%25zz",
			want: "/privacy",
		},
		{
			name: "a back nested inside a back is dropped rather than recursed",
			raw:  "/privacy?back=%2Fprivacy%3Fback%3D%252Fdashboard",
			want: "/privacy?back=%2Fprivacy",
		},
		// A fragment lives in the path half, so cutting at "?" alone leaves it
		// whole. A browser never sends one to the server, but `back` is a
		// caller-controlled value that runs through this same function, so the
		// fragment is a live carrier there.
		{name: "a fragment is dropped", raw: "/login#email=victim@example.com", want: "/login"},
		{name: "a fragment after a query is dropped", raw: "/calendar?month=2026-08#email=victim@example.com", want: "/calendar?month=2026-08"},
		{
			name: "a fragment smuggled inside back is dropped",
			raw:  "/privacy?back=%2Flogin%23email%3Dvictim%40example.com",
			want: "/privacy?back=%2Flogin",
		},
		// An allowlist over names alone lets any value through under an allowed
		// key, which is the same leak wearing a different key. Each value is
		// therefore checked against the shape its own consumer accepts.
		{name: "a hostile step value is dropped", raw: "/login?step=victim@example.com", want: "/login"},
		{name: "an out-of-range step is dropped", raw: "/onboarding?step=7", want: "/onboarding"},
		{name: "a hostile month value is dropped", raw: "/calendar?month=victim@example.com", want: "/calendar"},
		{name: "a malformed month is dropped", raw: "/calendar?month=2026-13", want: "/calendar"},
		{name: "a hostile day value is dropped", raw: "/calendar?day=victim@example.com", want: "/calendar"},
		{name: "a hostile selected value is dropped", raw: "/calendar?selected=victim@example.com", want: "/calendar"},
		{name: "a hostile edit value is dropped", raw: "/calendar?edit=victim@example.com", want: "/calendar"},
		{
			name: "a hostile value under an allowlisted key does not survive beside a good one",
			raw:  "/calendar?month=2026-08&day=victim%40example.com",
			want: "/calendar?month=2026-08",
		},
		{
			name: "a hostile value smuggled under an allowlisted key inside back is dropped",
			raw:  "/privacy?back=%2Fcalendar%3Fmonth%3Dvictim%40example.com",
			want: "/privacy?back=%2Fcalendar",
		},
		// `back` must itself look like a local in-app path; every registered
		// route is ASCII letters, digits, "/", "-", "_" and ".".
		{name: "a back value that is not a path is dropped", raw: "/privacy?back=victim@example.com", want: "/privacy"},
		{name: "a back path outside the route character set is dropped", raw: "/privacy?back=%2Fvictim%40example.com", want: "/privacy"},
		{name: "an absolute back url is dropped", raw: "/privacy?back=https%3A%2F%2Fevil.example", want: "/privacy"},
		{name: "a protocol-relative back is dropped", raw: "/privacy?back=%2F%2Fevil.example", want: "/privacy"},
		// Positive controls: the fix must not degenerate into dropping
		// everything the pages actually navigate with.
		{name: "a real month survives", raw: "/calendar?month=2026-08", want: "/calendar?month=2026-08"},
		{name: "a real selected date survives", raw: "/calendar?selected=2026-08-14", want: "/calendar?selected=2026-08-14"},
		{name: "a real day survives", raw: "/calendar?day=2026-08-14", want: "/calendar?day=2026-08-14"},
		{name: "edit=true survives", raw: "/calendar?edit=true", want: "/calendar?edit=true"},
		{name: "edit=1 survives", raw: "/calendar?edit=1", want: "/calendar?edit=1"},
		{name: "onboarding step 1 survives", raw: "/onboarding?step=1", want: "/onboarding?step=1"},
		{name: "onboarding step 2 survives", raw: "/onboarding?step=2", want: "/onboarding?step=2"},
		{
			name: "a real back carrying a real month survives intact",
			raw:  "/privacy?back=%2Fcalendar%3Fmonth%3D2026-08",
			want: "/privacy?back=%2Fcalendar%3Fmonth%3D2026-08",
		},
		// Each shape's reject branch, walked by a value a reader can justify
		// rather than by one chosen to touch a line.
		{name: "a full date is not a month anchor", raw: "/calendar?month=2026-08-14", want: "/calendar"},
		{name: "a bare year is not a month anchor", raw: "/calendar?month=2026", want: "/calendar"},
		{name: "an impossible day is refused though its shape is right", raw: "/calendar?day=2026-02-31", want: "/calendar"},
		{name: "a month anchor is not a day", raw: "/calendar?selected=2026-08", want: "/calendar"},
		{name: "edit=0 is outside the truthy set", raw: "/calendar?edit=0", want: "/calendar"},
		{name: "edit=maybe is outside the truthy set", raw: "/calendar?edit=maybe", want: "/calendar"},
		{name: "step=0 is below the reachable range", raw: "/onboarding?step=0", want: "/onboarding"},
		{name: "a negative step is refused", raw: "/onboarding?step=-1", want: "/onboarding"},
		{name: "a non-numeric step is refused", raw: "/onboarding?step=two", want: "/onboarding"},
		// isLocalRoutePathShape's own reject branches: a character no registered
		// route can contain, and the empty path half.
		{name: "a back path with a space is not a route", raw: "/privacy?back=%2Fcalendar%20day", want: "/privacy"},
		{name: "a back path with a route-template colon is refused", raw: "/privacy?back=%2Fcalendar%2Fday%2F%3Adate", want: "/privacy"},
		{name: "an empty back value is dropped", raw: "/privacy?back=", want: "/privacy"},
		// Case-insensitive truthy spellings still survive, as ParseBoolLike reads them.
		{name: "edit=TRUE survives", raw: "/calendar?edit=TRUE", want: "/calendar?edit=TRUE"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := SanitizeCurrentPathQuery(testCase.raw); got != testCase.want {
				t.Fatalf("SanitizeCurrentPathQuery(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

// TestSanitizeCurrentPathQueryIsDeterministic pins the ordering guarantee: two
// callers rendering the same request must produce byte-identical markup, so the
// retained parameters are emitted in a sorted order rather than a map's.
func TestSanitizeCurrentPathQueryIsDeterministic(t *testing.T) {
	t.Parallel()

	// Comparing each result to the FIRST one would be satisfied by the identity
	// function, so the expected bytes are written out: the parameters arrive in
	// one order and must always be emitted in another, sorted one. Go randomizes
	// map iteration per range statement, so repeating the call is what turns an
	// unsorted implementation from flaky into reliably red.
	const raw = "/calendar?selected=2026-02-17&month=2026-02&edit=1&day=2026-02-17"
	const want = "/calendar?day=2026-02-17&edit=1&month=2026-02&selected=2026-02-17"

	for attempt := range 64 {
		if got := SanitizeCurrentPathQuery(raw); got != want {
			t.Fatalf("attempt %d: SanitizeCurrentPathQuery(%q) = %q, want %q", attempt, raw, got, want)
		}
	}
}

// TestEveryValueMatchesShapeRefusesAMissingShape pins the fail-closed guard for
// a shape that is absent rather than unsatisfied. Nothing in
// currentPathQueryShapes is nil today, so the state is constructed here
// directly instead of being reached through a request — the guard exists for a
// later edit that adds a key and forgets its shape, where calling the nil func
// would panic on a request path rather than dropping the parameter.
func TestEveryValueMatchesShapeRefusesAMissingShape(t *testing.T) {
	t.Parallel()

	if everyValueMatchesShape([]string{"2026-02"}, nil) {
		t.Fatal("expected a missing shape to refuse the value, not accept it")
	}
	if everyValueMatchesShape(nil, nil) {
		t.Fatal("expected a missing shape to refuse even an empty value list")
	}
}

// TestCurrentPathQueryShapesHasNoMissingShape is the companion anchor: it proves
// the guard above is defending an invariant that currently holds, so a nil entry
// added later is caught here as well as absorbed there.
func TestCurrentPathQueryShapesHasNoMissingShape(t *testing.T) {
	t.Parallel()

	if len(currentPathQueryShapes) == 0 {
		t.Fatal("expected the shape map to be populated")
	}
	for key, matchesShape := range currentPathQueryShapes {
		if matchesShape == nil {
			t.Fatalf("query parameter %q is allowlisted with no shape to check its value against", key)
		}
	}
	if _, present := currentPathQueryShapes[currentPathBackParameter]; present {
		t.Fatal("back must not sit in the shape map: its policy rewrites the value rather than judging it")
	}
}
