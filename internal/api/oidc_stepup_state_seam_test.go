package api

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// On the completion path the callback state is matched at ONE seam —
// dispatchStepupCompletion — and the per-purpose completions carry no copy of
// that check. What follows pins all three halves of that arrangement, because
// only together are they the property: the seam refusing is worth nothing if a
// completion can be reached around it, a completion without its own check is
// safe only while the seam has one, and a seam that refuses everything is not
// a check either.
//
// The check moved here because a per-purpose copy fixes the class at N of N+1:
// a fourth purpose added to the switch would inherit nothing. The AST guards
// below keep the inheritance true for that fourth purpose — one pins that no
// completion re-checks the state, the other that none of them is reachable
// around the seam — while the behavioural cases prove the three that exist
// today are refused by the seam and, when the state matches, reach a
// completion and run it.

const stepupSeamDispatchPath = "/test-only/stepup-dispatch"

// stepupSeamHarness owns a handler with no routes of its own. The seam is an
// internal method and no route can deliver a mismatching state to it — the
// callback refuses one before dispatch, and the continue leg rebuilds the
// exchange from the continuation it carries — so the cases below call it
// directly through a route they register themselves. The OIDC service is the
// shared stub: the identity-link completion is the one arm that reaches a
// service call before it can say anything, and that call is part of how its
// case proves the arm ran.
type stepupSeamHarness struct {
	handler  *Handler
	database *gorm.DB
	oidcStub *stubOIDCWorkflowService
}

func newStepupSeamHarness(t *testing.T) *stepupSeamHarness {
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
	stub := newStubOIDCWorkflowService(true)
	handler, err := NewHandler(testAppSecretKey, time.UTC, manager, true, newTestHandlerDependencies(database, manager, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	}))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}
	return &stepupSeamHarness{handler: handler, database: database, oidcStub: stub}
}

// signedInOwner puts one owner in this harness's database and returns the auth
// cookie that identifies her. Every completion resolves the owner from that
// session before it does anything of its own, so without one all three answer
// the same session refusal and no case can tell which arm ran.
func (harness *stepupSeamHarness) signedInOwner(t *testing.T, email string, localAuthEnabled bool) (models.User, string) {
	t.Helper()

	user := models.User{
		Email:               email,
		LocalAuthEnabled:    localAuthEnabled,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		CreatedAt:           time.Now().UTC(),
	}
	if err := harness.database.Create(&user).Error; err != nil {
		t.Fatalf("create the step-up owner: %v", err)
	}
	return user, issueAuthCookieForUser(t, user)
}

func (harness *stepupSeamHarness) dispatch(t *testing.T, state oidcStepupState, exchange oidcCallbackExchange, cookieHeader string) *http.Response {
	t.Helper()

	app := fiber.New()
	app.Use(harness.handler.LanguageMiddleware)
	app.Get(stepupSeamDispatchPath, func(c fiber.Ctx) error {
		return harness.handler.dispatchStepupCompletion(c, state, exchange)
	})

	request := httptest.NewRequest(http.MethodGet, stepupSeamDispatchPath, nil)
	if strings.TrimSpace(cookieHeader) != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	return mustAppResponse(t, app, request)
}

func dispatchStepupForTest(t *testing.T, state oidcStepupState, exchange oidcCallbackExchange) *http.Response {
	t.Helper()
	return newStepupSeamHarness(t).dispatch(t, state, exchange, "")
}

// stepupSeamPurposes builds one valid state per purpose the switch dispatches
// on, through the real constructors: a purpose that grows a new required field
// fails here rather than being fed a shape validAt would have refused. The
// owner id is a parameter because the completions compare it against the
// session, and a case that wants to be let past that comparison has to name
// the account it created.
func stepupSeamPurposes(t *testing.T, userID uint) map[string]oidcStepupState {
	t.Helper()

	localPassword, err := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, userID, "$2a$10$notarealhashnotarealhashnotarealhashnotarealhashnota")
	if err != nil {
		t.Fatalf("build local-password step-up state: %v", err)
	}
	erasure, err := newOIDCErasureStepupState(time.Now(), userID, oidcStepupErasureClearData)
	if err != nil {
		t.Fatalf("build erasure step-up state: %v", err)
	}
	identityLink, err := newOIDCIdentityLinkStepupState(time.Now(), userID)
	if err != nil {
		t.Fatalf("build identity-link step-up state: %v", err)
	}

	return map[string]oidcStepupState{
		"local password setup": localPassword,
		"erasure":              erasure,
		"identity link":        identityLink,
	}
}

