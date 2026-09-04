package api

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Every settings step-up (local-password enrollment, erasure, OIDC identity
// linking) starts on /settings, completes on /auth/oidc/callback, and returns
// the owner to /settings. Its verdict therefore travels in the flash cookie —
// and only ONE of that cookie's channels is read on the way back:
// buildSettingsViewData feeds the view service flash.SettingsSuccess and
// flash.SettingsError, while flash.AuthError is consumed by the auth pages, which
// this redirect never reaches. Flashed on the wrong channel, a refusal renders
// nothing at all: the owner sees the same settings page a success would have
// produced, minus the toast, with no way to tell "your identity is already
// linked elsewhere" from "linked".
//
// The tests below drive each of the three step-ups into a failure arm and then
// render the page the owner actually lands on, because the flash payload alone
// cannot answer whether anything was displayed.

// renderSettingsAfterCallback replays the flash cookie a step-up callback set,
// exactly as the browser would when it follows the 303, and returns the
// rendered settings page.
func renderSettingsAfterCallback(t *testing.T, fixture *oidcStepupFixture, callbackResponse *http.Response) string {
	t.Helper()

	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected the callback to set a flash cookie carrying the refusal")
	}

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, flashCookie.Name+"="+flashCookie.Value))
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("settings render after the step-up callback = %d, want 200", response.StatusCode)
	}
	return mustReadBodyString(t, response.Body)
}

func mustEnglishMessage(t *testing.T, key string) string {
	t.Helper()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	message := manager.Messages(i18n.LangEN)[key]
	if strings.TrimSpace(message) == "" {
		t.Fatalf("the en catalogue has no copy for %q", key)
	}
	return message
}

// TestSettingsStepupRefusalsRenderOnTheSettingsPage is the regression for the
// wrong-channel flash. Each case refuses at the exchange, so the request
// reaches the same place the successful case does and diverges exactly at the
// verdict; the assertion is then made on the RENDERED page, not on the cookie,
// since a refusal the page does not read is the defect itself.
func TestSettingsStepupRefusalsRenderOnTheSettingsPage(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		slug string
		// start begins the step-up under test and returns its response, which
		// carries the sealed step-up cookie.
		start func(t *testing.T, fixture *oidcStepupFixture) *http.Response
		// refuse arms the stub so the callback lands in one failure arm.
		refuse         func(fixture *oidcStepupFixture)
		flashKey       string
		translationKey string
	}{
		{
			name: "oidc identity link, identity already claimed",
			slug: "link-claimed",
			start: func(t *testing.T, fixture *oidcStepupFixture) *http.Response {
				t.Helper()
				return postOIDCIdentityLinkStepupStart(t, fixture)
			},
			refuse: func(fixture *oidcStepupFixture) {
				fixture.oidcStub.identityLinkReauthErr = services.ErrOIDCLinkFailed
			},
			flashKey:       settingsOIDCIdentityLinkClaimedErrorSpec().Key,
			translationKey: "settings.error.oidc_identity_already_linked",
		},
		{
			name: "erasure, stale reauth",
			slug: "erasure-stale",
			start: func(t *testing.T, fixture *oidcStepupFixture) *http.Response {
				t.Helper()
				return postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
			},
			refuse: func(fixture *oidcStepupFixture) {
				fixture.oidcStub.reauthErr = services.ErrOIDCReauthStale
			},
			flashKey:       settingsOIDCReauthStaleErrorSpec().Key,
			translationKey: "settings.error.oidc_reauth_stale",
		},
		{
			name: "local password enrollment, identity mismatch",
			slug: "password-mismatch",
			start: func(t *testing.T, fixture *oidcStepupFixture) *http.Response {
				t.Helper()
				return fixture.postStart(t, "EvenStronger2", "EvenStronger2")
			},
			refuse: func(fixture *oidcStepupFixture) {
				fixture.oidcStub.reauthErr = services.ErrOIDCReauthIdentityMismatch
			},
			flashKey:       settingsOIDCReauthMismatchErrorSpec().Key,
			translationKey: "settings.error.oidc_reauth_mismatch",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCStepupFixture(t, "settings-stepup-refusal-"+testCase.slug+"@example.com")

			startResponse := testCase.start(t, fixture)
			defer func() { _ = startResponse.Body.Close() }()
			stepupCookie := readStepupCookie(t, startResponse)
			state := extractStepupCallbackState(t, fixture)

			testCase.refuse(fixture)

			callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
			defer func() { _ = callbackResponse.Body.Close() }()
			if callbackResponse.StatusCode != http.StatusSeeOther {
				t.Fatalf("refused step-up callback = %d, want 303 back to /settings", callbackResponse.StatusCode)
			}
			if payload := decodeFlashCookieForTest(t, responseCookie(callbackResponse.Cookies(), flashCookieName).Value); payload.SettingsError != testCase.flashKey {
				t.Fatalf("expected refusal %q on the settings flash channel, got %q (auth channel holds %q)", testCase.flashKey, payload.SettingsError, payload.AuthError)
			}

			body := renderSettingsAfterCallback(t, fixture, callbackResponse)

			if !strings.Contains(body, `data-flash-key="`+testCase.translationKey+`"`) {
				t.Fatalf("the settings page carries no error banner for the refused step-up: expected the stable key %q in the rendered page", testCase.translationKey)
			}
			if message := mustEnglishMessage(t, testCase.translationKey); !strings.Contains(body, message) {
				t.Fatalf("the refusal banner rendered without its copy: expected %q in the rendered page", message)
			}
			// No separate "and no success toast" assertion: resolveSettingsStatusKeys
			// resolves the success flash first and returns before it looks at the
			// error one, so a refusal that reached the page as a success would leave
			// no error banner and fail the check above. It could not fail on its own.
		})
	}
}

