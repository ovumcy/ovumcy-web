package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// Guards for the legacy-cost half of the auth timing-equalization story.
//
// The equalize* helpers make every early-return branch spend a full
// passwordHashCost bcrypt comparison, so "no such address" costs the same as a
// wrong password. That holds only while the REAL comparison also costs
// passwordHashCost. A stored hash minted before the cost was raised carries its
// own, lower cost, and bcrypt work doubles per cost step — so a wrong password
// against such an account answers ~4x faster than an unknown address, which is
// the account-enumeration oracle (CWE-208 / CWE-204) read backwards.
//
// These tests account WORK, not wall-clock time: a latency budget is flake-prone
// on shared CI runners (the reason the older login timing guard was rewritten as
// a call counter), while the property at stake — "every rejection adds up to
// 2^passwordHashCost units" — is arithmetic and settles exactly.

// bcryptWorkUnits states bcrypt's own work model: the KDF runs 2^cost rounds, so
// a comparison against a hash at cost c is worth 2^c units and the work of
// several comparisons adds.
func bcryptWorkUnits(cost int) int64 {
	return int64(1) << cost
}

// mintBcryptHashAtCost produces a stored-hash fixture at an arbitrary cost —
// how a row written before the cost raise looks today.
func mintBcryptHashAtCost(t *testing.T, value string, cost int) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(value), cost)
	if err != nil {
		t.Fatalf("mint bcrypt hash at cost %d: %v", cost, err)
	}
	return string(hash)
}

// withTopUpCompareLedger wraps the single seam through which the shipped
// spendBcryptCostTopUp spends its bcrypt work, so a test accounts the
// comparisons the code ACTUALLY makes and reads each one's cost off the hash it
// was handed.
//
// This guard used to recompute the same total from bcryptCostTopUpSchedule
// instead. That proved only that the schedule agreed with itself: replacing the
// body of spendBcryptCostTopUp with a bare return left the oracle wide open and
// every assertion in this file green.
func withTopUpCompareLedger(t *testing.T, ledger *bcryptWorkLedger) {
	t.Helper()
	original := timingTopUpCompare
	timingTopUpCompare = func(hash []byte, operand []byte) {
		ledger.units += bcryptWorkUnits(mustBcryptCost(t, string(hash)))
		original(hash, operand)
	}
	t.Cleanup(func() { timingTopUpCompare = original })
}

// bcryptWorkLedger accumulates the work the equalizing helpers spend on a
// request, so two rejection paths can be compared as totals.
type bcryptWorkLedger struct {
	units          int64
	topUpHashes    []string
	topUpOperands  []string
	equalizerCalls int
}

func (ledger *bcryptWorkLedger) drain() int64 {
	units := ledger.units
	ledger.units = 0
	return units
}

// withLoginWorkLedger wraps both login-side helpers so the test can account
// their work. Each wrapper still calls the production helper, so the real
// bcrypt work is spent exactly as it ships and only the accounting is added.
func withLoginWorkLedger(t *testing.T) *bcryptWorkLedger {
	t.Helper()
	ledger := &bcryptWorkLedger{}

	originalEqualize := equalizeAuthCredentialsTiming
	equalizeAuthCredentialsTiming = func(password string) {
		ledger.equalizerCalls++
		ledger.units += bcryptWorkUnits(mustBcryptCost(t, credentialsTimingEqualizationHash))
		originalEqualize(password)
	}

	originalTopUp := topUpAuthCredentialsTiming
	topUpAuthCredentialsTiming = func(storedHash string, password string) {
		ledger.topUpHashes = append(ledger.topUpHashes, storedHash)
		ledger.topUpOperands = append(ledger.topUpOperands, password)
		originalTopUp(storedHash, password)
	}

	t.Cleanup(func() {
		equalizeAuthCredentialsTiming = originalEqualize
		topUpAuthCredentialsTiming = originalTopUp
	})

	withTopUpCompareLedger(t, ledger)
	return ledger
}

