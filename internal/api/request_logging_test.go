package api

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeLogError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil error yields empty string", nil, ""},
		{"generic message is preserved", errors.New("internal server error"), "internal server error"},
		{"email is masked", errors.New("lookup failed for owner@example.com"), "lookup failed for :email"},
		{"opaque token is masked", errors.New("token AbCdEf0123456789AbCdEf012345 rejected"), "token :token rejected"},
		{"surrounding whitespace trimmed", errors.New("  boom  "), "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeLogError(tc.err); got != tc.want {
				t.Fatalf("SafeLogError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestSafeLogErrorMasksSecretShapesAndKeepsDiagnostics pins the full redaction
// contract of the Fiber request log's error field, which is the always-on net
// that keeps a secret out of the log when a handler returns an error carrying
// user input.
//
// The masked rows cover every secret shape this app can put in front of it. Two
// of them — the calendar-feed token and the base32 TOTP secret — are long enough
// for the length-based opaque rule to see. The other two are not: a recovery
// code is 19 characters and a submitted TOTP code is 6, so both sit below the
// 24-character opaque floor and reached the log verbatim until the shape-specific
// rules landed. Length alone therefore cannot be the whole defense, and lowering
// the floor is not the fix.
//
// That is what the preserved rows exist to prove. This field is the operator's
// only diagnostic signal for a failed request, and `SafeRequestLogPath` writes
// route templates into the neighbouring column, so a rule broad enough to swallow
// an ISO date, an HTTP status, a route template, ordinary prose or a long numeric
// id would trade one defect for another. Each preserved row names a value that a
// generic short-run rule would have destroyed.
func TestSafeLogErrorMasksSecretShapesAndKeepsDiagnostics(t *testing.T) {
	t.Parallel()

	// Shape-accurate synthetic values drawn from the alphabets the real
	// generators use: the feed token is selector(16)+verifier(32) over the
	// unambiguous 32-symbol alphabet, and the TOTP secret is 32 base32
	// characters. Neither authenticates anything.
	const feedToken = "ABCDEFGHJKLMNPQR234567892345678923456789ABCDEFGH"
	const totpSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

	cases := []struct {
		name string
		// message is the error text handed to SafeLogError.
		message string
		// secret, when set, must not survive anywhere in the output.
		secret string
		want   string
	}{
		{
			name:    "issued recovery code is masked",
			message: "rejected value OVUM-A1B2-C3D4-E5F6",
			secret:  "OVUM-A1B2-C3D4-E5F6",
			want:    "rejected value :code",
		},
		{
			name:    "separator-free recovery code is masked",
			message: "rejected value OVUMA1B2C3D4E5F6",
			secret:  "OVUMA1B2C3D4E5F6",
			want:    "rejected value :code",
		},
		{
			name:    "lowercase recovery code is masked",
			message: "rejected value ovum-a1b2-c3d4-e5f6",
			secret:  "ovum-a1b2-c3d4-e5f6",
			want:    "rejected value :code",
		},
		{
			name:    "submitted six-digit code is masked",
			message: "totp challenge 481920 refused",
			secret:  "481920",
			want:    "totp challenge :code refused",
		},
		{
			name:    "calendar feed token is masked",
			message: "feed " + feedToken + " unknown",
			secret:  feedToken,
			want:    "feed :token unknown",
		},
		{
			name:    "base32 totp secret is masked",
			message: "secret " + totpSecret + " undecryptable",
			secret:  totpSecret,
			want:    "secret :token undecryptable",
		},
		{
			name:    "email is masked",
			message: "lookup failed for owner@example.com",
			secret:  "owner@example.com",
			want:    "lookup failed for :email",
		},
		{
			name:    "iso date survives",
			message: "no entry for 2026-07-26",
			want:    "no entry for 2026-07-26",
		},
		{
			name:    "http status survives",
			message: "upstream answered 503",
			want:    "upstream answered 503",
		},
		{
			name:    "ordinary sentence survives",
			message: "internal server error",
			want:    "internal server error",
		},
		{
			name:    "route template survives",
			message: "no handler for /api/v1/days/:date",
			want:    "no handler for /api/v1/days/:date",
		},
		{
			name:    "long numeric identifier survives",
			message: "body limit 16777216 exceeded at 1753574400",
			want:    "body limit 16777216 exceeded at 1753574400",
		},
		{
			name:    "ovum as a domain word survives",
			message: "ovum release estimate unavailable",
			want:    "ovum release estimate unavailable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := SafeLogError(errors.New(tc.message))
			if tc.secret != "" && strings.Contains(got, tc.secret) {
				t.Fatalf("SafeLogError leaked %q verbatim into the request log: %q", tc.secret, got)
			}
			if got != tc.want {
				t.Fatalf("SafeLogError(%q) = %q, want %q", tc.message, got, tc.want)
			}
		})
	}
}

// TestSanitizeRequestLogSegmentClassifies pins every branch of the per-segment
// classifier that decides how a raw path component is masked for the request
// log. Each row asserts the resulting placeholder for exactly one branch, so a
// negated `case` in sanitizeRequestLogSegment (a survivor class in the mutation
// report) flips at least one expectation. The classifier is the single point
// where a numeric id, uuid, date, email, or opaque token is redacted, so its
// branch behavior is a privacy contract, not incidental formatting.
func TestSanitizeRequestLogSegmentClassifies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		segment string
		want    string
	}{
		{"empty stays empty", "", ""},
		{"whitespace collapses to empty", "   ", ""},
		{"route param passthrough", ":date", ":date"},
		{"wildcard passthrough", "*", "*"},
		{"iso date becomes date placeholder", "2024-06-15", ":date"},
		{"numeric id becomes id placeholder", "42", ":id"},
		{"large numeric id becomes id placeholder", "18446744073709551615", ":id"},
		{"uuid becomes id placeholder", "123e4567-e89b-12d3-a456-426614174000", ":id"},
		{"email becomes email placeholder", "owner@example.com", ":email"},
		{"opaque token becomes token placeholder", "AbCdEf0123456789AbCdEf012345", ":token"},
		{"short plain word is preserved", "settings", "settings"},
		{"non-date same-length word is preserved", "abcd-ef-gh", "abcd-ef-gh"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeRequestLogSegment(tc.segment); got != tc.want {
				t.Fatalf("sanitizeRequestLogSegment(%q) = %q, want %q", tc.segment, got, tc.want)
			}
		})
	}
}

