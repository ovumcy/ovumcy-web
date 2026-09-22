package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// These tests guard the re-auth timing oracle SEC-L3 closes: both
// ValidateCurrentPassword (the erasure gate, via VerifyReauthPassword) and
// ValidatePasswordChange (the password-change gate) must spend the same
// bcrypt-shaped work on the refusal decided by ACCOUNT STATE — "no local
// password" — as on a wrong-password compare, so an attacker already holding
// a session cannot tell the two apart by response latency.
//
// The refusals decided by the caller's own submission (blank field, mismatched
// confirmation) are deliberately NOT equalized: their latency discloses
// nothing, and neither draws down the reauthPolicy budget, so paying a
// full-cost bcrypt there would be uncapped CPU per request. The tests below
// pin both halves — the calls that must happen and the ones that must not.
//
// Per the repo's timing-equalization test discipline, this uses call counters
// and the shared bcryptWorkLedger (auth_service_timing_cost_topup_test.go),
// never a wall-clock threshold. The placeholder hash's bcrypt-compatibility is
// already pinned by TestCredentialsTimingEqualizationHashIsBcryptCompatible in
// auth_service_credentials_timing_test.go — both equalizers share the same
// constant, so that test doubles as this one's compatibility proof.

func withCountingSettingsReauthEqualizer(t *testing.T) *int {
	t.Helper()

	original := equalizeSettingsReauthTiming
	count := 0
	equalizeSettingsReauthTiming = func(string) {
		count++
	}
	t.Cleanup(func() {
		equalizeSettingsReauthTiming = original
	})
	return &count
}

// withSettingsReauthWorkLedger is withLoginWorkLedger's settings counterpart:
// it accounts the bcrypt work a refusal actually spends — every comparison the
// equalizer body and the top-up make, each at the cost of the hash it was
// handed — so two refusal paths can be compared as totals rather than as
// durations.
func withSettingsReauthWorkLedger(t *testing.T) *bcryptWorkLedger {
	t.Helper()
	ledger := &bcryptWorkLedger{}

	originalEqualize := equalizeSettingsReauthTiming
	equalizeSettingsReauthTiming = func(password string) {
		ledger.equalizerCalls++
		originalEqualize(password)
	}
	t.Cleanup(func() {
		equalizeSettingsReauthTiming = originalEqualize
	})

	withEqualizerCompareLedger(t, ledger)
	withTopUpCompareLedger(t, ledger)
	return ledger
}

// TestEqualizeSettingsReauthTimingSpendsThePlaceholderCompare covers the
// equalizer's own body, which every call-site test below replaces wholesale:
// the production equalizer must hand the submitted password to bcrypt against
// the shared placeholder hash — a compare against any other (for example an
// unparseable) hash returns before doing the cost-12 work the equalizer exists
// to spend.
func TestEqualizeSettingsReauthTimingSpendsThePlaceholderCompare(t *testing.T) {
	const submittedPassword = "AnyPass1!"
	recorded := withEqualizerCompareRecorder(t)

	equalizeSettingsReauthTiming(submittedPassword)

	assertEqualizerSpent(t, *recorded, []string{credentialsTimingEqualizationHash}, submittedPassword)
}

func TestValidateCurrentPasswordEqualizesTimingForNoLocalPassword(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidateCurrentPassword("", "AnyPass1!")

	if !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
		t.Fatalf("expected ErrSettingsLocalPasswordNotSet, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on the no-local-password path, got %d", *count)
	}
}

// TestValidateCurrentPasswordSpendsNothingOnBlankSubmission is the budget half
// of the invariant: a blank password is the caller's own input, so it must NOT
// buy a bcrypt. VerifyReauthPassword counts only ErrSettingsPasswordInvalid as
// a failure, so an equalized blank submission would be full-cost work no
// re-auth budget caps — repeatable at the /api limiter's rate.
//
// Both account states are driven, and that is the whole point: pinning only
// the hash-present one leaves a blank submission costing nothing there and a
// full bcrypt on an account with no local password, which is the account-state
// distinguisher this file exists to close — buyable with an empty body, by a
// caller that guesses no password at all.
func TestValidateCurrentPasswordSpendsNothingOnBlankSubmission(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		passwordHash string
	}{
		{name: "account with a local password", passwordHash: "ignored-hash"},
		{name: "account with no local password", passwordHash: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			count := withCountingSettingsReauthEqualizer(t)
			service := NewSettingsService(nil)

			err := service.ValidateCurrentPassword(testCase.passwordHash, "   ")

			if !errors.Is(err, ErrSettingsPasswordMissing) {
				t.Fatalf("expected ErrSettingsPasswordMissing, got %v", err)
			}
			if *count != 0 {
				t.Fatalf("expected no equalization call on the blank-submission path, got %d", *count)
			}
		})
	}
}

// TestValidateCurrentPasswordDoesNotEqualizeOnRealCompare pins the
// counter-based tests to the branch they claim to cover: once a real hash and a
// non-blank password reach bcrypt.CompareHashAndPassword, the equalizer must
// NOT also run — the real compare already spends the cost, and double-spending
// it would mask a regression that removed the real compare entirely.
func TestValidateCurrentPasswordDoesNotEqualizeOnRealCompare(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if err := service.ValidateCurrentPassword(string(passwordHash), "WrongPass1"); !errors.Is(err, ErrSettingsPasswordInvalid) {
		t.Fatalf("expected ErrSettingsPasswordInvalid, got %v", err)
	}
	if *count != 0 {
		t.Fatalf("expected 0 equalization calls once the real compare runs, got %d", *count)
	}
}

