package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// revokingWriteHooks is the seam the race regressions below commit another
// write through: it sits around the real user repository, so each posture
// change still runs its real compare-and-set, and only the moment before that
// write is handed to the test. The request has been authenticated by then —
// the auth middleware loaded the account on the way in — so a revocation
// committed in beforeWrite is exactly one that lands between the session's
// check and the write it authorises.
//
// storedVersion, when set, hands the write the version stored at that moment
// instead of the one the request authenticated with: the compare-and-set
// degrades to the unconditional bump these writes made before they took a
// version, which is what the negative controls measure.
//
// sessionIssuanceFault is installed as the handler's seam of the same name, so
// a site's write can commit and the session re-issue after it still fail.
type revokingWriteHooks struct {
	beforeWrite          func()
	storedVersion        func() int
	sessionIssuanceFault func() error
}

// install rebuilds the three services that make a revoking write before a
// session mint over the wrapped repository, configured as the composition root
// configures them.
func (hooks *revokingWriteHooks) install(dependencies *Dependencies, users *db.UserRepository) {
	repository := &revokingWriteUserRepository{UserRepository: users, hooks: hooks}
	dependencies.AuthService = services.NewAuthService(repository)
	settingsService := services.NewSettingsService(repository)
	settingsService.ConfigureReauthAttempts(
		[]byte(testAppSecretKey),
		services.NewAttemptLimiter(),
		services.DefaultSettingsReauthAttemptsLimit,
		services.DefaultSettingsReauthAttemptsWindow,
	)
	dependencies.SettingsService = settingsService
	dependencies.TOTPService = services.NewTOTPService(repository, []byte(testAppSecretKey), services.NewAttemptLimiter())
}

func (hooks *revokingWriteHooks) enter(expectedSessionVersion int) int {
	if hooks.beforeWrite != nil {
		hooks.beforeWrite()
	}
	if hooks.storedVersion != nil {
		return hooks.storedVersion()
	}
	return expectedSessionVersion
}

type revokingWriteUserRepository struct {
	*db.UserRepository
	hooks *revokingWriteHooks
}

func (repository *revokingWriteUserRepository) UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, expectedSessionVersion int, passwordHash string, mustChangePassword bool) error {
	return repository.UserRepository.UpdatePasswordAndRevokeSessions(ctx, userID, repository.hooks.enter(expectedSessionVersion), passwordHash, mustChangePassword)
}

func (repository *revokingWriteUserRepository) UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, expectedSessionVersion int, passwordHash string, recoveryHash string, mustChangePassword bool, beforeCommit func(sessionVersion int) error) error {
	return repository.UserRepository.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, userID, repository.hooks.enter(expectedSessionVersion), passwordHash, recoveryHash, mustChangePassword, beforeCommit)
}

func (repository *revokingWriteUserRepository) UpdateRecoveryCodeHashAndRevokeSessions(ctx context.Context, userID uint, expectedSessionVersion int, recoveryHash string, beforeCommit func(sessionVersion int) error) error {
	return repository.UserRepository.UpdateRecoveryCodeHashAndRevokeSessions(ctx, userID, repository.hooks.enter(expectedSessionVersion), recoveryHash, beforeCommit)
}

func (repository *revokingWriteUserRepository) UpdateTOTPFieldsAndRevokeSessions(ctx context.Context, userID uint, expectedSessionVersion int, encryptedSecret string, enabled bool) error {
	return repository.UserRepository.UpdateTOTPFieldsAndRevokeSessions(ctx, userID, repository.hooks.enter(expectedSessionVersion), encryptedSecret, enabled)
}

func (repository *revokingWriteUserRepository) ClearAllDataAndResetSettings(ctx context.Context, userID uint, expectedSessionVersion int) error {
	return repository.UserRepository.ClearAllDataAndResetSettings(ctx, userID, repository.hooks.enter(expectedSessionVersion))
}

// revokingWriteRun is one site's request, prepared up to the point of sending.
type revokingWriteRun struct {
	app        *fiber.App
	database   *gorm.DB
	userID     uint
	authCookie string
	send       func() *http.Response
	// flash marks a site that answers a browser navigation with a flash and a
	// redirect rather than with the mapped JSON error; htmx marks one answered
	// to an HTMX request, whose redirect rides HX-Redirect.
	flash bool
	htmx  bool
}

