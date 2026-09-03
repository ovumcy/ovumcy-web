package api

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// The egress ledger's render contract.
//
// Two claims the section may never make, and one it may never leak:
//
//   - a claim watermark is not a delivery time. It is a cycle anchor written
//     BEFORE the POST and handed back when the POST fails, so every row below
//     carries watermark values chosen to be unmistakable in a body, and no
//     rendered state may contain them.
//   - a stored selector is not a working link. The verifier plaintext is not
//     kept and its MAC cannot be recomputed, so "active" is not a proposition
//     this code can falsify and no feed sentence may assert it.
//   - the subscribe URL, the selector, and any prefix of them are shown exactly
//     once, on the dedicated reveal page, and never again.

// egressRenderRow is one row of the state matrix. The two watermarks are seeded
// on EVERY row, including the rows where no endpoint exists, because the
// question is whether anything derived from them can reach a body at all.
type egressRenderRow struct {
	name string

	outboundDeliveryEnabled bool

	webhookURLPlaintext string
	// webhookURLOwnerOffset shifts the AAD owner id used to seal the endpoint.
	// A non-zero value produces a ciphertext this owner's key material cannot
	// open, which is what a SECRET_KEY rotation leaves behind.
	webhookURLOwnerOffset uint
	webhookEnabled        bool
	notifyPeriod          bool
	notifyOvulation       bool
	deliveredAt           *time.Time

	feedSelector string
	// feedEpoch is applied literally; useCurrentFeedEpoch overrides it with the
	// epoch this instance derives from its own secret.
	feedEpoch           string
	useCurrentFeedEpoch bool
	revealedAt          *time.Time

	wantSection      services.EgressSectionState
	wantWebhookState services.EgressWebhookState
	wantFeedState    services.EgressFeedState
	// wantDeliveryEvidence is false where the state cannot vouch for the
	// endpoint the mark would describe.
	wantDeliveryEvidence bool
}

const (
	egressPeriodWatermark    = "2019-03-11"
	egressOvulationWatermark = "2019-03-25"
)