// stepupCompletionHandlers are the three functions CompleteOIDCLogin dispatches
// to when the callback carries a step-up cookie. Both source-derived guards
// below read exactly these bodies: this is where a settings step-up decides what
// the owner sees on the way back from the identity provider.
var stepupCompletionHandlers = map[string]string{
	"completeLocalPasswordSetupReauth": "handlers_settings_password.go",
	"completeErasureStepupReauth":      "handlers_settings_danger_stepup.go",
	"completeOIDCIdentityLinkStepup":   "handlers_settings_oidc_link.go",
}

// inlineStepupRefusalSpecs names every *ErrorSpec constructor the three handlers
// above call inline, mapped to the constructor itself. A test cannot call a
// function by a name it read out of a file, so this half is a lookup table — but
// it is not a free-standing list: the names ARE read from the sources and the
// two sets compared in both directions, so a handler that starts raising a new
// spec fails by name until it is listed here, and an entry no handler names
// fails as a stale one. Regression:
// TestStepupCallbackInlineRefusalSpecsMatchTheHandlerSources.
var inlineStepupRefusalSpecs = map[string]func() APIErrorSpec{
	"authOIDCAuthenticationFailedErrorSpec":        authOIDCAuthenticationFailedErrorSpec,
	"authOIDCUnavailableErrorSpec":                 authOIDCUnavailableErrorSpec,
	"authRecoveryCodePersistErrorSpec":             authRecoveryCodePersistErrorSpec,
	"settingsOIDCReauthMismatchErrorSpec":          settingsOIDCReauthMismatchErrorSpec,
	"settingsErasureNeedsAccountPasswordErrorSpec": settingsErasureNeedsAccountPasswordErrorSpec,
}

