package services

import (
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

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

	claimErr      error
	claimedSteps  map[uint]int64
	lastClaimUser uint
	lastClaimStep int64
}

func (stub *stubTOTPUserRepo) UpdateTOTPFields(userID uint, encryptedSecret string, enabled bool) error {
	stub.updateTOTPCalled = true
	stub.updatedUserID = userID
	stub.updatedSecret = encryptedSecret
	stub.updatedEnabled = enabled
	if stub.claimedSteps != nil {
		stub.claimedSteps[userID] = 0
	}
	return stub.updateErr
}

func (stub *stubTOTPUserRepo) ClaimTOTPStep(userID uint, step int64) (bool, error) {
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

func TestTOTPService_ValidateCodeRaw_Valid(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	if !svc.ValidateCodeRaw(key.Secret(), code) {
		t.Error("ValidateCodeRaw() returned false for a valid code")
	}
}

func TestTOTPService_ValidateCodeRaw_Invalid(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	key, err := svc.GenerateSetupKey("Ovumcy", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey() error: %v", err)
	}

	if svc.ValidateCodeRaw(key.Secret(), "000000") {
		t.Error("ValidateCodeRaw() returned true for '000000' — possible but extremely unlikely; rerun to confirm")
	}
}

func TestTOTPService_EnableTOTP_StoresEncryptedSecret(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	svc := NewTOTPService(repo, []byte("test-secret-key-32-bytes-padding!"), nil)

	rawSecret := "JBSWY3DPEHPK3PXP"
	if err := svc.EnableTOTP(42, rawSecret); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}

	if !repo.updateTOTPCalled {
		t.Fatal("EnableTOTP() did not call UpdateTOTPFields")
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

	if err := svc.EnableTOTP(1, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}
	encryptedSecret := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	valid, err := svc.ValidateCode(1, encryptedSecret, code)
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

	if err := svc.EnableTOTP(1, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}
	encryptedSecret := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	// First use — must succeed.
	valid, err := svc.ValidateCode(1, encryptedSecret, code)
	if err != nil {
		t.Fatalf("ValidateCode() first call error: %v", err)
	}
	if !valid {
		t.Fatal("ValidateCode() returned false for first valid use")
	}

	// Second use of the same code — must be rejected as replay with the
	// dedicated sentinel so the API layer can log it separately while still
	// returning the same response shape as an invalid code.
	valid, err = svc.ValidateCode(1, encryptedSecret, code)
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
	if err := svc.EnableTOTP(1, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP() error: %v", err)
	}
	encryptedSecret := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}
	valid, err := svc.ValidateCode(1, encryptedSecret, code)
	if err != nil || !valid {
		t.Fatalf("first use: valid=%v err=%v", valid, err)
	}

	// Fresh service, same repository (DB state survives).
	restarted := NewTOTPService(repo, secretKey, nil)
	valid, err = restarted.ValidateCode(1, encryptedSecret, code)
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

	if err := svc.EnableTOTP(1, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP() user 1 error: %v", err)
	}
	encrypted1 := repo.updatedSecret

	if err := svc.EnableTOTP(2, key.Secret()); err != nil {
		t.Fatalf("EnableTOTP() user 2 error: %v", err)
	}
	encrypted2 := repo.updatedSecret

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error: %v", err)
	}

	valid1, err := svc.ValidateCode(1, encrypted1, code)
	if err != nil || !valid1 {
		t.Fatalf("ValidateCode() user 1 failed: valid=%v err=%v", valid1, err)
	}

	// Same code, different userID — replay cache is per-user, so this must pass.
	valid2, err := svc.ValidateCode(2, encrypted2, code)
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

	if err := svc.DisableTOTP(99); err != nil {
		t.Fatalf("DisableTOTP() error: %v", err)
	}

	if !repo.updateTOTPCalled {
		t.Fatal("DisableTOTP() did not call UpdateTOTPFields")
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

	err := svc.EnableTOTP(1, "JBSWY3DPEHPK3PXP")
	if err == nil {
		t.Fatal("EnableTOTP() should propagate repo error")
	}
}

// --- rate-limit: verification (CheckRateLimit / RecordFailure / ResetAttempts) ---