type revokingWriteSite struct {
	name    string
	prepare func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun
	// effect renders the site's own write as stored, so "nothing was written"
	// is a comparison against the value before the request.
	effect func(t *testing.T, database *gorm.DB, userID uint) string
	// reissuesAfterCommit marks a site that re-issues this device's session
	// through refreshCurrentSession once its write committed, so a failed
	// re-issue leaves it signed out with the change in place.
	reissuesAfterCommit bool
	// reissueFailureSpec is the spec a reissuesAfterCommit site answers with
	// when the write committed but the reissue itself failed (WEB-85): the
	// change already landed, so it is never authSessionCreateErrorSpec.
	// nil for a site that is not reissuesAfterCommit.
	reissueFailureSpec func() APIErrorSpec
	// securityEventAction/securityEventOutcome name the audit line a
	// reissuesAfterCommit site's write logs BEFORE the reissue attempt
	// (precedent: "unlinked" before UnlinkOIDCIdentity's reissue), so a failed
	// reissue must still show the event as having fired.
	securityEventAction  string
	securityEventOutcome string
}

func revokingWriteSettingsContext(t *testing.T, email string, hooks *revokingWriteHooks) settingsSecurityTestContext {
	t.Helper()
	return newSettingsSecurityTestContextWithOptions(t, email, onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true, revokingWrites: hooks, sessionIssuanceFault: hooks.sessionIssuanceFault})
}

func settingsRevokingWriteRun(t *testing.T, ctx settingsSecurityTestContext, method string, path string, form url.Values, extraCookie string, htmx bool) revokingWriteRun {
	t.Helper()
	return revokingWriteRun{
		app:        ctx.app,
		database:   ctx.database,
		userID:     ctx.user.ID,
		authCookie: ctx.authCookie,
		htmx:       htmx,
		send: func() *http.Response {
			headers := map[string]string{"Accept": "application/json"}
			if htmx {
				headers = map[string]string{"HX-Request": "true"}
			}
			if extraCookie != "" {
				headers["Cookie"] = joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), extraCookie)
			}
			return settingsFormRequestWithCSRF(t, ctx, method, path, form, headers)
		},
	}
}

func storedUserForRace(t *testing.T, database *gorm.DB, userID uint) models.User {
	t.Helper()
	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("load user %d: %v", userID, err)
	}
	return user
}

func revokingWriteSites() []revokingWriteSite {
	sites := append(settingsPostureRevokingWriteSites(false), settingsPostureRevokingWriteSites(true)...)
	return append(sites, erasureRevokingWriteSites()...)
}

