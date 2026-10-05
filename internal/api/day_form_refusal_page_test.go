package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

// WEB-135 / WEB-126: a day form, the dashboard's usage-goal switch and the
// settings cycle form submitted without JavaScript answer a refusal as a page.
// A refusal before the handler (CSRF) keeps its status and key; a validation
// refusal of the day itself is a 422. Either way the page carries the localized
// message and one link back to the page hosting the form, built from the route,
// and no cookie rides on it.

const noJSBrowserAccept = "text/html,application/xhtml+xml"

// newRefusalPageContext is the settings test context with the app's error
// handler answering a refusal the way the production one does.
func newRefusalPageContext(t *testing.T, email string) settingsSecurityTestContext {
	t.Helper()
	return newSettingsSecurityTestContextWithOptions(t, email, onboardingTestAppOptions{enableCSRF: true, envelopeTransportErrors: true})
}

func englishCopy(t *testing.T, key string) string {
	t.Helper()
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	text := strings.TrimSpace(manager.Messages(i18n.LangEN)[key])
	if text == "" {
		t.Fatalf("locale en defines no %q", key)
	}
	return text
}

// assertRefusalPageCarrying reads a refusal the way a browser does: the status, a
// text/html body holding the localized message and exactly one link, the link
// target, and no cookie other than the CSRF one the middleware owns. The body is
// a whole page in the shared layout (WEB-264), with <html lang> naming the
// request's language — English for every caller of this helper; a request in
// another language is TestNoJSPageFormRefusalSpeaksTheRequestLanguage's.
func assertRefusalPageCarrying(t *testing.T, response *http.Response, status int, message string, back string) {
	t.Helper()
	assertRefusalPageIn(t, response, status, i18n.LangEN, message, back)
}

func assertRefusalPageIn(t *testing.T, response *http.Response, status int, language string, message string, back string) {
	t.Helper()
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, status)
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type %q, want text/html", contentType)
	}
	body := mustReadBodyString(t, response.Body)
	if want := `<html lang="` + language + `"`; !strings.Contains(body, want) {
		t.Fatalf("refusal is not a page in the shared layout speaking %q (no %s): %s", language, want, body)
	}
	if !strings.Contains(body, `id="main-content"`) {
		t.Fatalf("refusal page lacks the layout's main landmark: %s", body)
	}
	_, refusal, found := strings.Cut(body, "data-page-form-refusal")
	refusal, _, closed := strings.Cut(refusal, "</section>")
	if !found || !closed {
		t.Fatalf("refusal page lacks its content section: %s", body)
	}
	if !strings.Contains(refusal, template.HTMLEscapeString(message)) {
		t.Fatalf("refusal lacks the localized message %q: %s", message, refusal)
	}
	if got := strings.Count(refusal, "href="); got != 1 {
		t.Fatalf("refusal has %d links, want exactly the one back link: %s", got, refusal)
	}
	if want := `<a href="` + template.HTMLEscapeString(back) + `"`; !strings.Contains(refusal, want) {
		t.Fatalf("refusal lacks the back link %s: %s", want, refusal)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name != "ovumcy_csrf" {
			t.Fatalf("refusal page set the cookie %q; only the CSRF middleware's own may ride on it", cookie.Name)
		}
	}
}

// TestNoJSPageFormRefusalSpeaksTheRequestLanguage submits a day form without its
// CSRF token in Russian: the refusal page is the layout with <html lang="ru"> and
// the Russian copy, no cookie at all, and a signed-in request still renders the
// signed-out header — nothing of the account reaches the page.
func TestNoJSPageFormRefusalSpeaksTheRequestLanguage(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "refusal-language@example.com")
	_, iso := noJSDay()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/"+iso+"?source=calendar", strings.NewReader("_method=PUT&is_period=true"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", noJSBrowserAccept)
	request.Header.Set("Accept-Language", "ru")
	request.Header.Set("Cookie", ctx.authCookie+"; "+languageCookieName+"="+i18n.LangRU)
	response := mustAppResponse(t, ctx.app, request)
	if cookies := response.Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("CSRF refusal page set cookies %v", cookies)
	}

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	forbidden := strings.TrimSpace(manager.Messages(i18n.LangRU)["common.error.forbidden"])
	if forbidden == "" || forbidden == englishCopy(t, "common.error.forbidden") {
		t.Fatalf("locale ru has no copy of its own for common.error.forbidden: %q", forbidden)
	}
	body := mustReadBodyString(t, response.Body)
	if strings.Contains(body, ctx.user.Email) || strings.Contains(body, `action="/logout"`) {
		t.Fatalf("refusal page renders the signed-in account: %s", body)
	}
	response.Body = io.NopCloser(strings.NewReader(body))
	assertRefusalPageIn(t, response, http.StatusForbidden, i18n.LangRU, forbidden, calendarLanding(iso))
}