// settingsStepupRefusalSpecs collects every spec the three step-up completion
// handlers can flash. Two of its three parts are derived and one is not, and
// saying which is which is the point of this comment — the earlier wording
// claimed the whole set was derived, and that is precisely why nobody noticed
// that the enrollment callback's commit arm answered through
// respondPasswordChangeError and contributed no specs here at all.
//
//   - The mapper arms are derived: the same mappers the handlers call, fed the
//     same sentinels, so a mapper that starts returning a different spec changes
//     this set with nothing re-typed. mapSettingsPasswordChangeError joined them
//     when that commit arm was routed to /settings.
//   - The inline specs are derived by NAME from the handler sources, through
//     inlineStepupRefusalSpecs above.
//   - The specs raised inside the HELPERS those handlers call are hand-listed
//     below, each against the helper that raises it. Nothing derives this part: a
//     new spec inside applyClearData, applyDeleteAccount or
//     refreshCurrentSession has to be added here by hand.
func settingsStepupRefusalSpecs() []APIErrorSpec {
	foreign := errors.New("some provider failure the mappers do not recognize")

	specs := []APIErrorSpec{
		// applyClearData / applyDeleteAccount, once the re-auth passed but the
		// mutation itself failed.
		settingsClearDataErrorSpec(),
		settingsDeleteAccountErrorSpec(),
		// refreshCurrentSession, re-issuing this device's cookie after the
		// operation bumped auth_session_version. Both reach the callback only
		// because applyClearData hands its verdict back;
		// TestApplyClearDataReportsARefusedSessionReissueToItsCaller is what
		// keeps that true.
		authSessionCreateErrorSpec(),
		authWebSignInUnavailableErrorSpec(),
	}
	for _, construct := range inlineStepupRefusalSpecs {
		specs = append(specs, construct())
	}
	for _, err := range []error{
		services.ErrOIDCReauthStale,
		services.ErrOIDCLinkFailed,
		services.ErrOIDCDisabled,
		services.ErrOIDCUnavailable,
		services.ErrOIDCIdentityResolveFailed,
		foreign,
	} {
		specs = append(specs, mapOIDCIdentityLinkReauthError(err))
	}
	for _, err := range []error{
		services.ErrOIDCReauthStale,
		services.ErrOIDCReauthIdentityMismatch,
		services.ErrOIDCDisabled,
		services.ErrOIDCUnavailable,
		foreign,
	} {
		specs = append(specs, mapLocalPasswordSetupReauthError(err))
	}
	// The commit arm of completeLocalPasswordSetupReauth: the three sentinels
	// FinalizeLocalPasswordSetup can raise, plus an unrecognized error for the
	// mapper's default. Everything else mapSettingsPasswordChangeError handles is
	// raised by PrepareLocalPasswordHash on the form's own route, which answers
	// as a settings form and never reaches /auth/oidc/callback.
	for _, err := range []error{
		services.ErrSettingsPasswordChangeInvalidInput,
		services.ErrSettingsRecoveryCodeGenerateFailed,
		services.ErrSettingsPasswordUpdateFailed,
		foreign,
	} {
		specs = append(specs, mapSettingsPasswordChangeError(err))
	}
	return specs
}

// TestEverySettingsStepupRefusalKeyMapsToLocalizedCopy is the other half of the
// regression above, which can only drive three arms. Reaching the settings page
// is not enough: resolveSettingsStatusKeys looks the flashed key up in
// services.authErrorTranslationKeys and renders NOTHING when the lookup misses,
// so an unmapped key is the same blank page as the wrong flash channel. This
// walks every spec those handlers can flash and requires copy in every locale —
// the sibling of TestEveryTransportErrorKeyRendersLocalizedCopyInEveryLocale,
// for the step-up surface.
func TestEverySettingsStepupRefusalKeyMapsToLocalizedCopy(t *testing.T) {
	t.Parallel()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("expected the i18n manager to report supported languages")
	}

	for _, spec := range settingsStepupRefusalSpecs() {
		t.Run(spec.Key, func(t *testing.T) {
			translationKey := services.AuthErrorTranslationKey(spec.Key)
			if translationKey == "" {
				t.Fatalf("step-up refusal %q has no entry in services.authErrorTranslationKeys: the settings page renders an empty banner and the owner cannot tell the refusal from a success", spec.Key)
			}
			for _, language := range languages {
				if message := manager.Messages(language)[translationKey]; strings.TrimSpace(message) == "" {
					t.Errorf("locale %q has no copy for %q (mapped from step-up refusal %q)", language, translationKey, spec.Key)
				}
			}
		})
	}
}

// allowedStepupCompletionTerminals are the expressions a step-up completion
// handler may return. Each is a way back to a PAGE the browser returning from
// the identity provider can read; anything else answers that navigation with a
// JSON envelope or hands it to the top-level ErrorHandler.
//
//   - handler.redirectSettingsRefusal — the refusal channel, flash + 303.
//   - c.Redirect.Status.To — the plain redirects: /settings after a success or a
//     flow that finished elsewhere, /login after the account was deleted.
//   - handler.renderRecoveryCodeResponseWithContinuePath — the one arm that
//     renders rather than redirects, because the fresh recovery code is shown
//     once and cannot survive a redirect.
//   - respondOIDCSameOriginHandoff — a document whose only content is a
//     meta-refresh to a page on this origin. It is how the enrollment callback
//     reaches the recovery-code reveal: that surface claims the account's
//     one-time reveal mark and only a same-origin initiator may spend it, while
//     Sec-Fetch-Site is computed over the whole redirect chain, which a
//     provider callback starts off-origin. A 303 from here is refused there.
var allowedStepupCompletionTerminals = map[string]string{
	"handler.redirectSettingsRefusal":                    "the refusal channel the settings page reads",
	"c.Redirect.Status.To":                               "a plain redirect to a page",
	"handler.renderRecoveryCodeResponseWithContinuePath": "renders the one-time recovery code",
	"respondOIDCSameOriginHandoff":                       "a same-origin document that navigates to a page",
}

