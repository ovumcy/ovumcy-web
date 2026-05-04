package services

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// stubTOTPUserRepo is a minimal stub for TOTPUserRepository used in unit tests.
type stubTOTPUserRepo struct {
	updateErr        error
	updatedUserID    uint
	updatedSecret    string
	updatedEnabled   bool
	updateTOTPCalled bool
}

func (stub *stubTOTPUserRepo) UpdateTOTPFields(userID uint, encryptedSecret string, enabled bool) error {
	stub.updateTOTPCalled = true
	stub.updatedUserID = userID
	stub.updatedSecret = encryptedSecret
	stub.updatedEnabled = enabled
	return stub.updateErr
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

	// Second use of the same code — must be rejected as replay.
	valid, err = svc.ValidateCode(1, encryptedSecret, code)
	if err != nil {
		t.Fatalf("ValidateCode() replay call error: %v", err)
	}
	if valid {
		t.Error("ValidateCode() accepted a replayed code — replay protection not working")
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