type dayFormRefusalCase struct {
	account string
	page    string
	match   func(*html.Node) bool
	back    func(iso string) string
	seeded  bool
	// kept reports whether the entry the form would have changed is untouched.
	kept func(t *testing.T, ctx settingsSecurityTestContext, iso string) bool
}

func dayFormRefusalCases() map[string]dayFormRefusalCase {
	notLogged := func(t *testing.T, ctx settingsSecurityTestContext, iso string) bool {
		return !periodLoggedOn(t, ctx, iso)
	}
	return map[string]dayFormRefusalCase{
		"calendar day save": {
			account: "refusal-calendar-save@example.com",
			match:   formWithFlag("data-day-editor-form"),
			back:    calendarLanding,
			kept:    notLogged,
		},
		"calendar day delete": {
			account: "refusal-calendar-delete@example.com",
			match:   formWithFlag("data-day-delete-form"),
			back:    calendarLanding,
			seeded:  true,
			kept: func(t *testing.T, ctx settingsSecurityTestContext, iso string) bool {
				return periodLoggedOn(t, ctx, iso)
			},
		},
		"dashboard day save": {
			account: "refusal-dashboard-save@example.com",
			page:    "/dashboard",
			match:   formWithFlag("data-dashboard-save-form"),
			back:    func(string) string { return "/dashboard" },
			kept:    func(t *testing.T, ctx settingsSecurityTestContext, _ string) bool { return !periodLoggedOn(t, ctx, "") },
		},
		"dashboard usage goal switch": {
			account: "refusal-dashboard-goal@example.com",
			page:    "/dashboard",
			match:   formWithFlag("data-usage-goal-quick-switch-form"),
			back:    func(string) string { return "/dashboard" },
		},
		"settings cycle": {
			account: "refusal-settings-cycle@example.com",
			page:    "/settings",
			match:   formWithAttr("data-settings-draft-form", "cycle"),
			back:    func(string) string { return "/settings" },
		},
	}
}

// TestNoJSDayAndCycleFormsAnswerAPreHandlerRefusalAsAPage submits each form
// without its CSRF token, as a page left open past the token's life does.
func TestNoJSDayAndCycleFormsAnswerAPreHandlerRefusalAsAPage(t *testing.T) {
	t.Parallel()

	forbidden := englishCopy(t, "common.error.forbidden")
	for name, c := range dayFormRefusalCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newRefusalPageContext(t, c.account)
			day, iso := noJSDay()
			if c.seeded {
				if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: day, IsPeriod: true, Flow: models.FlowNone}).Error; err != nil {
					t.Fatalf("create daily log: %v", err)
				}
			}
			page := c.page
			if page == "" {
				page = "/calendar/day/" + iso + "?mode=edit"
			}
			form := renderNoJSForm(t, ctx.app, page, authCookieMap(t, ctx.authCookie), c.match)

			response := form.submit(t, ctx.app, url.Values{"is_period": {"true"}, "usage_goal": {"avoid_pregnancy"}}, "csrf_token")
			assertRefusalPageCarrying(t, response, http.StatusForbidden, forbidden, c.back(iso))
			if c.kept != nil && !c.kept(t, ctx, iso) {
				t.Fatal("the refused form changed the day")
			}
		})
	}
}

