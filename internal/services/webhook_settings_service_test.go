package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// stubWebhookRepo captures the last SaveWebhookSettings call so tests can assert
// the ciphertext the service produced and persisted.
type stubWebhookRepo struct {
	savedUserID  uint
	savedColumns models.WebhookSettingsColumns
	saveErr      error
	saveCalls    int
}

func (s *stubWebhookRepo) SaveWebhookSettings(_ context.Context, userID uint, settings models.WebhookSettingsColumns) error {
	s.saveCalls++
	s.savedUserID = userID
	s.savedColumns = settings
	return s.saveErr
}

const webhookTestSecretKey = "test-secret-key-32-bytes-padding!"

func newWebhookServiceForTest() (*WebhookSettingsService, *stubWebhookRepo) {
	repo := &stubWebhookRepo{}
	svc := NewWebhookSettingsService(repo, []byte(webhookTestSecretKey))
	return svc, repo
}

func TestValidateWebhookURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "https accepted", raw: "https://hooks.example.com/abc", wantErr: false},
		{name: "http accepted", raw: "http://192.0.2.10:9000/notify", wantErr: false},
		{name: "surrounding whitespace trimmed", raw: "  https://example.com/hook  ", wantErr: false},
		{name: "empty rejected", raw: "", wantErr: true},
		{name: "whitespace only rejected", raw: "   ", wantErr: true},
		{name: "ftp scheme rejected", raw: "ftp://example.com/file", wantErr: true},
		{name: "file scheme rejected", raw: "file:///etc/passwd", wantErr: true},
		{name: "javascript scheme rejected", raw: "javascript:alert(1)", wantErr: true},
		{name: "gopher scheme rejected", raw: "gopher://example.com", wantErr: true},
		{name: "scheme-relative rejected", raw: "//example.com/hook", wantErr: true},
		{name: "relative path rejected", raw: "/only/a/path", wantErr: true},
		{name: "hostless http rejected", raw: "http:///nohost", wantErr: true},
		{name: "CRLF rejected", raw: "https://example.com/\r\nHost: evil", wantErr: true},
		{name: "uppercase scheme accepted", raw: "HTTPS://example.com/hook", wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateWebhookURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got value %q", tc.raw, got)
				}
				if !errors.Is(err, ErrWebhookURLInvalid) {
					t.Fatalf("expected ErrWebhookURLInvalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if got == "" {
				t.Fatalf("expected a non-empty cleaned URL for %q", tc.raw)
			}
		})
	}
}

func TestValidateWebhookURLErrorDoesNotLeakURL(t *testing.T) {
	const secret = "https://user:s3cr3t@internal.example.com/wh"
	_, err := ValidateWebhookURL("ftp://user:s3cr3t@internal.example.com/wh")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != ErrWebhookURLInvalid.Error() {
		t.Fatalf("error string must be the static sentinel with no URL, got %q", got)
	}
	_ = secret
}