// stepupSeamMismatches are the shapes a callback state can take that are not
// the sealed one. An extension alone was too kind a case: a prefix test, a
// length test and a real comparison all refuse it, so it says only that
// SOMETHING looked at the value. The empty state is the shape a check written
// `carried != "" && !match` waves through, and the truncation is the one a
// prefix test waves through — together they pin that the two values must be
// equal, not merely related.
func stepupSeamMismatches(t *testing.T, sealed string) map[string]string {
	t.Helper()

	if len(sealed) < 2 {
		t.Fatalf("the sealed state %q is too short to truncate: the mismatch shapes below would stop distinguishing anything", sealed)
	}
	return map[string]string{
		"extended":  sealed + "-not-this-flow",
		"truncated": sealed[:len(sealed)-1],
		"empty":     "",
	}
}

func TestStepupDispatchRefusesACallbackStateThatDoesNotMatch(t *testing.T) {
	t.Parallel()

	mismatchKey := authOIDCAuthenticationFailedErrorSpec().Key

	for name, state := range stepupSeamPurposes(t, 1) {
		for shape, carried := range stepupSeamMismatches(t, state.State) {
			t.Run(name+"/"+shape, func(t *testing.T) {
				t.Parallel()

				response := dispatchStepupForTest(t, state, oidcCallbackExchange{
					Code:  "provider-code",
					State: carried,
				})

				assertFlashRefusal(t, response)
				flash := decodeFlashCookieForTest(t, responseCookie(response.Cookies(), flashCookieName).Value)
				if flash.SettingsError != mismatchKey {
					t.Fatalf("expected the state-mismatch refusal %q for the %s callback state, got %q", mismatchKey, shape, flash.SettingsError)
				}
			})
		}
	}
}

