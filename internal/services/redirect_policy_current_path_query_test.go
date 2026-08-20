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
		{name: "an allowlisted key present with an empty value is kept", raw: "/calendar?month=", want: "/calendar?month="},
		{name: "a dropped key present with an empty value is dropped", raw: "/login?email=", want: "/login"},
		{name: "a valueless dropped key is dropped", raw: "/login?email", want: "/login"},
		{
			name: "a repeated allowlisted key keeps every occurrence in order",
			raw:  "/calendar?month=2026-02&month=2026-03",
			want: "/calendar?month=2026-02&month=2026-03",
		},
		{name: "a repeated dropped key is dropped", raw: "/login?email=a%40b.c&email=d%40e.f", want: "/login"},
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

	raw := "/calendar?selected=2026-02-17&month=2026-02&edit=1&day=2026-02-17"
	first := SanitizeCurrentPathQuery(raw)

	for range 32 {
		if got := SanitizeCurrentPathQuery(raw); got != first {
			t.Fatalf("SanitizeCurrentPathQuery(%q) is not deterministic: %q then %q", raw, first, got)
		}
	}
}
