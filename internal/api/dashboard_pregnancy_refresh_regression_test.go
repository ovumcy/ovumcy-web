package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
		assertStatusCode(t, mustAppResponse(t, app, request), http.StatusOK)
	}

	save(models.PregnancyTestPositive)
	assertBlocks("after a positive result", render(), blocks{fertility: "unknown", removeShown: true})

	save(models.PregnancyTestNone)
	assertBlocks("after removing the result", render(), blocks{fertility: "fertile", emptyShown: true})
}
