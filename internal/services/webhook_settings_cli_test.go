package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// stubWebhookReader returns a fixed owner (or a not-found) so the CLI
// orchestration can be tested without a DB.
type stubWebhookReader struct {
	user     models.User
	found    bool
	lookErr  error
	gotEmail string
}

func (s *stubWebhookReader) FindByNormalizedEmailOptional(_ context.Context, email string) (models.User, bool, error) {
	s.gotEmail = email
	if s.lookErr != nil {
		return models.User{}, false, s.lookErr
	}
	return s.user, s.found, nil
}

// encryptTestWebhookURL produces a ciphertext for a plaintext endpoint bound to
// userID, exactly as SaveWebhookSettings would, so a "keep URL" merge path can
// be exercised with a realistic stored value.
func encryptTestWebhookURL(t *testing.T, plaintext string, userID uint) string {
	t.Helper()
	ciphertext, err := security.EncryptField(plaintext, []byte(webhookTestSecretKey), aadForWebhookURL(userID))
	if err != nil {
		t.Fatalf("encrypt test url: %v", err)
	}
	return ciphertext
}

func newWebhookCLIServiceForTest(reader *stubWebhookReader) (*WebhookSettingsCLIService, *stubWebhookRepo) {
	settings, repo := newWebhookServiceForTest()
	return NewWebhookSettingsCLIService(reader, settings), repo
}

// TestResolveWebhookSettingsHostOnly proves the status view exposes the host
// only — never the full URL, path, query, or userinfo token.
func TestResolveWebhookSettingsHostOnly(t *testing.T) {
	const userID = uint(42)
	reader := &stubWebhookReader{
		found: true,
		user: models.User{
			ID:                     userID,
			WebhookEnabled:         true,
			WebhookURL:             encryptTestWebhookURL(t, "https://user:tok_SECRET@ntfy.example.io/topic?token=tok_SECRET", userID),
			WebhookNotifyPeriod:    true,
			WebhookNotifyOvulation: false,
			ReminderLeadDays:       4,
		},
	}
	svc, _ := newWebhookCLIServiceForTest(reader)

	view, err := svc.ResolveWebhookSettings(context.Background(), "Owner@Example.com")
	if err != nil {
		t.Fatalf("ResolveWebhookSettings: %v", err)
	}
	if !view.Configured || !view.Enabled {
		t.Fatalf("expected configured+enabled, got %+v", view)
	}
	if view.Host != "ntfy.example.io" {
		t.Fatalf("expected host-only, got %q", view.Host)
	}
	if view.NotifyOvulation {
		t.Fatalf("expected notify-ovulation=false to be reflected, got %+v", view)
	}
	if view.ReminderLeadDays != 4 {
		t.Fatalf("expected lead days 4, got %d", view.ReminderLeadDays)
	}
	// The email must be normalized before the lookup.
	if reader.gotEmail != "owner@example.com" {
		t.Fatalf("expected normalized email lookup, got %q", reader.gotEmail)
	}
}

// TestApplyWebhookSettingsMergePreservesUnspecified is the core merge contract: a
// flag-only patch (toggle one field) must keep the stored URL and the other
// toggles intact, because SaveWebhookSettings overwrites ALL columns.
func TestApplyWebhookSettingsMergePreservesUnspecified(t *testing.T) {
	const userID = uint(7)
	const storedURL = "https://ntfy.example/topic-keep"
	reader := &stubWebhookReader{
		found: true,
		user: models.User{
			ID:                     userID,
			WebhookEnabled:         true,
			WebhookURL:             encryptTestWebhookURL(t, storedURL, userID),
			WebhookNotifyPeriod:    true,
			WebhookNotifyOvulation: true,
			ReminderLeadDays:       6,
		},
	}
	svc, repo := newWebhookCLIServiceForTest(reader)

	// Patch ONLY notify-period=false; everything else must be preserved.
	falseVal := false
	view, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", WebhookSettingsPatch{NotifyPeriod: &falseVal}, false)
	if err != nil {
		t.Fatalf("ApplyWebhookSettings: %v", err)
	}
	if repo.saveCalls != 1 {
		t.Fatalf("expected exactly one save, got %d", repo.saveCalls)
	}
	saved := repo.savedColumns
	if !saved.Enabled {
		t.Fatalf("expected enabled preserved, got %+v", saved)
	}
	if saved.NotifyPeriod {
		t.Fatalf("expected notify-period=false applied, got %+v", saved)
	}
	if !saved.NotifyOvulation {
		t.Fatalf("expected notify-ovulation preserved true, got %+v", saved)
	}
	if saved.ReminderLeadDays != 6 {
		t.Fatalf("expected lead days preserved 6, got %d", saved.ReminderLeadDays)
	}
	// The stored URL must be re-persisted (as fresh ciphertext) and round-trip.
	if saved.EncryptedURL == "" {
		t.Fatal("expected the kept URL to be re-persisted as ciphertext")
	}
	if strings.Contains(saved.EncryptedURL, storedURL) || strings.Contains(saved.EncryptedURL, "ntfy.example") {
		t.Fatalf("re-persisted URL must be ciphertext, got %q", saved.EncryptedURL)
	}
	got, _, err := security.DecryptField(saved.EncryptedURL, []byte(webhookTestSecretKey), aadForWebhookURL(userID))
	if err != nil {
		t.Fatalf("decrypt re-persisted url: %v", err)
	}
	if got != storedURL {
		t.Fatalf("kept URL mismatch: got %q want %q", got, storedURL)
	}
	if view.Host != "ntfy.example" {
		t.Fatalf("expected returned view host-only, got %q", view.Host)
	}
}