func TestEgressLedgerRendersNoWatermarkAndNoActiveClaim(t *testing.T) {
	delivered := time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC)
	revealed := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)

	rows := []egressRenderRow{
		{
			name:             "nothing configured",
			wantSection:      services.EgressSectionNoPathEnabled,
			wantWebhookState: services.EgressWebhookNotConfigured,
			wantFeedState:    services.EgressFeedNone,
		},
		{
			name: "endpoint stored under a key this instance no longer has",
			// The row that made this whole state machine necessary: it also
			// carries a delivery mark, from a delivery that really happened under
			// the old key. Rendering that mark here would print "the last delivery
			// was accepted" directly beneath "this instance cannot read it".
			webhookURLPlaintext:   "https://ntfy.example.test/topic",
			webhookURLOwnerOffset: 1,
			webhookEnabled:        true,
			notifyPeriod:          true,
			deliveredAt:           &delivered,
			wantSection:           services.EgressSectionNeedsAttention,
			wantWebhookState:      services.EgressWebhookUnreadable,
			wantFeedState:         services.EgressFeedNone,
		},
		{
			// C4's order, stated where it can actually fail. Every other unreadable
			// row here has delivery switched ON, so an implementation that asked
			// "is delivery enabled?" first would render all of them correctly and
			// only this one wrong — the row on which a broken application secret
			// hides behind "delivery is off" and is never surfaced to anyone.
			name:                    "unreadable endpoint with delivery switched off and no kind chosen",
			outboundDeliveryEnabled: true,
			webhookURLPlaintext:     "https://ntfy.example.test/topic",
			webhookURLOwnerOffset:   1,
			webhookEnabled:          false,
			notifyPeriod:            false,
			notifyOvulation:         false,
			deliveredAt:             &delivered,
			wantSection:             services.EgressSectionNeedsAttention,
			wantWebhookState:        services.EgressWebhookUnreadable,
			wantFeedState:           services.EgressFeedNone,
		},
		{
			name: "endpoint opens and names no host",
			// Distinct from unreadable: the key is fine and the stored value is
			// not a deliverable authority. A single boolean reported both as
			// "configured".
			webhookURLPlaintext:     "http://ntfy.example.test/\x7f",
			outboundDeliveryEnabled: true,
			webhookEnabled:          true,
			notifyPeriod:            true,
			deliveredAt:             &delivered,
			wantSection:             services.EgressSectionNeedsAttention,
			wantWebhookState:        services.EgressWebhookUnusable,
			wantFeedState:           services.EgressFeedNone,
		},
		{
			name:                "this instance runs no delivery pass",
			webhookURLPlaintext: "https://ntfy.example.test/topic",
			webhookEnabled:      true,
			notifyPeriod:        true,
			notifyOvulation:     true,
			deliveredAt:         &delivered,
			wantSection:         services.EgressSectionPathsEnabled,
			wantWebhookState:    services.EgressWebhookOutboundDisabled,
			wantFeedState:       services.EgressFeedNone,
		},
		{
			name:                    "delivery switched off",
			outboundDeliveryEnabled: true,
			webhookURLPlaintext:     "https://ntfy.example.test/topic",
			webhookEnabled:          false,
			notifyPeriod:            true,
			deliveredAt:             &delivered,
			wantSection:             services.EgressSectionNoPathEnabled,
			wantWebhookState:        services.EgressWebhookStoredOff,
			wantFeedState:           services.EgressFeedNone,
			wantDeliveryEvidence:    true,
		},
		{
			name:                    "delivery on with no reminder kind selected",
			outboundDeliveryEnabled: true,
			webhookURLPlaintext:     "https://ntfy.example.test/topic",
			webhookEnabled:          true,
			wantSection:             services.EgressSectionNoPathEnabled,
			wantWebhookState:        services.EgressWebhookStoredNoKinds,
			wantFeedState:           services.EgressFeedNone,
		},
		{
			name:                    "armed on one kind only",
			outboundDeliveryEnabled: true,
			webhookURLPlaintext:     "https://ntfy.example.test/topic",
			webhookEnabled:          true,
			notifyPeriod:            true,
			deliveredAt:             &delivered,
			wantSection:             services.EgressSectionPathsEnabled,
			wantWebhookState:        services.EgressWebhookArmed,
			wantFeedState:           services.EgressFeedNone,
			wantDeliveryEvidence:    true,
		},
		{
			name:             "link minted before the epoch was recorded",
			feedSelector:     "sel0000000000000000000000000000",
			revealedAt:       &revealed,
			wantSection:      services.EgressSectionPathsEnabled,
			wantWebhookState: services.EgressWebhookNotConfigured,
			wantFeedState:    services.EgressFeedIssuedBeforeRecorded,
		},
		{
			name:             "link minted under a key this instance no longer runs",
			feedSelector:     "sel1111111111111111111111111111",
			feedEpoch:        "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f",
			wantSection:      services.EgressSectionNeedsAttention,
			wantWebhookState: services.EgressWebhookNotConfigured,
			wantFeedState:    services.EgressFeedIssuedPreviousKey,
		},
		{
			name:                "link minted under the running key",
			feedSelector:        "sel2222222222222222222222222222",
			useCurrentFeedEpoch: true,
			revealedAt:          &revealed,
			wantSection:         services.EgressSectionPathsEnabled,
			wantWebhookState:    services.EgressWebhookNotConfigured,
			wantFeedState:       services.EgressFeedIssuedCurrentKey,
		},
		{
			name: "a broken endpoint outranks a working link",
			// needs_attention dominates paths_enabled, so a route that works
			// cannot mask one that does not.
			outboundDeliveryEnabled: true,
			webhookURLPlaintext:     "https://ntfy.example.test/topic",
			webhookURLOwnerOffset:   1,
			webhookEnabled:          true,
			notifyPeriod:            true,
			feedSelector:            "sel3333333333333333333333333333",
			useCurrentFeedEpoch:     true,
			wantSection:             services.EgressSectionNeedsAttention,
			wantWebhookState:        services.EgressWebhookUnreadable,
			wantFeedState:           services.EgressFeedIssuedCurrentKey,
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			owner := newSettingsSecurityTestContextWithOptions(t, "egress-"+strings.ReplaceAll(row.name, " ", "-")+"@example.com", onboardingTestAppOptions{
				enableCSRF:              true,
				outboundDeliveryEnabled: row.outboundDeliveryEnabled,
			})
			seedEgressRow(t, owner.database, owner.user.ID, row)

			body := fetchPageBody(t, owner.app, "/settings", owner.authCookie)

			// The card is its own swap target, and the browser status machinery
			// resolves the island only from an element that DECLARES itself a toast
			// surface. Without this attribute a save or a withdrawal renders no
			// status at all -- silently, on the success path.
			assertAttributeValue(t, body, "data-success-toast", "true")
			assertAttributeValue(t, body, "data-egress-section-state", string(row.wantSection))
			assertAttributeValue(t, body, "data-egress-webhook-state", string(row.wantWebhookState))
			assertAttributeValue(t, body, "data-egress-feed-state", string(row.wantFeedState))

			// No watermark value, and nothing derived from one, in any state.
			for _, watermark := range []string{egressPeriodWatermark, egressOvulationWatermark} {
				if strings.Contains(body, watermark) {
					t.Fatalf("a claim watermark (%s) reached the rendered settings body", watermark)
				}
			}

			// The delivery mark rides its own element, and only where the state
			// can vouch for the endpoint it describes.
			hasDeliveryEvidence := strings.Contains(body, "data-egress-webhook-delivered-at")
			if hasDeliveryEvidence != row.wantDeliveryEvidence {
				t.Fatalf("delivery evidence rendered=%t, want %t (state %s)", hasDeliveryEvidence, row.wantDeliveryEvidence, row.wantWebhookState)
			}

			// The feed's stored secret, in every spelling a page could carry it.
			if row.feedSelector != "" {
				if strings.Contains(body, row.feedSelector) {
					t.Fatal("the feed selector reached the settings body")
				}
				if strings.Contains(body, row.feedSelector[:8]) {
					t.Fatal("a prefix of the feed selector reached the settings body")
				}
				if strings.Contains(body, "/calendar/feed/") {
					t.Fatal("a subscribe URL reached the settings body")
				}
			}

			// The one word the feed may never be described with, in the language
			// under test. PR-28 carries the other five.
			for _, sentence := range extractAttributeValues(t, body, "data-egress-state-key") {
				if !strings.HasPrefix(sentence, "settings.egress.state.feed.") {
					continue
				}
				if strings.Contains(strings.ToLower(messageForKey(t, sentence)), "active") {
					t.Fatalf("feed state %q describes the link as active", sentence)
				}
			}
		})
	}
}