// TestValidatePasswordChangeDoesNotEqualizeOnRealCompare is the same guard for
// the password-change gate: without it, an equalizer call added after the real
// compare there would double-spend unnoticed.
func TestValidatePasswordChangeDoesNotEqualizeOnRealCompare(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if err := service.ValidatePasswordChange(string(passwordHash), "WrongPass1", "NewPass1", "NewPass1"); !errors.Is(err, ErrSettingsInvalidCurrentPassword) {
		t.Fatalf("expected ErrSettingsInvalidCurrentPassword, got %v", err)
	}
	if *count != 0 {
		t.Fatalf("expected 0 equalization calls once the real compare runs, got %d", *count)
	}
}

func TestValidatePasswordChangeSpendsNothingOnInvalidInput(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidatePasswordChange("hash", " ", "NewPass1", "NewPass1")

	if !errors.Is(err, ErrSettingsPasswordChangeInvalidInput) {
		t.Fatalf("expected ErrSettingsPasswordChangeInvalidInput, got %v", err)
	}
	if *count != 0 {
		t.Fatalf("expected no equalization call on the invalid-input path, got %d", *count)
	}
}

func TestValidatePasswordChangeSpendsNothingOnMismatch(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidatePasswordChange("hash", "StrongPass1", "NewPass1", "OtherPass1")

	if !errors.Is(err, ErrSettingsPasswordMismatch) {
		t.Fatalf("expected ErrSettingsPasswordMismatch, got %v", err)
	}
	if *count != 0 {
		t.Fatalf("expected no equalization call on the mismatch path, got %d", *count)
	}
}

func TestValidatePasswordChangeEqualizesTimingForNoLocalPassword(t *testing.T) {
	count := withCountingSettingsReauthEqualizer(t)
	service := NewSettingsService(nil)

	err := service.ValidatePasswordChange("", "StrongPass1", "NewPass1", "NewPass1")

	if !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
		t.Fatalf("expected ErrSettingsLocalPasswordNotSet, got %v", err)
	}
	if *count != 1 {
		t.Fatalf("expected exactly 1 bcrypt equalization call on the no-local-password path, got %d", *count)
	}
}

// TestSettingsReauthRefusalsSpendEqualWorkAcrossStoredCosts is the arithmetic
// leg the call counters cannot reach: the equalized no-local-password refusal
// and a wrong-password refusal must cost the SAME total bcrypt work, including
// when the account's stored hash predates passwordHashCost. Without the top-up
// on the real compare, a cost-10 account answers a wrong password with a
// quarter of the work the equalized branch spends, reopening the same oracle
// from the other side.
func TestSettingsReauthRefusalsSpendEqualWorkAcrossStoredCosts(t *testing.T) {
	service := NewSettingsService(nil)

	noLocalPassword := func() int64 {
		ledger := withSettingsReauthWorkLedger(t)
		if err := service.ValidateCurrentPassword("", "WrongPass1"); !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
			t.Fatalf("expected ErrSettingsLocalPasswordNotSet, got %v", err)
		}
		return ledger.units
	}()

	for storedCost := bcrypt.MinCost; storedCost <= passwordHashCost; storedCost++ {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), storedCost)
		if err != nil {
			t.Fatalf("hash password at cost %d: %v", storedCost, err)
		}

		ledger := withSettingsReauthWorkLedger(t)
		if err := service.ValidateCurrentPassword(string(passwordHash), "WrongPass1"); !errors.Is(err, ErrSettingsPasswordInvalid) {
			t.Fatalf("expected ErrSettingsPasswordInvalid, got %v", err)
		}
		spent := ledger.units + bcryptWorkUnits(storedCost)

		if spent != noLocalPassword {
			t.Fatalf("stored cost %d: wrong-password refusal spends %d work units, the no-local-password refusal spends %d", storedCost, spent, noLocalPassword)
		}
	}
}

// The equalized no-local-password refusal spends a full bcrypt, so it must draw
// the re-auth budget like a wrong password does; otherwise an OIDC-only session
// buys that compare on every request with nothing capping it. Both budgeted
// callers are covered — the erasure gate and the password-change gate.
func TestSettingsReauthNoLocalPasswordRefusalDrawsTheBudget(t *testing.T) {
	cases := []struct {
		name   string
		refuse func(service *SettingsService, attempt ReauthAttempt) error
	}{
		{"erasure", func(service *SettingsService, attempt ReauthAttempt) error {
			return service.VerifyReauthPassword(attempt, "", "AnyPass1")
		}},
		{"password change", func(service *SettingsService, attempt ReauthAttempt) error {
			user := &models.User{ID: 42, LocalAuthEnabled: true}
			return service.ChangePassword(context.Background(), attempt, user, "AnyPass1", "NewStrongPass2", "NewStrongPass2")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := withCountingSettingsReauthEqualizer(t)
			service := NewSettingsService(nil)
			service.ConfigureReauthAttempts([]byte("test-secret"), NewAttemptLimiter(), 2, time.Minute)
			attempt := ReauthAttempt{ClientKey: "203.0.113.10", UserID: 42, Now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}

			for i := 1; i <= 2; i++ {
				if err := tc.refuse(service, attempt); !errors.Is(err, ErrSettingsLocalPasswordNotSet) {
					t.Fatalf("refusal %d: got %v, want ErrSettingsLocalPasswordNotSet", i, err)
				}
			}
			if err := tc.refuse(service, attempt); !errors.Is(err, ErrSettingsReauthRateLimited) {
				t.Fatalf("after the budget: got %v, want ErrSettingsReauthRateLimited", err)
			}
			if *calls != 2 {
				t.Fatalf("equalizer ran %d times, want 2 — the rate-limited call must spend nothing", *calls)
			}
		})
	}
}
