package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// stubWebhookRepo captures the last SaveWebhookSettings call so tests can assert
// the ciphertext the service produced and persisted. loadUser / loadErr back the
// LoadSettingsByID read the write-only form-save path uses to resolve the
// "blank URL = keep existing" semantics.
type stubWebhookRepo struct {
	savedUserID  uint
	savedColumns models.WebhookSettingsColumns
	saveErr      error
	saveCalls    int
	loadUser     models.User
	loadErr      error
	loadCalls    int
}

func (s *stubWebhookRepo) SaveWebhookSettings(_ context.Context, userID uint, settings models.WebhookSettingsColumns) error {
	s.saveCalls++
	s.savedUserID = userID
	s.savedColumns = settings
	return s.saveErr
}

func (s *stubWebhookRepo) LoadSettingsByID(_ context.Context, _ uint) (models.User, error) {
	s.loadCalls++
	return s.loadUser, s.loadErr
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
		// Passes the CRLF guard but fails url.Parse itself (invalid percent
		// escape), so it exercises the parse-error branch rather than the
		// scheme/host checks.
		{name: "unparseable url rejected", raw: "http://example.com/%zz", wantErr: true},
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

// TestSaveWebhookSettingsDisabledWithURLPersistsCiphertext covers the "disabled
// but an endpoint was supplied" path: the owner turned delivery off yet kept a
// valid URL in the form. The URL must still be validated and encrypted so
// re-enabling later needs no re-entry, and webhook_enabled must persist as
// false. The stored value is ciphertext (not the plaintext) and round-trips
// back through DecryptWebhookURL.
func TestSaveWebhookSettingsDisabledWithURLPersistsCiphertext(t *testing.T) {
	svc, repo := newWebhookServiceForTest()
	const userID = 11
	const plaintextURL = "https://hooks.example.com/kept-while-disabled"

	if err := svc.SaveWebhookSettings(context.Background(), userID, WebhookSettingsUpdate{
		Enabled:          false,
		URL:              plaintextURL,
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: 2,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}

	if repo.saveCalls != 1 {
		t.Fatalf("expected exactly one save, got %d", repo.saveCalls)
	}
	if repo.savedColumns.Enabled {
		t.Fatal("expected webhook_enabled=false persisted while disabled")
	}
	stored := repo.savedColumns.EncryptedURL
	if stored == "" {
		t.Fatal("expected the kept URL to be stored as ciphertext, got empty")
	}
	if stored == plaintextURL {
		t.Fatal("stored webhook_url must be ciphertext, not the plaintext URL")
	}
	if repo.savedColumns.ReminderLeadDays != 2 {
		t.Fatalf("expected reminder_lead_days=2 persisted, got %d", repo.savedColumns.ReminderLeadDays)
	}

	got, err := svc.DecryptWebhookURL(userID, stored)
	if err != nil {
		t.Fatalf("DecryptWebhookURL: %v", err)
	}
	if got != plaintextURL {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plaintextURL)
	}
}

// TestSaveWebhookSettingsDisabledWithInvalidURLRejected covers the validation
// branch of the "disabled but URL supplied" path: an unparseable/other-scheme
// URL is rejected even when delivery is off, so a bad value never reaches
// persistence.
func TestSaveWebhookSettingsDisabledWithInvalidURLRejected(t *testing.T) {
	svc, repo := newWebhookServiceForTest()

	err := svc.SaveWebhookSettings(context.Background(), 12, WebhookSettingsUpdate{
		Enabled:         false,
		URL:             "file:///etc/passwd",
		NotifyPeriod:    true,
		NotifyOvulation: true,
	})
	if !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("expected ErrWebhookURLInvalid for a bad URL while disabled, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no persistence when the URL is rejected, got %d save calls", repo.saveCalls)
	}
}

// TestSaveWebhookSettingsEncryptFailurePropagates covers the encrypt-error
// branches in both the enabled and the disabled-with-URL paths: a service built
// with an empty secret key makes security.EncryptField fail, and the wrapped
// error must surface (no partial save). EncryptField requires a non-empty key,
// so an empty key deterministically triggers the failure without touching real
// crypto internals.
func TestSaveWebhookSettingsEncryptFailurePropagates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{name: "enabled path", enabled: true},
		{name: "disabled-with-url path", enabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubWebhookRepo{}
			svc := NewWebhookSettingsService(repo, nil) // empty secret key

			err := svc.SaveWebhookSettings(context.Background(), 13, WebhookSettingsUpdate{
				Enabled:         tc.enabled,
				URL:             "https://hooks.example.com/enc-fail",
				NotifyPeriod:    true,
				NotifyOvulation: true,
			})
			if err == nil {
				t.Fatal("expected an encrypt failure to surface, got nil")
			}
			if errors.Is(err, ErrWebhookURLInvalid) {
				t.Fatalf("expected an encrypt error, not a validation error: %v", err)
			}
			if repo.saveCalls != 0 {
				t.Fatalf("expected no persistence when encryption fails, got %d save calls", repo.saveCalls)
			}
		})
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

