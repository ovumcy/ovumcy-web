package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
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
			// The other freshness verdict, driven end to end because its whole
			// point is that the owner reads something different: the stale
			// sentence asks for a retry, and on a provider that never sends
			// auth_time the retry lands here again.
			name: "erasure, provider never dated the sign-in",
			slug: "erasure-auth-time-missing",
			start: func(t *testing.T, fixture *oidcStepupFixture) *http.Response {
				t.Helper()
				return postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
			},
			refuse: func(fixture *oidcStepupFixture) {
				fixture.oidcStub.reauthErr = services.ErrOIDCReauthAuthTimeMissing
			},
			flashKey:       settingsOIDCReauthAuthTimeMissingErrorSpec().Key,
			translationKey: "settings.error.oidc_reauth_auth_time_missing",
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

// stepupCompletionHandlers are the functions a step-up callback can leave
// through on the way back from the identity provider. Both source-derived
// guards below read exactly these bodies: this is where a settings step-up
// decides what the owner sees.
//
// The last three are the cross-site bounce, and they belong here for the same
// reason the per-purpose handlers do: they run on a provider-return
// navigation, so a `return err` in any of them reaches a browser as raw JSON —
// the regression this guard exists for. Listing only the per-purpose three
// would fix the class at three sites out of six.
var stepupCompletionHandlers = map[string]string{
	"completeLocalPasswordSetupReauth": "handlers_settings_password.go",
	"completeErasureStepupReauth":      "handlers_settings_danger_stepup.go",
	"completeOIDCIdentityLinkStepup":   "handlers_settings_oidc_link.go",
	"bounceStepupToSameSiteContinue":   "oidc_stepup_continuation.go",
	"dispatchStepupCompletion":         "oidc_stepup_continuation.go",
	"ContinueOIDCStepup":               "oidc_stepup_continuation.go",
}

// stepupSessionHelpers names the session-affecting helpers the completion
// handlers above delegate into: applyClearData and applyDeleteAccount share
// their session re-issue with refreshCurrentSession, which every other posture
// change also calls, and the identity-link completion re-issues through
// reissueSessionAfterIdentityChange. Scanned separately from
// stepupCompletionHandlers because these helpers return a spec and a verdict,
// not a single error, so they cannot share
// TestEveryStepupCallbackRefusalLeavesThroughTheSettingsRedirect's
// single-return-value shape check — they get their own derivation instead,
// below.
var stepupSessionHelpers = map[string]string{
	"applyClearData":                    "handlers_settings_danger_stepup.go",
	"applyDeleteAccount":                "handlers_settings_danger_stepup.go",
	"refreshCurrentSession":             "handlers_auth_token_helpers.go",
	"reissueSessionAfterIdentityChange": "handlers_settings_oidc_link.go",
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
	"settingsOIDCReauthMismatchErrorSpec":          settingsOIDCReauthMismatchErrorSpec,
	"settingsErasureNeedsAccountPasswordErrorSpec": settingsErasureNeedsAccountPasswordErrorSpec,
}

// inlineStepupSessionHelperSpecs is inlineStepupRefusalSpecs's counterpart for
// the stepupSessionHelpers: every *ErrorSpec constructor those helpers call
// inline, read from the
// sources and cross-checked in both directions by
// TestStepupSessionHelperRefusalSpecsMatchTheHelperSources. Before this guard
// the set was a hand-typed block inside settingsStepupRefusalSpecs, and
// nothing checked the hand-typed list against the helpers it claimed to
// describe.
var inlineStepupSessionHelperSpecs = map[string]func() APIErrorSpec{
	"settingsClearDataErrorSpec":                    settingsClearDataErrorSpec,
	"settingsDeleteAccountErrorSpec":                settingsDeleteAccountErrorSpec,
	"authSessionCreateErrorSpec":                    authSessionCreateErrorSpec,
	"authWebSignInUnavailableErrorSpec":             authWebSignInUnavailableErrorSpec,
	"settingsDataClearedSignInAgainErrorSpec":       settingsDataClearedSignInAgainErrorSpec,
	"authIdentityChangeAppliedSignInAgainErrorSpec": authIdentityChangeAppliedSignInAgainErrorSpec,
}