// stepupCompletionReturnPath renders a return expression as its dotted call
// chain — handler.redirectSettingsRefusal(c, spec) becomes
// "handler.redirectSettingsRefusal", c.Redirect().Status(x).To(y) becomes
// "c.Redirect.Status.To", and a bare identifier (return err) becomes its own
// name. Built from the AST rather than from the source text so a comment beside
// the call cannot change the verdict.
func stepupCompletionReturnPath(expression ast.Expr) string {
	switch node := expression.(type) {
	case *ast.CallExpr:
		return stepupCompletionReturnPath(node.Fun)
	case *ast.SelectorExpr:
		receiver := stepupCompletionReturnPath(node.X)
		if receiver == "" {
			return node.Sel.Name
		}
		return receiver + "." + node.Sel.Name
	case *ast.Ident:
		return node.Name
	default:
		return ""
	}
}

// parseStepupCompletionHandlers returns the AST body of each function named in
// stepupCompletionHandlers, failing if one has been renamed or moved — a guard
// that silently scans nothing is worse than no guard.
func parseStepupCompletionHandlers(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()

	bodies := map[string]*ast.FuncDecl{}
	for name, path := range stepupCompletionHandlers {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Name.Name != name {
				continue
			}
			bodies[name] = function
		}
		if bodies[name] == nil {
			t.Fatalf("%s declares no function %s: stepupCompletionHandlers is stale and this guard is scanning nothing", path, name)
		}
	}
	return bodies
}

// TestEveryStepupCallbackRefusalLeavesThroughTheSettingsRedirect is the barrier
// for the class the render test above can only sample. A step-up completion
// handler runs on /auth/oidc/callback, where isHTMX and acceptsJSON are both
// false and no path prefix matches /api/v1/users/current — so respondSettingsError
// falls through to apiError and respondMappedError's global arm goes straight
// there. Either way the reply to a browser navigation returning from the
// provider is a JSON error object rendered as the page.
//
// That is not a property of one arm, so it is not asserted arm by arm: four
// re-auth refusals in completeLocalPasswordSetupReauth went through
// redirectSettingsRefusal while the two commit arms right after them still
// answered through respondPasswordChangeError and a bare `return err`, and every
// test in the file passed. The exits are read out of the sources instead, and
// each one has to be a way back to a page.
func TestEveryStepupCallbackRefusalLeavesThroughTheSettingsRedirect(t *testing.T) {
	t.Parallel()

	for name, function := range parseStepupCompletionHandlers(t) {
		t.Run(name, func(t *testing.T) {
			returns := 0
			ast.Inspect(function.Body, func(node ast.Node) bool {
				statement, ok := node.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				returns++
				if len(statement.Results) != 1 {
					t.Errorf("%s returns %d values: a fiber handler returns exactly one error", name, len(statement.Results))
					return true
				}
				path := stepupCompletionReturnPath(statement.Results[0])
				if _, allowed := allowedStepupCompletionTerminals[path]; !allowed {
					t.Errorf(
						"%s (%s) returns %q, which is not a way back to a page. On /auth/oidc/callback that reply is a JSON envelope or fiber's ErrorHandler, shown to a browser returning from the identity provider. Map the failure to a spec and flash it with handler.redirectSettingsRefusal; add a terminal to allowedStepupCompletionTerminals only if it renders or redirects to a page itself.",
						name, stepupCompletionHandlers[name], path,
					)
				}
				return true
			})
			if returns == 0 {
				t.Fatalf("%s has no return statements: the scan found nothing to check", name)
			}
		})
	}
}