// TestEgressRenderPathNamesNoWatermarkIdentifier is the source-level half of the
// same rule. A body assertion catches a watermark VALUE reaching the page; this
// catches the identifier reaching the transport layer at all, which is the step
// before someone renders it.
func TestEgressRenderPathNamesNoWatermarkIdentifier(t *testing.T) {
	t.Parallel()

	forbidden := []string{"Watermark", "LastSentCycleStart", "last_sent_cycle_start"}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/api: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		contents, readErr := os.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), readErr)
		}
		for _, needle := range forbidden {
			if strings.Contains(string(contents), needle) {
				t.Fatalf("%s names %q: the transport layer must not reach a claim watermark", entry.Name(), needle)
			}
		}
	}
}

// TestSettingsViewDataNamesNoEgressKeyForANonOwner is the owner-isolation guard.
// It asserts ABSENCE, by a whitelist over key names rather than by checking a
// handful of known keys: a seventh egress key added later would otherwise reach
// a non-owner with nothing to notice it.
func TestSettingsViewDataNamesNoEgressKeyForANonOwner(t *testing.T) {
	handler, database := newEgressLedgerHandler(t, false)

	owner := createEgressLedgerUser(t, database, "egress-owner@example.com", models.RoleOwner)
	seedEgressRow(t, database, owner.ID, egressRenderRow{
		outboundDeliveryEnabled: false,
		webhookURLPlaintext:     "https://ntfy.example.test/topic",
		webhookEnabled:          true,
		notifyPeriod:            true,
		feedSelector:            "sel4444444444444444444444444444",
		useCurrentFeedEpoch:     true,
	})
	// "partner" is the only non-owner role the schema still admits, and no
	// account may hold it: the web tier accepts RoleOwner alone. It is the shape
	// of session this guard is about — a row left behind by an older role model.
	stranger := createEgressLedgerUser(t, database, "egress-stranger@example.com", "partner")
	seedEgressRow(t, database, stranger.ID, egressRenderRow{
		webhookURLPlaintext: "https://ntfy.example.test/topic",
		webhookEnabled:      true,
		feedSelector:        "sel5555555555555555555555555555",
		useCurrentFeedEpoch: true,
	})

	ownerData := buildSettingsViewDataForTest(t, handler, owner)
	if _, ok := ownerData["Egress"]; !ok {
		t.Fatal("expected the owner to receive the egress block; without it the absence below proves nothing")
	}

	strangerData := buildSettingsViewDataForTest(t, handler, stranger)
	for key := range strangerData {
		for _, forbidden := range []string{"Webhook", "CalendarFeed", "Egress"} {
			if strings.Contains(key, forbidden) {
				t.Fatalf("a non-owner session received the key %q: an egress fact must be ABSENT, not false", key)
			}
		}
	}
}