// settingsStepupRefusalSpecs collects every spec the three step-up completion
// handlers can flash. All three parts are derived now; the earlier wording
// claimed the whole set was derived while one part was still hand-typed, and
// that gap is precisely why nobody noticed that the enrollment callback's
// commit arm answered through respondPasswordChangeError and contributed no
// specs here at all — a derivation claim no test enforces is not a
// derivation.
//
//   - The mapper arms are derived: the same mappers the handlers call, fed the
//     same sentinels, so a mapper that starts returning a different spec changes
//     this set with nothing re-typed. mapSettingsPasswordChangeError joined them
//     when that commit arm was routed to /settings.
//   - The inline specs the three completion handlers raise directly are
//     derived by NAME from the handler sources, through inlineStepupRefusalSpecs
//     above, cross-checked by TestStepupCallbackInlineRefusalSpecsMatchTheHandlerSources.
//   - The specs raised inside the HELPERS those handlers call — applyClearData,
//     applyDeleteAccount, refreshCurrentSession, reissueSessionAfterIdentityChange —
//     are derived the same way, through inlineStepupSessionHelperSpecs,
//     cross-checked by TestStepupSessionHelperRefusalSpecsMatchTheHelperSources.
//     A new spec raised inside any of them now fails that guard by name instead of
//     silently missing this list the way the former hand-typed block could.
func settingsStepupRefusalSpecs() []APIErrorSpec {
	foreign := errors.New("some provider failure the mappers do not recognize")

	specs := []APIErrorSpec{}
	// applyClearData / applyDeleteAccount, once the re-auth passed but the
	// mutation itself failed, and refreshCurrentSession re-issuing this
	// device's cookie after the operation bumped auth_session_version. Both
	// of refreshCurrentSession's specs reach the callback only because
	// applyClearData hands its verdict back;
	// TestApplyClearDataReportsARefusedSessionReissueToItsCaller is what
	// keeps that true.
	for _, construct := range inlineStepupSessionHelperSpecs {
		specs = append(specs, construct())
	}
	for _, construct := range inlineStepupRefusalSpecs {
		specs = append(specs, construct())
	}
	for _, err := range []error{
		services.ErrOIDCReauthStale,
		services.ErrOIDCReauthAuthTimeMissing,
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
		services.ErrOIDCReauthAuthTimeMissing,
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
	// The delivery arm of completeLocalPasswordSetupReauth: the session or the
	// reveal could not be sealed before the enrollment committed, so it rolled
	// back (WEB-58). The mapper tells the reveal, a role and anything else apart.
	for _, err := range []error{
		fmt.Errorf("%w: %v", errRecoveryCodeRevealSeal, foreign),
		services.ErrAuthUnsupportedRole,
		foreign,
	} {
		specs = append(specs, mapRecoveryCodeDeliveryError(err))
	}
	return specs
}

// services.ErrOIDCReauthAuthTimeMissing wraps services.ErrOIDCReauthStale, so
// errors.Is answers true for the coarse sentinel on both verdicts. That is
// deliberate — a consumer knowing only the coarse one keeps refusing instead of
// dropping into a default arm — but it puts the whole distinction in the ORDER
// of the case clauses: a switch whose stale arm runs first tells an owner whose
// provider never sends auth_time to try again, which is the defect the split
// exists to remove. Today exactly two mappers match the coarse sentinel; the
// sweep below reads the shipped sources rather than naming them, so a third
// added later is covered the day it is written, with no allowlist to forget.
const (
	reauthStaleSentinelName       = "ErrOIDCReauthStale"
	reauthAuthTimeMissingSentinel = "ErrOIDCReauthAuthTimeMissing"
)

// TestEveryReauthStaleMatchIsPrecededByTheMissingAuthTimeMatch fails when any
// switch in the transport layer matches the coarse freshness sentinel without
// having matched the precise one first.
func TestEveryReauthStaleMatchIsPrecededByTheMissingAuthTimeMatch(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/api: %v", err)
	}

	var violations []string
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		parsed++
		violations = append(violations, unorderedReauthFreshnessClauses(t, name, string(source))...)
	}
	// Anchored on the count of FILES, not of matches: a tree where no switch
	// mentions the sentinel at all is reachable, while a sweep that read no
	// source is a vacuous verdict about a package nobody looked at.
	if parsed == 0 {
		t.Fatal("the sweep read no non-test Go file in internal/api — its verdict is vacuous")
	}
	if len(violations) > 0 {
		t.Fatalf("a switch matches %s before %s, so the wrapped verdict is swallowed and the owner is told to retry something that cannot succeed — put the precise case first:\n%s", reauthStaleSentinelName, reauthAuthTimeMissingSentinel, strings.Join(violations, "\n"))
	}
}

