package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// settingsAccountChangedKey is the stable key a settings write refused for
// another account renders in markup; settingsAccountChangedError is the key
// its JSON envelope carries. Both are spelled out so a 409 from any other
// cause — the day forms' own refusal included — cannot satisfy these tests.
const (
	settingsAccountChangedKey   = "settings.error.account_changed"
	settingsAccountChangedError = "settings form account changed"
)

// The two settings writes a rendered page hosts that are bound to the
// rendering account.
const (
	cycleSettingsRouteName = "PATCH /api/v1/users/current/cycle"
	importJSONRouteName    = "POST /api/v1/imports/json"
)

func assertSettingsAccountChangedRefusal(t *testing.T, label string, response *http.Response) {
	t.Helper()
	body := mustReadBodyString(t, response.Body)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("%s: must be refused 409, got %d %q", label, response.StatusCode, body)
	}
	if !strings.Contains(body, settingsAccountChangedKey) && !strings.Contains(body, settingsAccountChangedError) {
		t.Fatalf("%s: the 409 must carry the %s reason, got %q", label, settingsAccountChangedKey, body)
	}
	if strings.Contains(body, accountChangedKey) {
		t.Fatalf("%s: a refused settings write must not claim a day entry was not saved (%s), got %q", label, accountChangedKey, body)
	}
}

// settingsWriteCase is one way a rendered page writes through a bound
// settings route: the request it sends and the state of the account it lands
// on that the write changes.
type settingsWriteCase struct {
	route string
	shape string
	// request builds the write. binding "" sends none; page makes it the
	// request a rendered page sends (HTMX), otherwise a JSON API client's.
	request func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request
	state   func(t *testing.T, database *gorm.DB, userID uint) string
}

func settingsWriteCases(user models.User) []settingsWriteCase {
	alternativeGoal := services.AlternativeUsageGoals(user.UsageGoal)[0]
	cycleState := func(t *testing.T, database *gorm.DB, userID uint) string {
		t.Helper()
		stored := models.User{}
		if err := database.Select("cycle_length", "period_length", "usage_goal").First(&stored, userID).Error; err != nil {
			t.Fatalf("load cycle settings of user %d: %v", userID, err)
		}
		return fmt.Sprintf("cycle_length=%d period_length=%d usage_goal=%s", stored.CycleLength, stored.PeriodLength, stored.UsageGoal)
	}
	return []settingsWriteCase{
		{
			route: cycleSettingsRouteName,
			shape: "the settings cycle form",
			request: func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request {
				cycleLength := fmt.Sprintf("%d", 30+seq)
				if !page {
					request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(`{"cycle_length":`+cycleLength+`,"period_length":6}`))
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Accept", "application/json")
					return withSettingsWriteSession(request, cookie, binding)
				}
				form := url.Values{"cycle_length": {cycleLength}, "period_length": {"6"}}
				if binding != "" {
					form.Set(dayFormAccountWireName, binding)
				}
				request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("HX-Request", "true")
				return withSettingsWriteSession(request, cookie, "")
			},
			state: cycleState,
		},
		{
			route: cycleSettingsRouteName,
			shape: "the dashboard usage-goal switch",
			request: func(t *testing.T, cookie, binding string, page bool, _ int) *http.Request {
				if !page {
					request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(`{"usage_goal":"`+alternativeGoal+`"}`))
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Accept", "application/json")
					return withSettingsWriteSession(request, cookie, binding)
				}
				form := url.Values{"_method": {"PATCH"}, "usage_goal": {alternativeGoal}}
				if binding != "" {
					form.Set(dayFormAccountWireName, binding)
				}
				request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle?source=dashboard", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("HX-Request", "true")
				return withSettingsWriteSession(request, cookie, "")
			},
			state: cycleState,
		},
		{
			route: importJSONRouteName,
			shape: "the settings restore",
			request: func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request {
				body := fmt.Sprintf(`{"entries":[{"date":"2026-03-%02d","period":true,"flow":"medium","cycle_factors":[]}]}`, 1+seq)
				request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/json", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				if page {
					request.Header.Set("HX-Request", "true")
				} else {
					request.Header.Set("Accept", "application/json")
				}
				return withSettingsWriteSession(request, cookie, binding)
			},
			state: func(t *testing.T, database *gorm.DB, userID uint) string {
				t.Helper()
				var rows int64
				if err := database.Model(&models.DailyLog{}).Where("user_id = ?", userID).Count(&rows).Error; err != nil {
					t.Fatalf("count days of user %d: %v", userID, err)
				}
				return fmt.Sprintf("days=%d", rows)
			},
		},
	}
}