// TestNonOwnerSettingsBodyCarriesNoEgressMarkup pairs the map assertion above
// with the rendered half: no section, no hook, no control.
func TestNonOwnerSettingsBodyCarriesNoEgressMarkup(t *testing.T) {
	handler, database := newEgressLedgerHandler(t, true)

	stranger := createEgressLedgerUser(t, database, "egress-render-stranger@example.com", "partner")
	seedEgressRow(t, database, stranger.ID, egressRenderRow{
		webhookURLPlaintext: "https://ntfy.example.test/topic",
		webhookEnabled:      true,
		notifyPeriod:        true,
		feedSelector:        "sel6666666666666666666666666666",
		useCurrentFeedEpoch: true,
	})

	body := renderSettingsBodyForEgressTest(t, handler, stranger)
	for _, marker := range []string{"settings-egress", "data-egress-", "ntfy.example.test", "sel6666"} {
		if strings.Contains(body, marker) {
			t.Fatalf("a non-owner settings body carries %q", marker)
		}
	}
}

// TestEgressLedgerViewModelNamesAKeyForEveryState pins the transport projection
// against the domain enumerations. A state added to the state machine without a
// sentence would otherwise render an empty line, which reads as "nothing to say"
// rather than as a missing translation.
func TestEgressLedgerViewModelNamesAKeyForEveryState(t *testing.T) {
	t.Parallel()

	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("new i18n manager: %v", err)
	}
	messages := manager.Messages("en")

	for _, state := range services.EgressWebhookStates {
		assertMessageKeyResolves(t, messages, egressWebhookStateMessageKey(state), string(state))
	}
	for _, state := range services.EgressFeedStates {
		assertMessageKeyResolves(t, messages, egressFeedStateMessageKey(state), string(state))
	}
	for _, state := range services.EgressSectionStates {
		assertMessageKeyResolves(t, messages, egressSectionStateMessageKey(state), string(state))
	}
	for _, key := range egressWebhookPayloadMessageKeys(services.WebhookPayloadFields()) {
		assertMessageKeyResolves(t, messages, key, key)
	}
	for _, key := range egressFeedPayloadMessageKeys(services.CalendarFeedPayloadFields()) {
		assertMessageKeyResolves(t, messages, key, key)
	}
	if got, want := len(egressWebhookPayloadMessageKeys(services.WebhookPayloadFields())), len(services.WebhookPayloadFields()); got != want {
		t.Fatalf("webhook payload inventory lost a field in projection: %d keys for %d fields", got, want)
	}
	if got, want := len(egressFeedPayloadMessageKeys(services.CalendarFeedPayloadFields())), len(services.CalendarFeedPayloadFields()); got != want {
		t.Fatalf("feed payload inventory lost a field in projection: %d keys for %d fields", got, want)
	}
}

