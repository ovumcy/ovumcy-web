package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// countingSettingsLoader counts the settings reads a request issues. It is how
// "the answer was rebuilt from the database" is told apart from "the answer was
// assembled from what the caller asked for" — the two are indistinguishable by
// their rendered output on a successful write, which is exactly when the
// difference matters.
type countingSettingsLoader struct {
	inner *services.SettingsService
	loads int
}

func (loader *countingSettingsLoader) LoadSettings(ctx context.Context, userID uint) (models.User, error) {
	loader.loads++
	return loader.inner.LoadSettings(ctx, userID)
}

// TestWebhookRemovalRefusesWithoutAValidCSRFToken keeps the new destructive
// endpoint inside the endpoint defense-in-depth rule. It is a route that
// withdraws a delivery configuration in one request, where the same effect
// previously cost a full settings form submission.
func TestWebhookRemovalRefusesWithoutAValidCSRFToken(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-remove-csrf@example.com")
	seedEgressRow(t, owner.database, owner.user.ID, egressRenderRow{
		webhookURLPlaintext: "https://ntfy.example.test/topic",
		webhookEnabled:      true,
		notifyPeriod:        true,
	})

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current/webhook", nil)
	request.Header.Set("Cookie", owner.authCookie)
	response, err := owner.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("webhook removal without a token failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without a csrf token, got %d", response.StatusCode)
	}
	if reloadEgressUser(t, owner.database, owner.user.ID).WebhookURL == "" {
		t.Fatal("a request refused for CSRF still withdrew the endpoint")
	}
}

// TestWebhookRemovalRefusesAnUnauthenticatedCaller pairs with the CSRF case: the
// route answers the session, never the request alone.
func TestWebhookRemovalRefusesAnUnauthenticatedCaller(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-remove-anon@example.com")

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current/webhook", nil)
	request.Header.Set("Accept", "application/json")
	response, err := owner.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("anonymous webhook removal failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected an anonymous caller to be refused, got %d", response.StatusCode)
	}
}

// TestWebhookRemovalKeepsTheOwnersReminderKindsAndLeadWindow is the transport
// half of the write's contract: the handler reaches the dedicated writer, not
// the shared save. Routing it through SaveWebhookSettings would zero the shared
// lead window, which makes a cycle anchor due on exactly one calendar day.
func TestWebhookRemovalKeepsTheOwnersReminderKindsAndLeadWindow(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-remove-keeps@example.com")
	seedEgressRow(t, owner.database, owner.user.ID, egressRenderRow{
		webhookURLPlaintext: "https://ntfy.example.test/topic",
		webhookEnabled:      true,
		notifyPeriod:        true,
		notifyOvulation:     true,
	})
	if err := owner.database.Model(&models.User{}).Where("id = ?", owner.user.ID).Update("reminder_lead_days", 5).Error; err != nil {
		t.Fatalf("seed the lead window: %v", err)
	}

	response := deleteWebhookWithCSRF(t, owner)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected the withdrawal to succeed, got %d", response.StatusCode)
	}

	after := reloadEgressUser(t, owner.database, owner.user.ID)
	if after.WebhookURL != "" || after.WebhookEnabled {
		t.Fatal("expected the endpoint withdrawn")
	}
	if !after.WebhookNotifyPeriod || !after.WebhookNotifyOvulation {
		t.Fatal("withdrawing the endpoint forgot which reminders the owner had chosen")
	}
	if after.ReminderLeadDays != 5 {
		t.Fatalf("withdrawing the endpoint rewrote the shared lead window to %d", after.ReminderLeadDays)
	}
}

// TestWebhookRemovalWorksOnAnEndpointThisInstanceCannotRead is the case the old
// remove-checkbox could not serve. The save path validates and re-encrypts a
// URL, so the one row an owner most needs to withdraw — the one left behind by a
// rotated SECRET_KEY — was the one row it refused to act on.
func TestWebhookRemovalWorksOnAnEndpointThisInstanceCannotRead(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-remove-unreadable@example.com")
	seedEgressRow(t, owner.database, owner.user.ID, egressRenderRow{
		webhookURLPlaintext:   "https://ntfy.example.test/topic",
		webhookURLOwnerOffset: 1,
		webhookEnabled:        true,
		notifyPeriod:          true,
	})

	response := deleteWebhookWithCSRF(t, owner)
	defer func() { _ = response.Body.Close() }()

	if after := reloadEgressUser(t, owner.database, owner.user.ID); after.WebhookURL != "" {
		t.Fatal("an endpoint this instance cannot read must still be withdrawable")
	}
}

