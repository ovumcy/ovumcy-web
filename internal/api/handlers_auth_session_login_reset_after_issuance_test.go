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

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// Password sign-in forgives the client's failures only once the cookie that
// carries the sign-in onward has been issued: the session cookie, the
// TOTP-pending cookie, or the forced-reset cookie. A correct password whose
// sign-in could not continue proved nothing lasting, so it keeps the count it
// found. The session mint is refused through the handler's session-issuance
// seam, so the handler and the service run unchanged.
//
// The login budget keeps a client bucket and a per-address bucket, and a
// success clears only the client one. The probe therefore spends wrong
// passwords against a second account from the same client: its own address
// bucket is empty, so only the client bucket the first account left behind can
// make it rate limited.

var errLoginSessionRefusedByTest = errors.New("test: the session behind a correct password was refused")

type loginResetFixture struct {
	app        *fiber.App
	email      string
	probeEmail string
	// refuseNextSession makes the next session mint fail once; fired reports
	// whether it did.
	refuseNextSession *atomic.Bool
	fired             *atomic.Bool
}

// newLoginResetFixture seeds the account that signs in and a plain probe
// account. prepare shapes the first account (TOTP, forced reset) before the
// sign-in.
func newLoginResetFixture(t *testing.T, slug string, prepare func(t *testing.T, database *gorm.DB, user models.User)) loginResetFixture {
	t.Helper()
	refuse := &atomic.Bool{}
	fired := &atomic.Bool{}
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		sessionIssuanceFault: func() error {
			if refuse.CompareAndSwap(true, false) {
				fired.Store(true)
				return errLoginSessionRefusedByTest
			}
			return nil
		},
	})
	user := createOnboardingTestUser(t, database, "login-reset-"+slug+"@example.com", "StrongPass1", true)
	if prepare != nil {
		prepare(t, database, user)
	}
	probe := createOnboardingTestUser(t, database, "login-reset-probe-"+slug+"@example.com", "StrongPass1", true)
	return loginResetFixture{
		app:               app,
		email:             user.Email,
		probeEmail:        probe.Email,
		refuseNextSession: refuse,
		fired:             fired,
	}
}

// sendLoginJSON submits a password as a JSON client and returns the status
// and the body.
func sendLoginJSON(t *testing.T, app *fiber.App, email string, password string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(url.Values{
		"email":    {email},
		"password": {password},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response := mustAppResponse(t, app, request)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read login response: %v", err)
	}
	return response.StatusCode, string(body)
}

func assertLoginAnswer(t *testing.T, status int, body string, wantStatus int, wantFragment string, label string) {
	t.Helper()
	if status != wantStatus || !strings.Contains(body, wantFragment) {
		t.Fatalf("%s: status = %d body = %s, want %d carrying %s", label, status, body, wantStatus, wantFragment)
	}
}

func spendLoginFailures(t *testing.T, fixture loginResetFixture, count int) {
	t.Helper()
	for attempt := range count {
		status, body := sendLoginJSON(t, fixture.app, fixture.email, "WrongPass1")
		assertLoginAnswer(t, status, body, http.StatusUnauthorized, `"invalid credentials"`, "wrong password "+strconv.Itoa(attempt+1))
	}
}

func markLoginResetUserTOTP(t *testing.T, database *gorm.DB, user models.User) {
	t.Helper()
	setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))
}

func markLoginResetUserForcedReset(t *testing.T, database *gorm.DB, user models.User) {
	t.Helper()
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}
}

func TestLoginResetsTheBudgetOnlyAfterTheSignInLands(t *testing.T) {
	t.Run("refused session keeps the count", func(t *testing.T) {
		fixture := newLoginResetFixture(t, "refused", nil)
		spendLoginFailures(t, fixture, services.DefaultLoginAttemptsLimit-1)

		fixture.refuseNextSession.Store(true)
		status, body := sendLoginJSON(t, fixture.app, fixture.email, "StrongPass1")
		if !fixture.fired.Load() {
			t.Fatalf("anchor: the correct password never reached the session mint (status %d, body %s)", status, body)
		}
		assertLoginAnswer(t, status, body, http.StatusInternalServerError, `"failed to create session"`, "correct password whose session could not land")

		// One failure short of the limit was spent before the refused session.
		// Had the correct password cleared this client's count, the probe
		// account would need a full budget of failures to be rate limited.
		status, body = sendLoginJSON(t, fixture.app, fixture.probeEmail, "WrongPass1")
		assertLoginAnswer(t, status, body, http.StatusUnauthorized, `"invalid credentials"`, "the failure that reaches the limit")
		status, body = sendLoginJSON(t, fixture.app, fixture.probeEmail, "WrongPass1")
		assertLoginAnswer(t, status, body, http.StatusTooManyRequests, `"too many login attempts"`, "next password after the refused session kept the count")
	})

	t.Run("positive control: a landed session clears the count", func(t *testing.T) {
		assertLandedLoginClearsTheCount(t, newLoginResetFixture(t, "session", nil), http.StatusOK, `"ok"`)
	})

	t.Run("positive control: a TOTP-pending sign-in clears the count", func(t *testing.T) {
		assertLandedLoginClearsTheCount(t, newLoginResetFixture(t, "totp", markLoginResetUserTOTP), http.StatusOK, `"requires_totp":true`)
	})

	t.Run("positive control: a forced-reset sign-in clears the count", func(t *testing.T) {
		assertLandedLoginClearsTheCount(t, newLoginResetFixture(t, "forced", markLoginResetUserForcedReset), http.StatusForbidden, `"password change required"`)
	})
}

// assertLandedLoginClearsTheCount spends one failure short of the limit, signs
// in with the correct password on the arm the account takes, and holds that
// the probe account then still has two failures before this client is rate
// limited.
func assertLandedLoginClearsTheCount(t *testing.T, fixture loginResetFixture, wantStatus int, wantFragment string) {
	t.Helper()
	spendLoginFailures(t, fixture, services.DefaultLoginAttemptsLimit-1)

	status, body := sendLoginJSON(t, fixture.app, fixture.email, "StrongPass1")
	assertLoginAnswer(t, status, body, wantStatus, wantFragment, "correct password")

	for attempt := range 2 {
		status, body = sendLoginJSON(t, fixture.app, fixture.probeEmail, "WrongPass1")
		assertLoginAnswer(t, status, body, http.StatusUnauthorized, `"invalid credentials"`,
			"probe password "+strconv.Itoa(attempt+1)+" after the landed sign-in cleared the count")
	}
}
