package api

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

func TestRegisterClosedModeReturnsForbiddenJSONError(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	form := url.Values{
		"email":            {"closed-mode@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "registration disabled" {
		t.Fatalf("expected registration disabled error, got %q", got)
	}
}

func TestRegisterClosedModeRedirectDoesNotLeakEmailOrErrorInQuery(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	form := url.Values{
		"email":            {"closed-mode@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}

	location := strings.TrimSpace(response.Header.Get("Location"))
	if location != "/register" {
		t.Fatalf("expected redirect to /register, got %q", location)
	}
	if flashValue := responseCookieValue(response.Cookies(), flashCookieName); flashValue == "" {
		t.Fatalf("expected flash cookie for registration disabled redirect")
	}
}

func TestRegisterPageClosedModeRendersDisabledStateWithoutForm(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	request := httptest.NewRequest(http.MethodGet, "/register", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlAuthErrorByKey(document, "auth.error.registration_disabled") == nil {
		t.Fatal("expected register page to render auth.error.registration_disabled banner")
	}
	if htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-registration-disabled")
	}) == nil {
		t.Fatal("expected disabled register state marker")
	}
	if htmlElementByID(document, "register-form") != nil {
		t.Fatal("did not expect register form in closed mode")
	}
}

func TestLoginPageClosedModeHidesSignupCTA(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	rendered := string(body)
	if strings.Contains(rendered, "data-auth-signup-cta") {
		t.Fatalf("did not expect signup CTA in closed registration mode")
	}
}

// loginRegistrationCell is one cell of the login mode x REGISTRATION_MODE
// matrix, crossed with OIDC_AUTO_PROVISION. oidc false is the `local` login
// mode: OIDC is switched off, and autoProvision is moot (left false).
//
// Boot refuses OIDC_AUTO_PROVISION=true with closed registration, so the
// closed x autoProvision cells are a configuration production cannot reach;
// they are kept because the service still has to refuse there. The reachable
// closed instance is the autoProvision=false one.
type loginRegistrationCell struct {
	name          string
	oidc          bool
	loginMode     security.OIDCLoginMode
	registration  services.RegistrationMode
	autoProvision bool
}

// provisionsNewcomers is the one condition under which a never-seen OIDC
// identity becomes an account: OIDC on, auto-provision on, registration open.
func (cell loginRegistrationCell) provisionsNewcomers() bool {
	return cell.oidc && cell.autoProvision && cell.registration == services.RegistrationModeOpen
}

// localPublicAuth states what the cell's login mode lets an anonymous visitor
// do with a password; the expectation is written here, not read back from the
// code under test.
func (cell loginRegistrationCell) localPublicAuth() bool {
	return !cell.oidc || cell.loginMode != security.OIDCLoginModeOIDCOnly
}

// localRegistrationPermitted is the one condition under which an anonymous
// visitor may create an account with a password.
func (cell loginRegistrationCell) localRegistrationPermitted() bool {
	return cell.localPublicAuth() && cell.registration == services.RegistrationModeOpen
}

func loginRegistrationCells() []loginRegistrationCell {
	var cells []loginRegistrationCell
	for _, registration := range []services.RegistrationMode{services.RegistrationModeOpen, services.RegistrationModeClosed} {
		cells = append(cells, loginRegistrationCell{name: "local x " + string(registration), registration: registration})
		for _, autoProvision := range []bool{true, false} {
			suffix := " x " + string(registration) + " x auto-provision " + map[bool]string{true: "on", false: "off"}[autoProvision]
			cells = append(cells,
				loginRegistrationCell{name: "hybrid" + suffix, oidc: true, loginMode: security.OIDCLoginModeHybrid, registration: registration, autoProvision: autoProvision},
				loginRegistrationCell{name: "oidc_only" + suffix, oidc: true, loginMode: security.OIDCLoginModeOIDCOnly, registration: registration, autoProvision: autoProvision},
			)
		}
	}
	return cells
}

const (
	cellIdentityIssuer  = "https://id.example.com"
	cellIdentitySubject = "never-seen-subject"
	cellIdentityEmail   = "sso-newcomer@example.com"

	// The two flash keys of the WEB-493 pair, as the literal strings the
	// browser-side flash carries; written out so that renaming one in the
	// error mapping shows up here.
	cellFlashKeyProvisioningRefused = "sso temporarily unavailable"
	cellFlashKeyAccountUnavailable  = "sso sign-in unavailable"
)

// cellOIDCProvider is the provider double of the cell tests: the real
// OIDCLoginService, the real registration service and the real repositories
// sit behind it, so only the IdP round trip is replaced. It answers every code
// exchange with the one claim set it holds.
type cellOIDCProvider struct {
	config    security.OIDCConfig
	claims    security.OIDCClaims
	lastState string
}

func (provider *cellOIDCProvider) Enabled() bool { return provider.config.Enabled }
func (provider *cellOIDCProvider) LocalPublicAuthEnabled() bool {
	return provider.config.LocalPublicAuthEnabled()
}
func (provider *cellOIDCProvider) Config() security.OIDCConfig { return provider.config }
func (provider *cellOIDCProvider) AuthCodeURL(_ context.Context, state string, _ string, _ string, _ map[string]string) (string, error) {
	provider.lastState = state
	return cellIdentityIssuer + "/authorize", nil
}
func (provider *cellOIDCProvider) ExchangeCode(context.Context, string, string, string) (security.OIDCExchangeResult, error) {
	return security.OIDCExchangeResult{Claims: provider.claims}, nil
}
func (provider *cellOIDCProvider) UserInfoClaims(context.Context, security.OIDCExchangeResult) (security.OIDCClaims, error) {
	return security.OIDCClaims{}, security.ErrOIDCUserInfoUnavailable
}

// newLoginRegistrationCellApp builds the cell's app from the composition root.
// Like the other onboarding test apps it mounts no CSRF middleware and no
// rate limiters, so these cells say nothing about either; they pin only what
// the routes and services answer.
func newLoginRegistrationCellApp(t *testing.T, cell loginRegistrationCell, claims security.OIDCClaims) (*fiber.App, *gorm.DB, *cellOIDCProvider) {
	t.Helper()

	provider := &cellOIDCProvider{
		config: security.OIDCConfig{
			Enabled:       cell.oidc,
			LoginMode:     cell.loginMode,
			AutoProvision: cell.autoProvision,
			IssuerURL:     testOIDCIssuerURL,
		},
		claims: claims,
	}
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure:     true,
		auditLogEnabled:  true,
		registrationMode: cell.registration,
		oidcEnabled:      cell.oidc,
		oidcServiceFromDeps: func(dependencies Dependencies, repositories *db.Repositories) OIDCWorkflowService {
			provisioner, ok := dependencies.RegistrationService.(services.OIDCAutoProvisioner)
			if !ok {
				t.Fatalf("the composition root's registration service %T does not provision OIDC owners", dependencies.RegistrationService)
			}
			service := services.NewOIDCLoginService(provider, repositories.OIDCIdentities, repositories.Users, provisioner)
			// The composition root hands the same second-factor verifier to the
			// OIDC service; a hand-built one without it would sign in on fewer
			// factors than production.
			service.SetTOTPVerifier(dependencies.TOTPService)
			return service
		},
	})
	return app, database, provider
}

