package api

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
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
// period, fertile window, ovulation day or phase — and none of the verdicts that
// gate a projection (out of date, overdue, suppressed). The service model behind
// it is pinned by field name in the services package; these tests pin what that
// check cannot see: the map the handler hands the template (after the template
// defaults are merged in) and the HTML it renders, in both bands where those
// verdicts are live, for every day around them.
//
// The history is three 28-day cycles and a running one from 2026-03-26, so the
// reference length is 28. Cycle day 30 (2026-04-24) is out of date and not yet
// overdue (the gate is day 36); cycle day 40 (2026-05-04) is past the gate.
var dayPanelProjectionCycleStarts = []string{"2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26"}

// dayPanelProjectionWords are matched against payload keys and values, HTML
// attribute names and values, text and comments, and translation keys and copy,
// after dayPanelNormalize, so "IsPredicted", "late-cycle" and "next period" all
// reach the same spelling.
var dayPanelProjectionWords = []string{
	"predict", "fertil", "ovulat", "luteal", "follic", "phase", "estimat", "next_period",
	"stale", "outdated", "out_of_date", "overdue", "late_cycle", "suppress",
}

// dayPanelAllowedCopyKeys are panel strings that name a projection word and are
// still the panel's own: the cycle-start policy notice tells the owner a manual
// start on a future day will move the predictions, and claims no date.
var dayPanelAllowedCopyKeys = []string{"warning.future_cycle_start"}

type dayPanelProjectionBand struct {
	name        string
	now         time.Time
	cycleDay    int
	wantOverdue bool
	// dashboardCopyKey is the verdict copy the dashboard prints at this clock: the
	// anchor that the copy scan detects this band's own signal on a real page.
	dashboardCopyKey string
}

var dayPanelProjectionBands = []dayPanelProjectionBand{
	{name: "out of date", now: time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC), cycleDay: 30, dashboardCopyKey: "dashboard.cycle_day_stale_hint"},
	{name: "overdue", now: time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC), cycleDay: 40, wantOverdue: true, dashboardCopyKey: "dashboard.late_cycle.beyond_range.other"},
}

// dayPanelProjectionDays is every day the panel is requested for: the logged
// cycle start, the projected period, fertile window and ovulation of the running
// cycle, and the projected start after it.
func dayPanelProjectionDays() []time.Time {
	var days []time.Time
	for day := time.Date(2026, time.March, 24, 0, 0, 0, 0, time.UTC); day.Before(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)); day = day.AddDate(0, 0, 1) {
		days = append(days, day)
	}
	return days
}

func dayPanelNormalize(value string) string {
	var builder strings.Builder
	runes := []rune(value)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' && runes[i-1] >= 'a' && runes[i-1] <= 'z' {
			builder.WriteByte('_')
		}
		switch r {
		case '-', ' ', '.':
			builder.WriteByte('_')
		default:
			builder.WriteRune(r)
		}
	}
	return strings.ToLower(builder.String())
}

func dayPanelNamesProjection(value string) string {
	normalized := dayPanelNormalize(value)
	for _, word := range dayPanelProjectionWords {
		if strings.Contains(normalized, word) {
			return word
		}
	}
	return ""
}

// newDayPanelProjectionUser creates the owner with the fixture history. The
// account is dated a year before the band so the calendar's look-back floor,
// which follows the account's age, keeps the anchor months navigable.
func newDayPanelProjectionUser(t *testing.T, database *gorm.DB, band dayPanelProjectionBand) (models.User, []models.DailyLog) {
	t.Helper()
	user := createOnboardingTestUser(t, database, "day-panel-projection@example.com", "StrongPass1", true)
	user.CreatedAt = band.now.AddDate(-1, 0, 0)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("created_at", user.CreatedAt).Error; err != nil {
		t.Fatalf("date the account: %v", err)
	}
	logs := make([]models.DailyLog, 0, len(dayPanelProjectionCycleStarts))
	for _, raw := range dayPanelProjectionCycleStarts {
		day, err := time.Parse("2006-01-02", raw)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		entry := models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}
		if err := database.Create(&entry).Error; err != nil {
			t.Fatalf("create cycle start %s: %v", raw, err)
		}
		logs = append(logs, entry)
	}
	return user, logs
}