// TestBcryptCostTopUpScheduleClosesTheDeficitExactly is the arithmetic leg: for
// every cost a stored hash can carry, the real comparison plus the scheduled
// top-up must add up to one comparison at passwordHashCost — no more (wasted
// CPU on every wrong password) and no less (the oracle stays open). The
// expectation is derived from passwordHashCost, so raising the constant keeps
// the assertion honest instead of pinning the (10, 12) pair of the day.
func TestBcryptCostTopUpScheduleClosesTheDeficitExactly(t *testing.T) {
	for storedCost := bcrypt.MinCost; storedCost <= passwordHashCost; storedCost++ {
		total := bcryptWorkUnits(storedCost)
		for _, topUpCost := range bcryptCostTopUpSchedule(storedCost) {
			if topUpCost < storedCost || topUpCost >= passwordHashCost {
				t.Fatalf("stored cost %d: schedule names cost %d, outside [%d, %d)", storedCost, topUpCost, storedCost, passwordHashCost)
			}
			total += bcryptWorkUnits(topUpCost)
		}

		if want := bcryptWorkUnits(passwordHashCost); total != want {
			t.Fatalf("stored cost %d: real comparison plus top-up accounts to %d work units, want %d (2^passwordHashCost) — the deficit is not closed",
				storedCost, total, want)
		}
	}
}

// TestTimingTopUpPlaceholdersCoverEveryScheduledCost proves the schedule can
// actually be paid: a named cost with no minted placeholder, or one minted at
// the wrong cost, would leave the arithmetic above true and the shipped path
// still cheap.
func TestTimingTopUpPlaceholdersCoverEveryScheduledCost(t *testing.T) {
	for _, topUpCost := range bcryptCostTopUpSchedule(bcrypt.MinCost) {
		placeholder, ok := timingTopUpHashesByCost[topUpCost]
		if !ok {
			t.Fatalf("no top-up placeholder minted for cost %d — that step of the deficit cannot be paid", topUpCost)
		}
		if got := mustBcryptCost(t, string(placeholder)); got != topUpCost {
			t.Fatalf("top-up placeholder for cost %d is minted at cost %d — it buys the wrong amount of work", topUpCost, got)
		}
		if err := bcrypt.CompareHashAndPassword(placeholder, []byte("any")); !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			t.Fatalf("top-up placeholder for cost %d is not a usable bcrypt hash (%v) — the comparison would short-circuit", topUpCost, err)
		}
	}
}

// TestAuthenticateCredentialsAccountsEqualWorkForMissingAccountAndStaleHash is
// the leg that matters: it drives the shipped AuthenticateCredentials and
// compares the total work of the two rejection paths an enumerator can reach.
func TestAuthenticateCredentialsAccountsEqualWorkForMissingAccountAndStaleHash(t *testing.T) {
	const submittedPassword = "WrongGuess1!"
	staleHash := mintBcryptHashAtCost(t, "CorrectHorse1!", bcrypt.DefaultCost)
	staleCost := mustBcryptCost(t, staleHash)
	if staleCost >= passwordHashCost {
		t.Fatalf("fixture stored hash is at cost %d, want below passwordHashCost (%d)", staleCost, passwordHashCost)
	}

	ledger := withLoginWorkLedger(t)

	missing := NewAuthService(&stubAuthUserRepo{findByEmailErr: errors.New("not found")})
	if _, err := missing.AuthenticateCredentials(context.Background(), "nobody@example.com", submittedPassword); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for an unknown address, got %v", err)
	}
	missingUnits := ledger.drain()
	if ledger.equalizerCalls != 1 {
		t.Fatalf("expected the unknown-address branch to equalize exactly once, got %d calls", ledger.equalizerCalls)
	}
	if want := bcryptWorkUnits(passwordHashCost); missingUnits != want {
		t.Fatalf("unknown-address branch accounts to %d work units, want %d (2^passwordHashCost)", missingUnits, want)
	}

	stale := NewAuthService(&stubAuthUserRepo{findByEmailUser: models.User{
		ID:               7,
		Email:            "owner@example.com",
		Role:             models.RoleOwner,
		LocalAuthEnabled: true,
		PasswordHash:     staleHash,
	}})
	if _, err := stale.AuthenticateCredentials(context.Background(), "owner@example.com", submittedPassword); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for a wrong password, got %v", err)
	}
	// The real comparison ran against the stored hash and spent its cost; the
	// ledger only sees what the helpers added on top.
	staleUnits := ledger.drain() + bcryptWorkUnits(staleCost)

	if staleUnits != missingUnits {
		t.Fatalf("a wrong password against a cost-%d stored hash accounts to %d bcrypt work units while an unknown address accounts to %d: "+
			"the cheaper branch tells an attacker the account exists (CWE-208 / CWE-204)", staleCost, staleUnits, missingUnits)
	}
	if len(ledger.topUpHashes) != 1 {
		t.Fatalf("expected exactly 1 top-up on the failed-compare path, got %d", len(ledger.topUpHashes))
	}
	if ledger.topUpHashes[0] != staleHash {
		t.Fatal("the top-up ran against a hash other than the one the real comparison used — its cost would be read from the wrong operand")
	}
	if ledger.topUpOperands[0] != submittedPassword {
		t.Fatalf("expected the top-up to run against the submitted password, got %q", ledger.topUpOperands[0])
	}
}