func countUsers(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := database.Model(&models.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	return count
}

func freshCellIdentityClaims(email string) security.OIDCClaims {
	return security.OIDCClaims{
		Issuer:        cellIdentityIssuer,
		Subject:       cellIdentitySubject,
		Email:         email,
		EmailVerified: true,
		AuthTime:      time.Now().UTC().Add(-time.Minute),
	}
}

// TestLoginAndRegistrationModesComposeForAnAnonymousVisitorAtEveryCell crosses
// the three login modes with both registration modes and asks, per cell, what
// an anonymous visitor reaches: the register page, the register endpoint, and
// the sign-up CTA on /login. The cells where local registration is permitted
// are the positive controls of every refusal in the other cells: the same
// request shape yields an account, a form and a CTA there.
func TestLoginAndRegistrationModesComposeForAnAnonymousVisitorAtEveryCell(t *testing.T) {
	t.Parallel()

	for _, cell := range loginRegistrationCells() {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()

			app, database, _ := newLoginRegistrationCellApp(t, cell, freshCellIdentityClaims(cellIdentityEmail))

			registerPage := httptest.NewRequest(http.MethodGet, "/register", nil)
			registerPage.Header.Set("Accept-Language", "en")
			registerPageResponse := mustAppResponse(t, app, registerPage)
			if !cell.localPublicAuth() {
				assertStatusCode(t, registerPageResponse, http.StatusSeeOther)
				if location := registerPageResponse.Header.Get("Location"); location != "/login" {
					t.Fatalf("expected /register to redirect to /login, got %q", location)
				}
			} else {
				assertStatusCode(t, registerPageResponse, http.StatusOK)
				document := mustParseHTMLDocument(t, mustReadBodyString(t, registerPageResponse.Body))
				hasForm := htmlElementByID(document, "register-form") != nil
				hasDisabledMarker := htmlFindElement(document, func(node *html.Node) bool {
					return node.Type == html.ElementNode && htmlHasAttr(node, "data-registration-disabled")
				}) != nil
				if hasForm != cell.localRegistrationPermitted() || hasDisabledMarker == cell.localRegistrationPermitted() {
					t.Fatalf("register page: form=%v disabled marker=%v, but local registration permitted=%v", hasForm, hasDisabledMarker, cell.localRegistrationPermitted())
				}
			}

			loginPage := httptest.NewRequest(http.MethodGet, "/login", nil)
			loginPage.Header.Set("Accept-Language", "en")
			loginResponse := mustAppResponse(t, app, loginPage)
			assertStatusCode(t, loginResponse, http.StatusOK)
			loginBody := mustReadBodyString(t, loginResponse.Body)
			if hasCTA := strings.Contains(loginBody, "data-auth-signup-cta"); hasCTA != cell.localRegistrationPermitted() {
				t.Fatalf("sign-up CTA on /login: present=%v, want %v", hasCTA, cell.localRegistrationPermitted())
			}

			form := url.Values{
				"email":            {"anonymous-visitor@example.com"},
				"password":         {"StrongPass1"},
				"confirm_password": {"StrongPass1"},
				"consent":          {"true"},
			}
			register := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
			register.Header.Set("Accept", "application/json")
			register.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			registerResponse := mustAppResponse(t, app, register)
			switch {
			case cell.localRegistrationPermitted():
				assertStatusCode(t, registerResponse, http.StatusCreated)
			case !cell.localPublicAuth():
				assertStatusCode(t, registerResponse, http.StatusForbidden)
				if got := readAPIError(t, registerResponse.Body); got != "local sign-in unavailable" {
					t.Fatalf("expected the oidc_only refusal, got %q", got)
				}
			default:
				assertStatusCode(t, registerResponse, http.StatusForbidden)
				if got := readAPIError(t, registerResponse.Body); got != "registration disabled" {
					t.Fatalf("expected the closed-registration refusal, got %q", got)
				}
			}
			wantUsers := int64(0)
			if cell.localRegistrationPermitted() {
				wantUsers = 1
			}
			if got := countUsers(t, database); got != wantUsers {
				t.Fatalf("users after the anonymous registration attempt: got %d, want %d", got, wantUsers)
			}

			// Password sign-in follows the login mode alone, not the registration
			// mode: refused under oidc_only, accepted in every other cell (the
			// control that the refusal is the gate and not a broken fixture).
			const loginEmail, loginPassword = "existing-owner@example.com", "StrongPass1"
			createOnboardingTestUser(t, database, loginEmail, loginPassword, true)
			signIn := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(url.Values{
				"email":    {loginEmail},
				"password": {loginPassword},
			}.Encode()))
			signIn.Header.Set("Accept", "application/json")
			signIn.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			signInResponse := mustAppResponse(t, app, signIn)
			sessionCookie := responseCookie(signInResponse.Cookies(), authCookieName)
			signedIn := sessionCookie != nil && cellCookieIsLive(sessionCookie)
			if cell.localPublicAuth() {
				assertStatusCode(t, signInResponse, http.StatusOK)
				if !signedIn {
					t.Fatal("password sign-in is permitted in this cell: expected a session cookie")
				}
			} else {
				assertStatusCode(t, signInResponse, http.StatusForbidden)
				if got := readAPIError(t, signInResponse.Body); got != "local sign-in unavailable" {
					t.Fatalf("expected the oidc_only sign-in refusal, got %q", got)
				}
				if signedIn {
					t.Fatal("oidc_only: password sign-in must not issue a session cookie")
				}
			}

			// The local sign-in route for an account that exists. Where local public
			// auth is on, the sign-in succeeds (the control for the refusal); in
			// oidc_only the same request is turned away at the gate.
			createOnboardingTestUser(t, database, "seeded-owner@example.com", "StrongPass1", true)
			login := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(url.Values{
				"email":    {"seeded-owner@example.com"},
				"password": {"StrongPass1"},
			}.Encode()))
			login.Header.Set("Accept", "application/json")
			login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			loginPostResponse := mustAppResponse(t, app, login)
			if cell.localPublicAuth() {
				assertStatusCode(t, loginPostResponse, http.StatusOK)
				if authCookie := responseCookie(loginPostResponse.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
					t.Fatal("local sign-in must issue a session where local public auth is on")
				}
			} else {
				assertStatusCode(t, loginPostResponse, http.StatusForbidden)
				if got := readAPIError(t, loginPostResponse.Body); got != "local sign-in unavailable" {
					t.Fatalf("expected the oidc_only local sign-in refusal, got %q", got)
				}
				if authCookie := responseCookie(loginPostResponse.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
					t.Fatal("oidc_only: the local sign-in route must not issue a session")
				}
			}
		})
	}
}

