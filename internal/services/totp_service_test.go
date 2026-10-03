package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// invalidTOTPCodeForSkewWindow returns a 6-digit code proven NOT to validate
// against secret across the whole ±1-step skew window totp.Validate checks
// at the instant it is called (the enrollment check, VerifyEnrollmentCode,
// reads time.Now() itself and has no injectable clock, so this stays
// deterministic without one). It computes
// the codes for a wider window (±3 steps, i.e. ±90s around "now") than
// Validate's own ±1-step skew (±30s) so a step boundary crossed between this
// computation and the call under test cannot land the excluded set short,
// then the caller re-asserts the precondition with totp.Validate immediately
// before exercising the code path under test.
func invalidTOTPCodeForSkewWindow(t *testing.T, secret string) string {
	t.Helper()
	opts := totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	now := time.Now()
	excluded := make(map[string]bool, 7)
	for i := -3; i <= 3; i++ {
		at := now.Add(time.Duration(i) * 30 * time.Second)
		code, err := totp.GenerateCodeCustom(secret, at, opts)
		if err != nil {
			t.Fatalf("GenerateCodeCustom: %v", err)
		}
		excluded[code] = true
	}
	for _, candidate := range []string{
		"000000", "111111", "222222", "333333", "444444",
		"555555", "666666", "777777", "888888", "999999",
	} {
		if !excluded[candidate] {
			return candidate
		}
	}
	t.Fatal("invalidTOTPCodeForSkewWindow: every candidate collided with the skew window — widen the candidate set")
	return ""
}

// stubTOTPUserRepo is a minimal stub for TOTPUserRepository used in unit tests.
// claimedSteps emulates the persisted totp_last_used_step column per userID;
// ClaimTOTPStep applies the same "strictly greater than" semantics as the real
// repo so replay/concurrency cases can be exercised without a database.
type stubTOTPUserRepo struct {
	updateErr        error
	updatedUserID    uint
	updatedSecret    string
	updatedEnabled   bool
	updateTOTPCalled bool

	reencryptErr          error
	reencryptLost         bool
	reencryptCalled       bool
	reencryptedUserID     uint
	reencryptedOld        string
	reencryptedCiphertext string

	claimErr      error
	claimedSteps  map[uint]int64
	lastClaimUser uint
	lastClaimStep int64
}

func (stub *stubTOTPUserRepo) UpdateTOTPFieldsAndRevokeSessions(ctx context.Context, userID uint, _ int, encryptedSecret string, enabled bool, lastUsedStep int64) error {
	stub.updateTOTPCalled = true
	stub.updatedUserID = userID
	stub.updatedSecret = encryptedSecret
	stub.updatedEnabled = enabled
	if stub.claimedSteps != nil {
		stub.claimedSteps[userID] = lastUsedStep
	}
	return stub.updateErr
}

func (stub *stubTOTPUserRepo) UpgradeTOTPSecretCiphertextCAS(ctx context.Context, userID uint, oldCiphertext string, newCiphertext string) (bool, error) {
	stub.reencryptCalled = true
	stub.reencryptedUserID = userID
	stub.reencryptedOld = oldCiphertext
	stub.reencryptedCiphertext = newCiphertext
	if stub.reencryptErr != nil {
		return false, stub.reencryptErr
	}
	return !stub.reencryptLost, nil
}

func (stub *stubTOTPUserRepo) ClaimTOTPStep(ctx context.Context, userID uint, step int64) (bool, error) {
	if stub.claimErr != nil {
		return false, stub.claimErr
	}
	if stub.claimedSteps == nil {
		stub.claimedSteps = map[uint]int64{}
	}
	stub.lastClaimUser = userID
	stub.lastClaimStep = step
	if stub.claimedSteps[userID] >= step {
		return false, nil
	}
	stub.claimedSteps[userID] = step
	return true, nil
}