func TestNormalizeReminderLeadDaysClamps(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{in: -5, want: MinReminderLeadDays},
		{in: 0, want: 0},
		{in: 3, want: 3},
		{in: 14, want: 14},
		{in: 15, want: MaxReminderLeadDays},
		{in: 1000, want: MaxReminderLeadDays},
	}
	for _, tc := range cases {
		if got := NormalizeReminderLeadDays(tc.in); got != tc.want {
			t.Errorf("NormalizeReminderLeadDays(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestSaveWebhookSettingsEncryptStoreLoadDecryptRoundTrip is the service-level
// round-trip: SaveWebhookSettings encrypts the plaintext URL, hands the
// ciphertext to persistence, and DecryptWebhookURL recovers the original
// plaintext. The persisted value must NOT be the plaintext.
func TestSaveWebhookSettingsEncryptStoreLoadDecryptRoundTrip(t *testing.T) {
	svc, repo := newWebhookServiceForTest()
	const userID = 42
	const plaintextURL = "https://hooks.example.com/owner-42-secret-path"

	if err := svc.SaveWebhookSettings(context.Background(), userID, WebhookSettingsUpdate{
		Enabled:          true,
		URL:              plaintextURL,
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: 20, // out of bound: must clamp to 14
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}

	if repo.saveCalls != 1 || repo.savedUserID != userID {
		t.Fatalf("expected one save for user %d, got calls=%d user=%d", userID, repo.saveCalls, repo.savedUserID)
	}
	stored := repo.savedColumns
	if stored.ReminderLeadDays != MaxReminderLeadDays {
		t.Fatalf("expected clamped reminder_lead_days=%d, got %d", MaxReminderLeadDays, stored.ReminderLeadDays)
	}
	if stored.EncryptedURL == "" {
		t.Fatal("expected a non-empty ciphertext stored")
	}
	if stored.EncryptedURL == plaintextURL {
		t.Fatal("stored webhook_url must be ciphertext, not the plaintext URL")
	}

	got, err := svc.DecryptWebhookURL(userID, stored.EncryptedURL)
	if err != nil {
		t.Fatalf("DecryptWebhookURL: %v", err)
	}
	if got != plaintextURL {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plaintextURL)
	}
}

// TestDecryptWebhookURLRejectsCiphertextFromAnotherUser is the cross-user AAD
// contract, mirroring the TOTP ciphertext-from-another-user test: a webhook_url
// ciphertext sealed under owner A's aad must fail to open under owner B's aad,
// so an attacker with database write access cannot swap A's endpoint into B's
// row and have the notify pass deliver to it.
func TestDecryptWebhookURLRejectsCiphertextFromAnotherUser(t *testing.T) {
	svc, repo := newWebhookServiceForTest()
	const userA = 1
	const userB = 2
	const urlA = "https://hooks.example.com/owner-A"

	if err := svc.SaveWebhookSettings(context.Background(), userA, WebhookSettingsUpdate{
		Enabled:         true,
		URL:             urlA,
		NotifyPeriod:    true,
		NotifyOvulation: true,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings user A: %v", err)
	}
	ciphertextA := repo.savedColumns.EncryptedURL

	// Owner A opens their own ciphertext under their own aad — baseline.
	if got, err := svc.DecryptWebhookURL(userA, ciphertextA); err != nil || got != urlA {
		t.Fatalf("baseline decrypt for owner A = (%q, %v), want (%q, nil)", got, err, urlA)
	}

	// The cross-row swap: owner A's ciphertext placed in owner B's row must not
	// open under owner B's aad.
	got, err := svc.DecryptWebhookURL(userB, ciphertextA)
	if err == nil {
		t.Fatal("DecryptWebhookURL(B, ciphertextA) must fail: aad binding is not enforced")
	}
	if got != "" {
		t.Fatalf("expected empty plaintext on a rejected cross-user open, got %q", got)
	}

	// Sanity: the aad helpers for the two owners are distinct.
	if string(aadForWebhookURL(userA)) == string(aadForWebhookURL(userB)) {
		t.Fatal("aadForWebhookURL must differ per user id")
	}
}

func TestSaveWebhookSettingsRejectsInvalidURLWhenEnabled(t *testing.T) {
	svc, repo := newWebhookServiceForTest()

	err := svc.SaveWebhookSettings(context.Background(), 7, WebhookSettingsUpdate{
		Enabled:         true,
		URL:             "ftp://example.com/nope",
		NotifyPeriod:    true,
		NotifyOvulation: true,
	})
	if !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("expected ErrWebhookURLInvalid arming a webhook with a bad URL, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no persistence when the URL is rejected, got %d save calls", repo.saveCalls)
	}
}

// TestSaveWebhookSettingsDisabledWithNoURLClearsCiphertext confirms that
// turning delivery off with no endpoint clears any stored ciphertext (empty
// string), not an encrypted empty value.
func TestSaveWebhookSettingsDisabledWithNoURLClearsCiphertext(t *testing.T) {
	svc, repo := newWebhookServiceForTest()

	if err := svc.SaveWebhookSettings(context.Background(), 9, WebhookSettingsUpdate{
		Enabled:          false,
		URL:              "",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: 3,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}
	if repo.savedColumns.EncryptedURL != "" {
		t.Fatalf("expected empty stored URL when disabled with no endpoint, got %q", repo.savedColumns.EncryptedURL)
	}
	if repo.savedColumns.Enabled {
		t.Fatal("expected webhook_enabled=false persisted")
	}
}

// TestDecryptWebhookURLEmptyIsNoError confirms a blank stored value yields an
// empty URL with no error (webhook not configured), so the future notify pass
// treats it as "no endpoint" rather than an error.
func TestDecryptWebhookURLEmptyIsNoError(t *testing.T) {
	svc, _ := newWebhookServiceForTest()
	got, err := svc.DecryptWebhookURL(5, "")
	if err != nil {
		t.Fatalf("unexpected error for empty ciphertext: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty URL, got %q", got)
	}
}

// TestDecryptWebhookURLFailsAfterKeyRotation documents the fail-safe: a
// ciphertext sealed under one SECRET_KEY does not open under a different key, so
// the future notify pass skips that owner rather than deliver to garbage.
func TestDecryptWebhookURLFailsAfterKeyRotation(t *testing.T) {
	svc, repo := newWebhookServiceForTest()
	if err := svc.SaveWebhookSettings(context.Background(), 3, WebhookSettingsUpdate{
		Enabled:         true,
		URL:             "https://hooks.example.com/rotate",
		NotifyPeriod:    true,
		NotifyOvulation: true,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}
	ciphertext := repo.savedColumns.EncryptedURL

	rotated := NewWebhookSettingsService(&stubWebhookRepo{}, []byte("a-totally-different-32byte-key!!!"))
	if _, err := rotated.DecryptWebhookURL(3, ciphertext); err == nil {
		t.Fatal("expected decrypt to fail under a rotated SECRET_KEY")
	}

	// Guard the fixture: same key, same aad still opens (isolates the rotation
	// as the cause of the failure above).
	if _, _, err := security.DecryptField(ciphertext, []byte(webhookTestSecretKey), aadForWebhookURL(3)); err != nil {
		t.Fatalf("original-key decrypt should succeed: %v", err)
	}
}