type cellCallbackReply struct {
	response *http.Response
	started  bool
}

// runCellOIDCCallback starts a sign-in and completes the callback the way a
// browser returning from the provider does. started is false when the start
// route handed out no state (OIDC off), in which case no callback is sent.
func runCellOIDCCallback(t *testing.T, app *fiber.App, provider *cellOIDCProvider) cellCallbackReply {
	t.Helper()

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil || provider.lastState == "" {
		return cellCallbackReply{response: startResponse}
	}
	callback := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {provider.lastState},
		"code":  {"provider-code"},
	}.Encode()))
	callback.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callback.Header.Set("Cookie", stateCookie.String())
	return cellCallbackReply{response: mustAppResponse(t, app, callback), started: true}
}

func cellReplyFlashKey(t *testing.T, response *http.Response) string {
	t.Helper()
	cookie := responseCookie(response.Cookies(), exemptFlashCookieName)
	if cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		return ""
	}
	return decodeExemptFlashCookieForTest(t, cookie.Value).AuthError
}

// cellObservation is everything a callback reply shows an observer, plus the
// rows it left behind. cookieLines are the full Set-Cookie lines, sorted, with
// the flash value masked (it is sealed with a fresh nonce per reply, so two
// equal answers differ in it) and every Expires normalised; the flash key is
// compared separately.
type cellObservation struct {
	status           int
	location         string
	flashKey         string
	cookieLines      []string
	headers          string
	body             string
	hasAuthCookie    bool
	hasPendingCookie bool
	created          int64
	identities       int64
	// audit is the audit stream captured around the callback; the caller that
	// captures it fills it in.
	audit string
}