// TestAuthenticateCredentialsAccountsFullWorkForAnUnparseableStoredHash covers
// the branch where bcrypt.Cost has no answer: the real comparison rejects the
// row without running the KDF at all, so the whole passwordHashCost is owed —
// a zero-work rejection would be the loudest of the three signals.
func TestAuthenticateCredentialsAccountsFullWorkForAnUnparseableStoredHash(t *testing.T) {
	ledger := withLoginWorkLedger(t)

	service := NewAuthService(&stubAuthUserRepo{findByEmailUser: models.User{
		ID:               9,
		Email:            "owner@example.com",
		Role:             models.RoleOwner,
		LocalAuthEnabled: true,
		PasswordHash:     "not-a-bcrypt-hash",
	}})
	if _, err := service.AuthenticateCredentials(context.Background(), "owner@example.com", "WrongGuess1!"); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for an unparseable stored hash, got %v", err)
	}

	spent := ledger.drain()
	if want := bcryptWorkUnits(passwordHashCost); spent != want {
		t.Fatalf("unparseable stored hash accounts to %d work units, want %d (2^passwordHashCost)", spent, want)
	}
}

// TestAuthenticateCredentialsAddsNoWorkForACurrentCostHash is the other side of
// "exactly": an account already at passwordHashCost owes nothing, and paying a
// top-up there would both waste CPU on every wrong password and make the
// current-cost account the slow one.
func TestAuthenticateCredentialsAddsNoWorkForACurrentCostHash(t *testing.T) {
	currentHash := mintBcryptHashAtCost(t, "CorrectHorse1!", passwordHashCost)
	ledger := withLoginWorkLedger(t)

	service := NewAuthService(&stubAuthUserRepo{findByEmailUser: models.User{
		ID:               11,
		Email:            "owner@example.com",
		Role:             models.RoleOwner,
		LocalAuthEnabled: true,
		PasswordHash:     currentHash,
	}})
	if _, err := service.AuthenticateCredentials(context.Background(), "owner@example.com", "WrongGuess1!"); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for a wrong password, got %v", err)
	}

	toppedUp := ledger.drain()
	if toppedUp != 0 {
		t.Fatalf("a stored hash already at passwordHashCost was topped up by %d work units, want 0", toppedUp)
	}
	if total := bcryptWorkUnits(mustBcryptCost(t, currentHash)) + toppedUp; total != bcryptWorkUnits(passwordHashCost) {
		t.Fatalf("current-cost rejection accounts to %d work units, want %d (2^passwordHashCost)", total, bcryptWorkUnits(passwordHashCost))
	}
}

