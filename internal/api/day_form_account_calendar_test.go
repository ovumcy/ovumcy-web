package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// nearestHXHeaders returns the element whose hx-headers htmx would apply to a
// request issued by node: node itself or the closest ancestor declaring one.
func nearestHXHeaders(node *html.Node) *html.Node {
	for current := node; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && htmlHasAttr(current, "hx-headers") {
			return current
		}
	}
	return nil
}

func htmlIsWithin(node, ancestor *html.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current == ancestor {
			return true
		}
	}
	return false
}

// TestCalendarPageCarriesItsAccountBindingToEveryRequestFromInside pins the
// page-side half of the stale-tab defence. The calendar renders no day write: its
// editor is fetched, and the binding the fetched form carries is minted at
// fetch time for whoever is signed in then. Only a binding declared by the page
// itself, on every request made from inside it, can tell the fetch apart from a
// page rendered for the account that is now signed in.
func TestCalendarPageCarriesItsAccountBindingToEveryRequestFromInside(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "calendar-host")
	today := time.Now().In(time.UTC).Format("2006-01-02")
	request := httptest.NewRequest(http.MethodGet, "/calendar?day="+today, nil)
	request.Header.Set("Cookie", fixture.firstCookie)
	response := mustAppResponse(t, fixture.app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	view := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-calendar-view")
	})
	if view == nil {
		t.Fatal("the calendar page has no data-calendar-view element")
	}
	headers := map[string]string{}
	if err := json.Unmarshal([]byte(htmlAttr(view, "hx-headers")), &headers); err != nil {
		t.Fatalf("hx-headers %q on the calendar view is not a JSON object of strings: %v", htmlAttr(view, "hx-headers"), err)
	}
	if got := headers[dayFormAccountWireHeader]; got != fixture.firstBinding {
		t.Fatalf("the calendar view sends %s=%q, want the rendering account's binding %q", dayFormAccountWireHeader, got, fixture.firstBinding)
	}

	panel := htmlElementByID(document, "calendar-grid-panel")
	refresh := htmlElementByID(document, "calendar-grid-refresh")
	editor := htmlElementByID(document, "day-editor")
	if editor == nil {
		t.Fatal("the calendar page has no #day-editor")
	}
	dayButton := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "button" && htmlHasAttr(node, "data-day") && strings.HasPrefix(htmlAttr(node, "hx-get"), "/calendar/day/")
	})
	loader := htmlFindElement(editor, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "hx-trigger") == "load" && strings.HasPrefix(htmlAttr(node, "hx-get"), "/calendar/day/")
	})
	for name, origin := range map[string]*html.Node{
		"the grid panel":    panel,
		"the grid refresh":  refresh,
		"the day editor":    editor,
		"a day button":      dayButton,
		"the editor loader": loader,
	} {
		if origin == nil {
			t.Fatalf("%s was not found on the calendar page", name)
		}
		if got := nearestHXHeaders(origin); got != view {
			t.Errorf("requests from %s do not inherit the calendar view's account header (nearest hx-headers: %v)", name, got != nil)
		}
	}
	// The grid refresh swaps the panel (hx-select/hx-target), and the editor is
	// replaced by every fetch: the header must live outside both.
	for name, swapped := range map[string]*html.Node{"the grid panel": panel, "the day editor": editor, "the grid refresh": refresh} {
		if htmlIsWithin(view, swapped) {
			t.Errorf("the account header sits inside %s, which is swapped", name)
		}
	}
}