// observeCellReply reads the reply and the database; seededUsers is the
// number of user rows the test created before the callback. It consumes the
// response body, so the caller must not read it again.
func observeCellReply(t *testing.T, response *http.Response, database *gorm.DB, seededUsers int64) cellObservation {
	t.Helper()

	observation := cellObservation{
		status:   response.StatusCode,
		location: response.Header.Get("Location"),
		flashKey: cellReplyFlashKey(t, response),
		body:     mustReadBodyString(t, response.Body),
		created:  countUsers(t, database) - seededUsers,
	}
	if err := database.Model(&models.OIDCIdentity{}).Count(&observation.identities).Error; err != nil {
		t.Fatalf("count identities: %v", err)
	}
	for _, cookie := range response.Cookies() {
		masked := *cookie
		// Expires is now plus a lifetime, so two replies a second apart differ
		// in it; keep that one is set, not its value.
		if !masked.Expires.IsZero() {
			masked.Expires = time.Unix(1, 0).UTC()
		}
		masked.RawExpires = ""
		switch cookie.Name {
		case exemptFlashCookieName:
			masked.Value = "<flash>"
		case authCookieName:
			observation.hasAuthCookie = observation.hasAuthCookie || cellCookieIsLive(cookie)
		case oidcLinkPendingCookieName:
			observation.hasPendingCookie = observation.hasPendingCookie || cellCookieIsLive(cookie)
		}
		observation.cookieLines = append(observation.cookieLines, masked.String())
	}
	sort.Strings(observation.cookieLines)

	var headers []string
	for name, values := range response.Header {
		// Set-Cookie is compared line by line above, Date moves with the clock,
		// and Content-Length follows the body, which is compared whole.
		if name == "Set-Cookie" || name == "Date" || name == "Content-Length" {
			continue
		}
		headers = append(headers, name+": "+strings.Join(values, ", "))
	}
	sort.Strings(headers)
	observation.headers = strings.Join(headers, "\n")
	return observation
}

