package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/current", nil)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read current-user body: %v", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode current-user JSON %q: %v", body, err)
	}

	expectedFields := []string{"id", "email", "display_name", "role", "onboarding_completed", "local_auth_enabled", "must_change_password"}
	for _, field := range expectedFields {
		if _, ok := payload[field]; !ok {
			t.Fatalf("expected current-user payload to expose %q, got %v", field, payload)
		}
	}
	for _, leakedField := range []string{"password_hash", "password", "recovery_code_hash", "recovery_code", "totp_secret", "totp_secret_encrypted"} {
		if _, ok := payload[leakedField]; ok {
			t.Fatalf("did not expect current-user payload to expose %q (sensitive field leak): %v", leakedField, payload)
		}
	}
	if payload["email"] != user.Email {
		t.Fatalf("expected email %q, got %v", user.Email, payload["email"])
	}
	if payload["role"] != string(models.RoleOwner) {
		t.Fatalf("expected role %q, got %v", models.RoleOwner, payload["role"])
	}
	bodyString := string(body)
	for _, leak := range []string{"$2a$", "$2b$", "totp_secret"} {
		if strings.Contains(bodyString, leak) {
			t.Fatalf("current-user response contained sensitive token %q: %q", leak, bodyString)
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