// TestStepupDispatchLetsAMatchingStateReachItsCompletion keeps the case above
// from passing for the wrong reason: a seam that refused every callback would
// satisfy it exactly as the real one does. So each purpose here asserts a
// signal its OWN completion produces and the seam has no code to produce — the
// seam's only answer is a flashed refusal, and none of the three below is a
// refusal the seam can raise.
//
// The owner is signed in and already has a local password, which is what makes
// the three arms distinguishable: the password setup finds its work already
// done and leaves by a plain redirect with no flash at all, the erasure
// step-up refuses because an account with a password is back on the password
// gate, and the identity link — which does not care about the password — runs
// to the end and links. Without a session all three stop at the same session
// refusal, and the case could not say which arm it reached.
func TestStepupDispatchLetsAMatchingStateReachItsCompletion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		purpose string
		email   string
		reached func(t *testing.T, harness *stepupSeamHarness, user models.User, response *http.Response)
	}{
		{
			purpose: "local password setup",
			email:   "stepup-seam-password@example.com",
			reached: func(t *testing.T, _ *stepupSeamHarness, _ models.User, response *http.Response) {
				assertStatusCode(t, response, http.StatusSeeOther)
				if location := response.Header.Get("Location"); location != "/settings" {
					t.Fatalf("expected the completion to send the owner to /settings, got %q", location)
				}
				// No flash at all is the part the seam cannot imitate: every
				// answer it has is a flashed refusal.
				if flash := responseCookie(response.Cookies(), flashCookieName); flash != nil && strings.TrimSpace(flash.Value) != "" {
					payload := decodeFlashCookieForTest(t, flash.Value)
					t.Fatalf("expected no flash from the nothing-left-to-do arm of the local-password completion, got %+v — the seam answered instead of the completion", payload)
				}
			},
		},
		{
			purpose: "erasure",
			email:   "stepup-seam-erasure@example.com",
			reached: func(t *testing.T, _ *stepupSeamHarness, _ models.User, response *http.Response) {
				assertFlashRefusal(t, response)
				wanted := settingsErasureNeedsAccountPasswordErrorSpec().Key
				flash := decodeFlashCookieForTest(t, responseCookie(response.Cookies(), flashCookieName).Value)
				if flash.SettingsError != wanted {
					t.Fatalf("expected the erasure completion's own refusal %q, got %q — the seam refused before the completion ran", wanted, flash.SettingsError)
				}
			},
		},
		{
			purpose: "identity link",
			email:   "stepup-seam-link@example.com",
			reached: func(t *testing.T, harness *stepupSeamHarness, user models.User, response *http.Response) {
				assertStatusCode(t, response, http.StatusSeeOther)
				flash := responseCookie(response.Cookies(), flashCookieName)
				if flash == nil || strings.TrimSpace(flash.Value) == "" {
					t.Fatal("expected the identity-link completion to flash its success")
				}
				if payload := decodeFlashCookieForTest(t, flash.Value); payload.SettingsSuccess != "oidc_identity_linked" {
					t.Fatalf("expected settings_success=oidc_identity_linked, got %+v — the seam answered instead of the completion", payload)
				}
				// The side effect, not only the answer: the completion asked
				// the OIDC service to link THIS owner.
				if harness.oidcStub.lastIdentityLinkUserID != user.ID {
					t.Fatalf("expected the completion to link user %d, the service was asked for %d", user.ID, harness.oidcStub.lastIdentityLinkUserID)
				}
			},
		},
	}

	if purposes := stepupSeamPurposes(t, 1); len(cases) != len(purposes) {
		t.Fatalf("this control speaks for %d purposes but the seam dispatches %d: a purpose whose completion nothing here reaches is covered by the refusal case alone, which a seam refusing everything also satisfies", len(cases), len(purposes))
	}

	for _, testCase := range cases {
		t.Run(testCase.purpose, func(t *testing.T) {
			t.Parallel()

			harness := newStepupSeamHarness(t)
			user, authCookie := harness.signedInOwner(t, testCase.email, true)
			state, known := stepupSeamPurposes(t, user.ID)[testCase.purpose]
			if !known {
				t.Fatalf("no step-up state is built for %q: this case would dispatch a zero payload and prove nothing", testCase.purpose)
			}

			response := harness.dispatch(t, state, oidcCallbackExchange{
				Code:  "provider-code",
				State: state.State,
			}, authCookie)

			testCase.reached(t, harness, user, response)
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

	match := firstStepupStateComparison(t, "dispatchStepupCompletion", dispatch)
	if !match.pos.IsValid() {
		t.Fatal("dispatchStepupCompletion does not weigh the callback state against the sealed one: a purpose whose completion carries no check of its own is now unguarded")
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
	if match.pos > switchPos {
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
		if copied := firstStepupStateComparison(t, name, completion); copied.pos.IsValid() {
			t.Errorf("%s weighs the callback state against the sealed one itself, in `%s`: the seam owns that check, and a copy here is the N-of-N+1 split this arrangement replaced", name, copied.source)
		}
	}
}

// TestEveryStepupCompletionIsReachedOnlyThroughTheDispatchSeam is what makes
// the guard above worth anything. "No completion re-checks the state" is a
// safety property only while every completion is entered through the seam that
// does check it; a handler calling one directly would run a purpose's
// completion on a callback state nobody compared, and every other case in this
// file would stay green. So the package's own sources are read and each
// mention of a completion is attributed to the function it sits in.
//
// Deliberately narrower than it sounds: it sees a completion NAMED in source,
// which covers a direct call and a method value handed elsewhere, and it would
// not see one reached through an interface or a reflective lookup. Neither
// exists here, and either would have to be built around the switch this file's
// arity check already watches.
func TestEveryStepupCompletionIsReachedOnlyThroughTheDispatchSeam(t *testing.T) {
	t.Parallel()

	const seam = "dispatchStepupCompletion"

	completions := map[string]int{}
	for _, name := range stepupPerPurposeCompletions {
		completions[name] = 0
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fileSet := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		parsed, err := parser.ParseFile(fileSet, name, source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		functions := []*ast.FuncDecl{}
		for _, declaration := range parsed.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				functions = append(functions, function)
			}
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, isCompletion := completions[selector.Sel.Name]; !isCompletion {
				return true
			}
			completions[selector.Sel.Name]++
			if caller := enclosingFunctionName(functions, selector.Pos()); caller != seam {
				t.Errorf(
					"%s reaches %s at %s, around the seam: %s is where the callback state is matched, and a completion entered anywhere else runs on a state nobody compared. Hand the state and the exchange to handler.%s and let it select the purpose.",
					caller, selector.Sel.Name, fileSet.Position(selector.Pos()), seam, seam,
				)
			}
			return true
		})
	}

	if scanned == 0 {
		t.Fatal("no non-test source was scanned: this guard read nothing")
	}
	for name, mentions := range completions {
		if mentions == 0 {
			t.Errorf("%s is named nowhere in the package's sources: it is either dead or renamed, and this guard is watching a function nothing dispatches to", name)
		}
	}
}

// enclosingFunctionName reports which declared function a position falls
// inside, or "package scope" for a mention outside every function body — a
// method value stored in a package-level var reaches the completion too, and
// naming it that way keeps such a site from being attributed to whichever
// function happens to be declared nearby.
func enclosingFunctionName(functions []*ast.FuncDecl, pos token.Pos) string {
	for _, function := range functions {
		if function.Pos() <= pos && pos < function.End() {
			return function.Name.Name
		}
	}
	return "package scope"
}

// stepupStateComparison is a place where the sealed step-up's state and the
// one the callback carried meet in a single expression.
type stepupStateComparison struct {
	pos    token.Pos
	source string
}

// firstStepupStateComparison finds that place however it is spelled. Keying on
// the identifier matchesState would key the guard on a spelling: a copy
// written `state.State != exchange.State` is the same check restored to the
// same completion, and it costs the constant-time comparison matchesState
// performs on top of it. What is looked for instead is the pair of values —
// the sealed State and the carried State meeting in one expression — plus a
// call that asks the sealed state about the carried one, which is the shape
// matchesState and any renamed successor take.
//
// The operand names come from the signature, so a parameter rename cannot
// blind it. A comparison that first copies either value into a local is not
// seen; chasing that needs type information this guard does not load, and the
// behavioural cases above are what stand behind it.
func firstStepupStateComparison(t *testing.T, name string, function *ast.FuncDecl) stepupStateComparison {
	t.Helper()

	sealed, carried := stepupStateOperands(t, name, function)
	found := stepupStateComparison{pos: token.NoPos}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if found.pos.IsValid() {
			return false
		}
		switch expression := node.(type) {
		case *ast.BinaryExpr:
			if expression.Op != token.EQL && expression.Op != token.NEQ {
				return true
			}
			if !readsField(expression, sealed, "State") || !readsField(expression, carried, "State") {
				return true
			}
		case *ast.CallExpr:
			if !readsField(expression, carried, "State") {
				return true
			}
			// Either the sealed state is the receiver being asked — whatever
			// the method is called — or both values are handed to a comparison
			// helper (subtle.ConstantTimeCompare, strings.EqualFold, …).
			if !rootedAt(expression.Fun, sealed) && !readsField(expression, sealed, "State") {
				return true
			}
		default:
			return true
		}
		found = stepupStateComparison{pos: node.Pos(), source: renderNode(node)}
		return false
	})
	return found
}