// cellCookieIsLive reports whether a Set-Cookie sets a value rather than
// clearing one: non-empty, not expired by Max-Age and not expired by date.
func cellCookieIsLive(cookie *http.Cookie) bool {
	if strings.TrimSpace(cookie.Value) == "" || cookie.MaxAge < 0 {
		return false
	}
	return cookie.Expires.IsZero() || cookie.Expires.After(time.Now())
}

// assertCellReplyHoldsNoIdentity requires that neither the asserted email nor
// the subject shows in the redirect, any cookie line or the body, in any of
// the forms a URL or a form would carry them.
func assertCellReplyHoldsNoIdentity(t *testing.T, observation cellObservation) {
	t.Helper()

	surface := observation.location + "\n" + strings.Join(observation.cookieLines, "\n") + "\n" + observation.body
	for _, needle := range []string{cellIdentityEmail, url.QueryEscape(cellIdentityEmail), cellIdentitySubject} {
		if strings.Contains(surface, needle) {
			t.Fatalf("the callback reply carries the asserted identity %q", needle)
		}
	}
}

// TestOIDCCallbackForANeverSeenIdentityHonoursTheRegistrationModeAtEveryCell
// drives the callback across OIDC_AUTO_PROVISION and the registration mode.
// Where auto-provision is on and registration is open the identity is
// provisioned (the positive control); in every other OIDC cell the callback
// creates no user row and links no identity. Where OIDC is off no sign-in
// starts at all: the start route answers 303 to /login with the disabled
// flash. The zeros are read as refusals by the provisioning cell beside them:
// the same request, identity and provider double provision there.
func TestOIDCCallbackForANeverSeenIdentityHonoursTheRegistrationModeAtEveryCell(t *testing.T) {
	t.Parallel()

	for _, cell := range loginRegistrationCells() {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()

			app, database, provider := newLoginRegistrationCellApp(t, cell, freshCellIdentityClaims(cellIdentityEmail))
			reply := runCellOIDCCallback(t, app, provider)

			if !cell.oidc {
				if reply.started {
					t.Fatal("OIDC is off: the start route must not hand out a sign-in state")
				}
				assertStatusCode(t, reply.response, http.StatusSeeOther)
				if location := reply.response.Header.Get("Location"); location != "/login" {
					t.Fatalf("OIDC off: expected the start route to redirect to /login, got %q", location)
				}
				if got, want := cellReplyFlashKey(t, reply.response), cellFlashKeyProvisioningRefused; got != want {
					t.Fatalf("OIDC off: expected the disabled flash %q, got %q", want, got)
				}
				if got := countUsers(t, database); got != 0 {
					t.Fatalf("OIDC off must create no user, got %d", got)
				}
				return
			}
			if !reply.started {
				t.Fatalf("anchor: OIDC is on, the start route must hand out a state (status %d)", reply.response.StatusCode)
			}

			response := reply.response
			seen := observeCellReply(t, response, database, 0)

			if cell.provisionsNewcomers() {
				assertStatusCode(t, response, http.StatusSeeOther)
				if !seen.hasAuthCookie {
					t.Fatal("open registration with auto-provision: expected the provisioned owner to be signed in")
				}
				if seen.created != 1 || seen.identities != 1 {
					t.Fatalf("open registration with auto-provision: expected one user and one linked identity, got %d and %d", seen.created, seen.identities)
				}
				return
			}

			assertStatusCode(t, response, http.StatusSeeOther)
			if seen.location != "/login" {
				t.Fatalf("expected redirect to /login, got %q", seen.location)
			}
			if seen.hasAuthCookie {
				t.Fatal("no session may be issued for a never-seen identity that is not provisioned")
			}
			if seen.hasPendingCookie {
				t.Fatal("no pending-link cookie may be parked for a never-seen identity that is not provisioned")
			}
			if seen.created != 0 || seen.identities != 0 {
				t.Fatalf("the callback must create no user and link no identity, got %d users and %d identities", seen.created, seen.identities)
			}
			assertCellReplyHoldsNoIdentity(t, seen)
			// The refusal key is pinned, not just "some flash": the zero rows must
			// come from the refusal under test, not an earlier failure.
			//   - auto-provision off (open or closed): the service answers
			//     account-unavailable before it reaches the registration service.
			//   - auto-provision on with closed registration: boot refuses this
			//     configuration, so the cell is defence in depth; the provisioner
			//     refuses and the key is the provisioning one. WEB-493 may change it.
			want := cellFlashKeyAccountUnavailable
			if cell.autoProvision {
				want = cellFlashKeyProvisioningRefused
			}
			if got := cellReplyFlashKey(t, response); got != want {
				t.Fatalf("expected the refusal flash %q, got %q", want, got)
			}
		})
	}
}

