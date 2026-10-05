package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The dashboard journal saves a pregnancy-test result by itself and never
// reloads the page; the browser then asks the dashboard for its status header
// again and swaps that block in. This pins the server half of that exchange:
// the page rendered right after the save carries every block the swap relies
// on — the header with the fertility claim withdrawn (or restored), and the
// field with its Remove action (or its empty wording) — so the refreshed header
// and the in-place field can never disagree with what was saved.
func TestDashboardRenderedAfterAPregnancyTestSaveCarriesTheUpdatedBlocks(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-pregnancy-refresh@example.com", "StrongPass1", true)
	// Cycle day 12 of a 28-day account: inside the fertile window.
	dashboardSuppressionSeed(t, database, user.ID, nil)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	type blocks struct {
		fertility   string
		removeShown bool
		emptyShown  bool
	}
	render := func() blocks {
		t.Helper()
		document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
		header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
		if header == nil {
			t.Fatal("expected the dashboard status header")
		}
		field := pregnancyTestField(t, document)
		return blocks{
			fertility:   htmlAttr(header, "data-fertility-status"),
			removeShown: pregnancyTestShowsHook(field, "data-pregnancy-test-remove"),
			emptyShown:  pregnancyTestShowsHook(field, "data-pregnancy-test-empty"),
		}
	}
	assertBlocks := func(stage string, got blocks, want blocks) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %+v, want %+v", stage, got, want)
		}
	}

	assertBlocks("baseline", render(), blocks{fertility: "fertile", emptyShown: true})

	todayKey := services.DateAtLocation(time.Now().UTC(), time.UTC).Format("2006-01-02")
	save := func(result string) {
		t.Helper()
		form := url.Values{"flow": {models.FlowNone}, "pregnancy_test": {result}}
		request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+todayKey, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", authCookie)
		bindDayWriteForTest(t, request, user.ID)
		assertStatusCode(t, mustAppResponse(t, app, request), http.StatusOK)
	}

	save(models.PregnancyTestPositive)
	assertBlocks("after a positive result", render(), blocks{fertility: "unknown", removeShown: true})

	save(models.PregnancyTestNone)
	assertBlocks("after removing the result", render(), blocks{fertility: "fertile", emptyShown: true})
}

// The swap inserts a block that appears after the nearest block before it, so
// it is only correct while every ordered block is a direct child of the status
// header. The blocks are read from the script's own list, so one added later is
// checked too; a hook the dashboard does not render in this state is skipped,
// and the ribbon and the disclaimer are always rendered, which keeps the walk
// from passing over nothing.
func TestDashboardPregnancyRefreshOrderedBlocksShareTheStatusHeaderParent(t *testing.T) {
	source, err := os.ReadFile("../../web/src/js/app/53-dashboard-autosave.js")
	if err != nil {
		t.Fatalf("read the dashboard autosave script: %v", err)
	}
	list := regexp.MustCompile(`(?s)DASHBOARD_PREGNANCY_ORDERED = \[(.*?)\];`).FindSubmatch(source)
	if list == nil {
		t.Fatal("the ordered block list is not found in the script")
	}
	hooks := regexp.MustCompile(`selector: "\[(data-[a-z-]+)\]"`).FindAllSubmatch(list[1], -1)
	if len(hooks) < 2 {
		t.Fatalf("expected the ordered block list to name its hooks, got %d", len(hooks))
	}

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-pregnancy-parent@example.com", "StrongPass1", true)
	dashboardSuppressionSeed(t, database, user.ID, nil)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected the dashboard status header")
	}
	found := map[string]bool{}
	for _, hook := range hooks {
		name := string(hook[1])
		node := dashboardElementByDataAttr(header, name)
		if node == nil {
			continue
		}
		found[name] = true
		if node.Parent != header {
			t.Errorf("%s is not a direct child of the status header; the swap's insertion assumes it", name)
		}
	}
	for _, name := range []string{"data-dashboard-cycle-ribbon", "data-dashboard-prediction-disclaimer"} {
		if !found[name] {
			t.Errorf("%s was not found in the rendered header; the check would be vacuous", name)
		}
	}
}

// The status line is the live region that announces the paused or resumed
// predictions: it must exist, marked, in the first paint, because a region
// inserted later is not announced.
func TestDashboardStatusLineIsALiveRegionFromFirstPaint(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-status-live@example.com", "StrongPass1", true)
	dashboardSuppressionSeed(t, database, user.ID, nil)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
	line := dashboardElementByDataAttr(document, "data-dashboard-status-line")
	if line == nil {
		t.Fatal("expected the status line")
	}
	if got := htmlAttr(line, "aria-live"); got != "polite" {
		t.Fatalf("status line aria-live = %q, want polite", got)
	}
}
