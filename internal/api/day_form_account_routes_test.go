package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

const dayFormAccountWireHeader = "X-Ovumcy-Day-Form-Account"

// dayWriteRoutes enumerates every state-changing route under /api/v1/days from
// the router itself, so a write route added later is held to the binding
// without anyone listing it here.
func dayWriteRoutes(t *testing.T, app *fiber.App) []fiber.Route {
	t.Helper()
	var routes []fiber.Route
	seen := map[string]bool{}
	for _, route := range app.GetRoutes(true) {
		switch route.Method {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions, fiber.MethodConnect, fiber.MethodTrace:
			continue
		}
		if !strings.HasPrefix(route.Path, "/api/v1/days") || seen[route.Method+" "+route.Path] {
			continue
		}
		seen[route.Method+" "+route.Path] = true
		routes = append(routes, route)
	}
	for _, required := range []string{"PUT /api/v1/days/:date", "PATCH /api/v1/days/:date", "DELETE /api/v1/days/:date", "POST /api/v1/days/:date/cycle-start"} {
		if !seen[required] {
			t.Fatalf("the router enumeration lost %s — the day write routes resolved to %v", required, seen)
		}
	}
	return routes
}

func dayWriteRequest(route fiber.Route, day time.Time, cookie string, form url.Values, htmx bool) *http.Request {
	path := strings.ReplaceAll(route.Path, ":date", day.Format("2006-01-02"))
	if form == nil {
		form = url.Values{}
	}
	form.Set("flow", models.FlowNone)
	form.Set("mood", "5")
	form.Set("notes", "written from a page rendered for another account")
	request := httptest.NewRequest(route.Method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", cookie)
	if htmx {
		request.Header.Set("HX-Request", "true")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	return request
}

func assertDayUntouched(t *testing.T, database *gorm.DB, userID uint, day time.Time, mood int, notes string) {
	t.Helper()
	entry, err := fetchLogByDateForTest(database, userID, day, time.UTC)
	if err != nil {
		t.Fatalf("load day for user %d: %v", userID, err)
	}
	if entry.Mood != mood || entry.Notes != notes || entry.CycleStart {
		t.Fatalf("user %d on %s must be untouched (mood=%d notes=%q cycle_start=false), got mood=%d notes=%q cycle_start=%t",
			userID, day.Format("2006-01-02"), mood, notes, entry.Mood, entry.Notes, entry.CycleStart)
	}
}

type dayFormAccountFixture struct {
	app                         *fiber.App
	database                    *gorm.DB
	first, second               models.User
	firstCookie, secondCookie   string
	firstBinding, secondBinding string
}

func newDayFormAccountFixture(t *testing.T, prefix string) dayFormAccountFixture {
	t.Helper()
	app, database := newOnboardingTestApp(t)
	first := createOnboardingTestUser(t, database, prefix+"-first@example.com", "StrongPass1", true)
	second := createOnboardingTestUser(t, database, prefix+"-second@example.com", "StrongPass1", true)
	fixture := dayFormAccountFixture{app: app, database: database, first: first, second: second}
	fixture.firstCookie = loginAndExtractAuthCookie(t, app, first.Email, "StrongPass1")
	fixture.secondCookie = loginAndExtractAuthCookie(t, app, second.Email, "StrongPass1")
	fixture.firstBinding = renderedDayFormAccount(t, app, fixture.firstCookie, "/dashboard")
	fixture.secondBinding = renderedDayFormAccount(t, app, fixture.secondCookie, "/dashboard")
	return fixture
}

// TestEveryDayWriteRouteRefusesAPageFromAnotherAccount drives the stale-tab
// hazard on every day write a rendered page can issue — save, delete (the
// dashboard undo, the clear action, the calendar delete form) and cycle-start:
// a page rendered for the first account, used after the second signed in in
// another tab, must leave the second account's day untouched. A page that
// sends no binding at all is refused the same way.
func TestEveryDayWriteRouteRefusesAPageFromAnotherAccount(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "day-write-routes")
	base := time.Date(2026, time.May, 4, 0, 0, 0, 0, time.UTC)

	for index, route := range dayWriteRoutes(t, fixture.app) {
		day := base.AddDate(0, 0, 2*index)
		absentDay := day.AddDate(0, 0, 1)
		seedDayForTest(t, fixture.database, fixture.second.ID, day, 2, "second account's own note")
		seedDayForTest(t, fixture.database, fixture.second.ID, absentDay, 2, "second account's own note")

		name := route.Method + " " + route.Path
		response := mustAppResponse(t, fixture.app, dayWriteRequest(route, day, fixture.secondCookie, url.Values{dayFormAccountWireName: {fixture.firstBinding}}, true))
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("%s: a page rendered for another account must be refused 409, got %d", name, response.StatusCode)
		}
		assertDayUntouched(t, fixture.database, fixture.second.ID, day, 2, "second account's own note")

		response = mustAppResponse(t, fixture.app, dayWriteRequest(route, absentDay, fixture.secondCookie, nil, true))
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("%s: an HTMX write without any binding must be refused 409, got %d", name, response.StatusCode)
		}
		assertDayUntouched(t, fixture.database, fixture.second.ID, absentDay, 2, "second account's own note")
	}
}