// TestEgressMessageKeysAreLiteralsRatherThanAssembled is the constructive half of
// the reachability contract. fmt.Sprintf("settings.egress.state.webhook.%s", …)
// is the shape this file exists to refuse: it would either fail the reachability
// barrier by name or, once registered as a key family, put the catalogue's
// reachability behind a second declaration free to drift from the switch above.
func TestEgressMessageKeysAreLiteralsRatherThanAssembled(t *testing.T) {
	t.Parallel()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "settings_egress_view_models.go", nil, 0)
	if err != nil {
		t.Fatalf("parse the egress view models: %v", err)
	}

	literals := 0
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok {
			if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector {
				if ident, isIdent := selector.X.(*ast.Ident); isIdent && ident.Name == "fmt" {
					t.Fatalf("settings_egress_view_models.go assembles a value through fmt.%s; every message key must be a literal", selector.Sel.Name)
				}
			}
		}
		basic, ok := node.(*ast.BasicLit)
		if ok && basic.Kind == token.STRING && strings.Contains(basic.Value, "settings.egress.") {
			literals++
		}
		return true
	})
	if literals < len(services.EgressWebhookStates)+len(services.EgressFeedStates)+len(services.EgressSectionStates) {
		t.Fatalf("expected one literal key per state, found %d", literals)
	}
}

// --- helpers -------------------------------------------------------------

// aadForWebhookURLInTest restates the production AAD format deliberately: this
// is a FIXTURE, sealing a value the production save path would refuse to write
// (an unparseable URL) or would seal under the wrong owner. Nothing here is
// asserted against, so it cannot agree with the implementation by construction.
func aadForWebhookURLInTest(userID uint) []byte {
	return []byte(fmt.Sprintf("ovumcy.field.webhook_url:%d", userID))
}

func seedEgressRow(t *testing.T, database *gorm.DB, userID uint, row egressRenderRow) {
	t.Helper()

	updates := map[string]any{
		"webhook_enabled":          row.webhookEnabled,
		"webhook_notify_period":    row.notifyPeriod,
		"webhook_notify_ovulation": row.notifyOvulation,
		"webhook_url":              "",
		// Seeded on every row: the question is whether anything derived from a
		// claim watermark can reach a body in ANY state, not only in the states
		// where a delivery could plausibly be described.
		"webhook_period_last_sent_cycle_start":    egressPeriodWatermark,
		"webhook_ovulation_last_sent_cycle_start": egressOvulationWatermark,
		"webhook_last_delivered_at":               row.deliveredAt,
		"calendar_feed_selector":                  row.feedSelector,
		"calendar_feed_revealed_at":               row.revealedAt,
		"calendar_feed_key_epoch":                 row.feedEpoch,
	}

	if row.webhookURLPlaintext != "" {
		sealedFor := userID + row.webhookURLOwnerOffset
		ciphertext, err := security.EncryptField(row.webhookURLPlaintext, []byte(testAppSecretKey), aadForWebhookURLInTest(sealedFor))
		if err != nil {
			t.Fatalf("seal the test endpoint: %v", err)
		}
		updates["webhook_url"] = ciphertext
	}

	if row.useCurrentFeedEpoch {
		epoch, err := security.CalendarFeedKeyEpoch([]byte(testAppSecretKey))
		if err != nil {
			t.Fatalf("derive the running feed key epoch: %v", err)
		}
		updates["calendar_feed_key_epoch"] = epoch
	}

	if err := database.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		t.Fatalf("seed the egress row: %v", err)
	}
}