// assertDayPanelProjectionBand fails unless the band's clock puts the fixture
// on the named cycle day and under the named verdict, so a drift in the gates
// cannot leave the panel checks running where nothing is withheld. It returns
// the projection of the clean (not overdue) band, which the anchors read.
func assertDayPanelProjectionBand(t *testing.T, band dayPanelProjectionBand, user *models.User, logs []models.DailyLog) services.CycleStats {
	t.Helper()
	stats := services.BuildCycleStatsFromLogs(user, logs, band.now, time.UTC)
	if stats.CurrentCycleDay != band.cycleDay {
		t.Fatalf("fixture: %s must be cycle day %d, got %d", band.name, band.cycleDay, stats.CurrentCycleDay)
	}
	published, verdict := services.PublishedStats(user, stats, logs, services.DateAtLocation(band.now, time.UTC), time.UTC)
	overdue := slices.Contains(verdict.Reasons, services.SuppressionReasonCycleOverdue)
	if band.wantOverdue {
		if !overdue || !verdict.PredictionsSuppressed {
			t.Fatalf("fixture: %s must be suppressed as overdue, verdict %+v", band.name, verdict)
		}
		return stats
	}
	if !published.CycleDataStale || overdue || verdict.PredictionsSuppressed || verdict.FertilitySuppressed {
		t.Fatalf("fixture: %s must be out of date without being suppressed, stale %v verdict %+v", band.name, published.CycleDataStale, verdict)
	}
	return stats
}