func TestTOTPService_CheckRateLimit_BelowLimit_ReturnsNil(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for i := 0; i < DefaultTOTPAttemptsLimit-1; i++ {
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

	for i := 0; i < DefaultTOTPAttemptsLimit; i++ {
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

	for i := 0; i < DefaultTOTPAttemptsLimit; i++ {
		svc.RecordFailure(secretKey, "1.2.3.4", 1, now)
	}
	if err := svc.CheckRateLimit(secretKey, "1.2.3.4", 1, now); !errors.Is(err, ErrTOTPRateLimited) {
		t.Fatalf("precondition: limiter not tripped after %d failures, err=%v", DefaultTOTPAttemptsLimit, err)
	}

	svc.ResetAttempts(secretKey, "1.2.3.4", 1)

	if err := svc.CheckRateLimit(secretKey, "1.2.3.4", 1, now); err != nil {
		t.Errorf("CheckRateLimit() after ResetAttempts = %v, want nil", err)
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

	for i := 0; i < DefaultTOTPAttemptsLimit; i++ {
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

	for i := 0; i < DefaultTOTPAttemptsLimit; i++ {
		svc.RecordFailure(secretKey, "1.1.1.1", 100, now)
	}

	if err := svc.CheckRateLimit(secretKey, "1.1.1.1", 100, now); !errors.Is(err, ErrTOTPRateLimited) {
		t.Fatalf("client 1.1.1.1 should be limited, got %v", err)
	}
	if err := svc.CheckRateLimit(secretKey, "2.2.2.2", 200, now); err != nil {
		t.Errorf("client 2.2.2.2 should not be limited, got %v", err)
	}
}

// --- rate-limit: disable (CheckDisableRateLimit / RecordDisableFailure / ResetDisableAttempts) ---

func TestTOTPService_CheckDisableRateLimit_BelowLimit_ReturnsNil(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for i := 0; i < DefaultTOTPDisableAttemptsLimit-1; i++ {
		svc.RecordDisableFailure(secretKey, "1.2.3.4", 1, now)
	}

	if err := svc.CheckDisableRateLimit(secretKey, "1.2.3.4", 1, now); err != nil {
		t.Errorf("CheckDisableRateLimit() after %d failures = %v, want nil", DefaultTOTPDisableAttemptsLimit-1, err)
	}
}

func TestTOTPService_CheckDisableRateLimit_AtLimit_ReturnsErrTOTPDisableRateLimited(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for i := 0; i < DefaultTOTPDisableAttemptsLimit; i++ {
		svc.RecordDisableFailure(secretKey, "1.2.3.4", 1, now)
	}

	err := svc.CheckDisableRateLimit(secretKey, "1.2.3.4", 1, now)
	if !errors.Is(err, ErrTOTPDisableRateLimited) {
		t.Errorf("CheckDisableRateLimit() after %d failures = %v, want ErrTOTPDisableRateLimited", DefaultTOTPDisableAttemptsLimit, err)
	}
}

func TestTOTPService_ResetDisableAttempts_ClearsLimit(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for i := 0; i < DefaultTOTPDisableAttemptsLimit; i++ {
		svc.RecordDisableFailure(secretKey, "1.2.3.4", 1, now)
	}
	if err := svc.CheckDisableRateLimit(secretKey, "1.2.3.4", 1, now); !errors.Is(err, ErrTOTPDisableRateLimited) {
		t.Fatalf("precondition: disable limiter not tripped after %d failures, err=%v", DefaultTOTPDisableAttemptsLimit, err)
	}

	svc.ResetDisableAttempts(secretKey, "1.2.3.4", 1)

	if err := svc.CheckDisableRateLimit(secretKey, "1.2.3.4", 1, now); err != nil {
		t.Errorf("CheckDisableRateLimit() after ResetDisableAttempts = %v, want nil", err)
	}
}

// TestTOTPService_DisableAndVerifyLimitsAreIndependent verifies that the
// "totp" and "totp.disable" scopes use separate buckets — exhausting the
// disable limit must not trip the verification limit and vice versa.
func TestTOTPService_DisableAndVerifyLimitsAreIndependent(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)
	now := time.Now()

	for i := 0; i < DefaultTOTPDisableAttemptsLimit; i++ {
		svc.RecordDisableFailure(secretKey, "1.2.3.4", 1, now)
	}
	if err := svc.CheckDisableRateLimit(secretKey, "1.2.3.4", 1, now); !errors.Is(err, ErrTOTPDisableRateLimited) {
		t.Fatalf("precondition: disable limiter not tripped, err=%v", err)
	}
	if err := svc.CheckRateLimit(secretKey, "1.2.3.4", 1, now); err != nil {
		t.Errorf("verify limiter must be independent of disable limiter, got %v", err)
	}
}