func newEgressLedgerHandler(t *testing.T, outboundDeliveryEnabled bool) (*Handler, *gorm.DB) {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "ovumcy-egress-test.db")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler, err := NewHandler(testAppSecretKey, time.UTC, manager, false, newTestHandlerDependencies(database, manager, onboardingTestAppOptions{
		outboundDeliveryEnabled: outboundDeliveryEnabled,
	}))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}
	return handler, database
}

func createEgressLedgerUser(t *testing.T, database *gorm.DB, email string, role string) *models.User {
	t.Helper()

	user := models.User{
		Email:               email,
		Role:                role,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create %s: %v", email, err)
	}
	return &user
}

// buildSettingsViewDataForTest drives the real view-data builder inside a live
// request context, which is what csrfToken and the language middleware need.
func buildSettingsViewDataForTest(t *testing.T, handler *Handler, user *models.User) fiber.Map {
	t.Helper()

	var built fiber.Map
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/settings", func(c fiber.Ctx) error {
		copied := *user
		data, err := handler.buildSettingsViewData(c, &copied, FlashPayload{})
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString(err.Error())
		}
		built = data
		return c.SendStatus(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings view data request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 building the settings view data, got %d", response.StatusCode)
	}
	return built
}

// renderSettingsBodyForEgressTest renders the real settings template from the real
// view data, so the assertion covers the markup and not only the map.
func renderSettingsBodyForEgressTest(t *testing.T, handler *Handler, user *models.User) string {
	t.Helper()

	body := ""
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/settings", func(c fiber.Ctx) error {
		copied := *user
		data, err := handler.buildSettingsViewData(c, &copied, FlashPayload{})
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString(err.Error())
		}
		data["CurrentUser"] = &copied
		return handler.render(c, "settings", data)
	})

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings render request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body = mustReadBodyString(t, response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 rendering settings, got %d: %s", response.StatusCode, body)
	}
	return body
}

func assertMessageKeyResolves(t *testing.T, messages map[string]string, key string, subject string) {
	t.Helper()

	if strings.TrimSpace(key) == "" {
		t.Fatalf("%s has no message key", subject)
	}
	if strings.TrimSpace(messages[key]) == "" {
		t.Fatalf("%s names %q, which the catalogue does not answer", subject, key)
	}
}

func messageForKey(t *testing.T, key string) string {
	t.Helper()

	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("new i18n manager: %v", err)
	}
	return manager.Messages("en")[key]
}

// assertAttributeValue fails unless the body carries exactly the expected value
// for the attribute. It compares the whole attribute text so a state that is a
// PREFIX of another cannot satisfy an assertion about the longer one.
func assertAttributeValue(t *testing.T, body string, attribute string, want string) {
	t.Helper()

	needle := fmt.Sprintf("%s=%q", attribute, want)
	if !strings.Contains(body, needle) {
		t.Fatalf("expected %s in the rendered body", needle)
	}
}

func extractAttributeValues(t *testing.T, body string, attribute string) []string {
	t.Helper()

	values := make([]string, 0, 4)
	prefix := attribute + "=\""
	rest := body
	for {
		index := strings.Index(rest, prefix)
		if index < 0 {
			return values
		}
		rest = rest[index+len(prefix):]
		end := strings.Index(rest, "\"")
		if end < 0 {
			return values
		}
		values = append(values, rest[:end])
		rest = rest[end:]
	}
}