// settingsPostureRevokingWriteSites are the settings-page posture changes that
// serve both JSON clients and the page's own HTMX forms. The HTMX variant is
// the one a signed-out refusal reaches from the browser: the cookie is already
// cleared, so the answer has to send the page to /login rather than swap an
// error envelope into a page that no longer has a session.
func settingsPostureRevokingWriteSites(htmx bool) []revokingWriteSite {
	suffix, tag := "", ""
	if htmx {
		suffix, tag = " (HTMX)", "-htmx"
	}
	return []revokingWriteSite{
		{
			name:                 "password change" + suffix,
			reissuesAfterCommit:  true,
			reissueFailureSpec:   passwordChangedSignInAgainErrorSpec,
			securityEventAction:  "auth.password_change",
			securityEventOutcome: "success",
			prepare: func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun {
				ctx := revokingWriteSettingsContext(t, "race-password-change"+tag+"@example.com", hooks)
				return settingsRevokingWriteRun(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
					"current_password": {"StrongPass1"},
					"new_password":     {"EvenStronger2"},
					"confirm_password": {"EvenStronger2"},
				}, "", htmx)
			},
			effect: func(t *testing.T, database *gorm.DB, userID uint) string {
				return storedUserForRace(t, database, userID).PasswordHash
			},
		},
		{
			name: "recovery code regeneration" + suffix,
			prepare: func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun {
				ctx := revokingWriteSettingsContext(t, "race-recovery-regenerate"+tag+"@example.com", hooks)
				return settingsRevokingWriteRun(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{"password": {"StrongPass1"}}, "", htmx)
			},
			effect: func(t *testing.T, database *gorm.DB, userID uint) string {
				return storedUserForRace(t, database, userID).RecoveryCodeHash
			},
		},
		{
			name:                 "TOTP enrollment" + suffix,
			reissuesAfterCommit:  true,
			reissueFailureSpec:   totpEnabledSignInAgainErrorSpec,
			securityEventAction:  "settings.2fa.verify",
			securityEventOutcome: "enabled",
			prepare: func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun {
				ctx := revokingWriteSettingsContext(t, "race-totp-enable"+tag+"@example.com", hooks)
				key, err := getTOTPServiceForTest(ctx.database).GenerateSetupKey("Ovumcy", ctx.user.Email)
				if err != nil {
					t.Fatalf("GenerateSetupKey: %v", err)
				}
				code, err := totp.GenerateCode(key.Secret(), time.Now())
				if err != nil {
					t.Fatalf("GenerateCode: %v", err)
				}
				setupCookie := sealTOTPSetupCookieForTest(t, []byte(testAppSecretKey), ctx.user.ID, key.Secret())
				return settingsRevokingWriteRun(t, ctx, http.MethodPut, "/api/v1/users/current/2fa", url.Values{"code": {code}, "password": {"StrongPass1"}}, setupCookie, htmx)
			},
			effect: func(t *testing.T, database *gorm.DB, userID uint) string {
				user := storedUserForRace(t, database, userID)
				return fmt.Sprintf("enabled=%v secret=%q", user.TOTPEnabled, user.TOTPSecret)
			},
		},
		{
			name:                 "TOTP disable" + suffix,
			reissuesAfterCommit:  true,
			reissueFailureSpec:   totpDisabledSignInAgainErrorSpec,
			securityEventAction:  "settings.2fa.disable",
			securityEventOutcome: "disabled",
			prepare: func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun {
				ctx := revokingWriteSettingsContext(t, "race-totp-disable"+tag+"@example.com", hooks)
				if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, ctx.user.AuthSessionVersion, "JBSWY3DPEHPK3PXP"); err != nil {
					t.Fatalf("EnableTOTP setup: %v", err)
				}
				ctx.refreshAuthCookie(t)
				return settingsRevokingWriteRun(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", url.Values{"password": {"StrongPass1"}}, "", htmx)
			},
			effect: func(t *testing.T, database *gorm.DB, userID uint) string {
				user := storedUserForRace(t, database, userID)
				return fmt.Sprintf("enabled=%v secret=%q", user.TOTPEnabled, user.TOTPSecret)
			},
		},
	}
}

func erasureRevokingWriteSites() []revokingWriteSite {
	return []revokingWriteSite{
		{
			name: "clear data",
			prepare: func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun {
				ctx := revokingWriteSettingsContext(t, "race-clear-data@example.com", hooks)
				day := models.DailyLog{UserID: ctx.user.ID, Date: time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowMedium}
				if err := ctx.database.Create(&day).Error; err != nil {
					t.Fatalf("create a tracked day: %v", err)
				}
				return settingsRevokingWriteRun(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{"password": {"StrongPass1"}}, "", false)
			},
			effect: func(t *testing.T, database *gorm.DB, userID uint) string {
				var days int64
				if err := database.Model(&models.DailyLog{}).Where("user_id = ?", userID).Count(&days).Error; err != nil {
					t.Fatalf("count tracked days: %v", err)
				}
				return fmt.Sprintf("days=%d", days)
			},
		},
		{
			// The same wipe submitted by a plain browser form: the refused write
			// has already cleared the cookie, so the answer has to land on /login
			// rather than as the JSON envelope the site above asks for.
			name: "clear data from a browser form",
			prepare: func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun {
				ctx := revokingWriteSettingsContext(t, "race-clear-data-browser@example.com", hooks)
				day := models.DailyLog{UserID: ctx.user.ID, Date: time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowMedium}
				if err := ctx.database.Create(&day).Error; err != nil {
					t.Fatalf("create a tracked day: %v", err)
				}
				run := settingsRevokingWriteRun(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{"password": {"StrongPass1"}}, "", false)
				run.flash = true
				run.send = func() *http.Response {
					return settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{"password": {"StrongPass1"}}, map[string]string{"Accept": "text/html"})
				}
				return run
			},
			effect: func(t *testing.T, database *gorm.DB, userID uint) string {
				var days int64
				if err := database.Model(&models.DailyLog{}).Where("user_id = ?", userID).Count(&days).Error; err != nil {
					t.Fatalf("count tracked days: %v", err)
				}
				return fmt.Sprintf("days=%d", days)
			},
		},
		{
			name: "local password enrollment",
			prepare: func(t *testing.T, hooks *revokingWriteHooks) revokingWriteRun {
				fixture := newOIDCStepupFixtureWithOptions(t, "race-local-password@example.com", onboardingTestAppOptions{revokingWrites: hooks}, nil)
				startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
				t.Cleanup(func() { _ = startResponse.Body.Close() })
				stepupCookie := readStepupCookie(t, startResponse)
				state := extractStepupCallbackState(t, fixture)
				return revokingWriteRun{
					app:        fixture.app,
					database:   fixture.database,
					userID:     fixture.user.ID,
					authCookie: fixture.authCookie,
					flash:      true,
					send: func() *http.Response {
						response := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
						t.Cleanup(func() { _ = response.Body.Close() })
						return response
					},
				}
			},
			effect: func(t *testing.T, database *gorm.DB, userID uint) string {
				user := storedUserForRace(t, database, userID)
				return fmt.Sprintf("local=%v password=%q recovery=%q", user.LocalAuthEnabled, user.PasswordHash, user.RecoveryCodeHash)
			},
		},
	}
}

