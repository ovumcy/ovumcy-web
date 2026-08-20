package api

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// Basic identity / catalog handler smoke regressions. These cover the
// always-on "who am I", "is this app up", and "what symptoms do I have"
// endpoints — small surfaces but each is a public contract: a wrapper
// or a healthcheck probe relies on the response shape staying stable.

func TestHealthEndpointReturnsOKJSON(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertStatusCode(t, response, http.StatusOK)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read healthz body: %v", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode healthz JSON %q: %v", body, err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", payload["status"])
	}
}

// TestHealthEndpointStaysGreenWithStorageGone pins the liveness/readiness
// split from the liveness side: /healthz must keep answering 200 after the
// storage handle is gone, because the container healthcheck probes it and a
// transient database failure must not restart the process. Without this the
// split is one accidental repository call away from becoming a restart loop.
func TestHealthEndpointStaysGreenWithStorageGone(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	closeTestDatabase(t, database)

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertStatusCode(t, response, http.StatusOK)
	if body := mustReadBodyString(t, response.Body); body != readinessProbeOKBody {
		t.Fatalf("expected liveness body %q with storage gone, got %q", readinessProbeOKBody, body)
	}
}

// readinessProbeOKBody and readinessProbeUnavailableBody are the exact bytes
// /readyz is allowed to emit. They are spelled out here rather than imported
// from the handler so a change to either response has to be made twice, on
// purpose: this endpoint is unauthenticated, so its body is a contract with
// anyone on the network and must never grow driver, path, or error detail.
const (
	readinessProbeOKBody          = `{"status":"ok"}`
	readinessProbeUnavailableBody = `{"status":"unavailable"}`
)

// TestReadyEndpointReturns200WithLiveStorage is the readiness happy path and
// the positive anchor for the 503 test below: it proves the probe really
// reaches a working database rather than always answering the same way.
func TestReadyEndpointReturns200WithLiveStorage(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertStatusCode(t, response, http.StatusOK)
	if body := mustReadBodyString(t, response.Body); body != readinessProbeOKBody {
		t.Fatalf("expected readiness body %q, got %q", readinessProbeOKBody, body)
	}
}

// TestReadyEndpointReturns503OnceStorageIsGone drives the real failure: the
// app's own *sql.DB is closed underneath it, so the probe hits a genuinely
// dead handle rather than a stub that agrees to fail. It also pins the body
// bytes, because a 503 that leaks the driver name or the database path would
// hand an unauthenticated caller a free reconnaissance surface.
func TestReadyEndpointReturns503OnceStorageIsGone(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	closeTestDatabase(t, database)

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertStatusCode(t, response, http.StatusServiceUnavailable)

	body := mustReadBodyString(t, response.Body)
	if body != readinessProbeUnavailableBody {
		t.Fatalf("expected readiness body %q, got %q", readinessProbeUnavailableBody, body)
	}
	for _, leaked := range []string{"sql", "sqlite", "driver", "database", ".db", "SELECT"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leaked)) {
			t.Fatalf("readiness failure body leaked %q: %q", leaked, body)
		}
	}
}

// closeTestDatabase closes the handle the test app was built with, simulating
// the storage layer disappearing under a running process. The app's own
// t.Cleanup closes it again; database/sql tolerates the second close.
func closeTestDatabase(t *testing.T, database *gorm.DB) {
	t.Helper()

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("resolve sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
}

func TestGetCurrentUserWithoutAuthCookieReturnsUnauthorized(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/users/current", nil))
	if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 401 or redirect for unauthenticated current-user, got %d", response.StatusCode)
	}
}

