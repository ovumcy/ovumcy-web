package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// The day panel (/calendar/day/:date) is a view of ONE recorded day: its log and
// the cycle-start policy. It was decided to carry no projection — no predicted
// period, fertile window or ovulation day — and none of the verdicts that gate a
// projection (out of date, overdue, suppressed). The service model behind it is
// pinned by name in the services package; these tests pin the two things that
// check cannot see: the fiber.Map the handler hands the template and the HTML
// the template renders, in both bands where those verdicts are live.
//
// The history is three 28-day cycles and a running one from 2026-03-26, so the
// reference length is 28. Cycle day 30 (2026-04-24) is out of date and not yet
// overdue; cycle day 40 (2026-05-04) is past the overdue gate.
var dayPanelProjectionCycleStarts = []string{"2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26"}

// dayPanelProjectionWords are matched case-insensitively, with "-" read as "_",
// against payload keys, HTML attribute names and values, rendered text, and
// translation keys. The overdue copy lives under "late_cycle" keys.
var dayPanelProjectionWords = []string{"predict", "fertil", "ovulat", "stale", "overdue", "late_cycle", "suppress"}

type dayPanelProjectionBand struct {
	name string
	now  time.Time
	// dates are the panels requested: today, the projected start already past
	// due, and the projected start of the cycle chained after it.
	dates []string
}

var dayPanelProjectionBands = []dayPanelProjectionBand{
	{name: "out of date (cycle day 30)", now: time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC), dates: []string{"2026-04-24", "2026-04-23", "2026-05-21"}},
	{name: "overdue (cycle day 40)", now: time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC), dates: []string{"2026-05-04", "2026-04-23", "2026-05-21"}},
}

func dayPanelProjectionLogs(t *testing.T, database *gorm.DB, userID uint) []models.DailyLog {
	t.Helper()
	logs := make([]models.DailyLog, 0, len(dayPanelProjectionCycleStarts))
	for _, raw := range dayPanelProjectionCycleStarts {
		day, err := time.Parse("2006-01-02", raw)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		entry := models.DailyLog{UserID: userID, Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}
		if err := database.Create(&entry).Error; err != nil {
			t.Fatalf("create cycle start %s: %v", raw, err)
		}
		logs = append(logs, entry)
	}
	return logs
}

// assertDayPanelProjectionBand fails unless band's clock puts the fixture in the
// verdict it is named for, so a drift in the gates cannot leave the panel checks
// running against a band where nothing is withheld.
func assertDayPanelProjectionBand(t *testing.T, band dayPanelProjectionBand, user *models.User, logs []models.DailyLog) {
	t.Helper()
	stats := services.BuildCycleStatsFromLogs(user, logs, band.now, time.UTC)
	published, verdict := services.PublishedStats(user, stats, logs, services.DateAtLocation(band.now, time.UTC), time.UTC)
	overdue := slices.Contains(verdict.Reasons, services.SuppressionReasonCycleOverdue)
	if strings.HasPrefix(band.name, "overdue") {
		if !overdue || !verdict.PredictionsSuppressed {
			t.Fatalf("fixture: %s must be suppressed as overdue, verdict %+v", band.name, verdict)
		}
		return
	}
	if !published.CycleDataStale || overdue || verdict.PredictionsSuppressed {
		t.Fatalf("fixture: %s must be out of date without being suppressed, stale %v verdict %+v", band.name, published.CycleDataStale, verdict)
	}
}

func dayPanelNamesProjection(value string) string {
	lower := strings.ReplaceAll(strings.ToLower(value), "-", "_")
	for _, word := range dayPanelProjectionWords {
		if strings.Contains(lower, word) {
			return word
		}
	}
	return ""
}

// TestCalendarDayPanelPayloadCarriesNoProjectionSignal reads the map
// buildDayEditorPartialData returns — the handler's half of the panel, which the
// service-model check does not reach — for every requested day in both bands,
// in view and in edit mode.
func TestCalendarDayPanelPayloadCarriesNoProjectionSignal(t *testing.T) {
	t.Parallel()

	for _, band := range dayPanelProjectionBands {
		t.Run(band.name, func(t *testing.T) {
			t.Parallel()

			_, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "day-panel-payload@example.com", "StrongPass1", true)
			logs := dayPanelProjectionLogs(t, database, user.ID)
			assertDayPanelProjectionBand(t, band, &user, logs)

			manager, err := i18n.NewManager("en")
			if err != nil {
				t.Fatalf("init i18n: %v", err)
			}
			handler, err := NewHandler(testAppSecretKey, time.UTC, manager, false, newTestHandlerDependencies(database, manager))
			if err != nil {
				t.Fatalf("init handler: %v", err)
			}

			for _, raw := range band.dates {
				day, err := services.ParseDayDate(raw, time.UTC)
				if err != nil {
					t.Fatalf("parse %s: %v", raw, err)
				}
				for _, editMode := range []bool{false, true} {
					payload, err := handler.buildDayEditorPartialData(context.Background(), &user, "en", manager.Messages("en"), day, band.now, time.UTC, editMode)
					if err != nil {
						t.Fatalf("%s edit=%v: %v", raw, editMode, err)
					}
					if _, ok := payload["AllowManualCycleStart"]; !ok {
						t.Fatalf("anchor: %s edit=%v payload must carry the cycle-start policy keys, got %d keys", raw, editMode, len(payload))
					}
					for key := range payload {
						if word := dayPanelNamesProjection(key); word != "" {
							t.Errorf("%s edit=%v: payload key %q names %q; the day panel was decided to carry no projection signal", raw, editMode, key, word)
						}
					}
				}
			}
		})
	}
}