// TestReauthFreshnessClauseOrderSweepClassifiesItsOwnFixtures proves the sweep
// reports both verdicts, on sources the test owns rather than on the tree it
// judges.
func TestReauthFreshnessClauseOrderSweepClassifiesItsOwnFixtures(t *testing.T) {
	t.Parallel()

	const ordered = `package fixture

func mapIt(err error) int {
	switch {
	case errors.Is(err, services.ErrOIDCReauthAuthTimeMissing):
		return 1
	case errors.Is(err, services.ErrOIDCReauthStale):
		return 2
	}
	return 0
}
`
	const swapped = `package fixture

func mapIt(err error) int {
	switch {
	case errors.Is(err, services.ErrOIDCReauthStale):
		return 2
	case errors.Is(err, services.ErrOIDCReauthAuthTimeMissing):
		return 1
	}
	return 0
}
`
	if hits := unorderedReauthFreshnessClauses(t, "ordered.go", ordered); len(hits) != 0 {
		t.Fatalf("a switch matching the precise sentinel first must pass, got %v", hits)
	}
	if hits := unorderedReauthFreshnessClauses(t, "swapped.go", swapped); len(hits) != 1 {
		t.Fatalf("a switch matching the coarse sentinel first must report exactly one violation, got %d: %v", len(hits), hits)
	}
}

// unorderedReauthFreshnessClauses returns one entry per case clause that
// matches the coarse freshness sentinel with no earlier clause in the SAME
// switch matching the precise one.
func unorderedReauthFreshnessClauses(t *testing.T, display string, source string) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, display, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", display, err)
	}

	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		switchStmt, isSwitch := node.(*ast.SwitchStmt)
		if !isSwitch || switchStmt.Body == nil {
			return true
		}
		preciseSeen := false
		for _, statement := range switchStmt.Body.List {
			clause, isClause := statement.(*ast.CaseClause)
			if !isClause {
				continue
			}
			if clauseNamesIdentifier(clause, reauthAuthTimeMissingSentinel) {
				preciseSeen = true
				continue
			}
			if clauseNamesIdentifier(clause, reauthStaleSentinelName) && !preciseSeen {
				violations = append(violations, fmt.Sprintf("  %s:%d", display, fileSet.Position(clause.Pos()).Line))
			}
		}
		return true
	})
	return violations
}

// clauseNamesIdentifier reports whether a case clause's expressions mention the
// given identifier, qualified (services.Err…) or bare.
func clauseNamesIdentifier(clause *ast.CaseClause, name string) bool {
	found := false
	for _, expr := range clause.List {
		ast.Inspect(expr, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if isIdentifier && identifier.Name == name {
				found = true
			}
			return !found
		})
	}
	return found
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
//   - handler.redirectSignedOutRefusal — the same, for a refusal raised after
//     the auth cookie was cleared: flash on the auth channel + 303 to /login,
//     the one page that still renders for a device with no session.
//   - c.Redirect.Status.To — the plain redirects: /settings after a success or a
//     flow that finished elsewhere, /login after the account was deleted.
//   - respondOIDCSameOriginHandoff — a document whose only content is a
//     meta-refresh to a page on this origin. It is how the enrollment callback
//     reaches the recovery-code reveal: that surface claims the account's
//     one-time reveal mark and only a same-origin initiator may spend it, while
//     Sec-Fetch-Site is computed over the whole redirect chain, which a
//     provider callback starts off-origin. A 303 from here is refused there.
//   - handler.refuseOIDCStepupCallback — the same refusal channel, picking the
//     303 or that same-origin document by how the callback ARRIVED. Whether a
//     303 out of the cross-site POST still carries Lax cookies depends on
//     whether the browser judges the redirect chain or only its ends; the
//     document does not depend on the answer.
//   - handler.dispatchStepupCompletion and handler.bounceStepupToSameSiteContinue
//     — the two arms of the cross-site bounce. They are terminals only because
//     they are themselves on stepupCompletionHandlers above: whatever they
//     return is scanned by this same guard, so admitting them delegates the
//     check rather than skipping it.
var allowedStepupCompletionTerminals = map[string]string{
	"handler.redirectSettingsRefusal":          "the refusal channel the settings page reads",
	"handler.redirectSignedOutRefusal":         "the refusal channel the sign-in page reads, once the cookie is gone",
	"handler.refuseOIDCStepupCallback":         "the refusal channel, by the route the arrival can carry",
	"c.Redirect.Status.To":                     "a plain redirect to a page",
	"respondOIDCSameOriginHandoff":             "a same-origin document that navigates to a page",
	"handler.dispatchStepupCompletion":         "dispatches to a handler this guard also scans",
	"handler.bounceStepupToSameSiteContinue":   "bounces to the same-site leg this guard also scans",
	"handler.completeLocalPasswordSetupReauth": "a per-purpose completion this guard also scans",
	"handler.completeErasureStepupReauth":      "a per-purpose completion this guard also scans",
	"handler.completeOIDCIdentityLinkStepup":   "a per-purpose completion this guard also scans",
}

