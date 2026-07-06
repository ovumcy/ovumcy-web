package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// webhookJSONRequestWithCSRF posts a raw JSON body carrying the auth + csrf
// cookies and the CSRF token in the X-CSRF-Token header (a JSON body has no
// csrf_token form field, so the header is the token source the extractor uses).
func webhookJSONRequestWithCSRF(t *testing.T, ctx settingsSecurityTestContext, body string, headers map[string]string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/webhook", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", ctx.csrfToken)
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("webhook json request failed: %v", err)
	}
	return response
}

// webhookTestAppSecretKey mirrors the "test-secret-key" the onboarding test app
// wires into BuildDependencies, so a test-side WebhookSettingsService decrypts
// exactly what the handler encrypted.
const webhookTestAppSecretKey = "test-secret-key"

func webhookDecryptService(t *testing.T, ctx settingsSecurityTestContext) *services.WebhookSettingsService {
	t.Helper()
	return services.NewWebhookSettingsService(db.NewRepositories(ctx.database).Users, []byte(webhookTestAppSecretKey))
}

func reloadUserForWebhook(t *testing.T, ctx settingsSecurityTestContext, userID uint) models.User {
	t.Helper()
	var reloaded models.User
	if err := ctx.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded
}

// TestWebhookSaveEncryptsURLAtRestAndRoundTrips is the encrypt-at-rest contract:
// saving a valid URL persists CIPHERTEXT (never the plaintext) in webhook_url,
// and the stored value round-trips back through DecryptWebhookURL to the exact
// plaintext the owner submitted.
func TestWebhookSaveEncryptsURLAtRestAndRoundTrips(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-encrypt@example.com")
	const plaintextURL = "https://ntfy.example/owner-secret-topic?token=tk_do_not_leak"

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {plaintextURL},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on webhook save, got %d", response.StatusCode)
	}

	reloaded := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if !reloaded.WebhookEnabled {
		t.Fatal("expected webhook_enabled=true persisted")
	}
	if reloaded.WebhookURL == "" {
		t.Fatal("expected a stored webhook_url ciphertext")
	}
	if reloaded.WebhookURL == plaintextURL {
		t.Fatal("stored webhook_url must be ciphertext, not the plaintext URL")
	}
	if strings.Contains(reloaded.WebhookURL, "tk_do_not_leak") || strings.Contains(reloaded.WebhookURL, "owner-secret-topic") {
		t.Fatal("stored webhook_url ciphertext must not contain plaintext fragments")
	}

	decrypted, err := webhookDecryptService(t, ctx).DecryptWebhookURL(reloaded.ID, reloaded.WebhookURL)
	if err != nil {
		t.Fatalf("decrypt round-trip: %v", err)
	}
	if decrypted != plaintextURL {
		t.Fatalf("round-trip mismatch: got %q, want %q", decrypted, plaintextURL)
	}
}

// TestWebhookSaveNonHTTPSchemeRejectedNothingPersisted covers the save-time
// scheme guard reused from the service: a non-http(s) URL is rejected with 400
// and nothing is persisted (webhook stays unconfigured/disabled).
func TestWebhookSaveNonHTTPSchemeRejectedNothingPersisted(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-scheme@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {"file:///etc/passwd"},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, map[string]string{"Accept": "application/json"})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-http(s) webhook URL, got %d", response.StatusCode)
	}

	reloaded := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if reloaded.WebhookEnabled {
		t.Fatal("expected webhook to stay disabled after a rejected save")
	}
	if reloaded.WebhookURL != "" {
		t.Fatalf("expected no webhook_url persisted after a rejected save, got %q", reloaded.WebhookURL)
	}
}

// TestWebhookSaveTogglesPersist confirms the enable + per-kind notify toggles
// persist independently (delivery off, both kinds narrowed).
func TestWebhookSaveTogglesPersist(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-toggles@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		// Delivery disabled, no URL: allowed (nothing to arm), toggles still save.
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"false"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on webhook toggle save, got %d", response.StatusCode)
	}

	reloaded := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if reloaded.WebhookEnabled {
		t.Fatal("expected webhook_enabled=false persisted")
	}
	if !reloaded.WebhookNotifyPeriod {
		t.Fatal("expected webhook_notify_period=true persisted")
	}
	if reloaded.WebhookNotifyOvulation {
		t.Fatal("expected webhook_notify_ovulation=false persisted")
	}
}