// storeWebhookURLForForm seals plaintextURL under userID's aad and returns the
// ciphertext, so the form-save tests can seed a stored endpoint the way the
// database row would hold it.
func storeWebhookURLForForm(t *testing.T, userID uint, plaintextURL string) string {
	t.Helper()
	ciphertext, err := security.EncryptField(plaintextURL, []byte(webhookTestSecretKey), aadForWebhookURL(userID))
	if err != nil {
		t.Fatalf("seed ciphertext: %v", err)
	}
	return ciphertext
}

// TestSaveWebhookSettingsFromFormBlankURLKeepsStoredEndpoint is the write-only
// contract: submitting no URL (URLProvided=false) while an endpoint is already
// stored preserves that endpoint — the service decrypts the stored ciphertext
// and re-supplies it — so the secret never has to round-trip through transport.
func TestSaveWebhookSettingsFromFormBlankURLKeepsStoredEndpoint(t *testing.T) {
	const userID = 21
	const storedURL = "https://ntfy.example/kept-topic"
	svc, repo := newWebhookServiceForTest()
	repo.loadUser = models.User{WebhookURL: storeWebhookURLForForm(t, userID, storedURL), ReminderLeadDays: 7}

	if err := svc.SaveWebhookSettingsFromForm(context.Background(), userID, WebhookSettingsFormUpdate{
		Enabled:         true,
		NotifyPeriod:    true,
		NotifyOvulation: false,
		URLProvided:     false,
	}); err != nil {
		t.Fatalf("SaveWebhookSettingsFromForm: %v", err)
	}

	if repo.saveCalls != 1 {
		t.Fatalf("expected one save, got %d", repo.saveCalls)
	}
	if !repo.savedColumns.Enabled {
		t.Fatal("expected webhook_enabled=true persisted")
	}
	if repo.savedColumns.ReminderLeadDays != 7 {
		t.Fatalf("expected reminder_lead_days preserved at 7, got %d", repo.savedColumns.ReminderLeadDays)
	}
	got, err := svc.DecryptWebhookURL(userID, repo.savedColumns.EncryptedURL)
	if err != nil {
		t.Fatalf("decrypt persisted url: %v", err)
	}
	if got != storedURL {
		t.Fatalf("expected stored endpoint preserved (%q), got %q", storedURL, got)
	}
}

// TestSaveWebhookSettingsFromFormReplacesURLWhenProvided confirms a newly-typed
// URL replaces the stored one and is re-encrypted (ciphertext, not plaintext).
func TestSaveWebhookSettingsFromFormReplacesURLWhenProvided(t *testing.T) {
	const userID = 22
	const newURL = "https://gotify.example/message?token=abc123"
	svc, repo := newWebhookServiceForTest()
	repo.loadUser = models.User{WebhookURL: storeWebhookURLForForm(t, userID, "https://old.example/topic"), ReminderLeadDays: 3}

	if err := svc.SaveWebhookSettingsFromForm(context.Background(), userID, WebhookSettingsFormUpdate{
		Enabled:         true,
		NotifyPeriod:    true,
		NotifyOvulation: true,
		URL:             newURL,
		URLProvided:     true,
	}); err != nil {
		t.Fatalf("SaveWebhookSettingsFromForm: %v", err)
	}
	if repo.savedColumns.EncryptedURL == newURL {
		t.Fatal("stored URL must be ciphertext, not plaintext")
	}
	got, err := svc.DecryptWebhookURL(userID, repo.savedColumns.EncryptedURL)
	if err != nil {
		t.Fatalf("decrypt persisted url: %v", err)
	}
	if got != newURL {
		t.Fatalf("expected replaced endpoint %q, got %q", newURL, got)
	}
}

