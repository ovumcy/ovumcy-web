package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
// matrix. oidc false is the `local` login mode: OIDC is switched off.
type loginRegistrationCell struct {
	name         string
	oidc         bool
	loginMode    security.OIDCLoginMode
	registration services.RegistrationMode
}

// localPublicAuth states what the cell's login mode lets an anonymous visitor
// do with a password; the expectation is written here, not read back from the
// code under test.
func (cell loginRegistrationCell) localPublicAuth() bool {
	return !(cell.oidc && cell.loginMode == security.OIDCLoginModeOIDCOnly)
}

// localRegistrationPermitted is the one condition under which an anonymous
// visitor may create an account with a password.
func (cell loginRegistrationCell) localRegistrationPermitted() bool {
	return cell.localPublicAuth() && cell.registration == services.RegistrationModeOpen
}

func loginRegistrationCells() []loginRegistrationCell {
	var cells []loginRegistrationCell
	for _, registration := range []services.RegistrationMode{services.RegistrationModeOpen, services.RegistrationModeClosed} {
		cells = append(cells,
			loginRegistrationCell{name: "local x " + string(registration), registration: registration},
			loginRegistrationCell{name: "hybrid x " + string(registration), oidc: true, loginMode: security.OIDCLoginModeHybrid, registration: registration},
			loginRegistrationCell{name: "oidc_only x " + string(registration), oidc: true, loginMode: security.OIDCLoginModeOIDCOnly, registration: registration},
		)
	}
	return cells
}

const (
	cellIdentityIssuer  = "https://id.example.com"
	cellIdentitySubject = "never-seen-subject"
	cellIdentityEmail   = "sso-newcomer@example.com"
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

func newLoginRegistrationCellApp(t *testing.T, cell loginRegistrationCell, claims security.OIDCClaims) (*fiber.App, *gorm.DB, *cellOIDCProvider) {
	t.Helper()

	provider := &cellOIDCProvider{
		config: security.OIDCConfig{
			Enabled:       cell.oidc,
			LoginMode:     cell.loginMode,
			AutoProvision: true,
			IssuerURL:     testOIDCIssuerURL,
		},
		claims: claims,
	}
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure:     true,
		registrationMode: cell.registration,
		oidcServiceFromDeps: func(dependencies Dependencies, repositories *db.Repositories) OIDCWorkflowService {
			provisioner, ok := dependencies.RegistrationService.(services.OIDCAutoProvisioner)
			if !ok {
				t.Fatalf("the composition root's registration service %T does not provision OIDC owners", dependencies.RegistrationService)
			}
			return services.NewOIDCLoginService(provider, repositories.OIDCIdentities, repositories.Users, provisioner)
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

// TestOIDCCallbackForANeverSeenIdentityHonoursTheRegistrationModeAtEveryCell
// drives the callback with OIDC_AUTO_PROVISION on. Where registration is open
// the identity is provisioned (the positive control); where it is closed the
// callback creates no user row and links no identity. Where OIDC is off no
// sign-in starts at all. The closed zero is read as a refusal at the
// registration check by the open cells beside it: the same request, identity
// and provider double provision there.
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
				if got := countUsers(t, database); got != 0 {
					t.Fatalf("OIDC off must create no user, got %d", got)
				}
				return
			}
			if !reply.started {
				t.Fatalf("anchor: OIDC is on, the start route must hand out a state (status %d)", reply.response.StatusCode)
			}

			response := reply.response
			authCookie := responseCookie(response.Cookies(), authCookieName)
			var linked int64
			if err := database.Model(&models.OIDCIdentity{}).Count(&linked).Error; err != nil {
				t.Fatalf("count identities: %v", err)
			}
			users := countUsers(t, database)

			if cell.registration == services.RegistrationModeOpen {
				assertStatusCode(t, response, http.StatusSeeOther)
				if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
					t.Fatal("open registration: expected the provisioned owner to be signed in")
				}
				if users != 1 || linked != 1 {
					t.Fatalf("open registration: expected one user and one linked identity, got %d and %d", users, linked)
				}
				return
			}

			assertStatusCode(t, response, http.StatusSeeOther)
			if location := response.Header.Get("Location"); location != "/login" {
				t.Fatalf("closed registration: expected redirect to /login, got %q", location)
			}
			if authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
				t.Fatal("closed registration: no session may be issued for a never-seen identity")
			}
			if users != 0 || linked != 0 {
				t.Fatalf("closed registration: the callback must create no user and link no identity, got %d users and %d identities", users, linked)
			}
			if cellReplyFlashKey(t, response) == "" {
				t.Fatal("closed registration: expected the refusal flash on the exempt channel (the zero rows come from the registration check, not a dropped request)")
			}
		})
	}
}

// TestOIDCCallbackDoesNotTellANeverSeenIdentityFromOneWhoseEmailHasAnAccount
// holds the closed-registration cells to the no-account-existence-leak rule:
// a visitor whose identity is new gets the same reply whether or not a local
// account already holds the asserted email. The open cells are the control
// that the fixture tells the two apart at all (one provisions, the other is
// refused).
func TestOIDCCallbackDoesNotTellANeverSeenIdentityFromOneWhoseEmailHasAnAccount(t *testing.T) {
	t.Parallel()

	replyFor := func(t *testing.T, cell loginRegistrationCell, accountHoldsEmail bool) (*http.Response, string, int64) {
		t.Helper()
		app, database, provider := newLoginRegistrationCellApp(t, cell, freshCellIdentityClaims(cellIdentityEmail))
		var seeded int64
		if accountHoldsEmail {
			createOnboardingTestUser(t, database, cellIdentityEmail, "StrongPass1", true)
			seeded = 1
		}
		reply := runCellOIDCCallback(t, app, provider)
		if !reply.started {
			t.Fatalf("anchor: the sign-in did not start (status %d)", reply.response.StatusCode)
		}
		return reply.response, cellReplyFlashKey(t, reply.response), countUsers(t, database) - seeded
	}

	for _, cell := range loginRegistrationCells() {
		if !cell.oidc {
			continue
		}
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()

			fresh, freshKey, freshCreated := replyFor(t, cell, false)
			held, heldKey, heldCreated := replyFor(t, cell, true)

			if cell.registration == services.RegistrationModeOpen {
				if freshCreated != 1 || heldCreated != 0 {
					t.Fatalf("control: the fresh identity must provision and the held email must not, got %d and %d new users", freshCreated, heldCreated)
				}
				return
			}
			if freshCreated != 0 || heldCreated != 0 {
				t.Fatalf("closed registration: no callback may create a user, got %d and %d", freshCreated, heldCreated)
			}
			if fresh.StatusCode != held.StatusCode {
				t.Fatalf("status tells the cases apart: never-seen identity %d, email held by an account %d", fresh.StatusCode, held.StatusCode)
			}
			if fresh.Header.Get("Location") != held.Header.Get("Location") {
				t.Fatalf("redirect tells the cases apart: %q vs %q", fresh.Header.Get("Location"), held.Header.Get("Location"))
			}
			if freshKey == "" || freshKey != heldKey {
				t.Fatalf("flash key tells the cases apart: never-seen identity %q, email held by an account %q", freshKey, heldKey)
			}
		})
	}
}