// TestIsDateRequestLogSegment pins the date shape used to redact ?date path
// components. The false-cases straddle the '0'..'9' digit boundary (a '/' just
// below '0', a ':' just above '9') and the fixed '-' separator positions, so a
// boundary/negation mutant on either comparison in isDateRequestLogSegment is
// observable as a shape that must NOT be treated as a date.
func TestIsDateRequestLogSegment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		segment string
		want    bool
	}{
		{"2024-06-15", true},
		{"0000-00-00", true},   // all '0': exercises the char < '0' boundary
		{"2019-09-29", true},   // carries '9's: exercises the char > '9' boundary
		{"2024-06-1", false},   // one char too short
		{"2024-06-150", false}, // one char too long
		{"2024/06-15", false},  // separator position holds a slash
		{"2024-0X-15", false},  // letter where a digit is required
		{"2024-06-1/", false},  // '/' is 0x2F, one below '0'
		{"2024-06-1:", false},  // ':' is 0x3A, one above '9'
		{"20240-6-15", false},  // '-' missing at index 4
	}

	for _, tc := range cases {
		t.Run(tc.segment, func(t *testing.T) {
			t.Parallel()
			if got := isDateRequestLogSegment(tc.segment); got != tc.want {
				t.Fatalf("isDateRequestLogSegment(%q) = %v, want %v", tc.segment, got, tc.want)
			}
		})
	}
}