// stepupCompletionDelegates are the terminals above that are admitted ONLY
// because they are themselves scanned. Pinned here so the delegation cannot
// rot into an exemption: dropping a name from stepupCompletionHandlers while
// leaving it in the terminal list would silently stop checking it.
var stepupCompletionDelegates = []string{
	"handler.dispatchStepupCompletion",
	"handler.bounceStepupToSameSiteContinue",
	"handler.completeLocalPasswordSetupReauth",
	"handler.completeErasureStepupReauth",
	"handler.completeOIDCIdentityLinkStepup",
}

func TestEveryDelegatingStepupTerminalIsItselfScanned(t *testing.T) {
	t.Parallel()

	for _, terminal := range stepupCompletionDelegates {
		if _, allowed := allowedStepupCompletionTerminals[terminal]; !allowed {
			t.Errorf("%s is listed as a delegate but is not an allowed terminal", terminal)
		}
		name := terminal[strings.LastIndex(terminal, ".")+1:]
		if _, scanned := stepupCompletionHandlers[name]; !scanned {
			t.Errorf(
				"%s is admitted as a terminal only because this guard scans it, but %s is not in stepupCompletionHandlers — the delegation is now an exemption",
				terminal, name)
		}
	}
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

// parseNamedFunctionBodies returns the AST body of each function named in
// sources (function name -> declaring file), failing if one has been renamed
// or moved — a guard that silently scans nothing is worse than no guard.
func parseNamedFunctionBodies(t *testing.T, sources map[string]string) map[string]*ast.FuncDecl {
	t.Helper()

	bodies := map[string]*ast.FuncDecl{}
	for name, path := range sources {
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
			t.Fatalf("%s declares no function %s: the source map is stale and this guard is scanning nothing", path, name)
		}
	}
	return bodies
}

// parseStepupCompletionHandlers returns the AST body of each function named in
// stepupCompletionHandlers.
func parseStepupCompletionHandlers(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	return parseNamedFunctionBodies(t, stepupCompletionHandlers)
}

