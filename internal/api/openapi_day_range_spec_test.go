package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

const dayRangeSpecPath = "/api/v1/days"

// dayRangeSpecQueryRequired reports whether the operation's parameter list
// declares the named query parameter with `required: true`. A `$ref` item is
// not followed: the shared FromQuery/ToQuery components are declared optional
// for the export operations that also use them.
func dayRangeSpecQueryRequired(parameters []string, name string) bool {
	var items [][]string
	for _, line := range parameters {
		if strings.HasPrefix(line, "- ") {
			items = append(items, []string{strings.TrimPrefix(line, "- ")})
			continue
		}
		if len(items) > 0 {
			items[len(items)-1] = append(items[len(items)-1], line)
		}
	}
	for _, item := range items {
		named, inQuery, required := false, false, false
		for _, line := range item {
			switch line {
			case "name: " + name:
				named = true
			case "in: query":
				inQuery = true
			case "required: true":
				required = true
			}
		}
		if named && inQuery {
			return required
		}
	}
	return false
}

// yamlScalarText reads a one-line YAML scalar in any of its three styles, so an
// example key written without quotes is compared as the key, not misreported.
func yamlScalarText(raw string) string {
	value := strings.TrimSpace(raw)
	if unquoted, err := strconv.Unquote(value); err == nil && strings.HasPrefix(value, `"`) {
		return unquoted
	}
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value
}