// TestWebhookSaveBlankURLKeepsStoredEndpoint is the write-only round-trip at the
// HTTP boundary: after a URL is stored, a second save that omits the URL (only
// toggling delivery) preserves the stored endpoint rather than clearing it.
func TestWebhookSaveBlankURLKeepsStoredEndpoint(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-keep@example.com")
	const storedURL = "https://ntfy.example/keep-me"

	first := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {storedURL},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, nil)
	_ = first.Body.Close()
	if first.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on first webhook save, got %d", first.StatusCode)
	}
	before := reloadUserForWebhook(t, ctx, ctx.user.ID)

	// Second save: no URL field, still enabled. The stored endpoint must survive.
	second := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_notify_period":    {"false"},
		"webhook_notify_ovulation": {"true"},
	}, nil)
	_ = second.Body.Close()
	if second.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on second webhook save, got %d", second.StatusCode)
	}

	after := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if after.WebhookURL == "" {
		t.Fatal("expected the stored endpoint to survive a blank-URL save")
	}
	decrypted, err := webhookDecryptService(t, ctx).DecryptWebhookURL(after.ID, after.WebhookURL)
	if err != nil {
		t.Fatalf("decrypt kept url: %v", err)
	}
	if decrypted != storedURL {
		t.Fatalf("expected kept endpoint %q, got %q", storedURL, decrypted)
	}
	if after.WebhookNotifyPeriod {
		t.Fatal("expected the notify_period toggle to update on the second save")
	}
	_ = before
}

// TestWebhookSaveRemoveClearsStoredEndpoint confirms the remove affordance
// clears the stored ciphertext and forces delivery off.
func TestWebhookSaveRemoveClearsStoredEndpoint(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-remove@example.com")

	first := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {"https://ntfy.example/remove-me"},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, nil)
	_ = first.Body.Close()
	if reloadUserForWebhook(t, ctx, ctx.user.ID).WebhookURL == "" {
		t.Fatal("precondition: expected a stored endpoint before removal")
	}

	remove := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_remove_url":       {"true"},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, nil)
	_ = remove.Body.Close()
	if remove.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on webhook remove, got %d", remove.StatusCode)
	}

	after := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if after.WebhookURL != "" {
		t.Fatalf("expected stored endpoint cleared by remove, got %q", after.WebhookURL)
	}
	if after.WebhookEnabled {
		t.Fatal("expected delivery forced off by remove")
	}
}

// TestWebhookSettingsPageNeverRendersStoredURL is the HEADLINE security test:
// after a URL with an embedded token is stored, the /settings page HTML must
// contain neither the plaintext URL, nor its token, nor the path/query — only a
// "configured" status plus at most the hostname. This pins the "no secret in
// HTML fields / transport" invariant for the webhook URL.
func TestWebhookSettingsPageNeverRendersStoredURL(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-writeonly@example.com")
	const (
		secretToken = "tk_super_secret_value"
		secretPath  = "owner-private-topic"
		host        = "ntfy.internal.example"
		plaintext   = "https://" + host + "/" + secretPath + "?token=" + secretToken
	)

	save := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {plaintext},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, nil)
	_ = save.Body.Close()
	if save.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on webhook save, got %d", save.StatusCode)
	}

	// Confirm the URL really is stored (so the test is meaningful), then assert
	// the rendered page never exposes it.
	stored := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if stored.WebhookURL == "" {
		t.Fatal("precondition: expected the webhook URL to be stored before rendering")
	}

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings page 200, got %d", response.StatusCode)
	}
	body := mustReadBodyString(t, response.Body)

	for _, forbidden := range []string{plaintext, secretToken, secretPath, "token=" + secretToken, stored.WebhookURL} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("settings page HTML leaked a webhook secret fragment %q", forbidden)
		}
	}

	// The status hook must report configured, and the hostname (non-secret) may
	// appear — proving the section renders without the secret.
	document := mustParseHTMLDocument(t, body)
	if htmlElementByAttr(document, "data-webhook-status", "configured") == nil {
		t.Fatal("expected the webhook status hook to report 'configured'")
	}
	if !strings.Contains(body, host) {
		t.Fatalf("expected the non-secret hostname %q to be shown as status", host)
	}
}