// Every posture change that revokes the account's sessions and then re-issues
// this device's one writes only from the version the request authenticated
// with. A sign-out everywhere committed after the request was authenticated
// and before its write must therefore refuse the write, sign this device out,
// and leave the account at the version the sign-out produced. The negative
// control runs the same interleaving with the write taking whatever version is
// stored — the unconditional bump — and the change goes through a revocation
// it never saw; the positive control shows an undisturbed request still
// succeeds on a session the next request accepts, while the one it arrived
// with is revoked.
func TestSettingsRevokingWritesRefuseASessionARevocationCommittedMidwayWouldNotReach(t *testing.T) {
	t.Parallel()

	modes := []struct {
		name              string
		revoke            bool
		fromStoredVersion bool
	}{
		{name: "sign-out everywhere before the write", revoke: true},
		{name: "negative control: write from the stored version", revoke: true, fromStoredVersion: true},
		{name: "positive control: no concurrent write"},
	}
	for _, site := range revokingWriteSites() {
		for _, mode := range modes {
			t.Run(site.name+"/"+mode.name, func(t *testing.T) {
				t.Parallel()

				hooks := &revokingWriteHooks{}
				run := site.prepare(t, hooks)
				users := db.NewRepositories(run.database).Users
				versionBefore := storedAuthSessionVersion(t, run.database, run.userID)
				effectBefore := site.effect(t, run.database, run.userID)
				revocations := 0
				if mode.revoke {
					// The hook runs on the request's goroutine, where t.Fatalf is not allowed.
					hooks.beforeWrite = func() {
						revocations++
						if err := users.BumpAuthSessionVersion(context.Background(), run.userID); err != nil {
							t.Errorf("sign out everywhere: %v", err)
						}
					}
				}
				if mode.fromStoredVersion {
					hooks.storedVersion = func() int {
						var stored models.User
						if err := run.database.First(&stored, run.userID).Error; err != nil {
							t.Errorf("load user %d: %v", run.userID, err)
						}
						return stored.AuthSessionVersion
					}
				}

				response := run.send()

				if mode.revoke && revocations != 1 {
					t.Fatalf("expected the sign-out to run once inside the request, ran %d times: the race under test never happened", revocations)
				}
				versionAfter := storedAuthSessionVersion(t, run.database, run.userID)
				effectAfter := site.effect(t, run.database, run.userID)

				if mode.revoke && !mode.fromStoredVersion {
					assertRevokedMidwayRefusal(t, run, response)
					if versionAfter != versionBefore+1 {
						t.Fatalf("expected the account left at the sign-out's version %d, got %d", versionBefore+1, versionAfter)
					}
					if effectAfter != effectBefore {
						t.Fatalf("the refused write still landed: before %s, after %s", effectBefore, effectAfter)
					}
					return
				}

				if response.StatusCode >= http.StatusBadRequest {
					t.Fatalf("expected the change to succeed, got %d: %s", response.StatusCode, mustReadBodyString(t, response.Body))
				}
				if run.flash {
					assertNoSettingsErrorFlash(t, response)
				}
				if effectAfter == effectBefore {
					t.Fatalf("expected the site's write to land, stored value unchanged: %s", effectAfter)
				}
				wantVersion := versionBefore + 1
				if mode.revoke {
					wantVersion++
				}
				if versionAfter != wantVersion {
					t.Fatalf("expected the stored session version %d, got %d", wantVersion, versionAfter)
				}
				if mode.revoke {
					return
				}
				if !linkConfirmSessionOpensDashboard(t, run.app, response) {
					t.Fatal("expected the re-issued session to open /dashboard")
				}
				assertPreRequestSessionRevoked(t, run.app, run.authCookie)
			})
		}
	}
}

