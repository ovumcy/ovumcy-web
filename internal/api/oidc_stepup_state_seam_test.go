package api

import (
	"go/ast"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// The callback state is matched at ONE seam — dispatchStepupCompletion — and
// the per-purpose completions carry no copy of that check. What follows pins
// both halves of that arrangement, because only together are they the
// property: the seam refusing is worth nothing if a completion can be reached
// around it, and a completion without its own check is safe only while the
// seam has one.
//
// The check moved here because a per-purpose copy fixes the class at N of N+1:
// a fourth purpose added to the switch would inherit nothing. The AST guard
// below keeps the inheritance true for that fourth purpose; the behavioural
// cases prove the three that exist today are refused by the seam and not by a
// check of their own.

const stepupSeamDispatchPath = "/test-only/stepup-dispatch"

// newStepupSeamHandler builds a handler with no routes of its own. The seam is
// an internal method and no route can deliver a mismatching state to it — the
// callback refuses one before dispatch, and the continue leg rebuilds the
// exchange from the continuation it carries — so the test calls it directly
// through a route it registers itself.
func newStepupSeamHandler(t *testing.T) *Handler {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{
		Driver:     db.DriverSQLite,
		SQLitePath: filepath.Join(t.TempDir(), "ovumcy-stepup-seam.db"),
	})
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
	handler, err := NewHandler(testAppSecretKey, time.UTC, manager, true, newTestHandlerDependencies(database, manager, onboardingTestAppOptions{
		cookieSecure: true,
	}))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}
	return handler
}

func dispatchStepupForTest(t *testing.T, state oidcStepupState, exchange oidcCallbackExchange) *http.Response {
	t.Helper()

	handler := newStepupSeamHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get(stepupSeamDispatchPath, func(c fiber.Ctx) error {
		return handler.dispatchStepupCompletion(c, state, exchange)
	})

	return mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, stepupSeamDispatchPath, nil))
}

// stepupSeamPurposes builds one valid state per purpose the switch dispatches
// on, through the real constructors: a purpose that grows a new required field
// fails here rather than being fed a shape validAt would have refused.
func stepupSeamPurposes(t *testing.T) map[string]oidcStepupState {
	t.Helper()

	localPassword, err := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, 1, "$2a$10$notarealhashnotarealhashnotarealhashnotarealhashnota")
	if err != nil {
		t.Fatalf("build local-password step-up state: %v", err)
	}
	erasure, err := newOIDCErasureStepupState(time.Now(), 1, oidcStepupErasureClearData)
	if err != nil {
		t.Fatalf("build erasure step-up state: %v", err)
	}
	identityLink, err := newOIDCIdentityLinkStepupState(time.Now(), 1)
	if err != nil {
		t.Fatalf("build identity-link step-up state: %v", err)
	}

	return map[string]oidcStepupState{
		"local password setup": localPassword,
		"erasure":              erasure,
		"identity link":        identityLink,
	}
}

func TestStepupDispatchRefusesACallbackStateThatDoesNotMatch(t *testing.T) {
	t.Parallel()

	mismatchKey := authOIDCAuthenticationFailedErrorSpec().Key

	for name, state := range stepupSeamPurposes(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			response := dispatchStepupForTest(t, state, oidcCallbackExchange{
				Code:  "provider-code",
				State: state.State + "-not-this-flow",
			})

			assertFlashRefusal(t, response)
			flash := decodeFlashCookieForTest(t, responseCookie(response.Cookies(), flashCookieName).Value)
			if flash.SettingsError != mismatchKey {
				t.Fatalf("expected the state-mismatch refusal %q, got %q", mismatchKey, flash.SettingsError)
			}
		})
	}
}

// TestStepupDispatchLetsAMatchingStateReachItsCompletion keeps the case above
// from passing for the wrong reason. Every completion refuses this request too
// — it carries no session, and each one resolves the owner from one — but it
// refuses with a DIFFERENT key. Without the pairing, a seam that refused
// everything unconditionally would look identical.
func TestStepupDispatchLetsAMatchingStateReachItsCompletion(t *testing.T) {
	t.Parallel()

	sessionKey := settingsOIDCReauthMismatchErrorSpec().Key

	for name, state := range stepupSeamPurposes(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			response := dispatchStepupForTest(t, state, oidcCallbackExchange{
				Code:  "provider-code",
				State: state.State,
			})

			assertFlashRefusal(t, response)
			flash := decodeFlashCookieForTest(t, responseCookie(response.Cookies(), flashCookieName).Value)
			if flash.SettingsError != sessionKey {
				t.Fatalf("expected the completion's own session refusal %q, got %q — the seam refused before the completion ran", sessionKey, flash.SettingsError)
			}
		})
	}
}

// stepupPerPurposeCompletions are the switch's current targets, named rather
// than derived so that adding an arm without adding it here fails the arity
// check below. A guard that scans only what it already knew about would let
// the fourth purpose — the one this arrangement exists for — in unexamined.
var stepupPerPurposeCompletions = []string{
	"completeLocalPasswordSetupReauth",
	"completeErasureStepupReauth",
	"completeOIDCIdentityLinkStepup",
}

// TestStepupStateIsMatchedAtTheDispatchSeamOnly is the structural half. The
// behavioural cases can only speak for the purposes that exist; this one
// speaks for the next one, by pinning that the check stands ahead of the
// switch rather than inside the arms it selects.
func TestStepupStateIsMatchedAtTheDispatchSeamOnly(t *testing.T) {
	t.Parallel()

	bodies := parseStepupCompletionHandlers(t)

	dispatch := bodies["dispatchStepupCompletion"]
	if dispatch == nil {
		t.Fatal("dispatchStepupCompletion is not scanned: stepupCompletionHandlers is stale and this guard checks nothing")
	}

	matchPos := firstMatchesStatePos(dispatch)
	if !matchPos.IsValid() {
		t.Fatal("dispatchStepupCompletion does not match the callback state: a purpose whose completion carries no check of its own is now unguarded")
	}

	switchPos := token.NoPos
	arms := 0
	ast.Inspect(dispatch.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if !switchPos.IsValid() {
			switchPos = statement.Pos()
		}
		for _, clause := range statement.Body.List {
			if caseClause, ok := clause.(*ast.CaseClause); ok && len(caseClause.List) > 0 {
				arms++
			}
		}
		return true
	})
	if !switchPos.IsValid() {
		t.Fatal("dispatchStepupCompletion no longer dispatches on a switch: this guard's ordering check reads nothing")
	}
	if matchPos > switchPos {
		t.Fatal("dispatchStepupCompletion matches the state after selecting a completion: an arm added below inherits nothing")
	}
	if arms != len(stepupPerPurposeCompletions) {
		t.Fatalf("the dispatch switch has %d purpose arms but %d are pinned here: a purpose was added without being brought under this guard", arms, len(stepupPerPurposeCompletions))
	}

	for _, name := range stepupPerPurposeCompletions {
		completion := bodies[name]
		if completion == nil {
			t.Fatalf("%s is not scanned: stepupCompletionHandlers is stale", name)
		}
		if firstMatchesStatePos(completion).IsValid() {
			t.Errorf("%s matches the callback state itself: the seam owns that check, and a copy here is the N-of-N+1 split this arrangement replaced", name)
		}
	}
}

func firstMatchesStatePos(function *ast.FuncDecl) token.Pos {
	found := token.NoPos
	ast.Inspect(function.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "matchesState" {
			return true
		}
		if !found.IsValid() {
			found = selector.Pos()
		}
		return true
	})
	return found
}