// TestOpenAPIDayRangeDeclaresTheRefusalsItAnswers drives the real GET
// /api/v1/days route with every shape of bad range and holds the spec to what
// it answered: both bounds declared required, and the 400 declaring exactly the
// keys the handler emits — no more, no fewer.
func TestOpenAPIDayRangeDeclaresTheRefusalsItAnswers(t *testing.T) {
	const apia = "Pacific/Apia"
	if _, err := time.LoadLocation(apia); err != nil {
		t.Fatalf("zoneinfo for %s unavailable: %v", apia, err)
	}

	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	spec := string(data)
	parameters := openAPIYAMLBlock(t, spec, "paths", dayRangeSpecPath, "get", "parameters")
	badRequest := openAPIYAMLBlock(t, spec, "paths", dayRangeSpecPath, "get", "responses", "'400'")

	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "day-range-spec@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	type answer struct {
		status      int
		contentType string
		body        []byte
	}
	send := func(query string, zone string, headers map[string]string) answer {
		target := dayRangeSpecPath
		if query != "" {
			target += "?" + query
		}
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Accept", fiber.MIMEApplicationJSON)
		cookie := authCookie
		if zone != "" {
			cookie = joinCookieHeader(authCookie, timezoneCookieName+"="+zone)
			request.Header.Set(timezoneHeaderName, zone)
		}
		request.Header.Set("Cookie", cookie)
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response := mustAppResponse(t, app, request)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("GET %s: read body: %v", target, err)
		}
		return answer{status: response.StatusCode, contentType: response.Header.Get(fiber.HeaderContentType), body: body}
	}

	// The edge the spec calls legal: equal bounds are one inclusive day, so a
	// row on that day is listed. Surrounding whitespace is trimmed, not refused.
	seeded := models.DailyLog{UserID: user.ID, Date: time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowLight}
	if err := database.Create(&seeded).Error; err != nil {
		t.Fatalf("seed day: %v", err)
	}
	for _, query := range []string{"from=2026-01-10&to=2026-01-10", "from=%202026-01-10%20&to=2026-01-10%20"} {
		got := send(query, "", nil)
		var listed []dayResponse
		if got.status != http.StatusOK || json.Unmarshal(got.body, &listed) != nil || len(listed) != 1 || listed[0].Date != "2026-01-10" {
			t.Fatalf("GET %s?%s answered %d (%s), want 200 listing the one seeded day: both bounds are inclusive",
				dayRangeSpecPath, query, got.status, got.body)
		}
	}

	for _, name := range []string{"from", "to"} {
		if !dayRangeSpecQueryRequired(parameters, name) {
			t.Errorf("GET %s: the spec does not declare query parameter %q `required: true`, yet the handler refuses a request without it:\n  %s",
				dayRangeSpecPath, name, strings.Join(parameters, "\n  "))
		}
	}

	cases := []struct {
		name  string
		query string
		zone  string
		key   string
	}{
		{name: "no bounds at all", query: "", key: "invalid from date"},
		{name: "from omitted", query: "to=2026-01-10", key: "invalid from date"},
		{name: "from blank", query: "from=%20%20&to=2026-01-10", key: "invalid from date"},
		{name: "from not YYYY-MM-DD", query: "from=2026-1-01&to=2026-01-10", key: "invalid from date"},
		{name: "from an impossible date", query: "from=2026-02-30&to=2026-03-10", key: "invalid from date"},
		{name: "from a day the request zone never had", query: "from=2011-12-30&to=2012-01-05", zone: apia, key: "invalid from date"},
		{name: "both bounds wrong", query: "from=nope&to=nope", key: "invalid from date"},
		{name: "to omitted", query: "from=2026-01-01", key: "invalid to date"},
		{name: "to not YYYY-MM-DD", query: "from=2026-01-01&to=20260110", key: "invalid to date"},
		{name: "to an impossible date", query: "from=2026-02-01&to=2026-02-30", key: "invalid to date"},
		{name: "to a day the request zone never had", query: "from=2011-12-25&to=2011-12-30", zone: apia, key: "invalid to date"},
		{name: "to before from", query: "from=2026-01-10&to=2026-01-09", key: "invalid range"},
	}
	emitted := map[string]bool{}
	for _, tc := range cases {
		got := send(tc.query, tc.zone, nil)
		where := fmt.Sprintf("GET %s (%s)", dayRangeSpecPath, tc.name)
		if got.status != http.StatusBadRequest {
			t.Errorf("%s: answered %d (%s), want 400", where, got.status, got.body)
			continue
		}
		if !strings.HasPrefix(got.contentType, fiber.MIMEApplicationJSON) {
			t.Errorf("%s: answered 400 as %q, not application/json", where, got.contentType)
			continue
		}
		var envelope struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Key      string `json:"key"`
				Category string `json:"category"`
				Target   string `json:"target"`
			} `json:"error_detail"`
		}
		if err := json.Unmarshal(got.body, &envelope); err != nil {
			t.Errorf("%s: 400 body is not JSON: %v", where, err)
			continue
		}
		detail := envelope.ErrorDetail
		if detail.Key != tc.key || envelope.Error != detail.Key {
			t.Errorf("%s: answered key %q (error %q), want %q", where, detail.Key, envelope.Error, tc.key)
		}
		emitted[detail.Key] = true
		requireSpecLine(t, badRequest, fmt.Sprintf("error: %q", detail.Key), where)
		requireSpecLine(t, badRequest,
			fmt.Sprintf("error_detail: { key: %q, category: %q, target: %q }", detail.Key, detail.Category, detail.Target), where)
	}
	requireSpecLine(t, badRequest, "schema: { $ref: '#/components/schemas/ApiError' }", "GET /api/v1/days 400")
	requireSpecMentions(t, badRequest, "`from` is checked first", "GET /api/v1/days 400")

	var declared []string
	for _, line := range badRequest {
		if key, ok := strings.CutPrefix(line, "error: "); ok {
			declared = append(declared, yamlScalarText(key))
		}
	}
	sort.Strings(declared)
	for _, key := range declared {
		if !emitted[key] {
			t.Errorf("GET %s 400: the spec declares key %q, which no refused range answered", dayRangeSpecPath, key)
		}
	}

	// HTMX outranks Accept: the same refusal as the status fragment.
	got := send("from=2026-01-10&to=2026-01-09", "", map[string]string{"HX-Request": "true"})
	if got.status != http.StatusBadRequest || !strings.HasPrefix(got.contentType, fiber.MIMETextHTML) || json.Valid(got.body) {
		t.Errorf("GET %s (HTMX): answered %d as %q (%q), want 400 as the text/html status fragment",
			dayRangeSpecPath, got.status, got.contentType, got.body)
	}
	requireSpecMentions(t, badRequest, "(`HX-Request: true`) gets the localized status fragment as `text/html`", "GET /api/v1/days 400")
}