// TestDayWriteBindingSourcesAreHeldToOneRule pins how the binding travels: the
// header carries it for a body-less request, every present copy must match
// whichever source it came in, and only a client that renders no page may omit
// it.
func TestDayWriteBindingSourcesAreHeldToOneRule(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "day-write-sources")
	day := time.Date(2026, time.April, 6, 0, 0, 0, 0, time.UTC)
	deleteRequest := func(day time.Time, cookie string, header []string, body url.Values, accept string) *http.Request {
		var reader *strings.Reader
		if body != nil {
			reader = strings.NewReader(body.Encode())
		} else {
			reader = strings.NewReader("")
		}
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/"+day.Format("2006-01-02")+"?source=dashboard", reader)
		if body != nil {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		request.Header.Set("Cookie", cookie)
		request.Header.Set("HX-Request", "true")
		if accept != "" {
			request.Header.Del("HX-Request")
			request.Header.Set("Accept", accept)
		}
		for _, value := range header {
			request.Header.Add(dayFormAccountWireHeader, value)
		}
		return request
	}

	t.Run("the header carries the binding of a body-less delete", func(t *testing.T) {
		seedDayForTest(t, fixture.database, fixture.first.ID, day, 3, "undo me")
		response := mustAppResponse(t, fixture.app, deleteRequest(day, fixture.firstCookie, []string{fixture.firstBinding}, nil, ""))
		if response.StatusCode >= 300 {
			t.Fatalf("a delete carrying its own account's binding in the header must succeed, got %d", response.StatusCode)
		}
		assertStoredDay(t, fixture.database, fixture.first.ID, day, 0, "")
	})

	t.Run("a header naming another account is refused", func(t *testing.T) {
		otherDay := day.AddDate(0, 0, 1)
		seedDayForTest(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
		response := mustAppResponse(t, fixture.app, deleteRequest(otherDay, fixture.secondCookie, []string{fixture.firstBinding}, nil, ""))
		assertStatusCode(t, response, http.StatusConflict)
		assertDayUntouched(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
	})

	t.Run("a valid copy in one source cannot mask a mismatching copy in another", func(t *testing.T) {
		otherDay := day.AddDate(0, 0, 2)
		seedDayForTest(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
		response := mustAppResponse(t, fixture.app, deleteRequest(otherDay, fixture.secondCookie, []string{fixture.secondBinding}, url.Values{"DAY_FORM_ACCOUNT": {fixture.firstBinding}}, ""))
		assertStatusCode(t, response, http.StatusConflict)
		assertDayUntouched(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")

		response = mustAppResponse(t, fixture.app, deleteRequest(otherDay, fixture.secondCookie, []string{fixture.secondBinding, fixture.firstBinding}, nil, ""))
		assertStatusCode(t, response, http.StatusConflict)
		assertDayUntouched(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
	})

	t.Run("a multipart copy is read and compared", func(t *testing.T) {
		otherDay := day.AddDate(0, 0, 3)
		seedDayForTest(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for name, value := range map[string]string{"flow": models.FlowNone, "mood": "5", dayFormAccountWireName: fixture.firstBinding} {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatalf("write multipart field: %v", err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close multipart body: %v", err)
		}
		request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+otherDay.Format("2006-01-02"), &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("Cookie", fixture.secondCookie)
		request.Header.Set("Accept", "application/json")
		response := mustAppResponse(t, fixture.app, request)
		assertStatusCode(t, response, http.StatusConflict)
		assertDayUntouched(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
	})

	t.Run("a plain form post from a page without the binding is refused", func(t *testing.T) {
		otherDay := day.AddDate(0, 0, 4)
		seedDayForTest(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
		request := httptest.NewRequest(http.MethodPost, "/api/v1/days/"+otherDay.Format("2006-01-02")+"?source=calendar", strings.NewReader(url.Values{"_method": {"DELETE"}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "text/html,application/xhtml+xml")
		request.Header.Set("Cookie", fixture.secondCookie)
		response := mustAppResponse(t, fixture.app, request)
		assertStatusCode(t, response, http.StatusConflict)
		assertDayUntouched(t, fixture.database, fixture.second.ID, otherDay, 2, "keep")
	})

	t.Run("a JSON client that renders no page may omit the binding", func(t *testing.T) {
		putDay := day.AddDate(0, 0, 5)
		response := putDayForm(t, fixture.app, fixture.secondCookie, putDay, url.Values{"flow": {models.FlowNone}, "mood": {"3"}, "notes": {"api"}}, false)
		assertStatusCode(t, response, http.StatusOK)
		assertStoredDay(t, fixture.database, fixture.second.ID, putDay, 3, "api")

		request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/"+putDay.Format("2006-01-02"), nil)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", fixture.secondCookie)
		response = mustAppResponse(t, fixture.app, request)
		if response.StatusCode >= 300 {
			t.Fatalf("a JSON delete without the binding must keep its behaviour, got %d", response.StatusCode)
		}
		assertStoredDay(t, fixture.database, fixture.second.ID, putDay, 0, "")
	})
}

// TestEveryRenderedDayWriteCarriesTheAccountBinding walks the markup of every
// page that renders a day write and requires each element addressing a write
// verb at /api/v1/days/* to carry the rendering account's binding — a form
// through its hidden field, a lone control through hx-vals. A call site that
// drops the dict key renders an empty binding, which this refuses here before
// the server refuses it in the browser.
func TestEveryRenderedDayWriteCarriesTheAccountBinding(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "day-write-markup")
	today := time.Now().In(time.UTC)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	seedDayForTest(t, fixture.database, fixture.first.ID, today, 3, "today")
	past := today.AddDate(0, 0, -3)
	seedDayForTest(t, fixture.database, fixture.first.ID, past, 3, "past")
	empty := today.AddDate(0, 0, -6)

	found := map[string]int{}
	for _, path := range []string{
		"/dashboard",
		"/calendar/day/" + past.Format("2006-01-02") + "?mode=edit",
		"/calendar/day/" + past.Format("2006-01-02"),
		"/calendar/day/" + empty.Format("2006-01-02"),
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Cookie", fixture.firstCookie)
		response := mustAppResponse(t, fixture.app, request)
		assertStatusCode(t, response, http.StatusOK)
		document, err := html.Parse(strings.NewReader(mustReadBodyString(t, response.Body)))
		if err != nil {
			t.Fatalf("%s: parse page: %v", path, err)
		}
		walkDayWriteElements(document, nil, func(element, form *html.Node, target string) {
			found[dayWriteElementHook(element)]++
			if got := dayWriteElementBinding(t, element, form); got != fixture.firstBinding {
				t.Errorf("%s: <%s %s=%q> carries account binding %q, want the rendering account's", path, element.Data, "target", target, got)
			}
		})
	}
	for _, hook := range []string{"data-dashboard-save-form", "data-dashboard-clear-button", "data-day-delete-form", "data-cycle-start-confirm-form"} {
		if found[hook] == 0 {
			t.Fatalf("the markup walk found no %s element — it resolved %v", hook, found)
		}
	}
}

var dayWriteAttributes = []string{"action", "hx-put", "hx-delete", "hx-post", "hx-patch"}

func walkDayWriteElements(node, form *html.Node, visit func(element, form *html.Node, target string)) {
	if node.Type == html.ElementNode {
		if node.Data == "form" {
			form = node
		}
		for _, attribute := range node.Attr {
			if slices.Contains(dayWriteAttributes, attribute.Key) && strings.HasPrefix(attribute.Val, "/api/v1/days/") {
				visit(node, form, attribute.Val)
				break
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkDayWriteElements(child, form, visit)
	}
}

func dayWriteElementHook(element *html.Node) string {
	for _, attribute := range element.Attr {
		switch attribute.Key {
		case "data-dashboard-save-form", "data-dashboard-clear-button", "data-day-delete-form", "data-cycle-start-confirm-form", "data-day-save-form":
			return attribute.Key
		}
	}
	return element.Data
}

// dayWriteElementBinding returns the binding an element sends: the hidden
// field of the form it is (or sits in), or else its own hx-vals entry.
func dayWriteElementBinding(t *testing.T, element, form *html.Node) string {
	t.Helper()
	if form != nil {
		if value, ok := hiddenFieldValue(form, dayFormAccountWireName); ok {
			return value
		}
	}
	for _, attribute := range element.Attr {
		if attribute.Key != "hx-vals" {
			continue
		}
		values := map[string]string{}
		if err := json.Unmarshal([]byte(attribute.Val), &values); err != nil {
			t.Fatalf("hx-vals %q is not a JSON object of strings: %v", attribute.Val, err)
		}
		return values[dayFormAccountWireName]
	}
	return ""
}

func hiddenFieldValue(node *html.Node, name string) (string, bool) {
	if node.Type == html.ElementNode && node.Data == "input" {
		var fieldName, value string
		for _, attribute := range node.Attr {
			switch attribute.Key {
			case "name":
				fieldName = attribute.Val
			case "value":
				value = attribute.Val
			}
		}
		if fieldName == name {
			return value, true
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if value, ok := hiddenFieldValue(child, name); ok {
			return value, true
		}
	}
	return "", false
}