func TestTOTPService_GenerateSetupKey(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}
	if key == nil {
		t.Fatal("GenerateSetupKey() returned nil key")
	}
	if key.Issuer() != "Ovumcy" {
		t.Errorf("Issuer = %q, want %q", key.Issuer(), "Ovumcy")
	}
	if key.AccountName() != "user@example.com" {
		t.Errorf("AccountName = %q, want %q", key.AccountName(), "user@example.com")
	}
	if key.Secret() == "" {
		t.Error("GenerateSetupKey() produced empty secret")
	}
}

// The enrollment check finds its step with findValidatedTOTPStep against the
// raw, not yet persisted, secret; these two pin that it accepts a current code
// and refuses one outside the whole skew window.
func TestFindValidatedTOTPStep_RawSecret_Valid(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}

	now := time.Now()
	code, err := totp.GenerateCode(key.Secret(), now)
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	step, found := findValidatedTOTPStep(key.Secret(), code, now)
	if !found {
		t.Fatal("findValidatedTOTPStep() found no step for a valid code")
	}
	if want := now.Unix() / totpStepSeconds; step != want {
		t.Errorf("findValidatedTOTPStep() step = %d, want %d", step, want)
	}
}

func TestFindValidatedTOTPStep_RawSecret_Invalid(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}

	code := invalidTOTPCodeForSkewWindow(t, key.Secret())
	if totp.Validate(code, key.Secret()) {
		t.Fatalf("test setup produced code %q which validates against the secret right now — precondition failed, cannot prove the negative", code)
	}

	if _, found := findValidatedTOTPStep(key.Secret(), code, time.Now()); found {
		t.Errorf("findValidatedTOTPStep() found a step for %q, proven invalid across the whole skew window", code)
	}
}

func TestTOTPService_EnableTOTP_StoresEncryptedSecret(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	rawSecret := "JBSWY3DPEHPK3PXP"
	if err := svc.EnableTOTP(context.Background(), 42, 1, rawSecret, pastEnrollmentStep()); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}

	if !repo.updateTOTPCalled {
		t.Fatal("EnableTOTP() did not call UpdateTOTPFieldsAndRevokeSessions")
	}
	if repo.updatedUserID != 42 {
		t.Errorf("userID = %d, want 42", repo.updatedUserID)
	}
	if !repo.updatedEnabled {
		t.Error("EnableTOTP() set enabled=false, want true")
	}
	if repo.updatedSecret == rawSecret {
		t.Error("EnableTOTP() stored the raw secret instead of encrypting it")
	}
	if repo.updatedSecret == "" {
		t.Error("EnableTOTP() stored an empty secret")
	}
}

func TestTOTPService_ValidateCode_EncryptDecryptRoundTrip(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}

	if err := svc.EnableTOTP(context.Background(), 1, 1, key.Secret(), pastEnrollmentStep()); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}
	encryptedSecret := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	valid, err := svc.ValidateCode(context.Background(), 1, encryptedSecret, code)
	if err != nil {
		t.Fatalf("ValidateCode() error: %v", err)
	}
	if !valid {
		t.Error("ValidateCode() returned false for a valid code after encrypt/decrypt round-trip")
	}
}

func TestTOTPService_ValidateCode_ReplayRejected(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}

	if err := svc.EnableTOTP(context.Background(), 1, 1, key.Secret(), pastEnrollmentStep()); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}
	encryptedSecret := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	// First use — must succeed.
	valid, err := svc.ValidateCode(context.Background(), 1, encryptedSecret, code)
	if err != nil {
		t.Fatalf("ValidateCode() first call error: %v", err)
	}
	if !valid {
		t.Fatal("ValidateCode() returned false for first valid use")
	}

	// Second use of the same code — must be rejected as replay with the
	// dedicated sentinel so the API layer can log it separately while still
	// returning the same response shape as an invalid code.
	valid, err = svc.ValidateCode(context.Background(), 1, encryptedSecret, code)
	if !errors.Is(err, ErrTOTPReplayed) {
		t.Fatalf("ValidateCode() replay error = %v, want ErrTOTPReplayed", err)
	}
	if valid {
		t.Error("ValidateCode() accepted a replayed code — replay protection not working")
	}
}

