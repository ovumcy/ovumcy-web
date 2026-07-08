//go:build gofuzz

package services

// ClusterFuzzLite / libFuzzer harnesses. These mirror the native `go test -fuzz`
// targets in policy_fuzz_test.go one-to-one, but compile under the
// go-118-fuzz-build testing shim, because that tool cannot pick up native
// testing.F fuzzers that live in _test.go files. Built ONLY under the `gofuzz`
// tag, so the default build / test / vet / lint / coverage never see this file.
//
// policy_fuzz_test.go stays the source of truth for `go test -fuzz`; keep the
// seed corpora and behavioral oracles here in sync with it when they change
// (rarely — these wrap stable parsing/validation policy helpers).

import (
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AdamKorcz/go-118-fuzz-build/testing"
)

// codecov:ignore:start -- compiled only under the gofuzz tag (libFuzzer engine),
// never in the default coverage run; the same behavior is covered by the native
// seed-corpus regressions in policy_fuzz_test.go.

func passwordOracleValid(password string) bool {
	var upper, lower, digit bool
	for _, r := range password {
		if unicode.IsUpper(r) {
			upper = true
		}
		if unicode.IsLower(r) {
			lower = true
		}
		if unicode.IsDigit(r) {
			digit = true
		}
	}
	return utf8.RuneCountInString(password) >= 8 &&
		len(password) <= maxPasswordBytes &&
		upper && lower && digit
}

func FuzzParseDayDate(f *testing.F) {
	for _, seed := range []string{
		"2024-02-29", " 2023-01-01 ", "", "not-a-date",
		"2024-13-40", "0000-01-01", "2024-02-30", "99999-01-01",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		loc := time.UTC
		got, err := ParseDayDate(raw, loc)
		if err != nil {
			return
		}
		if h, m, s := got.Clock(); h != 0 || m != 0 || s != 0 || got.Nanosecond() != 0 {
			t.Fatalf("ParseDayDate(%q) returned a non-midnight instant: %s", raw, got)
		}
		canonical := got.Format("2006-01-02")
		again, err := ParseDayDate(canonical, loc)
		if err != nil {
			t.Fatalf("re-parsing canonical %q failed: %v", canonical, err)
		}
		if !again.Equal(got) {
			t.Fatalf("ParseDayDate not a fixed point: %s != %s", again, got)
		}
	})
}

func FuzzParseDayRange(f *testing.F) {
	f.Add("2024-01-01", "2024-01-31")
	f.Add("2024-02-01", "2024-01-01")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, from, to string) {
		loc := time.UTC
		gotFrom, gotTo, err := ParseDayRange(from, to, loc)
		if err != nil {
			return
		}
		if gotTo.Before(gotFrom) {
			t.Fatalf("ParseDayRange(%q,%q) returned to<from: %s < %s", from, to, gotTo, gotFrom)
		}
	})
}

func FuzzValidatePasswordStrength(f *testing.F) {
	for _, seed := range []string{
		"Abcdef12", "short", "alllowercase", "ALLUPPER123",
		"nodigitsAB", "12345678", "Пароль123", "",
		"Aa1" + strings.Repeat("x", 68),
		"Aa1" + strings.Repeat("x", 70),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, password string) {
		err := ValidatePasswordStrength(password)
		wantValid := passwordOracleValid(password)

		switch {
		case wantValid && err != nil:
			t.Fatalf("ValidatePasswordStrength(%q) rejected a strong password: %v", password, err)
		case !wantValid && err == nil:
			t.Fatalf("ValidatePasswordStrength(%q) accepted a weak password", password)
		}

		if err == nil {
			extended := password + "xZ9"
			if len(extended) <= maxPasswordBytes {
				if err := ValidatePasswordStrength(extended); err != nil {
					t.Fatalf("appending characters made a strong password weak: %v", err)
				}
			}
		}
	})
}

func FuzzNormalizeAuthEmail(f *testing.F) {
	for _, seed := range []string{
		"User@Example.com", "  a@b.co ", "notanemail", "",
		"Имя@example.com", "a@b@c", "A@B.CO",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got := NormalizeAuthEmail(raw)
		if got == "" {
			return
		}
		if got != strings.ToLower(got) {
			t.Fatalf("NormalizeAuthEmail(%q) returned non-lowercase %q", raw, got)
		}
		if got != strings.TrimSpace(got) {
			t.Fatalf("NormalizeAuthEmail(%q) returned untrimmed %q", raw, got)
		}
		if _, err := mail.ParseAddress(got); err != nil {
			t.Fatalf("NormalizeAuthEmail(%q) returned unparseable %q", raw, got)
		}
		if again := NormalizeAuthEmail(got); again != got {
			t.Fatalf("NormalizeAuthEmail not idempotent: %q -> %q", got, again)
		}
	})
}

func FuzzNormalizeRecoveryCode(f *testing.F) {
	for _, seed := range []string{
		"OVUM-ABCD-EFGH-JKLM", "ovum abcd efgh jklm", "ABCDEFGHJKLM",
		"  OVUM-abcd-efgh-jklm  ", "short", "", "OVUMOVUMOVUMOVUM",
		"\xa8\xb2000\xd9",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got := NormalizeRecoveryCode(raw)
		if again := NormalizeRecoveryCode(got); again != got {
			t.Fatalf("NormalizeRecoveryCode not idempotent: %q -> %q -> %q", raw, got, again)
		}
	})
}

func FuzzSanitizeOnboardingCycleAndPeriod(f *testing.F) {
	for _, seed := range [][2]int{
		{28, 5}, {10, 20}, {200, 100}, {15, 14}, {90, 14}, {-5, -5}, {0, 0},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, cycle, period int) {
		gotCycle, gotPeriod := SanitizeOnboardingCycleAndPeriod(cycle, period)
		if gotCycle < 15 || gotCycle > 90 {
			t.Fatalf("cycle %d out of [15,90] for input (%d,%d)", gotCycle, cycle, period)
		}
		if gotPeriod < 1 || gotPeriod > 14 {
			t.Fatalf("period %d out of [1,14] for input (%d,%d)", gotPeriod, cycle, period)
		}
		if cap := MaxPeriodLengthForCycle(gotCycle); gotPeriod > cap {
			t.Fatalf("period %d exceeds per-cycle cap %d (cycle %d)", gotPeriod, cap, gotCycle)
		}
		c2, p2 := SanitizeOnboardingCycleAndPeriod(gotCycle, gotPeriod)
		if c2 != gotCycle || p2 != gotPeriod {
			t.Fatalf("not idempotent: (%d,%d) -> (%d,%d)", gotCycle, gotPeriod, c2, p2)
		}
	})
}

// codecov:ignore:end