// TestSaveWebhookSettingsFromFormRemoveClearsAndDisables confirms the remove
// affordance clears the stored endpoint and forces delivery off, even if the
// form also carried enabled=true and a URL.
func TestSaveWebhookSettingsFromFormRemoveClearsAndDisables(t *testing.T) {
	const userID = 23
	svc, repo := newWebhookServiceForTest()
	repo.loadUser = models.User{WebhookURL: storeWebhookURLForForm(t, userID, "https://ntfy.example/gone"), ReminderLeadDays: 5}

	if err := svc.SaveWebhookSettingsFromForm(context.Background(), userID, WebhookSettingsFormUpdate{
		Enabled:         true,
		NotifyPeriod:    true,
		NotifyOvulation: true,
		URL:             "https://ntfy.example/ignored",
		URLProvided:     true,
		RemoveURL:       true,
	}); err != nil {
		t.Fatalf("SaveWebhookSettingsFromForm: %v", err)
	}
	if repo.savedColumns.Enabled {
		t.Fatal("expected delivery forced off by remove")
	}
	if repo.savedColumns.EncryptedURL != "" {
		t.Fatalf("expected stored endpoint cleared, got %q", repo.savedColumns.EncryptedURL)
	}
}

// TestSaveWebhookSettingsFromFormEnableWithoutURLRejected confirms enabling with
// no stored and no supplied URL is rejected (ErrWebhookURLInvalid) and nothing
// persists — a webhook can never be armed without a deliverable target.
func TestSaveWebhookSettingsFromFormEnableWithoutURLRejected(t *testing.T) {
	const userID = 24
	svc, repo := newWebhookServiceForTest()
	repo.loadUser = models.User{WebhookURL: "", ReminderLeadDays: 3}

	err := svc.SaveWebhookSettingsFromForm(context.Background(), userID, WebhookSettingsFormUpdate{
		Enabled:         true,
		NotifyPeriod:    true,
		NotifyOvulation: true,
		URLProvided:     false,
	})
	if !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("expected ErrWebhookURLInvalid enabling with no endpoint, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no persistence, got %d save calls", repo.saveCalls)
	}
}

// TestSaveWebhookSettingsFromFormLoadErrorPropagates confirms a settings-load
// failure surfaces and nothing is persisted.
func TestSaveWebhookSettingsFromFormLoadErrorPropagates(t *testing.T) {
	svc, repo := newWebhookServiceForTest()
	repo.loadErr = errors.New("load boom")

	err := svc.SaveWebhookSettingsFromForm(context.Background(), 25, WebhookSettingsFormUpdate{Enabled: false})
	if err == nil {
		t.Fatal("expected the load error to surface")
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no persistence on load failure, got %d save calls", repo.saveCalls)
	}
}

// TestBuildWebhookURLDisplay covers the render-safe status/host projection: it
// exposes the hostname only (never the path/query/userinfo secret), reports
// not-configured for an empty value, and reports configured-but-hostless for a
// ciphertext that will not open.
func TestBuildWebhookURLDisplay(t *testing.T) {
	const userID = 31
	svc, _ := newWebhookServiceForTest()

	// Not configured.
	if got := svc.BuildWebhookURLDisplay(userID, ""); got.Configured || got.Host != "" {
		t.Fatalf("empty stored value should be not-configured, got %+v", got)
	}

	// Configured: host only, secret path/query/userinfo dropped.
	ciphertext := storeWebhookURLForForm(t, userID, "https://user:s3cr3t@ntfy.example.com:8443/topic?token=abc123")
	got := svc.BuildWebhookURLDisplay(userID, ciphertext)
	if !got.Configured {
		t.Fatal("expected configured=true for a stored endpoint")
	}
	if got.Host != "ntfy.example.com" {
		t.Fatalf("expected host-only 'ntfy.example.com', got %q", got.Host)
	}
	if strings.Contains(got.Host, "s3cr3t") || strings.Contains(got.Host, "token") || strings.Contains(got.Host, "topic") || strings.Contains(got.Host, "8443") {
		t.Fatalf("host projection leaked non-host components: %q", got.Host)
	}

	// Configured but un-openable (wrong-aad ciphertext): configured, no host.
	otherOwnerCiphertext := storeWebhookURLForForm(t, userID+1, "https://ntfy.example.com/topic")
	unopenable := svc.BuildWebhookURLDisplay(userID, otherOwnerCiphertext)
	if !unopenable.Configured || unopenable.Host != "" {
		t.Fatalf("un-openable ciphertext should be configured-but-hostless, got %+v", unopenable)
	}
}