// TestStepupCallbackInlineRefusalSpecsMatchTheHandlerSources keeps the
// hand-typed half of settingsStepupRefusalSpecs from falling behind the
// handlers. The terminal guard above proves every exit goes through the flash
// redirect; it cannot see WHICH spec rides it, so a newly raised spec would
// reach the settings page without ever being checked for localized copy. The
// names come from the sources, and the comparison runs both ways so the map can
// neither miss a spec nor keep one nothing raises.
func TestStepupCallbackInlineRefusalSpecsMatchTheHandlerSources(t *testing.T) {
	t.Parallel()

	named := map[string]bool{}
	for name, function := range parseStepupCompletionHandlers(t) {
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if !ok || !strings.HasSuffix(identifier.Name, "ErrorSpec") {
				return true
			}
			named[identifier.Name] = true
			if _, listed := inlineStepupRefusalSpecs[identifier.Name]; !listed {
				t.Errorf(
					"%s (%s) raises %s(), which inlineStepupRefusalSpecs does not list: its key is never checked for an entry in services.authErrorTranslationKeys, so it can reach /settings as an empty banner. Add it there.",
					name, stepupCompletionHandlers[name], identifier.Name,
				)
			}
			return true
		})
	}
	for name := range inlineStepupRefusalSpecs {
		if !named[name] {
			t.Errorf("inlineStepupRefusalSpecs lists %s(), which no step-up completion handler names any more: drop it, or the list oversells what it derives", name)
		}
	}
}

// TestLocalPasswordSetupCommitFailureRendersOnTheSettingsPage drives the arm the
// render test above could not reach: the step-up succeeds, the owner comes back
// from the provider, and the WRITE that enrolls the password fails. Before this
// PR that arm answered through respondPasswordChangeError, which on
// /auth/oidc/callback renders the JSON error envelope as the page.
//
// The write is failed at the database, not by stubbing the service, so the
// request travels the whole handler and diverges exactly where a real failure
// would. The assertion is made on the RENDERED settings page for the same reason
// as the sibling above: a refusal the page does not read is the defect itself.
func TestLocalPasswordSetupCommitFailureRendersOnTheSettingsPage(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-commit-failure@example.com")

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	// Refuse only UPDATEs on users: the callback still authenticates through the
	// same table and still validates the exchange, so the single thing that
	// changes is FinalizeLocalPasswordSetup's write.
	if err := fixture.database.Exec(
		`CREATE TRIGGER refuse_user_updates BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'forced write failure'); END;`,
	).Error; err != nil {
		t.Fatalf("arm the failing write: %v", err)
	}

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if err := fixture.database.Exec(`DROP TRIGGER refuse_user_updates;`).Error; err != nil {
		t.Fatalf("disarm the failing write: %v", err)
	}

	if callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback after a failed commit = %d, want 303 back to /settings", callbackResponse.StatusCode)
	}
	if location := callbackResponse.Header.Get("Location"); location != "/settings" {
		t.Fatalf("callback after a failed commit redirected to %q, want /settings", location)
	}
	wantKey := mapSettingsPasswordChangeError(services.ErrSettingsPasswordUpdateFailed).Key
	flashCookie := responseCookie(callbackResponse.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected the callback to set a flash cookie carrying the refusal")
	}
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.SettingsError != wantKey {
		t.Fatalf("expected refusal %q on the settings flash channel, got %q (auth channel holds %q)", wantKey, payload.SettingsError, payload.AuthError)
	}

	// The account is untouched: the write that failed is the one that would have
	// enrolled the password, so a refusal that still flipped local auth would be
	// a worse defect than the blank page.
	if persisted := reloadStepupUser(t, fixture); persisted.LocalAuthEnabled {
		t.Fatal("the failed commit left local auth enabled")
	}

	body := renderSettingsAfterCallback(t, fixture, callbackResponse)
	translationKey := services.AuthErrorTranslationKey(wantKey)
	if translationKey == "" {
		t.Fatalf("refusal %q has no entry in services.authErrorTranslationKeys", wantKey)
	}
	if !strings.Contains(body, `data-flash-key="`+translationKey+`"`) {
		t.Fatalf("the settings page carries no error banner for the failed commit: expected the stable key %q in the rendered page", translationKey)
	}
	if message := mustEnglishMessage(t, translationKey); !strings.Contains(body, message) {
		t.Fatalf("the refusal banner rendered without its copy: expected %q in the rendered page", message)
	}
}

// clearDataProbeVerdict is what applyClearData hands back to its caller: the
// spec key, and whether the operation may be reported as done.
type clearDataProbeVerdict struct {
	OK  bool   `json:"ok"`
	Key string `json:"key"`
}