// dayPanelProjectionCopy is every English string whose translation key names a
// projection word, keyed by that translation key. Each word must name a
// translation key, a PredictionSuppression field or the overdue suppression
// reason, so no word in the list can read as a check while matching nothing.
func dayPanelProjectionCopy(t *testing.T) map[string]string {
	t.Helper()
	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	anchors := []string{string(services.SuppressionReasonCycleOverdue)}
	verdict := reflect.TypeOf(services.PredictionSuppression{})
	for i := range verdict.NumField() {
		anchors = append(anchors, verdict.Field(i).Name)
	}
	copyByKey := map[string]string{}
	for key, value := range manager.Messages("en") {
		anchors = append(anchors, key)
		if dayPanelNamesProjection(key) != "" && strings.TrimSpace(value) != "" {
			copyByKey[key] = value
		}
	}
	for _, word := range dayPanelProjectionWords {
		if !slices.ContainsFunc(anchors, func(name string) bool {
			return strings.Contains(strings.ReplaceAll(strings.ToLower(name), "-", "_"), word)
		}) {
			t.Fatalf("anchor: no translation key, verdict field or overdue reason names %q", word)
		}
	}
	return copyByKey
}

// dayPanelHTMLProjectionHits lists every attribute name or value and every text
// node in markup that names a projection word, plus every projection string in
// copyByKey that appears in the text.
func dayPanelHTMLProjectionHits(t *testing.T, markup string, copyByKey map[string]string) []string {
	t.Helper()
	var hits []string
	var text strings.Builder
	tokenizer := html.NewTokenizer(strings.NewReader(markup))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			keys := make([]string, 0, len(copyByKey))
			for key := range copyByKey {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			rendered := text.String()
			for _, key := range keys {
				if strings.Contains(rendered, copyByKey[key]) {
					hits = append(hits, "copy of "+key+": "+copyByKey[key])
				}
			}
			return hits
		case html.TextToken:
			value := html.UnescapeString(string(tokenizer.Text()))
			text.WriteString(value)
			text.WriteString("\n")
			if word := dayPanelNamesProjection(value); word != "" {
				hits = append(hits, "text "+strings.TrimSpace(value))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			for _, attr := range token.Attr {
				if dayPanelNamesProjection(attr.Key) != "" || dayPanelNamesProjection(attr.Val) != "" {
					hits = append(hits, "<"+token.Data+" "+attr.Key+"=\""+attr.Val+"\">")
				}
			}
		}
	}
}

// TestCalendarDayPanelRendersNoProjectionSignal requests the panel over HTTP in
// both bands and reads the HTML for a projection word in markup or text and for
// any projection copy. Anchor: the calendar grid of the out-of-date band, served
// by the same app from the same history, does draw the projected starts and the
// projection copy — so the panel's silence is the panel's, not the fixture's.
func TestCalendarDayPanelRendersNoProjectionSignal(t *testing.T) {
	t.Parallel()

	copyByKey := dayPanelProjectionCopy(t)
	for _, band := range dayPanelProjectionBands {
		t.Run(band.name, func(t *testing.T) {
			t.Parallel()

			now := band.now
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
			user := createOnboardingTestUser(t, database, "day-panel-html@example.com", "StrongPass1", true)
			logs := dayPanelProjectionLogs(t, database, user.ID)
			assertDayPanelProjectionBand(t, band, &user, logs)
			cookie := issueAuthCookieForUser(t, user)

			get := func(path string) string {
				t.Helper()
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.Header.Set("Accept-Language", "en")
				request.Header.Set("Cookie", cookie)
				response := mustAppResponse(t, app, request)
				body := mustReadBodyString(t, response.Body)
				if response.StatusCode != http.StatusOK {
					t.Fatalf("GET %s = %d:\n%s", path, response.StatusCode, body)
				}
				return body
			}

			if !strings.HasPrefix(band.name, "overdue") {
				grid := get("/calendar?month=2026-05")
				for _, want := range []string{`data-day="2026-05-21"`, `data-calendar-state="predicted-period`} {
					if !strings.Contains(grid, want) {
						t.Fatalf("anchor: the out-of-date grid must draw %s", want)
					}
				}
				if len(dayPanelHTMLProjectionHits(t, grid, copyByKey)) == 0 {
					t.Fatal("anchor: the scan must find the projection the out-of-date grid draws")
				}
			}

			for _, raw := range band.dates {
				for _, suffix := range []string{"", "?mode=edit"} {
					path := "/calendar/day/" + raw + suffix
					panel := get(path)
					if !strings.Contains(panel, `data-day-editor`) && !strings.Contains(panel, "/calendar/day/"+raw) {
						t.Fatalf("anchor: %s must render the day panel, got:\n%s", path, panel)
					}
					if hits := dayPanelHTMLProjectionHits(t, panel, copyByKey); len(hits) > 0 {
						t.Errorf("%s renders a projection signal; the day panel was decided to carry none:\n%s", path, strings.Join(hits, "\n"))
					}
				}
			}
		})
	}
}