// A posture change whose write committed and whose session re-issue then
// failed has cleared this device's cookie inside refreshCurrentSession, so it
// is the same signed-out refusal as a revocation that raced the write: a JSON
// client keeps the mapped envelope, an HTMX form is sent to /login with the
// refusal on the auth flash channel. The fault is armed after prepare, whose
// own sign-in must still succeed.
//
// Each site answers its OWN reissueFailureSpec rather than the generic
// authSessionCreateErrorSpec (WEB-85): the write committed, so "failed to
// create session"/"failed to ... " would be false, and the site's own
// "... sign in again" key says so. The security event each site's write logs
// (enabled/disabled/success) is asserted too, over the audit stream captured
// around the request — which is why this test and its subtests do not call
// t.Parallel(): the stream is the package-wide *log.Logger, and asserting on
// it races against any OTHER test writing through it concurrently.
func TestSettingsPostureReissueFailureAnswersSignedOut(t *testing.T) {
	for _, site := range append(settingsPostureRevokingWriteSites(false), settingsPostureRevokingWriteSites(true)...) {
		if !site.reissuesAfterCommit {
			continue
		}
		t.Run(site.name, func(t *testing.T) {
			var armed atomic.Bool
			hooks := &revokingWriteHooks{sessionIssuanceFault: func() error {
				if armed.Load() {
					return errors.New("injected session issuance fault")
				}
				return nil
			}}
			run := site.prepare(t, hooks)
			effectBefore := site.effect(t, run.database, run.userID)

			armed.Store(true)
			originalWriter := log.Writer()
			var auditOutput bytes.Buffer
			log.SetOutput(&auditOutput)
			t.Cleanup(func() { log.SetOutput(originalWriter) })
			response := run.send()
			log.SetOutput(originalWriter)

			// Anti-vacuity: the write committed, so the refusal can only have
			// come from the re-issue after it.
			if effectAfter := site.effect(t, run.database, run.userID); effectAfter == effectBefore {
				t.Fatalf("expected the write to commit before the re-issue failed, stored value unchanged: %s", effectAfter)
			}
			assertMappedRefusal(t, run, response, site.reissueFailureSpec())
			assertSecurityEventNamesActor(t, auditOutput.String(), site.securityEventAction, site.securityEventOutcome, run.userID)
		})
	}
}

// assertMappedRefusal pins the refusal a signed-out-mid-request answers with:
// the given spec, and a cleared auth cookie that opens nothing. A page-bound
// answer lands on /login on the auth channel — the cleared cookie would bounce
// /settings there anyway and drop a settings flash on the way.
func assertMappedRefusal(t *testing.T, run revokingWriteRun, response *http.Response, spec APIErrorSpec) {
	t.Helper()
	if run.flash || run.htmx {
		assertSignedOutRefusal(t, response, spec.Key, run.htmx)
	} else {
		body := mustReadBodyString(t, response.Body)
		if response.StatusCode != spec.Status || !strings.Contains(body, spec.Key) {
			t.Fatalf("expected %d %q, got %d: %s", spec.Status, spec.Key, response.StatusCode, body)
		}
	}
	assertAuthCookieCleared(t, run.app, response)
}

