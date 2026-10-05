package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// These tests pin the accepted calendar range of the {date} path parameter,
// 1900-01-01 through 9999-12-30, on the real routes. A date outside it answers
// the same 400 "invalid date" as a malformed one; before the range existed,
// GET /api/v1/days/0001-01-01 answered 200 with an empty "date" (year 1 is Go's
// zero time, which the day reader treats as unset) and 9999-12-31 built a read
// range ending in year 10000, which cannot be encoded or ordered as text.

const invalidDateKey = "invalid date"

func TestGetDayAcceptsOnlyTheDocumentedDateRange(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "day-date-range-get@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	cases := []struct {
		date     string
		accepted bool
	}{
		{date: "0001-01-01", accepted: false},
		{date: "1899-12-31", accepted: false},
		{date: "1900-01-01", accepted: true},
		{date: "9999-12-30", accepted: true},
		{date: "9999-12-31", accepted: false},
	}

	// The bound is on the calendar date, so a zone at either end of the offset
	// range moves neither edge. Asia/Tokyo and Pacific/Auckland had a positive
	// offset in 1900, which is what would pull 1900-01-01 before the bound if the
	// comparison ran on the resolved instant; the other two had negative ones.
	for _, timezone := range []string{"", "Asia/Tokyo", "Pacific/Auckland", "Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		for _, tc := range cases {
			path := "/api/v1/days/" + tc.date
			status, raw := sendDayRequest(t, app, http.MethodGet, path, authCookie, timezone, "")
			if !tc.accepted {
				if status != http.StatusBadRequest {
					t.Errorf("GET %s (tz %q): status %d, want 400 (%s)", path, timezone, status, raw)
					continue
				}
				var body struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(raw, &body); err != nil || body.Error != invalidDateKey {
					t.Errorf("GET %s (tz %q): body %s, want the %q error", path, timezone, raw, invalidDateKey)
				}
				continue
			}
			if status != http.StatusOK {
				t.Errorf("GET %s (tz %q): status %d, want 200 (%s)", path, timezone, status, raw)
				continue
			}
			var day dayResponse
			if err := json.Unmarshal(raw, &day); err != nil {
				t.Errorf("GET %s (tz %q): decode: %v (%s)", path, timezone, err, raw)
				continue
			}
			if day.Date != tc.date {
				t.Errorf("GET %s (tz %q): date %q, want %q", path, timezone, day.Date, tc.date)
			}
		}
	}
}

func TestDayRoutesRefuseAnOutOfRangeDate(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "day-date-range-routes@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Every route that carries the :date parameter, keyed "METHOD path".
	requests := map[string]string{
		"HEAD /api/v1/days/:date":             "",
		"GET /api/v1/days/:date":              "",
		"PUT /api/v1/days/:date":              `{}`,
		"PATCH /api/v1/days/:date":            `{}`,
		"DELETE /api/v1/days/:date":           "",
		"POST /api/v1/days/:date/cycle-start": "",
		"GET /calendar/day/:date":             "",
	}

	registered := map[string]struct{}{}
	for _, route := range app.GetRoutes() {
		if !strings.Contains(route.Path, ":date") {
			continue
		}
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	// Fiber answers HEAD for every GET route without a route of its own; the two
	// day HEAD/GET pairs are explicit here, and the calendar panel's HEAD twin is
	// the same handler, so only the routes the table can request are compared.
	var missing []string
	for key := range registered {
		if _, listed := requests[key]; !listed && !strings.HasPrefix(key, "HEAD /calendar/") {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("routes with a :date parameter that this test does not exercise: %v", missing)
	}
	for key := range requests {
		if _, exists := registered[key]; !exists {
			t.Fatalf("test lists %q, which is not a registered route", key)
		}
	}

	for _, date := range []string{"0001-01-01", "1899-12-31", "9999-12-31"} {
		for key, body := range requests {
			method, pattern, _ := strings.Cut(key, " ")
			path := strings.Replace(pattern, ":date", date, 1)
			status, raw := sendDayRequest(t, app, method, path, authCookie, "", body)
			if status != http.StatusBadRequest {
				t.Errorf("%s %s: status %d, want 400 (%s)", method, path, status, raw)
				continue
			}
			// A HEAD answer has no body to read; its status is the whole contract.
			if method == http.MethodHead {
				continue
			}
			// The status alone is shared with other refusals of the same route (a
			// cycle-start answers 400 for its own reasons), so the key must name
			// the date.
			var envelope struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Error != invalidDateKey {
				t.Errorf("%s %s: body %s, want the %q error", method, path, raw, invalidDateKey)
			}
		}
	}

	var stored int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&stored).Error; err != nil {
		t.Fatalf("count day rows: %v", err)
	}
	if stored != 0 {
		t.Fatalf("refused dates stored %d day rows, want none", stored)
	}
}

func TestPutDayStoresTheLastAcceptedDate(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "day-date-range-put@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, date := range []string{"1900-01-01", "9999-12-30"} {
		path := "/api/v1/days/" + date
		status, raw := sendDayRequest(t, app, http.MethodPut, path, authCookie, "", `{"is_period":true}`)
		if status != http.StatusOK {
			t.Fatalf("PUT %s: status %d, want 200 (%s)", path, status, raw)
		}
		day := getDayForTest(t, app, path, authCookie, "")
		if day.ID == 0 || day.Date != date || !day.IsPeriod {
			t.Errorf("GET %s after PUT: id %d date %q is_period %t, want the stored period day", path, day.ID, day.Date, day.IsPeriod)
		}
	}
}

// The refused bounds of the list route are pinned beside the spec's own 400 keys
// (TestOpenAPIDayRangeDeclaresTheRefusalsItAnswers); this is the accepted edge.
func TestListDaysAcceptsTheWholeDocumentedRange(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "day-date-range-list@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	status, raw := sendDayRequest(t, app, http.MethodGet, "/api/v1/days?from=1900-01-01&to=9999-12-30", authCookie, "", "")
	if status != http.StatusOK {
		t.Errorf("GET days over the whole accepted range: status %d, want 200 (%s)", status, raw)
	}
}
