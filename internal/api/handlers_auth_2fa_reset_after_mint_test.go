package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// The sign-in TOTP step forgives the client's failures only once the session
// it earned has landed: the auth cookie is minted and the provider-logout state
// a TOTP-gated OIDC sign-in staged has moved onto it. A correct code whose
// session could not be issued proved nothing lasting, so it keeps the count it
// found. The relocation is refused from a gorm callback on the test's own
// database, so the handler, the service and the repository all run unchanged.
//
// The totp budget keeps a client bucket and a per-account bucket, and a
// success clears only the client one. The probe therefore spends codes against
// a second account from the same client: its own account bucket is nearly
// empty, so only the client bucket the first account left behind can make it
// rate limited.

var errTOTPChallengeLogoutStateRefusedByTest = errors.New("test: the provider-logout relocation behind a correct code was refused")

type totpChallengeResetFixture struct {
	app           *fiber.App
	database      *gorm.DB
	rawSecret     string
	pendingCookie string
	probeSecret   string
	probeCookie   string
}

// newTOTPChallengeResetFixture drives a TOTP-gated OIDC sign-in up to the
// challenge, so the pending cookie carries an opaque id under which the
// callback staged provider-logout state, and seeds a second TOTP account whose
// pending cookie the probe uses.
func newTOTPChallengeResetFixture(t *testing.T, slug string) totpChallengeResetFixture {
	t.Helper()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	secretKey := []byte(testHandlerSecretKey)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	user := createOnboardingTestUser(t, database, "totp-reset-"+slug+"@example.com", "StrongPass1", true)
	rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)
	var linked models.User
	if err := database.First(&linked, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	stub.result = services.OIDCLoginResult{
		User:         linked,
		RequiresTOTP: true,
		Logout: &services.OIDCLogoutState{
			UserID:                linked.ID,
			EndSessionEndpoint:    testOIDCIssuerURL + "/logout",
			IDTokenHint:           "eyJhbGciOiJSUzI1NiJ9.header.signature",
			PostLogoutRedirectURL: "https://app.example.com/",
		},
	}

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}
	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())
	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	pendingCookie := responseCookie(callbackResponse.Cookies(), totpPendingCookieName)
	if pendingCookie == nil || strings.TrimSpace(pendingCookie.Value) == "" {
		t.Fatal("expected a TOTP pending cookie from the OIDC callback")
	}

	probeUser := createOnboardingTestUser(t, database, "totp-reset-probe-"+slug+"@example.com", "StrongPass1", true)
	probeSecret := setupTOTPForUser(t, database, probeUser.ID, secretKey)

	return totpChallengeResetFixture{
		app:           app,
		database:      database,
		rawSecret:     rawSecret,
		pendingCookie: totpPendingCookieName + "=" + pendingCookie.Value,
		probeSecret:   probeSecret,
		probeCookie:   sealTOTPPendingCookieForTest(t, secretKey, probeUser.ID, false),
	}
}

// sendTOTPChallengeJSON submits code as a JSON client and returns the status
// and the body.
func sendTOTPChallengeJSON(t *testing.T, app *fiber.App, cookie string, code string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(url.Values{
		"code": {code},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", cookie)
	response := mustAppResponse(t, app, request)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read challenge response: %v", err)
	}
	return response.StatusCode, string(body)
}

func assertTOTPChallengeAnswer(t *testing.T, status int, body string, wantStatus int, wantKey string, label string) {
	t.Helper()
	if status != wantStatus || !strings.Contains(body, `"`+wantKey+`"`) {
		t.Fatalf("%s: status = %d body = %s, want %d carrying %q", label, status, body, wantStatus, wantKey)
	}
}

// wrongTOTPCode returns a six-digit code no step inside the validation skew
// window accepts for rawSecret.
func wrongTOTPCode(t *testing.T, rawSecret string) string {
	t.Helper()
	now := time.Now()
	valid := map[string]bool{}
	for _, offset := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		code, err := totp.GenerateCode(rawSecret, now.Add(offset))
		if err != nil {
			t.Fatalf("GenerateCode: %v", err)
		}
		valid[code] = true
	}
	for candidate := 0; ; candidate++ {
		code := strconv.Itoa(100000 + candidate)
		if !valid[code] {
			return code
		}
	}
}