// TestNoJSDayFormValidationRefusalIsAPageWithStatus422 pins the decision for a
// day the form's own values make invalid: 422 on the page-shaped refusal, with
// the day-entry copy and the link back to where the form is.
func TestNoJSDayFormValidationRefusalIsAPageWithStatus422(t *testing.T) {
	t.Parallel()

	invalidEntry := englishCopy(t, "dashboard.error.invalid_day_entry")
	cases := map[string]struct {
		account string
		page    func(iso string) string
		match   func(*html.Node) bool
		typed   url.Values
		back    func(iso string) string
	}{
		"calendar save, unparseable mood": {
			account: "validation-calendar-mood@example.com",
			page:    func(iso string) string { return "/calendar/day/" + iso + "?mode=edit" },
			match:   formWithFlag("data-day-editor-form"),
			typed:   url.Values{"is_period": {"true"}, "mood": {"abc"}},
			back:    calendarLanding,
		},
		"calendar save, unknown flow": {
			account: "validation-calendar-flow@example.com",
			page:    func(iso string) string { return "/calendar/day/" + iso + "?mode=edit" },
			match:   formWithFlag("data-day-editor-form"),
			typed:   url.Values{"is_period": {"true"}, "flow": {"torrential"}},
			back:    calendarLanding,
		},
		"dashboard save, unparseable mood": {
			account: "validation-dashboard-mood@example.com",
			page:    func(string) string { return "/dashboard" },
			match:   formWithFlag("data-dashboard-save-form"),
			typed:   url.Values{"is_period": {"true"}, "mood": {"abc"}},
			back:    func(string) string { return "/dashboard" },
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newRefusalPageContext(t, c.account)
			_, iso := noJSDay()
			form := renderNoJSForm(t, ctx.app, c.page(iso), authCookieMap(t, ctx.authCookie), c.match)

			response := form.submit(t, ctx.app, c.typed)
			assertRefusalPageCarrying(t, response, http.StatusUnprocessableEntity, invalidEntry, c.back(iso))
			if periodLoggedOn(t, ctx, "") {
				t.Fatal("the refused save stored an entry")
			}
		})
	}
}

// TestNoJSDayFormRefusalLinkIgnoresWhatTheRequestCarries submits a day form
// whose action a person (or a page that is not ours) rewrote: the date that does
// not parse and a source that is not one of the two pages never reach the href.
func TestNoJSDayFormRefusalLinkIgnoresWhatTheRequestCarries(t *testing.T) {
	t.Parallel()

	invalidEntry := englishCopy(t, "dashboard.error.invalid_day_entry")
	forbidden := englishCopy(t, "common.error.forbidden")
	cases := map[string]struct {
		action string
		typed  url.Values
		drop   []string
		status int
		want   string
	}{
		"bad date": {
			action: "/api/v1/days/2026-13-45?source=calendar",
			status: http.StatusUnprocessableEntity,
			want:   invalidEntry,
		},
		"bad date, delete": {
			action: "/api/v1/days/2026-13-45?source=calendar",
			typed:  url.Values{"_method": {"DELETE"}},
			status: http.StatusUnprocessableEntity,
			want:   invalidEntry,
		},
		"odd source, validation refusal": {
			action: "/api/v1/days/{iso}?source=%22%3E%3Cscript%3Ealert(1)%3C/script%3E",
			status: http.StatusUnprocessableEntity,
			want:   invalidEntry,
		},
		"odd source, CSRF refusal": {
			action: "/api/v1/days/{iso}?source=https://evil.example/",
			drop:   []string{"csrf_token"},
			status: http.StatusForbidden,
			want:   forbidden,
		},
		"bad date, CSRF refusal": {
			action: "/api/v1/days/%3Cscript%3E?source=calendar",
			drop:   []string{"csrf_token"},
			status: http.StatusForbidden,
			want:   forbidden,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newRefusalPageContext(t, "link-"+strings.NewReplacer(" ", "-", ",", "").Replace(name)+"@example.com")
			_, iso := noJSDay()
			form := renderNoJSForm(t, ctx.app, "/calendar/day/"+iso+"?mode=edit", authCookieMap(t, ctx.authCookie), formWithFlag("data-day-editor-form"))
			form.action = strings.ReplaceAll(c.action, "{iso}", iso)

			typed := c.typed
			if typed == nil {
				typed = url.Values{"mood": {"abc"}}
			}
			response := form.submit(t, ctx.app, typed, c.drop...)
			assertRefusalPageCarrying(t, response, c.status, c.want, "/dashboard")
		})
	}
}

// TestNoJSDayDeleteFailureAnswersAPageKeepingItsStatus pins a refusal the
// handler raises after validation: the delete the store fails keeps its 500 and
// still lands on a page with the link back to the calendar day, not on JSON,
// and the page says it in words rather than with the machine key.
func TestNoJSDayDeleteFailureAnswersAPageKeepingItsStatus(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "refusal-delete-failure@example.com")
	day, iso := noJSDay()
	if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: day, IsPeriod: true, Flow: models.FlowNone}).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}
	form := renderNoJSForm(t, ctx.app, "/calendar/day/"+iso+"?mode=edit", authCookieMap(t, ctx.authCookie), formWithFlag("data-day-delete-form"))
	if err := ctx.database.Migrator().DropTable(&models.DailyLog{}); err != nil {
		t.Fatalf("drop daily logs: %v", err)
	}

	response := form.submit(t, ctx.app, nil)
	assertRefusalPageCarrying(t, response, http.StatusInternalServerError, englishCopy(t, "common.error.internal_error"), calendarLanding(iso))
}