// withSettingsWriteSession sets the session cookie and, when binding is not
// empty, the binding header.
func withSettingsWriteSession(request *http.Request, cookie, binding string) *http.Request {
	request.Header.Set("Cookie", cookie)
	if binding != "" {
		request.Header.Set(dayFormAccountWireHeader, binding)
	}
	return request
}

// boundSettingsWriteRoutes resolves the bound settings write routes from the
// router itself, by method and path, so a route moved to another verb or path
// fails here rather than leaving the cases below aimed at nothing.
func boundSettingsWriteRoutes(t *testing.T, app *fiber.App) map[string]bool {
	t.Helper()
	registered := map[string]bool{}
	for _, route := range app.GetRoutes(true) {
		registered[route.Method+" "+route.Path] = true
	}
	for _, required := range []string{cycleSettingsRouteName, importJSONRouteName} {
		if !registered[required] {
			t.Fatalf("the router no longer registers %s", required)
		}
	}
	return registered
}

// TestEverySettingsWriteRouteRefusesAPageFromAnotherAccount drives the
// stale-tab hazard on the settings writes a page hosts: the cycle settings
// form, the dashboard's usage-goal switch and the restore. A page rendered for
// the first account, used after the second signed in in another tab, must
// leave the second account's settings and days untouched; a page that sends no
// binding at all is refused the same way. Positive control: the same request
// under the second account's own binding reaches the handler and changes it.
func TestEverySettingsWriteRouteRefusesAPageFromAnotherAccount(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "settings-write-routes")
	registered := boundSettingsWriteRoutes(t, fixture.app)
	covered := map[string]bool{}

	for index, write := range settingsWriteCases(fixture.second) {
		if !registered[write.route] {
			t.Fatalf("%s: %s is not a registered route", write.shape, write.route)
		}
		covered[write.route] = true
		label := write.route + " (" + write.shape + ")"
		before := write.state(t, fixture.database, fixture.second.ID)

		response := mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, fixture.firstBinding, true, 2*index))
		assertSettingsAccountChangedRefusal(t, label+" with a binding of another account", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: a page of another account changed the signed-in account: %s -> %s", label, before, after)
		}

		response = mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, "", true, 2*index))
		assertSettingsAccountChangedRefusal(t, label+" with no binding", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: a page without a binding changed the signed-in account: %s -> %s", label, before, after)
		}

		// Any refusal before the handler — this guard, the owner guard —
		// answers 4xx and leaves the state as it was, so both are asserted.
		response = mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, fixture.secondBinding, true, 2*index+1))
		if body := mustReadBodyString(t, response.Body); response.StatusCode >= 400 {
			t.Fatalf("%s: the account's own binding must reach the handler and succeed, got %d %q", label, response.StatusCode, body)
		}
		if after := write.state(t, fixture.database, fixture.second.ID); after == before {
			t.Fatalf("%s: the account's own binding must reach the handler, but the state is unchanged (%s)", label, after)
		}
	}
	for _, required := range []string{cycleSettingsRouteName, importJSONRouteName} {
		if !covered[required] {
			t.Fatalf("no case drives %s", required)
		}
	}
}