// TestTOTPService_ValidateCode_ReplaySurvivesServiceRestart simulates a
// process restart by constructing a fresh TOTPService on top of the same
// repository state. Pre-#7 the replay window lived in TOTPService.usedCodes
// (an in-memory sync.Map) and a fresh instance accepted a captured code if
// replayed within the original 90s window.
func TestTOTPService_ValidateCode_ReplaySurvivesServiceRestart(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}
	if err := svc.EnableTOTP(context.Background(), 1, 1, key.Secret(), pastEnrollmentStep()); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}
	encryptedSecret := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}
	valid, err := svc.ValidateCode(context.Background(), 1, encryptedSecret, code)
	if err != nil || !valid {
		t.Fatalf("first use: valid=%v err=%v", valid, err)
	}

	// Fresh service, same repository (DB state survives).
	restarted := NewTOTPService(repo, secretKey, nil)
	valid, err = restarted.ValidateCode(context.Background(), 1, encryptedSecret, code)
	if !errors.Is(err, ErrTOTPReplayed) {
		t.Fatalf("post-restart replay error = %v, want ErrTOTPReplayed", err)
	}
	if valid {
		t.Error("replayed code accepted after service restart — replay state did not persist")
	}
}

func TestTOTPService_ValidateCode_SameCodeDifferentUser_Allowed(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}

	if err := svc.EnableTOTP(context.Background(), 1, 1, key.Secret(), pastEnrollmentStep()); err != nil {
		t.Fatalf("EnableTOTP() user 1 error: %v", err)
	}
	encrypted1 := repo.updatedSecret

	if err := svc.EnableTOTP(context.Background(), 2, 1, key.Secret(), pastEnrollmentStep()); err != nil {
		t.Fatalf("EnableTOTP() user 2 error: %v", err)
	}
	encrypted2 := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	valid1, err := svc.ValidateCode(context.Background(), 1, encrypted1, code)
	if err != nil || !valid1 {
		t.Fatalf("ValidateCode() user 1 failed: valid=%v err=%v", valid1, err)
	}

	// Same code, different userID — replay cache is per-user, so this must pass.
	valid2, err := svc.ValidateCode(context.Background(), 2, encrypted2, code)
	if err != nil {
		t.Fatalf("ValidateCode() user 2 error: %v", err)
	}
	if !valid2 {
		t.Error("ValidateCode() rejected same code for a different user — replay cache must be per-user")
	}
}

func TestTOTPService_DisableTOTP_ClearsFields(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	if err := svc.DisableTOTP(context.Background(), 99, 1); err != nil {
		t.Fatalf("DisableTOTP() error: %v", err)
	}

	if !repo.updateTOTPCalled {
		t.Fatal("DisableTOTP() did not call UpdateTOTPFieldsAndRevokeSessions")
	}
	if repo.updatedUserID != 99 {
		t.Errorf("userID = %d, want 99", repo.updatedUserID)
	}
	if repo.updatedEnabled {
		t.Error("DisableTOTP() set enabled=true, want false")
	}
	if repo.updatedSecret != "" {
		t.Errorf("DisableTOTP() stored %q, want empty string", repo.updatedSecret)
	}
}

func TestTOTPService_EnableTOTP_RepoError(t *testing.T) {
	repo := &stubTOTPUserRepo{updateErr: ErrTOTPUpdateFailed}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	err := svc.EnableTOTP(context.Background(), 1, 1, "JBSWY3DPEHPK3PXP", pastEnrollmentStep())
	if !errors.Is(err, ErrTOTPUpdateFailed) {
		t.Fatalf("EnableTOTP() error = %v, want the repo error as ErrTOTPUpdateFailed", err)
	}
}

func TestTOTPService_EnableTOTP_RefusesAZeroEnrollmentStep(t *testing.T) {
	for _, enrollment := range []TOTPEnrollmentStep{{}, {step: -1}} {
		repo := &stubTOTPUserRepo{}
		svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

		err := svc.EnableTOTP(context.Background(), 42, 1, "JBSWY3DPEHPK3PXP", enrollment)
		if !errors.Is(err, ErrTOTPEnrollmentStepMissing) {
			t.Fatalf("EnableTOTP(step %d) error = %v, want ErrTOTPEnrollmentStepMissing", enrollment.step, err)
		}
		if repo.updateTOTPCalled {
			t.Fatalf("EnableTOTP(step %d) wrote the TOTP columns", enrollment.step)
		}
	}
}