// TestGetCurrentUserReturnsMinimalIdentityShape locks the public contract a
// wrapper or external client needs to identify the session and decide what
// mutating calls it can make. The handler intentionally never includes
// sensitive fields (password/recovery hashes, TOTP secret), so this test
// also blocks accidental field leaks added by a future refactor.
//
// The shape is asserted over SEVERAL states of the same account, not just the
// freshly onboarded one: a key added conditionally — `if user.OIDCSubject !=
// "" { payload["oidc_subject"] = ... }` — is invisible to a guard that only
// ever renders the state where the condition is false. The claim here is that
// the key set does not depend on account state.
func TestGetCurrentUserReturnsMinimalIdentityShape(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "current-user-shape@example.com", "StrongPass1", true)
	if err := database.Model(&user).Updates(map[string]any{
		"display_name":         "Owner Display",
		"must_change_password": false,
	}).Error; err != nil {
		t.Fatalf("seed display fields: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertCurrentUserIdentityShape(t, app, authCookie, "local password, display name set", user.Email)

	// Local auth off and the display name cleared, so a key emitted only for
	// one polarity of those two cannot hide behind the seeded state.
	if err := database.Model(&user).Updates(map[string]any{
		"display_name":       "",
		"local_auth_enabled": false,
	}).Error; err != nil {
		t.Fatalf("clear display name and local auth: %v", err)
	}
	assertCurrentUserIdentityShape(t, app, authCookie, "local auth off, display name empty", user.Email)

	// An account with a linked OIDC identity. The identity row is written
	// directly because the DTO reads account state, not the login route that
	// produced it; the issuer and the subject are passed as forbidden values
	// so a subject reaching the wire under an existing key fails too.
	const (
		linkedIssuer  = "https://idp.example.com"
		linkedSubject = "oidc-subject-must-not-ship"
	)
	if err := database.Create(&models.OIDCIdentity{
		UserID:    user.ID,
		Issuer:    linkedIssuer,
		Subject:   linkedSubject,
		CreatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("link oidc identity: %v", err)
	}
	assertCurrentUserIdentityShape(t, app, authCookie, "oidc identity linked", user.Email, linkedIssuer, linkedSubject)

	// The DTO's other two flags have no reachable opposite polarity, so the
	// shape above is deliberately narrower than the flag combinations the
	// payload can spell: setting must_change_password revokes the live
	// session (services.ResolveAuthSession), and an account that has not
	// finished onboarding is answered by the middleware before the handler
	// runs. Both are pinned as refusals so a change that makes either state
	// reachable fails here, instead of leaving the key set unexercised for a
	// state the handler newly serves.
	for _, unreachable := range []struct {
		state  string
		update map[string]any
	}{
		{state: "must change password", update: map[string]any{"must_change_password": true}},
		{state: "onboarding incomplete", update: map[string]any{"must_change_password": false, "onboarding_completed": false}},
	} {
		if err := database.Model(&user).Updates(unreachable.update).Error; err != nil {
			t.Fatalf("apply %q state: %v", unreachable.state, err)
		}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/users/current", nil)
		request.Header.Set("Cookie", authCookie)
		if response := mustAppResponse(t, app, request); response.StatusCode == http.StatusOK {
			t.Fatalf("[%s] current-user answered 200: that state is now reachable, so the shape assertion above has to cover it too", unreachable.state)
		}
	}
}

// assertCurrentUserIdentityShape drives GET /api/v1/users/current with the
// given session and asserts the whole shape contract against whatever account
// state the caller has just written: the exact key set, the sensitive-field
// denylist, the identity values, and the raw-body markers. state names the
// account state under test so a failure says which one broke the contract.
func assertCurrentUserIdentityShape(t *testing.T, app *fiber.App, authCookie string, state string, expectedEmail string, forbiddenValues ...string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/current", nil)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("[%s] read current-user body: %v", state, err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("[%s] decode current-user JSON %q: %v", state, body, err)
	}

	// The documented key set, in the order the handler writes it. It is
	// pinned by EQUALITY rather than by presence on purpose: a presence
	// loop accepts every key it was not told about, so a sensitive field
	// added to the identity DTO later (an OIDC subject, a feed-token hash)
	// would reach every wrapper client with this suite green. Any new key
	// fails here until someone classifies it and extends this slice; the
	// denylist and the raw-body markers below stay as defense in depth for
	// a key that gets classified wrongly.
	expectedFields := []string{"id", "email", "display_name", "role", "onboarding_completed", "local_auth_enabled", "must_change_password"}
	expectedKeys := slices.Sorted(slices.Values(expectedFields))
	actualKeys := slices.Sorted(maps.Keys(payload))
	if !slices.Equal(actualKeys, expectedKeys) {
		t.Fatalf("[%s] expected current-user payload to expose exactly the keys %v, got %v", state, expectedKeys, actualKeys)
	}
	for _, leakedField := range []string{"password_hash", "password", "recovery_code_hash", "recovery_code", "totp_secret", "totp_secret_encrypted"} {
		if _, ok := payload[leakedField]; ok {
			t.Fatalf("[%s] did not expect current-user payload to expose %q (sensitive field leak): %v", state, leakedField, payload)
		}
	}
	if payload["email"] != expectedEmail {
		t.Fatalf("[%s] expected email %q, got %v", state, expectedEmail, payload["email"])
	}
	if payload["role"] != string(models.RoleOwner) {
		t.Fatalf("[%s] expected role %q, got %v", state, models.RoleOwner, payload["role"])
	}
	bodyString := string(body)
	for _, leak := range append([]string{"$2a$", "$2b$", "totp_secret"}, forbiddenValues...) {
		if strings.Contains(bodyString, leak) {
			t.Fatalf("[%s] current-user response contained sensitive token %q: %q", state, leak, bodyString)
		}
	}
}

func TestGetSymptomsWithoutAuthCookieReturnsUnauthorized(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/symptoms", nil))
	if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 401 or redirect for unauthenticated symptoms list, got %d", response.StatusCode)
	}
}