// TestApplyWebhookSettingsSetURLEncrypts proves setting a new URL stores it as
// ciphertext via the reused save path.
func TestApplyWebhookSettingsSetURLEncrypts(t *testing.T) {
	const userID = uint(3)
	reader := &stubWebhookReader{found: true, user: models.User{ID: userID, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, repo := newWebhookCLIServiceForTest(reader)

	trueVal := true
	patch := WebhookSettingsPatch{Enabled: &trueVal}
	patch.SetURL("https://ntfy.example/newtopic")
	if _, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", patch, false); err != nil {
		t.Fatalf("ApplyWebhookSettings: %v", err)
	}
	if repo.savedColumns.EncryptedURL == "" {
		t.Fatal("expected ciphertext stored for a new URL")
	}
	got, _, err := security.DecryptField(repo.savedColumns.EncryptedURL, []byte(webhookTestSecretKey), aadForWebhookURL(userID))
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "https://ntfy.example/newtopic" {
		t.Fatalf("URL mismatch: got %q", got)
	}
}

// TestApplyWebhookSettingsClearURL proves --clear-url wipes the stored endpoint.
func TestApplyWebhookSettingsClearURL(t *testing.T) {
	const userID = uint(11)
	reader := &stubWebhookReader{
		found: true,
		user:  models.User{ID: userID, WebhookEnabled: true, WebhookURL: encryptTestWebhookURL(t, "https://ntfy.example/old", userID), WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3},
	}
	svc, repo := newWebhookCLIServiceForTest(reader)

	falseVal := false
	patch := WebhookSettingsPatch{Enabled: &falseVal}
	patch.ClearURL()
	view, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", patch, false)
	if err != nil {
		t.Fatalf("ApplyWebhookSettings: %v", err)
	}
	if repo.savedColumns.EncryptedURL != "" {
		t.Fatalf("expected cleared endpoint, got %q", repo.savedColumns.EncryptedURL)
	}
	if view.Configured {
		t.Fatalf("expected view not configured after clear, got %+v", view)
	}
}

// TestApplyWebhookSettingsInvalidSchemeRejected proves an invalid scheme is
// rejected by the reused validator and nothing is persisted.
func TestApplyWebhookSettingsInvalidSchemeRejected(t *testing.T) {
	reader := &stubWebhookReader{found: true, user: models.User{ID: 5, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, repo := newWebhookCLIServiceForTest(reader)

	trueVal := true
	patch := WebhookSettingsPatch{Enabled: &trueVal}
	patch.SetURL("ftp://evil.example/x")
	_, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", patch, false)
	if err == nil || !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("expected ErrWebhookURLInvalid, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no save on invalid URL, got %d", repo.saveCalls)
	}
}

// TestApplyWebhookSettingsDryRunNoSave proves --dry-run validates but never calls
// SaveWebhookSettings, while still returning the would-be view.
func TestApplyWebhookSettingsDryRunNoSave(t *testing.T) {
	reader := &stubWebhookReader{found: true, user: models.User{ID: 8, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, repo := newWebhookCLIServiceForTest(reader)

	trueVal := true
	patch := WebhookSettingsPatch{Enabled: &trueVal}
	patch.SetURL("https://ntfy.example/dry")
	view, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", patch, true)
	if err != nil {
		t.Fatalf("dry-run ApplyWebhookSettings: %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("dry-run must not save, got %d calls", repo.saveCalls)
	}
	if view.Host != "ntfy.example" || !view.Enabled {
		t.Fatalf("expected would-be view host-only+enabled, got %+v", view)
	}
}

// TestApplyWebhookSettingsDryRunInvalidRejected proves --dry-run still rejects an
// invalid URL (validation parity with a real save).
func TestApplyWebhookSettingsDryRunInvalidRejected(t *testing.T) {
	reader := &stubWebhookReader{found: true, user: models.User{ID: 9, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, repo := newWebhookCLIServiceForTest(reader)

	trueVal := true
	patch := WebhookSettingsPatch{Enabled: &trueVal}
	patch.SetURL("not-a-url")
	if _, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", patch, true); !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("expected ErrWebhookURLInvalid on dry-run, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("dry-run invalid must not save, got %d", repo.saveCalls)
	}
}

// TestApplyWebhookSettingsLeadDaysClamped proves the reused service clamps
// reminder_lead_days into range.
func TestApplyWebhookSettingsLeadDaysClamped(t *testing.T) {
	reader := &stubWebhookReader{found: true, user: models.User{ID: 12, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, repo := newWebhookCLIServiceForTest(reader)

	lead := 99
	if _, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", WebhookSettingsPatch{ReminderLeadDays: &lead}, false); err != nil {
		t.Fatalf("ApplyWebhookSettings: %v", err)
	}
	if repo.savedColumns.ReminderLeadDays != MaxReminderLeadDays {
		t.Fatalf("expected clamp to %d, got %d", MaxReminderLeadDays, repo.savedColumns.ReminderLeadDays)
	}

	negLead := -4
	if _, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", WebhookSettingsPatch{ReminderLeadDays: &negLead}, false); err != nil {
		t.Fatalf("ApplyWebhookSettings: %v", err)
	}
	if repo.savedColumns.ReminderLeadDays != MinReminderLeadDays {
		t.Fatalf("expected clamp to %d, got %d", MinReminderLeadDays, repo.savedColumns.ReminderLeadDays)
	}
}

// TestResolveWebhookSettingsNotFound proves a missing owner yields
// ErrWebhookOwnerNotFound.
func TestResolveWebhookSettingsNotFound(t *testing.T) {
	reader := &stubWebhookReader{found: false}
	svc, _ := newWebhookCLIServiceForTest(reader)
	if _, err := svc.ResolveWebhookSettings(context.Background(), "ghost@example.com"); !errors.Is(err, ErrWebhookOwnerNotFound) {
		t.Fatalf("expected ErrWebhookOwnerNotFound, got %v", err)
	}
}

// TestResolveWebhookSettingsEmailValidation proves blank/invalid emails are
// rejected before any lookup.
func TestResolveWebhookSettingsEmailValidation(t *testing.T) {
	reader := &stubWebhookReader{found: true}
	svc, _ := newWebhookCLIServiceForTest(reader)
	if _, err := svc.ResolveWebhookSettings(context.Background(), "   "); !errors.Is(err, ErrOperatorUserEmailRequired) {
		t.Fatalf("expected ErrOperatorUserEmailRequired, got %v", err)
	}
}

// TestResolveWebhookSettingsLookupError proves a repository error is wrapped as a
// lookup failure (not a panic, not a not-found).
func TestResolveWebhookSettingsLookupError(t *testing.T) {
	reader := &stubWebhookReader{lookErr: errors.New("db down")}
	svc, _ := newWebhookCLIServiceForTest(reader)
	_, err := svc.ResolveWebhookSettings(context.Background(), "owner@example.com")
	if err == nil || !errors.Is(err, ErrOperatorUserLookupFailed) {
		t.Fatalf("expected ErrOperatorUserLookupFailed, got %v", err)
	}
}

// TestApplyWebhookSettingsNotConfiguredView proves a not-configured owner's view
// reports Configured=false with an empty host.
func TestApplyWebhookSettingsNotConfiguredView(t *testing.T) {
	reader := &stubWebhookReader{found: true, user: models.User{ID: 20, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, _ := newWebhookCLIServiceForTest(reader)

	// A flag-only patch that leaves the (empty) URL and delivery disabled.
	falseVal := false
	view, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", WebhookSettingsPatch{Enabled: &falseVal}, false)
	if err != nil {
		t.Fatalf("ApplyWebhookSettings: %v", err)
	}
	if view.Configured || view.Host != "" {
		t.Fatalf("expected not-configured empty-host view, got %+v", view)
	}
}

// TestApplyWebhookSettingsNotFound proves ApplyWebhookSettings surfaces the
// resolve-owner not-found error (its own error return, distinct from Resolve).
func TestApplyWebhookSettingsNotFound(t *testing.T) {
	reader := &stubWebhookReader{found: false}
	svc, repo := newWebhookCLIServiceForTest(reader)
	falseVal := false
	if _, err := svc.ApplyWebhookSettings(context.Background(), "ghost@example.com", WebhookSettingsPatch{Enabled: &falseVal}, false); !errors.Is(err, ErrWebhookOwnerNotFound) {
		t.Fatalf("expected ErrWebhookOwnerNotFound, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no save when owner is missing, got %d", repo.saveCalls)
	}
}

// TestApplyWebhookSettingsEmailValidation proves ApplyWebhookSettings rejects a
// blank email before any lookup or save.
func TestApplyWebhookSettingsEmailValidation(t *testing.T) {
	reader := &stubWebhookReader{found: true}
	svc, repo := newWebhookCLIServiceForTest(reader)
	falseVal := false
	if _, err := svc.ApplyWebhookSettings(context.Background(), "  ", WebhookSettingsPatch{Enabled: &falseVal}, false); !errors.Is(err, ErrOperatorUserEmailRequired) {
		t.Fatalf("expected ErrOperatorUserEmailRequired, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no save on blank email, got %d", repo.saveCalls)
	}
}

// TestResolveWebhookSettingsDecryptFailure proves that a stored ciphertext which
// fails to open (e.g. after a SECRET_KEY rotation, modeled here by an aad
// bound to a different user id) surfaces as an error rather than leaking or
// panicking — the status view fails closed.
func TestResolveWebhookSettingsDecryptFailure(t *testing.T) {
	const userID = uint(31)
	// Encrypt under a DIFFERENT user id so the aad binding mismatches at decrypt.
	badCiphertext := encryptTestWebhookURL(t, "https://ntfy.example/topic", userID+1)
	reader := &stubWebhookReader{found: true, user: models.User{ID: userID, WebhookEnabled: true, WebhookURL: badCiphertext, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, _ := newWebhookCLIServiceForTest(reader)

	if _, err := svc.ResolveWebhookSettings(context.Background(), "owner@example.com"); err == nil || !strings.Contains(err.Error(), "decrypt current webhook url") {
		t.Fatalf("expected a decrypt failure on resolve, got %v", err)
	}
}

// TestApplyWebhookSettingsDecryptFailure proves the same fail-closed behavior on
// the apply path (it decrypts the current URL to support a URL-keeping merge).
func TestApplyWebhookSettingsDecryptFailure(t *testing.T) {
	const userID = uint(33)
	badCiphertext := encryptTestWebhookURL(t, "https://ntfy.example/topic", userID+1)
	reader := &stubWebhookReader{found: true, user: models.User{ID: userID, WebhookEnabled: true, WebhookURL: badCiphertext, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, repo := newWebhookCLIServiceForTest(reader)

	falseVal := false
	if _, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", WebhookSettingsPatch{NotifyPeriod: &falseVal}, false); err == nil || !strings.Contains(err.Error(), "decrypt current webhook url") {
		t.Fatalf("expected a decrypt failure on apply, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no save when the current URL cannot be decrypted, got %d", repo.saveCalls)
	}
}

// TestApplyWebhookSettingsDryRunDisabledNoURL covers the dry-run validation
// branch for a disabled webhook with no endpoint: it is valid (nothing to
// deliver) and writes nothing.
func TestApplyWebhookSettingsDryRunDisabledNoURL(t *testing.T) {
	reader := &stubWebhookReader{found: true, user: models.User{ID: 40, WebhookNotifyPeriod: true, WebhookNotifyOvulation: true, ReminderLeadDays: 3}}
	svc, repo := newWebhookCLIServiceForTest(reader)

	falseVal := false
	view, err := svc.ApplyWebhookSettings(context.Background(), "owner@example.com", WebhookSettingsPatch{Enabled: &falseVal}, true)
	if err != nil {
		t.Fatalf("dry-run disabled+no-URL should validate, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("dry-run must not save, got %d", repo.saveCalls)
	}
	if view.Configured || view.Enabled {
		t.Fatalf("expected a disabled, not-configured view, got %+v", view)
	}
}