// pastEnrollmentStep is the step an enrollment confirmed an hour ago would have
// recorded: nonzero, as EnableTOTP requires, and below the step of any code a
// test sends now.
func pastEnrollmentStep() TOTPEnrollmentStep {
	return TOTPEnrollmentStep{step: time.Now().Add(-time.Hour).Unix() / totpStepSeconds}
}

// --- rate-limit: verification (CheckRateLimit / RecordFailure / ResetAttempts) ---

func TestTOTPService_CheckRateLimit_BelowLimit_ReturnsNil(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for range DefaultTOTPAttemptsLimit - 1 {
		svc.RecordFailure(secretKey, "1.2.3.4", 1, now)
	}

	if err := svc.CheckRateLimit(secretKey, "1.2.3.4", 1, now); err != nil {
		t.Errorf("CheckRateLimit() after %d failures = %v, want nil", DefaultTOTPAttemptsLimit-1, err)
	}
}

func TestTOTPService_CheckRateLimit_AtLimit_ReturnsErrTOTPRateLimited(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for range DefaultTOTPAttemptsLimit {
		svc.RecordFailure(secretKey, "1.2.3.4", 1, now)
	}

	err := svc.CheckRateLimit(secretKey, "1.2.3.4", 1, now)
	if !errors.Is(err, ErrTOTPRateLimited) {
		t.Errorf("CheckRateLimit() after %d failures = %v, want ErrTOTPRateLimited", DefaultTOTPAttemptsLimit, err)
	}
}

func TestTOTPService_ResetAttempts_ClearsLimit(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for range DefaultTOTPAttemptsLimit {
		svc.RecordFailure(secretKey, "1.2.3.4", 1, now)
	}
	if err := svc.CheckRateLimit(secretKey, "1.2.3.4", 1, now); !errors.Is(err, ErrTOTPRateLimited) {
		t.Fatalf("precondition: limiter not tripped after %d failures, err=%v", DefaultTOTPAttemptsLimit, err)
	}

	svc.ResetAttempts(secretKey, "1.2.3.4", 1)

	// The client bucket is forgiven: the same client is open for another account.
	if err := svc.CheckRateLimit(secretKey, "1.2.3.4", 2, now); err != nil {
		t.Errorf("CheckRateLimit() for another account after ResetAttempts = %v, want nil", err)
	}
	// The identity bucket pools every client's failures and is not reset.
	if err := svc.CheckRateLimit(secretKey, "5.6.7.8", 1, now); !errors.Is(err, ErrTOTPRateLimited) {
		t.Errorf("CheckRateLimit() for the same account from another client = %v, want ErrTOTPRateLimited", err)
	}
}

// TestTOTPService_CheckRateLimit_IdentityIsolation verifies that failures
// recorded for one user do not trip the limiter for another user from a
// different client. Both the client bucket and the HMAC'd identity bucket
// must be independent.
func TestTOTPService_CheckRateLimit_IdentityIsolation(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for range DefaultTOTPAttemptsLimit {
		svc.RecordFailure(secretKey, "client-A", 1, now)
	}

	if err := svc.CheckRateLimit(secretKey, "client-A", 1, now); !errors.Is(err, ErrTOTPRateLimited) {
		t.Fatalf("user 1 from client-A should be limited, got %v", err)
	}
	if err := svc.CheckRateLimit(secretKey, "client-B", 2, now); err != nil {
		t.Errorf("user 2 from client-B should not be limited (HMAC'd identity bucket independent), got %v", err)
	}
}