// TestEveryEgressMutationRebuildsItsBlockFromAReadAfterTheWrite is C9. A
// response assembled from the request's intent renders a state sentence the
// write has just falsified, beside a fresh success message — and it does so on
// SUCCESS, which is when nobody looks.
func TestEveryEgressMutationRebuildsItsBlockFromAReadAfterTheWrite(t *testing.T) {
	cases := []struct {
		name      string
		method    string
		path      string
		seed      egressRenderRow
		wantState string
	}{
		{
			name:   "withdrawing the endpoint",
			method: http.MethodDelete,
			path:   "/api/v1/users/current/webhook",
			seed: egressRenderRow{
				webhookURLPlaintext: "https://ntfy.example.test/topic",
				webhookEnabled:      true,
				notifyPeriod:        true,
			},
			wantState: `data-egress-webhook-state="not_configured"`,
		},
		{
			name:   "withdrawing the calendar link",
			method: http.MethodDelete,
			path:   "/api/v1/users/current/calendar-feed",
			seed: egressRenderRow{
				feedSelector:        "sel7777777777777777777777777777",
				useCurrentFeedEpoch: true,
			},
			wantState: `data-egress-feed-state="none"`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler, database, loader := newCountingEgressHandler(t)
			owner := createEgressLedgerUser(t, database, "egress-rebuild-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com", models.RoleOwner)
			seedEgressRow(t, database, owner.ID, testCase.seed)

			app := fiber.New()
			app.Use(handler.LanguageMiddleware)
			app.Use(func(c fiber.Ctx) error {
				reloaded := reloadEgressUser(t, database, owner.ID)
				c.Locals(contextUserKey, &reloaded)
				return c.Next()
			})
			app.Delete("/api/v1/users/current/webhook", handler.RemoveWebhookDestination)
			app.Delete("/api/v1/users/current/calendar-feed", handler.RevokeCalendarFeed)

			before := loader.loads
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			request.Header.Set("HX-Request", "true")
			request.Header.Set("Accept", "text/html")
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("mutation failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			body := mustReadBodyString(t, response.Body)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", response.StatusCode, body)
			}
			if loader.loads <= before {
				t.Fatal("the response was built without re-reading the row: a block assembled from the request's intent renders a sentence the write just falsified")
			}
			if !strings.Contains(body, testCase.wantState) {
				t.Fatalf("expected the rebuilt block to carry %s", testCase.wantState)
			}
			if !strings.Contains(body, "settings-egress-status") {
				t.Fatal("the rebuilt block carries no status island, so the outcome is announced nowhere")
			}
		})
	}
}