func spendTOTPChallengeFailures(t *testing.T, fixture totpChallengeResetFixture, count int) {
	t.Helper()
	for attempt := range count {
		status, body := sendTOTPChallengeJSON(t, fixture.app, fixture.pendingCookie, wrongTOTPCode(t, fixture.rawSecret))
		assertTOTPChallengeAnswer(t, status, body, http.StatusUnauthorized, "totp invalid code", "wrong code "+strconv.Itoa(attempt+1))
	}
}

// refuseOIDCLogoutStateSaveOnce fails the first insert into the provider-logout
// table with a storage error, before gorm runs it, and reports whether it fired.
// Every later insert passes.
func refuseOIDCLogoutStateSaveOnce(t *testing.T, database *gorm.DB) *atomic.Bool {
	t.Helper()
	fired := &atomic.Bool{}
	const name = "test:totp-challenge-refuse-logout-state-save"
	if err := database.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != (models.OIDCLogoutState{}).TableName() || !fired.CompareAndSwap(false, true) {
			return
		}
		_ = tx.AddError(errTOTPChallengeLogoutStateRefusedByTest)
	}); err != nil {
		t.Fatalf("register the refusing create callback: %v", err)
	}
	t.Cleanup(func() { _ = database.Callback().Create().Remove(name) })
	return fired
}

func TestTOTPChallengeResetsTheBudgetOnlyAfterTheSessionLands(t *testing.T) {
	t.Run("refused session keeps the count", func(t *testing.T) {
		fixture := newTOTPChallengeResetFixture(t, "refused")
		spendTOTPChallengeFailures(t, fixture, services.DefaultTOTPAttemptsLimit-1)

		fired := refuseOIDCLogoutStateSaveOnce(t, fixture.database)
		code, err := totp.GenerateCode(fixture.rawSecret, time.Now())
		if err != nil {
			t.Fatalf("GenerateCode: %v", err)
		}
		status, body := sendTOTPChallengeJSON(t, fixture.app, fixture.pendingCookie, code)
		if !fired.Load() {
			t.Fatalf("anchor: the correct code never reached the provider-logout relocation (status %d, body %s)", status, body)
		}
		assertTOTPChallengeAnswer(t, status, body, http.StatusInternalServerError, "failed to create session", "correct code whose session could not land")

		// One failure short of the limit was spent before the refused session.
		// Had the correct code cleared this client's count, the probe account
		// would need a full budget of failures to be rate limited.
		status, body = sendTOTPChallengeJSON(t, fixture.app, fixture.probeCookie, wrongTOTPCode(t, fixture.probeSecret))
		assertTOTPChallengeAnswer(t, status, body, http.StatusUnauthorized, "totp invalid code", "the failure that reaches the limit")
		status, body = sendTOTPChallengeJSON(t, fixture.app, fixture.probeCookie, wrongTOTPCode(t, fixture.probeSecret))
		assertTOTPChallengeAnswer(t, status, body, http.StatusTooManyRequests, "totp too many attempts", "next code after the refused session kept the count")
	})

	t.Run("positive control: a landed session clears the count", func(t *testing.T) {
		fixture := newTOTPChallengeResetFixture(t, "landed")
		spendTOTPChallengeFailures(t, fixture, services.DefaultTOTPAttemptsLimit-1)

		code, err := totp.GenerateCode(fixture.rawSecret, time.Now())
		if err != nil {
			t.Fatalf("GenerateCode: %v", err)
		}
		status, body := sendTOTPChallengeJSON(t, fixture.app, fixture.pendingCookie, code)
		if status != http.StatusOK {
			t.Fatalf("correct code: status = %d body = %s, want 200", status, body)
		}

		for attempt := range 2 {
			status, body = sendTOTPChallengeJSON(t, fixture.app, fixture.probeCookie, wrongTOTPCode(t, fixture.probeSecret))
			assertTOTPChallengeAnswer(t, status, body, http.StatusUnauthorized, "totp invalid code",
				"probe code "+strconv.Itoa(attempt+1)+" after the landed session cleared the count")
		}
	})
}