// probeApplyClearData calls applyClearData on a real request context and reports
// the (spec, ok) pair, because that pair — not the response body — is what
// completeErasureStepupReauth branches on.
//
// It runs on a route of its own rather than through the erasure callback: the
// refusal below is provoked through the ROLE gate inside setAuthCookie, and no
// registered route can carry a non-owner session into a handler (AuthRequired
// and OwnerOnly resolve an owner first). The status is stamped explicitly so a
// callee that already wrote a response of its own cannot make this probe fail on
// the status instead of on the verdict under test.
func probeApplyClearData(t *testing.T, handler *Handler, user *models.User) clearDataProbeVerdict {
	t.Helper()

	app := fiber.New()
	app.Post("/__probe/clear-data", func(c fiber.Ctx) error {
		spec, ok := handler.applyClearData(c, user)
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"ok": ok, "key": spec.Key})
	})

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodPost, "/__probe/clear-data", nil))
	defer func() { _ = response.Body.Close() }()

	verdict := clearDataProbeVerdict{}
	if err := json.Unmarshal([]byte(mustReadBodyString(t, response.Body)), &verdict); err != nil {
		t.Fatalf("decode the applyClearData verdict: %v", err)
	}
	return verdict
}

// TestApplyClearDataReportsARefusedSessionReissueToItsCaller pins the seam the
// terminal guard above cannot see. That guard proves every exit of
// completeErasureStepupReauth leads back to a page; it cannot prove the erasure
// branch ever TAKES the refusal exit, and for the session re-issue it did not.
//
// The re-issue used to run through a wrapper that answered with
// respondMappedError and returned its nil — nil on the refusal path exactly as
// on the success path — so the `if err != nil` guard in applyClearData could
// never fire. The wipe completed, auth_session_version was bumped, this device's
// cookie was NOT re-issued past the bump, and applyClearData still answered
// ok=true: the callback flashed `data_cleared` and redirected onto an
// already-written response, telling the owner the wipe succeeded on a session
// that dies at the next request.
//
// The assertion is therefore made on the pair, not on a written response: a
// response was written in the defective version too, which is precisely why
// nothing noticed. The refusal is provoked through the role gate
// (services.ValidateSupportedWebUser, checked inside setAuthCookie before
// anything is sealed) rather than by breaking the AEAD seal, so the arm is
// reached by a row a test can create. The success case beside it is the anchor:
// same probe, same wipe, opposite verdict.
func TestApplyClearDataReportsARefusedSessionReissueToItsCaller(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		slug    string
		role    string
		wantOK  bool
		wantKey string
	}{
		{
			name: "the session re-issue is refused",
			slug: "refused",
			// The one non-owner value the users table's CHECK constraint still
			// accepts, which is what makes this arm reachable from a row rather
			// than only from a fault injected into the codec.
			role:    "partner",
			wantOK:  false,
			wantKey: authWebSignInUnavailableErrorSpec().Key,
		},
		{
			name:   "the session is re-issued",
			slug:   "reissued",
			role:   models.RoleOwner,
			wantOK: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stub := newStubOIDCWorkflowService(true)
			_, database, handler := newSettingsMutationStepupApp(t, stub)

			user := models.User{
				Email:               "clear-data-reissue-" + testCase.slug + "@example.com",
				LocalAuthEnabled:    false,
				Role:                testCase.role,
				OnboardingCompleted: true,
				AuthSessionVersion:  1,
				CycleLength:         28,
				PeriodLength:        5,
				AutoPeriodFill:      true,
				CreatedAt:           time.Now().UTC(),
			}
			if err := database.Create(&user).Error; err != nil {
				t.Fatalf("create the account under test: %v", err)
			}
			entry := models.DailyLog{UserID: user.ID, Date: time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatalf("seed a day entry for the wipe to remove: %v", err)
			}

			verdict := probeApplyClearData(t, handler, &user)

			if verdict.OK != testCase.wantOK {
				t.Fatalf(
					"applyClearData reported ok=%t, want %t: the caller branches on this pair, and a refused session re-issue reported as success flashes data_cleared at an owner whose cookie was never re-issued past the auth_session_version bump",
					verdict.OK, testCase.wantOK,
				)
			}
			if verdict.Key != testCase.wantKey {
				t.Fatalf(
					"applyClearData handed back spec key %q, want %q: the spec has to be the one the session re-issue chose, since that is the copy the settings page renders",
					verdict.Key, testCase.wantKey,
				)
			}

			// Anti-vacuity: the wipe itself must have happened in both cases, so
			// a refusal above can only have come from the session re-issue and
			// not from ClearAllData failing earlier.
			var remaining int64
			if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&remaining).Error; err != nil {
				t.Fatalf("count day entries after the wipe: %v", err)
			}
			if remaining != 0 {
				t.Fatalf("day entries after applyClearData = %d, want 0: the probe never reached the session re-issue", remaining)
			}
		})
	}
}