// TestEgressLedgerFieldsAreUnchangedByAnyNumberOfFeedPolls is the .ics
// telemetry prohibition, stated as the only test that can fail.
//
// It seeds a PRE-032 row — a selector and a bcrypt verifier hash with no MAC —
// because that is the only row a poll still writes to: the first successful poll
// backfills the MAC. A fixture built from current code already carries a MAC, so
// the same assertion over it passes without the subject ever being exercised,
// which is how a per-poll signal could be introduced under a green suite.
func TestEgressLedgerFieldsAreUnchangedByAnyNumberOfFeedPolls(t *testing.T) {
	owner := newSettingsSecurityTestContextWithOptions(t, "egress-polls@example.com", onboardingTestAppOptions{enableCSRF: true})

	fullToken, columns, err := services.GenerateCalendarFeedToken([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("mint a feed token: %v", err)
	}
	epoch, err := security.CalendarFeedKeyEpoch([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("derive the running feed key epoch: %v", err)
	}
	if err := owner.database.Model(&models.User{}).Where("id = ?", owner.user.ID).Updates(map[string]any{
		"calendar_feed_selector":      columns.Selector,
		"calendar_feed_verifier_hash": columns.VerifierHash,
		// The pre-032 shape: no MAC, so the first poll has a write to make.
		"calendar_feed_verifier_mac": "",
		"calendar_feed_key_epoch":    epoch,
	}).Error; err != nil {
		t.Fatalf("seed a pre-032 feed row: %v", err)
	}

	beforeBody := fetchPageBody(t, owner.app, "/settings", owner.authCookie)
	before := egressLedgerFingerprint(t, beforeBody)

	for poll := range 3 {
		request := httptest.NewRequest(http.MethodGet, "/calendar/feed/"+url.PathEscape(fullToken)+".ics", nil)
		response, pollErr := owner.app.Test(request, testConfigNoTimeout)
		if pollErr != nil {
			t.Fatalf("poll %d failed: %v", poll, pollErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("poll %d answered %d; the assertion below would then be measuring a refused request", poll, response.StatusCode)
		}
		_ = response.Body.Close()
	}

	// Anti-vacuity anchor: the polls must actually have reached the row, which
	// the MAC backfill proves. Without it a token that never verified would
	// leave the ledger unchanged for the wrong reason.
	if reloadEgressUser(t, owner.database, owner.user.ID).CalendarFeedVerifierMAC == "" {
		t.Fatal("no poll reached the row (the MAC was never backfilled), so this test measured nothing")
	}

	after := egressLedgerFingerprint(t, fetchPageBody(t, owner.app, "/settings", owner.authCookie))
	if before != after {
		t.Fatalf("polling the feed changed what the ledger says.\n  before: %s\n  after:  %s\nPolls are deliberately unaudited; no ledger field may answer them.", before, after)
	}
}

// TestEgressSectionRendersTheMedicalDisclaimerExactlyOnce holds the merge's own
// arithmetic. Two sections each carried the safety sentence; one section must
// carry it once — the constitution's requirement is the count of SURFACES that
// render it, and a merged surface that renders it twice is a formatting defect
// rather than extra safety.
func TestEgressSectionRendersTheMedicalDisclaimerExactlyOnce(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-disclaimer@example.com")
	seedEgressRow(t, owner.database, owner.user.ID, egressRenderRow{
		webhookURLPlaintext: "https://ntfy.example.test/topic",
		webhookEnabled:      true,
		notifyPeriod:        true,
		feedSelector:        "sel8888888888888888888888888888",
		useCurrentFeedEpoch: true,
	})

	body := fetchPageBody(t, owner.app, "/settings", owner.authCookie)
	if count := strings.Count(body, "data-egress-disclaimer"); count != 1 {
		t.Fatalf("expected exactly one medical-safety disclaimer in the egress section, found %d", count)
	}
	if !strings.Contains(body, "not medical advice or a method of contraception") {
		t.Fatal("the egress section dropped the medical-safety sentence")
	}
}

// TestEgressSectionEnumeratesWhatEachRouteCarries pins the surface that answers
// the question "nothing leaves" is really about. A page that names the routes
// and stays silent about their contents answers a weaker question.
func TestEgressSectionEnumeratesWhatEachRouteCarries(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-payload@example.com")
	seedEgressRow(t, owner.database, owner.user.ID, egressRenderRow{
		webhookURLPlaintext: "https://ntfy.example.test/topic",
		webhookEnabled:      true,
		notifyPeriod:        true,
		feedSelector:        "sel9999999999999999999999999999",
		useCurrentFeedEpoch: true,
	})

	body := fetchPageBody(t, owner.app, "/settings", owner.authCookie)
	for _, hook := range []string{
		"data-egress-webhook-payload", "data-egress-feed-payload",
		"data-egress-webhook-status", "data-egress-feed-status",
		"data-egress-webhook-evidence", "data-egress-feed-evidence",
		"data-egress-webhook-host", "data-egress-feed-hint",
		"data-egress-section-status", "data-egress-path",
		"data-settings-webhook-remove",
	} {
		if !strings.Contains(body, hook) {
			t.Fatalf("the egress section renders no %s", hook)
		}
	}

	// The predicted date and the safety sentence are payload, not decoration.
	messages := mustEnglishMessages(t)
	for _, key := range []string{
		"settings.egress.payload.webhook.event_date",
		"settings.egress.payload.webhook.disclaimer",
		"settings.egress.payload.feed.uid",
	} {
		if sentence := messages[key]; sentence == "" || !strings.Contains(body, sentence) {
			t.Fatalf("the payload inventory does not name %s", key)
		}
	}
}

// TestPrivacyPageLinksTheLedgerOnlyForASignedInReader keeps /privacy free of
// anything about this instance's configuration: the page is unauthenticated, so
// even the presence of a link into an owner-only surface is shown only to a
// reader who already has a session.
func TestPrivacyPageLinksTheLedgerOnlyForASignedInReader(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-privacy-link@example.com")

	anonymous := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	anonymousResponse, err := owner.app.Test(anonymous, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("anonymous privacy request failed: %v", err)
	}
	anonymousBody := mustReadBodyString(t, anonymousResponse.Body)
	_ = anonymousResponse.Body.Close()
	if strings.Contains(anonymousBody, "data-privacy-ledger-link") {
		t.Fatal("the unauthenticated privacy page links an owner-only surface")
	}

	signedIn := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	signedIn.Header.Set("Cookie", owner.authCookie)
	signedInResponse, err := owner.app.Test(signedIn, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("signed-in privacy request failed: %v", err)
	}
	signedInBody := mustReadBodyString(t, signedInResponse.Body)
	_ = signedInResponse.Body.Close()
	if !strings.Contains(signedInBody, "data-privacy-ledger-link") {
		t.Fatal("a signed-in reader is offered no route to the ledger")
	}
	if !strings.Contains(signedInBody, "/settings#settings-egress") {
		t.Fatal("the ledger link does not point at the merged section")
	}
}

// --- helpers -------------------------------------------------------------

// egressLedgerFingerprint reduces a rendered settings body to exactly the egress
// facts, so a comparison across two renders cannot be satisfied by unrelated
// churn and cannot miss a change to one of them.
func egressLedgerFingerprint(t *testing.T, body string) string {
	t.Helper()

	parts := make([]string, 0, 8)
	for _, attribute := range []string{
		"data-egress-section-state",
		"data-egress-webhook-state",
		"data-egress-feed-state",
		"data-egress-state-key",
	} {
		parts = append(parts, attribute+"="+strings.Join(extractAttributeValues(t, body, attribute), "|"))
	}
	parts = append(parts, "delivered-at-present="+boolText(strings.Contains(body, "data-egress-webhook-delivered-at")))
	parts = append(parts, "revealed-at-present="+boolText(strings.Contains(body, "data-egress-feed-revealed-at")))
	for _, attribute := range []string{"data-egress-webhook-delivered-at", "data-egress-feed-revealed-at"} {
		if index := strings.Index(body, attribute); index >= 0 {
			parts = append(parts, attribute+"-context="+body[maxInt(0, index-120):index])
		}
	}
	return strings.Join(parts, "; ")
}

func boolText(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func reloadEgressUser(t *testing.T, database *gorm.DB, userID uint) models.User {
	t.Helper()

	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return user
}

func mustEnglishMessages(t *testing.T) map[string]string {
	t.Helper()

	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("new i18n manager: %v", err)
	}
	return manager.Messages("en")
}

func deleteWebhookWithCSRF(t *testing.T, owner settingsSecurityTestContext) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current/webhook", nil)
	request.Header.Set("Cookie", owner.authCookie)
	if owner.csrfCookie != nil {
		request.AddCookie(owner.csrfCookie)
	}
	request.Header.Set("X-CSRF-Token", owner.csrfToken)
	request.Header.Set("Accept", "application/json")

	response, err := owner.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("webhook removal failed: %v", err)
	}
	return response
}

// newCountingEgressHandler builds a handler whose settings view service reads
// through a counting loader, so a response's provenance is observable.
func newCountingEgressHandler(t *testing.T) (*Handler, *gorm.DB, *countingSettingsLoader) {
	t.Helper()

	handler, database := newEgressLedgerHandler(t, true)
	dependencies := newTestHandlerDependencies(database, mustEnglishManager(t), onboardingTestAppOptions{outboundDeliveryEnabled: true})

	loader := &countingSettingsLoader{inner: dependencies.SettingsService}
	handler.settingsViewService = services.NewSettingsViewService(
		loader,
		dependencies.ExportService,
		dependencies.SymptomService,
		services.NewEgressLedgerService(dependencies.WebhookSettingsService, dependencies.CalendarFeedSettings, true),
	)
	handler.webhookSettingsSvc = dependencies.WebhookSettingsService
	handler.calendarFeedSettings = dependencies.CalendarFeedSettings
	_ = db.NewRepositories(database)
	return handler, database, loader
}

func mustEnglishManager(t *testing.T) *i18n.Manager {
	t.Helper()

	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("new i18n manager: %v", err)
	}
	return manager
}