// TestDayWriteRefusalsKeepTheirStatusAndEnvelopeForOtherClients pins the other
// side: the 422 page belongs to a form submitted without JavaScript. A JSON
// caller, an htmx caller, a form POST that asks for JSON and a client sending
// the real verb keep the 400 or 403 and the shape they always had.
func TestDayWriteRefusalsKeepTheirStatusAndEnvelopeForOtherClients(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "refusal-other-clients@example.com")
	_, iso := noJSDay()
	target := "/api/v1/days/" + iso + "?source=calendar"

	send := func(method string, body string, contentType string, headers map[string]string, withCSRF bool) *http.Response {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Accept-Language", "en")
		cookie := ctx.authCookie
		if withCSRF {
			request.Header.Set("X-CSRF-Token", ctx.csrfToken)
			cookie += "; " + ctx.csrfCookie.Name + "=" + ctx.csrfCookie.Value
		}
		request.Header.Set("Cookie", cookie)
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		return mustAppResponse(t, ctx.app, request)
	}
	assertJSONError := func(t *testing.T, response *http.Response, status int, key string) {
		t.Helper()
		defer func() { _ = response.Body.Close() }()
		assertStatusCode(t, response, status)
		if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("Content-Type %q, want JSON", contentType)
		}
		if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `"error":"`+key+`"`) {
			t.Fatalf("body %s lacks the key %q", body, key)
		}
	}
	form := "is_period=true&mood=abc"

	t.Run("JSON body", func(t *testing.T) {
		response := send(http.MethodPut, "{", "application/json", map[string]string{"Accept": "application/json"}, true)
		assertJSONError(t, response, http.StatusBadRequest, "invalid payload")
	})
	t.Run("form POST that asks for JSON", func(t *testing.T) {
		response := send(http.MethodPost, form+"&_method=PUT", "application/x-www-form-urlencoded", map[string]string{"Accept": "application/json"}, true)
		assertStatusCode(t, response, http.StatusBadRequest)
		if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("Content-Type %q, want JSON", contentType)
		}
		_ = response.Body.Close()
	})
	for name, accept := range map[string]string{"form POST with no Accept": "", "form POST that accepts anything": "*/*"} {
		t.Run(name, func(t *testing.T) {
			response := send(http.MethodPost, form+"&_method=PUT", "application/x-www-form-urlencoded", map[string]string{"Accept": accept}, true)
			assertStatusCode(t, response, http.StatusBadRequest)
			if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("Content-Type %q, want JSON", contentType)
			}
			_ = response.Body.Close()
		})
	}
	t.Run("htmx", func(t *testing.T) {
		response := send(http.MethodPut, form, "application/x-www-form-urlencoded", map[string]string{"HX-Request": "true"}, true)
		defer func() { _ = response.Body.Close() }()
		assertStatusCode(t, response, http.StatusBadRequest)
		body := mustReadBodyString(t, response.Body)
		if !strings.Contains(body, "data-flash-key") || strings.Contains(body, "href=") {
			t.Fatalf("htmx refusal is not the bare status fragment: %s", body)
		}
	})
	t.Run("real PUT without a token, browser Accept", func(t *testing.T) {
		response := send(http.MethodPut, form, "application/x-www-form-urlencoded", map[string]string{"Accept": noJSBrowserAccept}, false)
		assertJSONError(t, response, http.StatusForbidden, "forbidden")
	})
	t.Run("real DELETE without a token, browser Accept", func(t *testing.T) {
		response := send(http.MethodDelete, "", "application/x-www-form-urlencoded", map[string]string{"Accept": noJSBrowserAccept}, false)
		assertJSONError(t, response, http.StatusForbidden, "forbidden")
	})
}