// TestTOTPService_CheckRateLimit_ClientIPIsolation verifies that failures
// recorded from one client IP do not trip the limiter for another client IP
// when the user identity also differs.
func TestTOTPService_CheckRateLimit_ClientIPIsolation(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for range DefaultTOTPAttemptsLimit {
		svc.RecordFailure(secretKey, "1.1.1.1", 100, now)
	}

	if err := svc.CheckRateLimit(secretKey, "1.1.1.1", 100, now); !errors.Is(err, ErrTOTPRateLimited) {
		t.Fatalf("client 1.1.1.1 should be limited, got %v", err)
	}
	if err := svc.CheckRateLimit(secretKey, "2.2.2.2", 200, now); err != nil {
		t.Errorf("client 2.2.2.2 should not be limited, got %v", err)
	}
}

// --- rate-limit: disable (the account's password re-auth ReauthBudget) ---

func TestTOTPService_CheckDisableRateLimit_BelowLimit_ReturnsNil(t *testing.T) {
	settings := newDisableBudgetSettings([]byte("test-secret-key-32-bytes-padding!"), nil)
	now := time.Now()

	for range DefaultSettingsReauthAttemptsLimit - 1 {
		recordDisableFailure(settings, "1.2.3.4", 1, now)
	}

	if err := checkDisableBudget(settings, "1.2.3.4", 1, now); err != nil {
		t.Errorf("disable budget after %d failures = %v, want nil", DefaultSettingsReauthAttemptsLimit-1, err)
	}
}

func TestTOTPService_CheckDisableRateLimit_AtLimit_ReturnsErrSettingsReauthRateLimited(t *testing.T) {
	settings := newDisableBudgetSettings([]byte("test-secret-key-32-bytes-padding!"), nil)
	now := time.Now()

	for range DefaultSettingsReauthAttemptsLimit {
		recordDisableFailure(settings, "1.2.3.4", 1, now)
	}

	err := checkDisableBudget(settings, "1.2.3.4", 1, now)
	if !errors.Is(err, ErrSettingsReauthRateLimited) {
		t.Errorf("disable budget after %d failures = %v, want ErrSettingsReauthRateLimited", DefaultSettingsReauthAttemptsLimit, err)
	}
}

func TestTOTPService_ResetDisableAttempts_ClearsLimit(t *testing.T) {
	settings := newDisableBudgetSettings([]byte("test-secret-key-32-bytes-padding!"), nil)
	now := time.Now()

	for range DefaultSettingsReauthAttemptsLimit {
		recordDisableFailure(settings, "1.2.3.4", 1, now)
	}
	if err := checkDisableBudget(settings, "1.2.3.4", 1, now); !errors.Is(err, ErrSettingsReauthRateLimited) {
		t.Fatalf("precondition: disable limiter not tripped after %d failures, err=%v", DefaultSettingsReauthAttemptsLimit, err)
	}

	resetDisableBudget(settings, "1.2.3.4", 1)

	if err := checkDisableBudget(settings, "1.2.3.4", 1, now); err != nil {
		t.Errorf("disable budget after Reset = %v, want nil", err)
	}
	// Disabling TOTP is session-bound: the account's counter is cleared too,
	// so the same account is open from any client.
	if err := checkDisableBudget(settings, "5.6.7.8", 1, now); err != nil {
		t.Errorf("disable budget for the same account from another client = %v, want nil", err)
	}
}

// TestTOTPService_DisableAndVerifyLimitsAreIndependent verifies that the
// sign-in "totp" scope and the disable confirmation's re-auth budget use
// separate buckets on one shared limiter — exhausting the disable limit must
// not trip the verification limit.
func TestTOTPService_DisableAndVerifyLimitsAreIndependent(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	limiter := NewAttemptLimiter()
	svc := NewTOTPService(repo, secretKey, limiter)
	settings := newDisableBudgetSettings(secretKey, limiter)
	now := time.Now()

	for range DefaultSettingsReauthAttemptsLimit {
		recordDisableFailure(settings, "1.2.3.4", 1, now)
	}
	if err := checkDisableBudget(settings, "1.2.3.4", 1, now); !errors.Is(err, ErrSettingsReauthRateLimited) {
		t.Fatalf("precondition: disable limiter not tripped, err=%v", err)
	}
	if err := svc.CheckRateLimit(secretKey, "1.2.3.4", 1, now); err != nil {
		t.Errorf("verify limiter must be independent of disable limiter, got %v", err)
	}
}