// TestWebhookSaveScopedToOwner is the cross-owner IDOR guard: owner B's request
// (its own session) can only ever write its own row. Owner A's stored endpoint
// is untouched by B's save. Combined with the AAD binding on the ciphertext,
// this proves a webhook save never crosses the owner boundary.
func TestWebhookSaveScopedToOwner(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-owner-a@example.com")

	// Owner A stores an endpoint.
	saveA := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {"https://ntfy.example/owner-a"},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, nil)
	_ = saveA.Body.Close()
	ownerABefore := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if ownerABefore.WebhookURL == "" {
		t.Fatal("precondition: owner A endpoint should be stored")
	}

	// A second independent owner B on the same instance.
	ownerB := createOnboardingTestUser(t, ctx.database, "webhook-owner-b@example.com", "StrongPass1", true)
	authB := loginAndExtractAuthCookieWithCSRF(t, ctx.app, ownerB.Email, "StrongPass1")
	csrfCookieB, csrfTokenB := loadSettingsCSRFContext(t, ctx.app, authB)

	formB := url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {"https://ntfy.example/owner-b"},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
		"csrf_token":               {csrfTokenB},
	}
	requestB := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/webhook", strings.NewReader(formB.Encode()))
	requestB.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	requestB.Header.Set("Cookie", settingsCookieHeader(authB, csrfCookieB))
	responseB, err := ctx.app.Test(requestB, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("owner B webhook save failed: %v", err)
	}
	_ = responseB.Body.Close()
	if responseB.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 for owner B save, got %d", responseB.StatusCode)
	}

	// Owner A's row is unchanged; owner B's row got B's endpoint.
	ownerAAfter := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if ownerAAfter.WebhookURL != ownerABefore.WebhookURL {
		t.Fatal("owner A's stored endpoint must not change when owner B saves")
	}
	ownerBRow := reloadUserForWebhook(t, ctx, ownerB.ID)
	decrypted, err := webhookDecryptService(t, ctx).DecryptWebhookURL(ownerB.ID, ownerBRow.WebhookURL)
	if err != nil {
		t.Fatalf("decrypt owner B url: %v", err)
	}
	if decrypted != "https://ntfy.example/owner-b" {
		t.Fatalf("owner B row should hold owner B endpoint, got %q", decrypted)
	}
}

// TestWebhookSaveHTMXReturnsSuccessMarkup covers the HTMX response branch: an
// HX-Request save returns 200 with dismissible success status markup (not a
// redirect), mirroring the other settings HTMX saves.
func TestWebhookSaveHTMXReturnsSuccessMarkup(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-htmx@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/webhook", url.Values{
		"webhook_enabled":          {"true"},
		"webhook_url":              {"https://ntfy.example/htmx-topic"},
		"webhook_notify_period":    {"true"},
		"webhook_notify_ovulation": {"true"},
	}, map[string]string{"HX-Request": "true", "Accept-Language": "en"})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	if htmlElementByTagAndClass(mustParseHTMLDocument(t, mustReadBodyString(t, response.Body)), "div", "status-ok") == nil {
		t.Fatal("expected htmx success status markup for webhook update")
	}
}

// TestWebhookSaveAcceptsJSONBody covers the JSON body-binding path and the JSON
// success response block: a Content-Type: application/json save persists and the
// endpoint echoes the ok/status/toggle payload.
func TestWebhookSaveAcceptsJSONBody(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-json-body@example.com")

	response := webhookJSONRequestWithCSRF(t, ctx, `{"webhook_enabled":true,"webhook_url":"https://ntfy.example/json-topic","webhook_notify_period":true,"webhook_notify_ovulation":false}`, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)

	reloaded := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if !reloaded.WebhookEnabled {
		t.Fatal("expected webhook_enabled=true persisted from JSON body")
	}
	if reloaded.WebhookNotifyOvulation {
		t.Fatal("expected webhook_notify_ovulation=false persisted from JSON body")
	}
	decrypted, err := webhookDecryptService(t, ctx).DecryptWebhookURL(reloaded.ID, reloaded.WebhookURL)
	if err != nil {
		t.Fatalf("decrypt json-body url: %v", err)
	}
	if decrypted != "https://ntfy.example/json-topic" {
		t.Fatalf("expected JSON-body endpoint stored, got %q", decrypted)
	}
}

// TestWebhookSaveRejectsMalformedJSONBody covers the JSON-bind parse-error
// branch of parseWebhookSettingsInput: a syntactically broken JSON body is a 400
// invalid-input (not a 500), and nothing is persisted.
func TestWebhookSaveRejectsMalformedJSONBody(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "webhook-bad-json@example.com")

	response := webhookJSONRequestWithCSRF(t, ctx, `{"webhook_enabled":true,`, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusBadRequest)

	reloaded := reloadUserForWebhook(t, ctx, ctx.user.ID)
	if reloaded.WebhookEnabled || reloaded.WebhookURL != "" {
		t.Fatal("expected nothing persisted after a malformed JSON webhook body")
	}
}

// TestMapSettingsWebhookSaveErrorDefaultsToInternal pins the error-mapping
// contract directly: ErrWebhookURLInvalid maps to a 400 validation spec, and any
// other error maps to the 500 internal spec (the default branch a live handler
// only reaches on a persistence failure).
func TestMapSettingsWebhookSaveErrorDefaultsToInternal(t *testing.T) {
	invalid := mapSettingsWebhookSaveError(services.ErrWebhookURLInvalid)
	if invalid.Status != http.StatusBadRequest {
		t.Fatalf("expected ErrWebhookURLInvalid -> 400, got %d", invalid.Status)
	}

	other := mapSettingsWebhookSaveError(errors.New("db write exploded"))
	if other.Status != http.StatusInternalServerError {
		t.Fatalf("expected a generic error -> 500, got %d", other.Status)
	}
}