// TestEveryDayAndCycleRefusalHasLocalizedCopyInEveryLocale walks, from the
// source, every global-target spec the day save, the day delete and the cycle
// settings handlers can hand apiError, and requires copy for each in every
// locale: the page that now carries a browser's refusal has no machine key to
// fall back on, so an unmapped key would be shown to the person verbatim. The
// walk starts at the handlers and follows each function that returns a spec, so
// a spec added to one of them later is covered without a list to update. The
// settings-form-target specs of the cycle route are not on this arm: a browser
// is flash-redirected for them. The manual cycle-start mark's refusals, which
// these three never raise and which predate the page, are its own route's.
func TestEveryDayAndCycleRefusalHasLocalizedCopyInEveryLocale(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list package sources: %v", err)
	}
	handlerMethods := map[string]*ast.FuncDecl{}
	specFuncs := map[string]*ast.FuncDecl{}
	stringConsts := map[string]string{}
	fileSet := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			if general, ok := decl.(*ast.GenDecl); ok && general.Tok == token.CONST {
				// A key may be spelled as a package constant (unauthorizedErrorKey):
				// it is resolved through its declaration, never skipped.
				for _, spec := range general.Specs {
					valueSpec := spec.(*ast.ValueSpec)
					for index, name := range valueSpec.Names {
						if index >= len(valueSpec.Values) {
							continue
						}
						if literal, ok := valueSpec.Values[index].(*ast.BasicLit); ok && literal.Kind == token.STRING {
							value, err := strconv.Unquote(literal.Value)
							if err != nil {
								t.Fatalf("unquote %s: %v", literal.Value, err)
							}
							stringConsts[name.Name] = value
						}
					}
				}
				continue
			}
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if fn.Recv == nil {
				if results := fn.Type.Results; results != nil && len(results.List) == 1 {
					if result, ok := results.List[0].Type.(*ast.Ident); ok && result.Name == "APIErrorSpec" {
						specFuncs[fn.Name.Name] = fn
					}
				}
				continue
			}
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if receiver, ok := star.X.(*ast.Ident); ok && receiver.Name == "Handler" {
					handlerMethods[fn.Name.Name] = fn
				}
			}
		}
	}

	mentionsCycleStartMark := func(clause *ast.CaseClause) bool {
		found := false
		for _, expr := range clause.List {
			ast.Inspect(expr, func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok &&
					(selector.Sel.Name == "ErrManualCycleStartConfirmationNeeded" || selector.Sel.Name == "ErrManualCycleStartReplaceRequired") {
					found = true
				}
				return !found
			})
		}
		return found
	}

	keys := map[string]bool{}
	visited := map[string]bool{}
	var visit func(fn *ast.FuncDecl)
	visit = func(fn *ast.FuncDecl) {
		if visited[fn.Name.Name] {
			return
		}
		visited[fn.Name.Name] = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if clause, ok := node.(*ast.CaseClause); ok && mentionsCycleStartMark(clause) {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if callee.Name == "globalErrorSpec" && len(call.Args) == 3 {
				switch key := call.Args[2].(type) {
				case *ast.BasicLit:
					unquoted, err := strconv.Unquote(key.Value)
					if err != nil {
						t.Fatalf("unquote %s: %v", key.Value, err)
					}
					keys[unquoted] = true
				case *ast.Ident:
					value, ok := stringConsts[key.Name]
					if !ok {
						t.Fatalf("%s: globalErrorSpec key %s is no package string constant the walk can resolve", fn.Name.Name, key.Name)
					}
					keys[value] = true
				default:
					t.Fatalf("%s: globalErrorSpec key is neither a literal nor a constant: the walk cannot read it", fn.Name.Name)
				}
				return true
			}
			if spec, ok := specFuncs[callee.Name]; ok {
				visit(spec)
			}
			return true
		})
	}
	for _, root := range []string{"UpsertDay", "PatchDay", "resolveUpsertDayRequest", "DeleteDay", "UpdateCycleSettings", "updateUsageGoalOnly"} {
		fn, ok := handlerMethods[root]
		if !ok {
			t.Fatalf("handler method %s not found: the walk lost its root", root)
		}
		visit(fn)
	}
	for _, named := range []string{
		"invalid date", "invalid payload", "invalid symptom ids", "invalid bbt value", "unauthorized",
		"failed to load day", "failed to create day", "failed to update day", "failed to delete day",
		"failed to update cycle settings",
	} {
		if !keys[named] {
			t.Fatalf("the walk found no %q among %d keys: it is broken, not the class empty", named, len(keys))
		}
	}

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	for key := range keys {
		t.Run(key, func(t *testing.T) {
			translationKey := services.AuthErrorTranslationKey(key)
			if translationKey == "" {
				t.Fatalf("day or cycle refusal key %q has no entry in the error translation map: the refusal page would show it raw", key)
			}
			for _, language := range manager.SupportedLanguages() {
				if strings.TrimSpace(manager.Messages(language)[translationKey]) == "" {
					t.Errorf("key %q maps to %q, which locale %q does not define", key, translationKey, language)
				}
			}
		})
	}
}