// TestGetSymptomsReturnsBuiltinCatalogForOwner locks the catalog content the
// frontend keys off: the builtin "Cramps" symptom (seeded for every owner)
// must appear in the response. Asserting a known catalog *name* survives
// future struct-tag renames or JSON-shape refactors, unlike asserting Go
// struct field keys.
func TestGetSymptomsReturnsBuiltinCatalogForOwner(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "symptoms-catalog@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/symptoms", nil)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read symptoms body: %v", err)
	}
	if !strings.Contains(string(body), `"Cramps"`) && !strings.Contains(string(body), `"name":"Cramps"`) {
		t.Fatalf("expected builtin 'Cramps' symptom in owner catalog, got %q", body)
	}
}

// TestGetSymptomsDoesNotLeakOtherOwnerCustomSymptom locks the owner-scoping
// privacy invariant. Owner A creates a uniquely-named custom symptom; owner
// B (separate account) listing /api/v1/symptoms must not see it. Failure
// here would mean the symptom catalog leaks PHI-adjacent labels across
// accounts.
func TestGetSymptomsDoesNotLeakOtherOwnerCustomSymptom(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	ownerA := createOnboardingTestUser(t, database, "symptoms-owner-a@example.com", "StrongPass1", true)
	ownerB := createOnboardingTestUser(t, database, "symptoms-owner-b@example.com", "StrongPass1", true)

	const ownerASymptomName = "OwnerA-SecretMarker-9f1e2"
	if err := database.Create(&models.SymptomType{
		UserID: ownerA.ID,
		Name:   ownerASymptomName,
		Icon:   "🔒",
		Color:  "#112233",
	}).Error; err != nil {
		t.Fatalf("seed owner A custom symptom: %v", err)
	}

	authCookieB := loginAndExtractAuthCookie(t, app, ownerB.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/symptoms", nil)
	request.Header.Set("Cookie", authCookieB)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read symptoms body: %v", err)
	}
	if strings.Contains(string(body), ownerASymptomName) {
		t.Fatalf("owner B's symptoms catalog leaked owner A's custom symptom %q: %s", ownerASymptomName, body)
	}
}