// stepupStateOperands names the two parameters such a comparison has to read,
// taken from the signature rather than assumed to be spelled "state" and
// "exchange".
func stepupStateOperands(t *testing.T, name string, function *ast.FuncDecl) (string, string) {
	t.Helper()

	sealed, carried := "", ""
	for _, field := range function.Type.Params.List {
		identifier, ok := field.Type.(*ast.Ident)
		if !ok || len(field.Names) == 0 {
			continue
		}
		switch identifier.Name {
		case "oidcStepupState":
			sealed = field.Names[0].Name
		case "oidcCallbackExchange":
			carried = field.Names[0].Name
		}
	}
	if sealed == "" || carried == "" {
		t.Fatalf(
			"%s takes no sealed step-up state and callback exchange pair (found %q and %q): this guard cannot tell what a comparison inside it compares, and would report clean on a copy it simply failed to recognise",
			name, sealed, carried,
		)
	}
	return sealed, carried
}

// readsField reports whether the subtree reads receiver.field anywhere.
func readsField(node ast.Node, receiver, field string) bool {
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		selector, ok := inner.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != field {
			return true
		}
		if identifier, ok := selector.X.(*ast.Ident); ok && identifier.Name == receiver {
			found = true
		}
		return true
	})
	return found
}

// rootedAt reports whether expression is a selector on the named identifier —
// the receiver half of state.matchesState(…).
func rootedAt(expression ast.Expr, receiver string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == receiver
}

// renderNode prints an offending expression back as source, so a failure names
// the comparison it found and not only the function holding it.
func renderNode(node ast.Node) string {
	buffer := bytes.Buffer{}
	if err := printer.Fprint(&buffer, token.NewFileSet(), node); err != nil {
		return "<unprintable expression>"
	}
	return strings.Join(strings.Fields(buffer.String()), " ")
}