// assertRevokedMidwayRefusal is assertMappedRefusal pinned to
// authSessionCreateErrorSpec, for the sites whose write itself was refused by
// a revocation that raced it (nothing committed, unlike the reissue-only
// failures above).
func assertRevokedMidwayRefusal(t *testing.T, run revokingWriteRun, response *http.Response) {
	t.Helper()
	assertMappedRefusal(t, run, response, authSessionCreateErrorSpec())
}

// assertIdentityChangeAppliedRefusal is assertMappedRefusal's counterpart for
// reissueSessionAfterIdentityChange's OTHER refusal: the link or unlink (or
// its no-op already-linked confirmation) has already gone through by the time
// a separate revocation's version wins the reload, so "failed to create
// session" would be false.
func assertIdentityChangeAppliedRefusal(t *testing.T, run revokingWriteRun, response *http.Response) {
	t.Helper()
	assertMappedRefusal(t, run, response, authIdentityChangeAppliedSignInAgainErrorSpec())
}

func assertAuthCookieCleared(t *testing.T, app *fiber.App, response *http.Response) {
	t.Helper()
	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected the auth cookie cleared, got %+v", authCookie)
	}
	if linkConfirmSessionOpensDashboard(t, app, response) {
		t.Fatal("expected no usable session after the refusal")
	}
}

func assertNoSettingsErrorFlash(t *testing.T, response *http.Response) {
	t.Helper()
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		return
	}
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.SettingsError != "" {
		t.Fatalf("expected no settings refusal, got %q", payload.SettingsError)
	}
}

// assertPreRequestSessionRevoked requires the exact bounce AuthRequired gives
// a revoked session on a page route.
func assertPreRequestSessionRevoked(t *testing.T, app *fiber.App, cookieHeader string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", cookieHeader)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Fatalf("expected the pre-request session bounced to /login, got %d Location %q", response.StatusCode, response.Header.Get("Location"))
	}
}

// realIdentityOIDCWorkflowService keeps the stub for the provider round trip
// and routes the identity write — the link a step-up completes, and the
// unlink — through the real service and repository, so the write's
// compare-and-set and the handler's post-write re-read meet the same
// database. The provider exchange is the stub's in every api test; what is
// under test here starts after it.
//
// beforeWrite and afterWrite commit a concurrent write on either side of the
// identity write; storedVersion hands the write the version stored at that
// moment instead of the one the request authenticated with.
type realIdentityOIDCWorkflowService struct {
	*stubOIDCWorkflowService
	real *services.OIDCLoginService

	beforeWrite   func()
	afterWrite    func()
	storedVersion func() int
}

func (service *realIdentityOIDCWorkflowService) enter(expectedSessionVersion int) int {
	if service.beforeWrite != nil {
		service.beforeWrite()
	}
	if service.storedVersion != nil {
		return service.storedVersion()
	}
	return expectedSessionVersion
}

func (service *realIdentityOIDCWorkflowService) leave() {
	if service.afterWrite != nil {
		service.afterWrite()
	}
}

func (service *realIdentityOIDCWorkflowService) CompleteIdentityLinkReauth(ctx context.Context, code string, codeVerifier string, expectedNonce string, targetUserID uint, expectedSessionVersion int, maxAuthAge time.Duration, now time.Time) (int, error) {
	if _, err := service.stubOIDCWorkflowService.CompleteIdentityLinkReauth(ctx, code, codeVerifier, expectedNonce, targetUserID, expectedSessionVersion, maxAuthAge, now); err != nil {
		return 0, err
	}
	linkedVersion, err := service.real.ConfirmAndLinkIdentity(ctx, targetUserID, service.enter(expectedSessionVersion), service.identityLinkClaims, now)
	service.leave()
	return linkedVersion, err
}

func (service *realIdentityOIDCWorkflowService) UnlinkIdentity(ctx context.Context, user models.User, identityID uint) (int, error) {
	user.AuthSessionVersion = service.enter(user.AuthSessionVersion)
	unlinkedVersion, err := service.real.UnlinkIdentity(ctx, user, identityID)
	service.leave()
	return unlinkedVersion, err
}