// TestIsUUIDRequestLogSegment pins the canonical 8-4-4-4-12 hex-with-dashes
// shape. The negative rows exercise each hex sub-range boundary ('/'/':' around
// 0-9, backtick/'g' around a-f, '@'/'G' around A-F) and a misplaced separator,
// so every range comparison and the '-' position check in
// isUUIDRequestLogSegment has a distinguishing input.
func TestIsUUIDRequestLogSegment(t *testing.T) {
	t.Parallel()

	// Both boundary letters (a and f / A and F) appear at non-separator
	// positions so a `<= 'f'`/`<= 'F'` boundary shift to `< 'f'`/`< 'F'` (and the
	// `>= 'a'`/`>= 'A'` lower bounds) leaves a valid hex char failing the range →
	// detectable.
	const validLower = "faab4567-e89b-12d3-a456-4266141740cf"
	const validUpper = "FAAB4567-E89B-12D3-A456-4266141740CF"

	cases := []struct {
		name    string
		segment string
		want    bool
	}{
		{"lowercase hex", validLower, true},
		{"uppercase hex", validUpper, true},
		{"too short", validLower[:35], false},
		{"too long", validLower + "0", false},
		{"digit boundary below zero", strings.Replace(validLower, "1", "/", 1), false},
		{"digit boundary above nine", strings.Replace(validLower, "1", ":", 1), false},
		{"hex letter above f", strings.Replace(validLower, "e", "g", 1), false},
		{"hex letter above F", strings.Replace(validUpper, "E", "G", 1), false},
		{"char below lowercase a", strings.Replace(validLower, "a", "`", 1), false},
		{"char below uppercase A", strings.Replace(validUpper, "A", "@", 1), false},
		{"separator position holds hex", strings.Replace(validLower, "-", "x", 1), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isUUIDRequestLogSegment(tc.segment); got != tc.want {
				t.Fatalf("isUUIDRequestLogSegment(%q) = %v, want %v", tc.segment, got, tc.want)
			}
		})
	}
}

// TestIsNumericRequestLogSegment pins the numeric-id detector: the empty guard
// and the ParseUint result path (a leading sign or letter is not a bare
// unsigned id), so a negated empty check is caught.
func TestIsNumericRequestLogSegment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		segment string
		want    bool
	}{
		{"0", true},
		{"42", true},
		{"", false},
		{"-1", false},
		{"1a", false},
	}

	for _, tc := range cases {
		t.Run(tc.segment, func(t *testing.T) {
			t.Parallel()
			if got := isNumericRequestLogSegment(tc.segment); got != tc.want {
				t.Fatalf("isNumericRequestLogSegment(%q) = %v, want %v", tc.segment, got, tc.want)
			}
		})
	}
}

// TestIsOpaqueRequestLogSegment pins the opaque-token detector used to redact
// long random-looking components. The length rows straddle the 24-char minimum
// (23 rejected, 24 accepted) and the character rows straddle every allowed
// class ('/'/':' around 0-9, '@'/'[' and backtick/'{' around the letter
// ranges, plus the '-'/'_' allowance and a disallowed '.'), so each boundary
// and case in isOpaqueRequestLogSegment has a failing witness.
func TestIsOpaqueRequestLogSegment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		segment string
		want    bool
	}{
		{"exactly 24 chars accepted", strings.Repeat("a", 24), true},
		{"23 chars rejected", strings.Repeat("a", 23), false},
		{"long mixed token accepted", "AbCdEf0123456789AbCdEf012345", true},
		{"dash and underscore allowed", "abc-def_0123456789ABCDEFG", true},
		{"dot is disallowed", "abc.def0123456789ABCDEF012", false},
		{"space is disallowed", "abc def0123456789ABCDEF012", false},
		{"char below digit range", "abc/def0123456789ABCDEF012", false},
		{"char above digit range", "abc:def0123456789ABCDEF012", false},
		{"char above uppercase Z", "abc[def0123456789ABCDEF012", false},
		{"char above lowercase z", "abc{def0123456789ABCDEF012", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isOpaqueRequestLogSegment(tc.segment); got != tc.want {
				t.Fatalf("isOpaqueRequestLogSegment(%q) = %v, want %v", tc.segment, got, tc.want)
			}
		})
	}
}

// TestSanitizeRequestLogPathMasksMultiSegment pins the whole-path walk in
// sanitizeRequestLogPath: the leading index-0 skip is preserved, every later
// segment is classified, and the empty/prefix normalization keeps a leading
// slash. This is the exported redaction seam used before a raw path reaches a
// log line.
func TestSanitizeRequestLogPathMasksMultiSegment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"root stays root", "/", "/"},
		{"empty becomes root", "", "/"},
		{"missing leading slash gains one", "days/2024-06-15", "/days/:date"},
		{"ids and tokens masked", "/api/v1/days/42/owner@example.com", "/api/v1/days/:id/:email"},
		{"uuid masked mid-path", "/users/123e4567-e89b-12d3-a456-426614174000/edit", "/users/:id/edit"},
		{"plain segments preserved", "/settings/profile", "/settings/profile"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeRequestLogPath(tc.path); got != tc.want {
				t.Fatalf("sanitizeRequestLogPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