// TestRecoveryLookupAccountsEqualWorkForMissingAccountAndStaleHashes is the
// same guard for the recovery reset, which verifies two stored hashes and
// equalizes both on its early returns. Recovery-code hashes are never re-minted
// on use — there is no rehash-if-stale for them — so a cost written before the
// raise stays below passwordHashCost for the life of the code.
func TestRecoveryLookupAccountsEqualWorkForMissingAccountAndStaleHashes(t *testing.T) {
	const submittedCode = "OVM-ABCD-EFGH-JKLM"
	const submittedPassword = "WrongGuess1!"

	staleRecoveryHash := mintBcryptHashAtCost(t, NormalizeRecoveryCode("OVM-ZZZZ-YYYY-XXXX"), bcrypt.DefaultCost)
	stalePasswordHash := mintBcryptHashAtCost(t, "CorrectHorse1!", bcrypt.DefaultCost)

	ledger := &bcryptWorkLedger{}
	originalTopUp := topUpRecoveryLookupTiming
	topUpRecoveryLookupTiming = func(recoveryHash string, code string, passwordHash string, password string) {
		ledger.topUpHashes = append(ledger.topUpHashes, recoveryHash, passwordHash)
		originalTopUp(recoveryHash, code, passwordHash, password)
	}
	t.Cleanup(func() { topUpRecoveryLookupTiming = originalTopUp })
	withTopUpCompareLedger(t, ledger)

	// The unknown-address baseline is read off the two placeholder constants
	// rather than measured, because equalizeRecoveryCodeLookupTiming compares
	// against them with a literal bcrypt call the barrier test requires it to
	// keep. Three guards make that reading safe, and none of them is this test:
	// TestTimingEqualizationHashesMatchTargetCost pins both placeholders to
	// passwordHashCost, and
	// TestRecoveryLookupSpendsBothCredentialComparesWithoutShortCircuit pins
	// both comparisons to running unconditionally AND pins both early-return
	// branches to actually calling the equalizer — without that last check,
	// deleting the call here would leave an unknown address costing nothing and
	// this test still green.
	missing := NewAuthService(&stubAuthUserRepo{})
	if _, err := missing.FindUserByEmailRecoveryCodeAndPassword(context.Background(), "nobody@example.com", submittedCode, submittedPassword); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Fatalf("expected ErrRecoveryCodeNotFound for an unknown address, got %v", err)
	}
	missingUnits := bcryptWorkUnits(mustBcryptCost(t, recoveryCodeTimingEqualizationHash)) +
		bcryptWorkUnits(mustBcryptCost(t, credentialsTimingEqualizationHash))

	stale := NewAuthService(&stubAuthUserRepo{
		findByEmailOptionalFound: true,
		findByEmailOptionalUser: models.User{
			ID:               13,
			Email:            "owner@example.com",
			Role:             models.RoleOwner,
			LocalAuthEnabled: true,
			PasswordHash:     stalePasswordHash,
			RecoveryCodeHash: staleRecoveryHash,
		},
	})
	if _, err := stale.FindUserByEmailRecoveryCodeAndPassword(context.Background(), "owner@example.com", submittedCode, submittedPassword); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Fatalf("expected ErrRecoveryCodeNotFound for a wrong code, got %v", err)
	}
	staleUnits := ledger.drain() +
		bcryptWorkUnits(mustBcryptCost(t, staleRecoveryHash)) +
		bcryptWorkUnits(mustBcryptCost(t, stalePasswordHash))

	if staleUnits != missingUnits {
		t.Fatalf("a rejected recovery reset against stored hashes at cost %d/%d accounts to %d bcrypt work units while an unknown address accounts to %d: "+
			"the cheaper branch tells an attacker the account exists (CWE-208 / CWE-204)",
			mustBcryptCost(t, staleRecoveryHash), mustBcryptCost(t, stalePasswordHash), staleUnits, missingUnits)
	}
	if len(ledger.topUpHashes) != 2 {
		t.Fatalf("expected the rejection to top up both stored operands, got %d", len(ledger.topUpHashes))
	}
	if ledger.topUpHashes[0] != staleRecoveryHash || ledger.topUpHashes[1] != stalePasswordHash {
		t.Fatal("the recovery top-up ran against hashes other than the ones the real comparisons used")
	}
}