// Linking or unlinking an identity revokes the account's sessions and
// re-issues this device's one. A sign-out everywhere committed before the
// write refuses it; one committed after the write but before the re-read lets
// the change stand and signs this device out, because a session re-issued at
// the version the re-read shows would outlive that sign-out. An
// already-linked pair writes nothing, so the re-read's check alone refuses it.
// The negative control has the write take whatever version is stored and the
// change goes through a revocation it never saw; the positive control shows
// an undisturbed change re-issues a session the next request accepts, while
// the one it arrived with is revoked.
func TestSettingsIdentityChangesRefuseASessionARevocationCommittedMidwayWouldNotReach(t *testing.T) {
	t.Parallel()

	const linkIssuer, linkSubject = "https://id.example.com", "race-linked-subject"
	cases := []struct {
		name              string
		unlink            bool
		alreadyLinked     bool
		revokeBefore      bool
		revokeAfter       bool
		fromStoredVersion bool
		wantRefused       bool
		// wantChangeApplied marks a refusal that comes from
		// reissueSessionAfterIdentityChange's post-commit version-mismatch arm
		// rather than the write's own compare-and-set: the link or unlink (or its
		// no-op already-linked confirmation) has already gone through by the
		// time the race is observed, so the refusal answers
		// authIdentityChangeAppliedSignInAgainErrorSpec instead of
		// authSessionCreateErrorSpec. "sign-out before the write" (without
		// alreadyLinked) still fails the write's own CAS and keeps the old
		// refusal; only a write that already committed (revokeAfter) or never
		// needed to (alreadyLinked) reaches the new one.
		wantChangeApplied bool
		// htmx sends the unlink as the settings page's HTMX request instead of
		// JSON, so the signed-out refusal has a page to redirect.
		htmx bool
	}{
		{name: "link: sign-out before the write", revokeBefore: true, wantRefused: true},
		{name: "link: sign-out after the write", revokeAfter: true, wantRefused: true, wantChangeApplied: true},
		{name: "link: sign-out before an already-linked confirmation", alreadyLinked: true, revokeBefore: true, wantRefused: true, wantChangeApplied: true},
		{name: "link: negative control: write from the stored version", revokeBefore: true, fromStoredVersion: true},
		{name: "link: positive control: no concurrent write"},
		{name: "unlink: sign-out before the write", unlink: true, revokeBefore: true, wantRefused: true},
		{name: "unlink: sign-out after the write", unlink: true, revokeAfter: true, wantRefused: true, wantChangeApplied: true},
		{name: "unlink: negative control: write from the stored version", unlink: true, revokeBefore: true, fromStoredVersion: true},
		{name: "unlink: positive control: no concurrent write", unlink: true},
		{name: "unlink (htmx): sign-out before the write", unlink: true, htmx: true, revokeBefore: true, wantRefused: true},
		{name: "unlink (htmx): sign-out after the write", unlink: true, htmx: true, revokeAfter: true, wantRefused: true, wantChangeApplied: true},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var workflow *realIdentityOIDCWorkflowService
			fixture := newOIDCStepupFixtureWithOptions(t, fmt.Sprintf("race-identity-%d@example.com", index), onboardingTestAppOptions{}, func(stub *stubOIDCWorkflowService) OIDCWorkflowService {
				workflow = &realIdentityOIDCWorkflowService{stubOIDCWorkflowService: stub}
				return workflow
			})
			repositories := db.NewRepositories(fixture.database)
			workflow.real = services.NewOIDCLoginService(enabledOIDCProviderForLinkTest{}, repositories.OIDCIdentities, repositories.Users, nil)
			fixture.oidcStub.identityLinkClaims = security.OIDCClaims{Issuer: linkIssuer, Subject: linkSubject}
			if tc.alreadyLinked {
				// Inserted directly: a link through the repository would bump
				// the version and revoke the fixture's session before the test.
				identity := models.OIDCIdentity{UserID: fixture.user.ID, Issuer: linkIssuer, Subject: linkSubject, CreatedAt: time.Now().UTC()}
				if err := fixture.database.Create(&identity).Error; err != nil {
					t.Fatalf("pre-link the identity: %v", err)
				}
			}

			revocations := 0
			revoke := func() {
				revocations++
				if err := repositories.Users.BumpAuthSessionVersion(context.Background(), fixture.user.ID); err != nil {
					t.Errorf("sign out everywhere: %v", err)
				}
			}

			var send func() *http.Response
			if tc.unlink {
				giveLinkFixtureAPassword(t, fixture)
				identityID := strconv.FormatUint(uint64(fixture.identity.ID), 10)
				send = func() *http.Response {
					var response *http.Response
					if tc.htmx {
						response = deleteOIDCIdentityWithFormat(t, fixture, identityID, linkFixturePassword, "text/html", true)
					} else {
						response = deleteOIDCIdentity(t, fixture, identityID, linkFixturePassword, true)
					}
					t.Cleanup(func() { _ = response.Body.Close() })
					return response
				}
			} else {
				startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
				t.Cleanup(func() { _ = startResponse.Body.Close() })
				stepupCookie := readStepupCookie(t, startResponse)
				state := extractStepupCallbackState(t, fixture)
				send = func() *http.Response {
					response := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
					t.Cleanup(func() { _ = response.Body.Close() })
					return response
				}
			}

			if tc.revokeBefore {
				workflow.beforeWrite = revoke
			}
			if tc.revokeAfter {
				workflow.afterWrite = revoke
			}
			if tc.fromStoredVersion {
				// The hooks run on the request's goroutine, where t.Fatalf is not allowed.
				workflow.storedVersion = func() int {
					var stored models.User
					if err := fixture.database.First(&stored, fixture.user.ID).Error; err != nil {
						t.Errorf("load user %d: %v", fixture.user.ID, err)
					}
					return stored.AuthSessionVersion
				}
			}
			versionBefore := storedAuthSessionVersion(t, fixture.database, fixture.user.ID)

			response := send()

			if (tc.revokeBefore || tc.revokeAfter) && revocations != 1 {
				t.Fatalf("expected the sign-out to run once inside the request, ran %d times: the race under test never happened", revocations)
			}
			// The identity write lands unless it was a no-op or a revocation
			// before it refused it.
			written := !tc.alreadyLinked && (!tc.revokeBefore || tc.fromStoredVersion)
			wantVersion := versionBefore
			if tc.revokeBefore || tc.revokeAfter {
				wantVersion++
			}
			if written {
				wantVersion++
			}
			if got := storedAuthSessionVersion(t, fixture.database, fixture.user.ID); got != wantVersion {
				t.Fatalf("expected the stored session version %d, got %d", wantVersion, got)
			}
			if tc.unlink {
				var remaining int64
				if err := fixture.database.Model(&models.OIDCIdentity{}).Where("id = ?", fixture.identity.ID).Count(&remaining).Error; err != nil {
					t.Fatalf("count the identity: %v", err)
				}
				if wantRemaining := !written; (remaining == 1) != wantRemaining {
					t.Fatalf("expected the identity present=%v, found %d rows", wantRemaining, remaining)
				}
			} else {
				_, linked, err := repositories.OIDCIdentities.FindByIssuerSubject(context.Background(), linkIssuer, linkSubject)
				if wantLinked := written || tc.alreadyLinked; err != nil || linked != wantLinked {
					t.Fatalf("expected linked=%v, got linked=%v err=%v", wantLinked, linked, err)
				}
			}

			run := revokingWriteRun{app: fixture.app, flash: !tc.unlink, htmx: tc.htmx}
			if tc.wantRefused {
				if tc.wantChangeApplied {
					assertIdentityChangeAppliedRefusal(t, run, response)
				} else {
					assertRevokedMidwayRefusal(t, run, response)
				}
				return
			}
			if response.StatusCode >= http.StatusBadRequest {
				t.Fatalf("expected the change to succeed, got %d: %s", response.StatusCode, mustReadBodyString(t, response.Body))
			}
			if !tc.unlink {
				flashCookie := responseCookie(response.Cookies(), flashCookieName)
				if flashCookie == nil || decodeFlashCookieForTest(t, flashCookie.Value).SettingsSuccess != "oidc_identity_linked" {
					t.Fatal("expected the oidc_identity_linked flash")
				}
			}
			if !linkConfirmSessionOpensDashboard(t, fixture.app, response) {
				t.Fatal("expected the re-issued session to open /dashboard")
			}
			if !tc.fromStoredVersion {
				assertPreRequestSessionRevoked(t, fixture.app, fixture.authCookie)
			}
		})
	}
}