// TestOIDCCallbackDoesNotTellANeverSeenIdentityFromOneWhoseEmailHasAnAccount
// holds every OIDC cell to the no-account-existence-leak rule: a visitor
// whose identity is new gets the same reply whether or not a local account
// already holds the asserted email. The provisioning cell (open registration,
// auto-provision on) is the control that the fixture tells the two apart at
// all (one provisions, the other is refused).
//
// The identity whose email an account holds is refused in every cell: no
// session, no identity row, 303 to /login and the account-unavailable flash.
//
// Where auto-provision is off (the reachable closed instance, and the open
// one), the never-seen identity must get that same flash key. The anchor key
// is shared by five errors, so the claim rests on the audit line too: the
// held identity's line carries cause=link_required, the never-seen one's
// cause=account_unavailable, which only the service's own refusal writes.
//
// WEB-493: with auto-provision on and registration closed (a configuration
// boot refuses, so defence in depth) a never-seen identity is answered "sso
// temporarily unavailable", an identity whose email an account holds "sso
// sign-in unavailable". The test pins that difference as a tripwire (it goes
// red when the oracle is fixed) and holds everything else equal meanwhile.
//
// Not parallel: the audit stream is the process-wide logger, swapped here.
func TestOIDCCallbackDoesNotTellANeverSeenIdentityFromOneWhoseEmailHasAnAccount(t *testing.T) {
	replyFor := func(t *testing.T, cell loginRegistrationCell, accountHoldsEmail bool) cellObservation {
		t.Helper()
		app, database, provider := newLoginRegistrationCellApp(t, cell, freshCellIdentityClaims(cellIdentityEmail))
		var seeded int64
		if accountHoldsEmail {
			createOnboardingTestUser(t, database, cellIdentityEmail, "StrongPass1", true)
			seeded = 1
		}
		originalWriter := log.Writer()
		defer log.SetOutput(originalWriter)
		var audit bytes.Buffer
		log.SetOutput(&audit)
		reply := runCellOIDCCallback(t, app, provider)
		log.SetOutput(originalWriter)
		if !reply.started {
			t.Fatalf("anchor: the sign-in did not start (status %d)", reply.response.StatusCode)
		}
		observation := observeCellReply(t, reply.response, database, seeded)
		observation.audit = audit.String()
		return observation
	}

	for _, cell := range loginRegistrationCells() {
		if !cell.oidc {
			continue
		}
		t.Run(cell.name, func(t *testing.T) {
			fresh := replyFor(t, cell, false)
			held := replyFor(t, cell, true)

			// The identity whose email an account holds is never signed in, never
			// linked, never parked as a pending link, and answered with the
			// account-unavailable flash; nothing of the asserted identity is echoed.
			if held.hasAuthCookie || held.hasPendingCookie {
				t.Fatalf("an email held by an account must not sign in or park a link: auth cookie=%v pending cookie=%v", held.hasAuthCookie, held.hasPendingCookie)
			}
			if held.identities != 0 || held.created != 0 {
				t.Fatalf("an identity whose email an account holds must link no identity and create no user, got %d identities and %d users", held.identities, held.created)
			}
			if held.status != http.StatusSeeOther || held.location != "/login" {
				t.Fatalf("expected 303 to /login for the held email, got %d %q", held.status, held.location)
			}
			if want := cellFlashKeyAccountUnavailable; held.flashKey != want {
				t.Fatalf("expected the account-unavailable flash %q for the held email, got %q", want, held.flashKey)
			}
			if !strings.Contains(held.audit, `cause="link_required"`) {
				t.Fatalf("the held email's audit line must carry cause=link_required, got %q", held.audit)
			}
			assertCellReplyHoldsNoIdentity(t, held)

			if cell.provisionsNewcomers() {
				if fresh.created != 1 || held.created != 0 {
					t.Fatalf("control: the fresh identity must provision and the held email must not, got %d and %d new users", fresh.created, held.created)
				}
				return
			}
			if fresh.created != 0 || held.created != 0 {
				t.Fatalf("no callback may create a user, got %d and %d", fresh.created, held.created)
			}
			if fresh.identities != 0 {
				t.Fatalf("closed registration: no callback may link an identity, got %d", fresh.identities)
			}
			if fresh.hasAuthCookie || fresh.hasPendingCookie {
				t.Fatalf("closed registration: a never-seen identity must not sign in or park a link: auth cookie=%v pending cookie=%v", fresh.hasAuthCookie, fresh.hasPendingCookie)
			}
			assertCellReplyHoldsNoIdentity(t, fresh)
			// Everything but the flash key is already identical.
			if fresh.status != held.status || fresh.location != held.location {
				t.Fatalf("status/redirect tell the cases apart: never-seen identity %d %q, email held by an account %d %q", fresh.status, fresh.location, held.status, held.location)
			}
			if strings.Join(fresh.cookieLines, "\n") != strings.Join(held.cookieLines, "\n") {
				t.Fatalf("the Set-Cookie lines tell the cases apart (flash value masked):\n%q\nvs\n%q", fresh.cookieLines, held.cookieLines)
			}
			if fresh.headers != held.headers {
				t.Fatalf("the response headers tell the cases apart:\n%s\nvs\n%s", fresh.headers, held.headers)
			}
			if fresh.body != held.body {
				t.Fatalf("the body tells the cases apart (%d vs %d bytes)", len(fresh.body), len(held.body))
			}
			if !cell.autoProvision {
				// The reachable instance: the never-seen identity is refused by the
				// service itself, with the held email's own flash key.
				if fresh.flashKey == "" || fresh.flashKey != held.flashKey {
					t.Fatalf("the flash key tells the cases apart: never-seen identity %q, email held by an account %q", fresh.flashKey, held.flashKey)
				}
				if !strings.Contains(fresh.audit, `cause="account_unavailable"`) {
					t.Fatalf("the never-seen identity's audit line must carry cause=account_unavailable, got %q", fresh.audit)
				}
				return
			}
			// WEB-493 tripwire: this configuration is refused at boot, so the fix is
			// defence in depth. The flash key still tells the cases apart; the test
			// goes red when the oracle is fixed.
			if fresh.flashKey == held.flashKey {
				t.Fatalf("WEB-493 fixed: replace this inequality with an equality requirement and drop the two exact pins (both keys are now %q)", fresh.flashKey)
			}
			// The exact keys, as literals: a key renamed in the error mapping must
			// not pass unseen, and a difference between two unrelated failures must
			// not satisfy the tripwire above.
			if fresh.flashKey != cellFlashKeyProvisioningRefused {
				t.Fatalf("WEB-493: the never-seen identity's key is %q, expected %q. A pair of new, different keys means the oracle is still open: do not re-pin", fresh.flashKey, cellFlashKeyProvisioningRefused)
			}
			if held.flashKey != cellFlashKeyAccountUnavailable {
				t.Fatalf("WEB-493: the held email's key is %q, expected %q. A pair of new, different keys means the oracle is still open: do not re-pin", held.flashKey, cellFlashKeyAccountUnavailable)
			}
		})
	}
}