// TestSettingsWriteRoutesLetOnlyAClientWithoutAPageOmitTheBinding pins the
// other half of the rule: a JSON API client renders no page and may omit the
// binding, and its write lands; a binding it does send is still verified.
func TestSettingsWriteRoutesLetOnlyAClientWithoutAPageOmitTheBinding(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "settings-write-json")
	boundSettingsWriteRoutes(t, fixture.app)

	for index, write := range settingsWriteCases(fixture.second) {
		label := write.route + " (" + write.shape + ", JSON client)"
		before := write.state(t, fixture.database, fixture.second.ID)

		response := mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, fixture.firstBinding, false, 2*index))
		assertSettingsAccountChangedRefusal(t, label+" with a binding of another account", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: a binding of another account changed the signed-in account: %s -> %s", label, before, after)
		}

		// The restore script sends the header even when its page carries no
		// binding; an empty value is a binding that names no account.
		emptyHeader := write.request(t, fixture.secondCookie, "", false, 2*index)
		emptyHeader.Header.Set(dayFormAccountWireHeader, "")
		response = mustAppResponse(t, fixture.app, emptyHeader)
		assertSettingsAccountChangedRefusal(t, label+" with an empty binding header", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: an empty binding header changed the signed-in account: %s -> %s", label, before, after)
		}

		response = mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, "", false, 2*index+1))
		if body := mustReadBodyString(t, response.Body); response.StatusCode >= 300 {
			t.Fatalf("%s: a JSON client without the binding must keep its behaviour, got %d %q", label, response.StatusCode, body)
		}
		if after := write.state(t, fixture.database, fixture.second.ID); after == before {
			t.Fatalf("%s: a JSON client without the binding must reach the handler, but the state is unchanged (%s)", label, after)
		}
	}
}

// TestEveryRenderedSettingsWriteCarriesTheAccountBinding walks the markup of
// every page hosting a bound settings write and requires each element that
// addresses /api/v1/users/current/cycle to carry the rendering account's
// binding in its form, and the restore section — whose script sends the
// export through fetch — to carry it for the script to send.
func TestEveryRenderedSettingsWriteCarriesTheAccountBinding(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "settings-write-markup")
	found := map[string]int{}
	for _, path := range []string{"/dashboard", "/settings"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Cookie", fixture.firstCookie)
		response := mustAppResponse(t, fixture.app, request)
		assertStatusCode(t, response, http.StatusOK)
		document, err := html.Parse(strings.NewReader(mustReadBodyString(t, response.Body)))
		if err != nil {
			t.Fatalf("%s: parse page: %v", path, err)
		}
		walkSettingsWriteElements(document, nil, func(element, form *html.Node, target string) {
			found[settingsWriteElementHook(element)]++
			if got := dayWriteElementBinding(t, element, form); got != fixture.firstBinding {
				t.Errorf("%s: <%s target=%q> carries account binding %q, want the rendering account's", path, element.Data, target, got)
			}
		})
		walkElementsWithAttribute(document, "data-import-section", func(element *html.Node) {
			found["data-import-section"]++
			if got := htmlAttributeValue(element, "data-import-account"); got != fixture.firstBinding {
				t.Errorf("%s: the restore section carries account binding %q, want the rendering account's", path, got)
			}
		})
	}
	for _, hook := range []string{"data-usage-goal-quick-switch-form", "data-settings-draft-form", "data-import-section"} {
		if found[hook] == 0 {
			t.Fatalf("the markup walk found no %s element — it resolved %v", hook, found)
		}
	}
}

func walkSettingsWriteElements(node, form *html.Node, visit func(element, form *html.Node, target string)) {
	if node.Type == html.ElementNode {
		if node.Data == "form" {
			form = node
		}
		for _, attribute := range node.Attr {
			path, _, _ := strings.Cut(attribute.Val, "?")
			if (attribute.Key == "action" || strings.HasPrefix(attribute.Key, "hx-")) && path == "/api/v1/users/current/cycle" {
				visit(node, form, attribute.Val)
				break
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkSettingsWriteElements(child, form, visit)
	}
}

func settingsWriteElementHook(element *html.Node) string {
	for _, attribute := range element.Attr {
		switch attribute.Key {
		case "data-usage-goal-quick-switch-form", "data-settings-draft-form":
			return attribute.Key
		}
	}
	return element.Data
}

func walkElementsWithAttribute(node *html.Node, name string, visit func(element *html.Node)) {
	if node.Type == html.ElementNode {
		for _, attribute := range node.Attr {
			if attribute.Key == name {
				visit(node)
				break
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkElementsWithAttribute(child, name, visit)
	}
}

func htmlAttributeValue(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