// TestStaleCalendarPageCannotReadOrWriteAnotherAccountsDay drives the hazard on
// the calendar, where the editor is fetched: a tab rendered for the first
// account is used after the second signed in in another tab of the browser. The
// fetch and the grid refresh are refused rather than answered with the second
// account's day, and a write from a fetched editor carries the page's binding
// beside the form's, so the two disagree and nothing is written.
func TestStaleCalendarPageCannotReadOrWriteAnotherAccountsDay(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "stale-calendar")
	day := time.Date(2026, time.June, 8, 0, 0, 0, 0, time.UTC)
	const privateNote = "private note of the second account"
	seedDayForTest(t, fixture.database, fixture.second.ID, day, 2, privateNote)
	dayPath := "/calendar/day/" + day.Format("2006-01-02")

	get := func(t *testing.T, path, cookie string, header []string, htmx bool) *http.Response {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Cookie", cookie)
		if htmx {
			request.Header.Set("HX-Request", "true")
		}
		for _, value := range header {
			request.Header.Add(dayFormAccountWireHeader, value)
		}
		return mustAppResponse(t, fixture.app, request)
	}

	t.Run("a fetch or grid refresh from a page of another account is refused and shows nothing", func(t *testing.T) {
		for _, path := range []string{dayPath, dayPath + "?mode=edit", "/calendar?month=2026-06"} {
			body := assertAccountChangedRefusal(t, "GET "+path, get(t, path, fixture.secondCookie, []string{fixture.firstBinding}, true))
			if strings.Contains(body, privateNote) {
				t.Fatalf("GET %s: the refusal leaks the second account's day: %q", path, body)
			}
			// A copy that is valid beside a copy that is not cannot mask it.
			assertAccountChangedRefusal(t, "GET "+path+" with two copies", get(t, path, fixture.secondCookie, []string{fixture.secondBinding, fixture.firstBinding}, true))
		}
	})

	t.Run("the page of the account that is signed in is answered", func(t *testing.T) {
		response := get(t, dayPath+"?mode=edit", fixture.secondCookie, []string{fixture.secondBinding}, true)
		assertStatusCode(t, response, http.StatusOK)
		if body := mustReadBodyString(t, response.Body); !strings.Contains(body, privateNote) {
			t.Fatalf("the second account's own page must read its own day, got %q", body)
		}
		assertStatusCode(t, get(t, "/calendar?month=2026-06", fixture.firstCookie, []string{fixture.firstBinding}, true), http.StatusOK)
	})

	t.Run("an HTMX read that names no account is refused, a direct navigation is answered", func(t *testing.T) {
		assertAccountChangedRefusal(t, "HTMX GET "+dayPath, get(t, dayPath, fixture.secondCookie, nil, true))
		assertAccountChangedRefusal(t, "HTMX GET /calendar", get(t, "/calendar", fixture.secondCookie, nil, true))
		// Opened by URL, or by the no-JavaScript form: the response renders its
		// own data and its own binding for the account signed in now.
		assertStatusCode(t, get(t, dayPath+"?mode=edit", fixture.secondCookie, nil, false), http.StatusOK)
		assertStatusCode(t, get(t, "/calendar", fixture.secondCookie, nil, false), http.StatusOK)
	})

	t.Run("a write from an editor fetched under the second account, sent by the first account's page, writes nothing", func(t *testing.T) {
		// The fetch as the pre-fix server answered it: the second account's day
		// with the second account's binding in the form.
		fetched := renderedDayFormAccount(t, fixture.app, fixture.secondCookie, dayPath+"?mode=edit")
		if fetched != fixture.secondBinding {
			t.Fatalf("the fetched editor must carry the signed-in account's binding, got %q", fetched)
		}
		for index, route := range dayWriteRoutes(t, fixture.app) {
			writeDay := day.AddDate(0, 0, 10+index)
			seedDayForTest(t, fixture.database, fixture.second.ID, writeDay, 2, "second account's own note")
			request := dayWriteRequest(route, writeDay, fixture.secondCookie, url.Values{dayFormAccountWireName: {fetched}}, true)
			request.Header.Set(dayFormAccountWireHeader, fixture.firstBinding)
			assertAccountChangedRefusal(t, route.Method+" "+route.Path+" carrying header of the first account and field of the second", mustAppResponse(t, fixture.app, request))
			assertDayUntouched(t, fixture.database, fixture.second.ID, writeDay, 2, "second account's own note")
		}
	})
}