// parseStepupSessionHelpers returns the AST body of each function named in
// stepupSessionHelpers.
func parseStepupSessionHelpers(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	return parseNamedFunctionBodies(t, stepupSessionHelpers)
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

// TestStepupSessionHelperRefusalSpecsMatchTheHelperSources is
// TestStepupCallbackInlineRefusalSpecsMatchTheHandlerSources's counterpart for
// the session-affecting helpers the completion handlers delegate into
// (applyClearData, applyDeleteAccount, refreshCurrentSession). Before this
// guard, settingsStepupRefusalSpecs listed those three helpers' specs by
// hand, and nothing checked the hand-typed list against the sources it
// claimed to describe — a new spec raised inside any of the three could reach
// /auth/oidc/callback with every test in the file still green. The names come
// from the sources, and the comparison runs both ways so the map can neither
// miss a spec nor keep one nothing raises.
func TestStepupSessionHelperRefusalSpecsMatchTheHelperSources(t *testing.T) {
	t.Parallel()

	named := map[string]bool{}
	for name, function := range parseStepupSessionHelpers(t) {
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
			if _, listed := inlineStepupSessionHelperSpecs[identifier.Name]; !listed {
				t.Errorf(
					"%s (%s) raises %s(), which inlineStepupSessionHelperSpecs does not list: settingsStepupRefusalSpecs derives its helper block from this map, so an unlisted spec never reaches the step-up refusal render test. Add it there.",
					name, stepupSessionHelpers[name], identifier.Name,
				)
			}
			return true
		})
	}
	for name := range inlineStepupSessionHelperSpecs {
		if !named[name] {
			t.Errorf("inlineStepupSessionHelperSpecs lists %s(), which no session helper names any more: drop it, or the list oversells what it derives", name)
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
// spec key, whether the operation may be reported as done, and whether this
// device was signed out on the way.
type clearDataProbeVerdict struct {
	OK        bool   `json:"ok"`
	SignedOut bool   `json:"signed_out"`
	Key       string `json:"key"`
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
		spec, outcome := handler.applyClearData(c, user)
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"ok":         outcome == clearDataApplied,
			"signed_out": outcome == clearDataRefusedSignedOut,
			"key":        spec.Key,
		})
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
		name          string
		slug          string
		role          string
		wantOK        bool
		wantSignedOut bool
		wantKey       string
	}{
		{
			name: "the session re-issue is refused",
			slug: "refused",
			// The one non-owner value the users table's CHECK constraint still
			// accepts, which is what makes this arm reachable from a row rather
			// than only from a fault injected into the codec.
			role:          "partner",
			wantOK:        false,
			wantSignedOut: true,
			wantKey:       settingsDataClearedSignInAgainErrorSpec().Key,
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
			if verdict.SignedOut != testCase.wantSignedOut {
				t.Fatalf(
					"applyClearData reported signed_out=%t, want %t: the refused re-issue has already cleared the cookie, so a caller reading it as a live-session refusal sends the owner to /settings, which bounces to /login without the message",
					verdict.SignedOut, testCase.wantSignedOut,
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

// TestClearAllDataAnswersARefusedSessionReissueByFormat is
// TestApplyClearDataReportsARefusedSessionReissueToItsCaller's HTTP-level
// counterpart: applyClearData reports the refusal correctly, but ClearAllData
// is the route a browser actually reaches, and respondMappedError's plain-HTML
// arm for /api/v1/users/current redirects to /settings — which requires the
// very cookie applyClearData just cleared. That bounces to /login and drops
// the SettingsError flash on a channel /login never reads, the same trap
// completeErasureStepupReauth answers through redirectSignedOutRefusal. The
// three formats leave differently — JSON keeps the mapped envelope, HTMX is sent
// to /login by HX-Redirect, a plain form by 303 — so all three are driven.
func TestClearAllDataAnswersARefusedSessionReissueByFormat(t *testing.T) {
	t.Parallel()

	wantKey := settingsDataClearedSignInAgainErrorSpec().Key

	for _, testCase := range []struct {
		name       string
		slug       string
		accept     string
		htmx       bool
		wantStatus int
	}{
		{name: "plain html client lands on /login, not /settings", slug: "html", accept: "text/html", wantStatus: http.StatusSeeOther},
		{name: "json client gets the mapped 401 envelope", slug: "json", accept: "application/json", wantStatus: http.StatusUnauthorized},
		{name: "htmx client is sent to /login by HX-Redirect", slug: "htmx", accept: "text/html", htmx: true, wantStatus: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stub := newStubOIDCWorkflowService(true)
			_, database, handler := newSettingsMutationStepupApp(t, stub)

			password := "StrongPass1"
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
			if err != nil {
				t.Fatalf("hash probe password: %v", err)
			}
			user := models.User{
				Email:            "clear-data-format-" + testCase.slug + "@example.com",
				LocalAuthEnabled: true,
				PasswordHash:     string(hash),
				// Same non-owner value the table's CHECK constraint still accepts,
				// which is what makes the session re-issue fail from a row a test
				// can create rather than from an injected codec fault.
				Role:                "partner",
				OnboardingCompleted: true,
				AuthSessionVersion:  1,
				CycleLength:         28,
				PeriodLength:        5,
				AutoPeriodFill:      true,
				CreatedAt:           time.Now().UTC(),
			}
			if err := database.Create(&user).Error; err != nil {
				t.Fatalf("create the probe account: %v", err)
			}

			// Same reason as TestOIDCIdentityUnlinkReissueFailureIsReportedAsARefusal:
			// no registered route can carry a non-owner session into ClearAllData
			// (OwnerOnly resolves the role first), so the probe injects the session
			// user directly via Locals, the same seam ClearAllData reads through
			// currentUser.
			app := fiber.New()
			app.Post("/__probe/clear-data", func(c fiber.Ctx) error {
				c.Locals(contextUserKey, &user)
				return handler.ClearAllData(c)
			})

			form := url.Values{"password": {password}}
			request := httptest.NewRequest(http.MethodPost, "/__probe/clear-data", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", testCase.accept)
			if testCase.htmx {
				request.Header.Set("HX-Request", "true")
			}
			response := mustAppResponse(t, app, request)
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != testCase.wantStatus {
				t.Fatalf("clear-data refused reissue (%s) = %d, want %d: %s", testCase.name, response.StatusCode, testCase.wantStatus, mustReadBodyString(t, response.Body))
			}

			if testCase.accept == "application/json" {
				body := mustReadBodyString(t, response.Body)
				if !strings.Contains(body, wantKey) {
					t.Fatalf("expected the JSON envelope to carry %q, got %q", wantKey, body)
				}
			} else {
				assertSignedOutRefusal(t, response, wantKey, testCase.htmx)
			}

			// Anti-vacuity: the wipe itself must have happened, so a refusal above
			// can only have come from the session re-issue and not from ClearAllData
			// failing earlier (e.g. on the password check).
			var remaining int64
			if err := database.Model(&models.User{}).Where("id = ? AND auth_session_version = ?", user.ID, 2).Count(&remaining).Error; err != nil {
				t.Fatalf("count the bumped row after the wipe: %v", err)
			}
			if remaining != 1 {
				t.Fatalf("expected the account row to carry auth_session_version=2 after the wipe, got %d matching rows: the probe never reached the session re-issue", remaining)
			}
		})
	}
}

// assertSignedOutRefusal pins redirectSignedOutRefusal's page-bound answer: the
// refusal rides the auth flash channel, and the browser is sent to /login — by
// 303, or by HX-Redirect for an HTMX caller. A redirect to /settings would be
// bounced to /login by the cleared cookie and arrive there without the message.
func assertSignedOutRefusal(t *testing.T, response *http.Response, wantKey string, htmx bool) {
	t.Helper()

	if htmx {
		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 carrying HX-Redirect, got %d", response.StatusCode)
		}
		if redirect := response.Header.Get("HX-Redirect"); redirect != "/login" {
			t.Fatalf("expected HX-Redirect /login, got %q", redirect)
		}
	} else {
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", response.StatusCode)
		}
		if location := response.Header.Get("Location"); location != "/login" {
			t.Fatalf("expected the refusal to land on /login, got Location %q", location)
		}
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || strings.TrimSpace(flashCookie.Value) == "" {
		t.Fatal("expected a flash cookie carrying the refusal")
	}
	if payload := decodeFlashCookieForTest(t, flashCookie.Value); payload.AuthError != wantKey {
		t.Fatalf("expected %q on the auth flash channel (the one /login reads), got AuthError=%q SettingsError=%q", wantKey, payload.AuthError, payload.SettingsError)
	}
}

// TestErasureStepupReissueFailureLandsOnLogin drives completeErasureStepupReauth
// itself through a wipe that commits and a session re-issue that then fails —
// the arm TestApplyClearDataReportsARefusedSessionReissueToItsCaller pins only
// at applyClearData's seam. The re-issue is failed through the handler's
// session-issuance seam, armed after the step-up start so the callback's own
// authentication still passes and only the post-wipe re-mint is refused.
func TestErasureStepupReissueFailureLandsOnLogin(t *testing.T) {
	t.Parallel()

	var armed atomic.Bool
	fault := func() error {
		if armed.Load() {
			return errors.New("injected session issuance fault")
		}
		return nil
	}
	fixture := newOIDCStepupFixtureWithOptions(t, "settings-erasure-reissue-failure@example.com", onboardingTestAppOptions{sessionIssuanceFault: fault}, nil)
	fixture.oidcStub.reauthErr = nil
	seedStepupDayEntry(t, fixture)

	startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	armed.Store(true)
	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	// Anti-vacuity: the wipe committed, so the refusal below can only have come
	// from the re-issue that follows it.
	if got := countStepupDayEntries(t, fixture); got != 0 {
		t.Fatalf("expected the wipe to have committed before the re-issue failed, day entries = %d", got)
	}
	assertSignedOutRefusal(t, callbackResponse, settingsDataClearedSignInAgainErrorSpec().Key, false)
	authCookie := responseCookie(callbackResponse.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected the auth cookie to be cleared after the refused re-issue, got %v", authCookie)
	}
}