// dayPanelPayloadHits walks a payload value: map keys, struct field names and
// string values that name a projection word. time.Time is a leaf.
func dayPanelPayloadHits(path string, value reflect.Value, hits *[]string) {
	if !value.IsValid() {
		return
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		if dayPanelNamesProjection(value.String()) != "" {
			*hits = append(*hits, fmt.Sprintf("%s = %q", path, value.String()))
		}
	case reflect.Map:
		keys := value.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		for _, key := range keys {
			name := fmt.Sprint(key)
			if dayPanelNamesProjection(name) != "" {
				*hits = append(*hits, path+"["+name+"]")
			}
			dayPanelPayloadHits(path+"["+name+"]", value.MapIndex(key), hits)
		}
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := range value.NumField() {
			field := value.Type().Field(i)
			if dayPanelNamesProjection(field.Name) != "" {
				*hits = append(*hits, path+"."+field.Name)
			}
			if field.IsExported() {
				dayPanelPayloadHits(path+"."+field.Name, value.Field(i), hits)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			dayPanelPayloadHits(fmt.Sprintf("%s[%d]", path, i), value.Index(i), hits)
		}
	}
}

// TestCalendarDayPanelPayloadCarriesNoProjectionSignal reads the map
// buildDayEditorPartialData returns, merged with the template defaults as
// renderPartial merges it, for every requested day in both bands and both
// modes. Messages is the whole translation catalogue, not state, and is the
// one key left out.
func TestCalendarDayPanelPayloadCarriesNoProjectionSignal(t *testing.T) {
	t.Parallel()

	for _, band := range dayPanelProjectionBands {
		t.Run(band.name, func(t *testing.T) {
			t.Parallel()

			// The app helper keeps its handler to itself, so the handler under test
			// is built on the same database here.
			_, database := newOnboardingTestApp(t)
			user, logs := newDayPanelProjectionUser(t, database, band)
			assertDayPanelProjectionBand(t, band, &user, logs)

			manager, err := i18n.NewManager("en")
			if err != nil {
				t.Fatalf("init i18n: %v", err)
			}
			handler, err := NewHandler(testAppSecretKey, time.UTC, manager, false, newTestHandlerDependencies(database, manager))
			if err != nil {
				t.Fatalf("init handler: %v", err)
			}

			for _, day := range dayPanelProjectionDays() {
				raw := day.Format("2006-01-02")
				for _, editMode := range []bool{false, true} {
					payload, err := handler.buildDayEditorPartialData(context.Background(), &user, "en", manager.Messages("en"), day, band.now, time.UTC, editMode)
					if err != nil {
						t.Fatalf("%s edit=%v: %v", raw, editMode, err)
					}
					if _, ok := payload["AllowManualCycleStart"]; !ok {
						t.Fatalf("anchor: %s edit=%v payload must carry the cycle-start policy keys, got %d keys", raw, editMode, len(payload))
					}
					merged := withTemplateDefaultsForTest(t, "/calendar/day/"+raw, maps.Clone(payload))
					if _, ok := merged["CSRFToken"]; !ok {
						t.Fatalf("anchor: %s edit=%v merged payload must carry the template defaults", raw, editMode)
					}
					delete(merged, "Messages")
					var hits []string
					dayPanelPayloadHits("payload", reflect.ValueOf(map[string]any(merged)), &hits)
					if len(hits) > 0 {
						t.Errorf("%s edit=%v: the day panel payload was decided to carry no projection signal:\n%s", raw, editMode, strings.Join(hits, "\n"))
					}
				}
			}
		})
	}
}

// dayPanelProjectionCopy is a matcher for every English string whose translation
// key names a projection word, keyed by that translation key. A printf verb
// matches any run of text, so a count or a date filled in still matches. Each
// word must name a translation key or English string, a PredictionSuppression
// field or the overdue suppression reason, so no word in the list can read as a
// check while matching nothing.
func dayPanelProjectionCopy(t *testing.T) map[string]*regexp.Regexp {
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
	verb := regexp.MustCompile(`%[-+# 0-9.\[\]]*[a-zA-Z]`)
	copyByKey := map[string]*regexp.Regexp{}
	for key, value := range manager.Messages("en") {
		anchors = append(anchors, key, value)
		if dayPanelNamesProjection(key) == "" || slices.Contains(dayPanelAllowedCopyKeys, key) {
			continue
		}
		literal := verb.Split(value, -1)
		// A string that is mostly a verb ("%s — %s", "around %s") would match any
		// text; the projected dates it carries are the date check's job.
		if len(strings.TrimSpace(strings.Join(literal, ""))) < 12 {
			continue
		}
		for i := range literal {
			literal[i] = regexp.QuoteMeta(literal[i])
		}
		copyByKey[key] = regexp.MustCompile(strings.Join(literal, ".+?"))
	}
	for _, word := range dayPanelProjectionWords {
		if !slices.ContainsFunc(anchors, func(name string) bool {
			return strings.Contains(dayPanelNormalize(name), word)
		}) {
			t.Fatalf("anchor: no translation key or string, verdict field or overdue reason names %q", word)
		}
	}
	return copyByKey
}

// dayPanelHTMLProjectionHits lists every attribute name or value, text node and
// comment in markup that names a projection word, and every projection string
// in copyByKey the text contains. Text equal to an allowed panel string is the
// panel's own.
func dayPanelHTMLProjectionHits(t *testing.T, markup string, copyByKey map[string]*regexp.Regexp, allowed []string) []string {
	t.Helper()
	var hits []string
	var text strings.Builder
	tokenizer := html.NewTokenizer(strings.NewReader(markup))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			rendered := text.String()
			for _, key := range slices.Sorted(maps.Keys(copyByKey)) {
				if copyByKey[key].MatchString(rendered) {
					hits = append(hits, "copy of "+key)
				}
			}
			return hits
		case html.TextToken, html.CommentToken:
			value := strings.TrimSpace(html.UnescapeString(string(tokenizer.Text())))
			if slices.Contains(allowed, value) {
				continue
			}
			text.WriteString(value)
			text.WriteString("\n")
			if dayPanelNamesProjection(value) != "" {
				hits = append(hits, "text "+value)
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

// dayPanelDatePattern matches a date in the forms the panel could print it: ISO,
// and English month-day and day-month, long and short.
func dayPanelDatePattern(day time.Time) *regexp.Regexp {
	forms := []string{day.Format("2006-01-02")}
	for _, layout := range []string{"January 2", "Jan 2", "2 January", "2 Jan"} {
		forms = append(forms, `\b`+regexp.QuoteMeta(day.Format(layout))+`\b`)
	}
	return regexp.MustCompile(strings.Join(forms, "|"))
}

// TestCalendarDayPanelRendersNoProjectionSignal requests the panel over HTTP in
// both bands, for every day around the projection and in both modes, and reads
// the HTML for a projection word, any projection copy, and any projected date
// other than the day itself. Anchors, from the same app and history: the grid
// of the out-of-date band draws the projected starts in their own cells, and
// the dashboard at each band's clock prints that band's verdict copy, which the
// same scan detects — so the panel's silence is the panel's.
func TestCalendarDayPanelRendersNoProjectionSignal(t *testing.T) {
	t.Parallel()

	copyByKey := dayPanelProjectionCopy(t)
	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	var allowed []string
	for _, key := range dayPanelAllowedCopyKeys {
		allowed = append(allowed, manager.Messages("en")[key])
	}

	for _, band := range dayPanelProjectionBands {
		t.Run(band.name, func(t *testing.T) {
			t.Parallel()

			now := band.now
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
			user, logs := newDayPanelProjectionUser(t, database, band)
			stats := assertDayPanelProjectionBand(t, band, &user, logs)
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

			dashboardHits := dayPanelHTMLProjectionHits(t, get("/dashboard"), copyByKey, allowed)
			if !slices.Contains(dashboardHits, "copy of "+band.dashboardCopyKey) {
				t.Fatalf("anchor: the scan must find %s on the dashboard at this clock, found:\n%s", band.dashboardCopyKey, strings.Join(dashboardHits, "\n"))
			}

			// The projection the panel must not print. In the overdue band nothing is
			// projected anywhere, so the clean band's dates are the ones a leak would
			// carry: they are recomputed here for the out-of-date clock.
			projected := stats
			if band.wantOverdue {
				projected = services.BuildCycleStatsFromLogs(&user, logs, dayPanelProjectionBands[0].now, time.UTC)
			}
			projectedDays := []time.Time{projected.NextPeriodStart, projected.NextPeriodStart.AddDate(0, 0, 28), projected.OvulationDate}
			for day := projected.FertilityWindowStart; !day.After(projected.FertilityWindowEnd); day = day.AddDate(0, 0, 1) {
				projectedDays = append(projectedDays, day)
			}
			first, last := dayPanelProjectionDays()[0], dayPanelProjectionDays()[len(dayPanelProjectionDays())-1]
			for _, day := range projectedDays {
				if day.IsZero() || day.Before(first) || day.After(last) {
					t.Fatalf("anchor: projected day %v must fall inside the requested days", day)
				}
			}

			if !band.wantOverdue {
				for month, start := range map[string]time.Time{"2026-04": projected.NextPeriodStart, "2026-05": projected.NextPeriodStart.AddDate(0, 0, 28)} {
					cell := regexp.MustCompile(`data-day="` + start.Format("2006-01-02") + `"\s+data-calendar-state="predicted-period`)
					if !cell.MatchString(get("/calendar?month=" + month)) {
						t.Fatalf("anchor: the out-of-date grid must draw %s as a predicted period start", start.Format("2006-01-02"))
					}
				}
			}

			for _, day := range dayPanelProjectionDays() {
				raw := day.Format("2006-01-02")
				var leaks []*regexp.Regexp
				for _, projectedDay := range projectedDays {
					if !projectedDay.Equal(day) && !slices.Contains(dayPanelProjectionCycleStarts, projectedDay.Format("2006-01-02")) {
						leaks = append(leaks, dayPanelDatePattern(projectedDay))
					}
				}
				for _, mode := range []struct{ suffix, marker string }{
					{"", `data-day-editor-open="` + raw + `"`},
					{"?mode=edit", `data-day-editor-date="` + raw + `"`},
				} {
					path := "/calendar/day/" + raw + mode.suffix
					panel := get(path)
					if !strings.Contains(panel, mode.marker) {
						t.Fatalf("anchor: %s must render the day panel (%s), got:\n%s", path, mode.marker, panel)
					}
					hits := dayPanelHTMLProjectionHits(t, panel, copyByKey, allowed)
					for _, leak := range leaks {
						if found := leak.FindString(panel); found != "" {
							hits = append(hits, "projected date "+found)
						}
					}
					if len(hits) > 0 {
						t.Errorf("%s renders a projection signal; the day panel was decided to carry none:\n%s", path, strings.Join(hits, "\n"))
					}
				}
			}
		})
	}
}
