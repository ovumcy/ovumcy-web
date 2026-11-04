package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// dbSnapshot is every row of every table, each rendered as one string per
// column, keyed by table. It is built by enumerating the live schema, so a table
// or column added later is covered without touching this test.
type dbSnapshot map[string][]map[string]string

func snapshotWholeDatabase(t *testing.T, database *gorm.DB) dbSnapshot {
	t.Helper()
	tables, err := database.Migrator().GetTables()
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("the schema lists no tables, so the scan would measure nothing")
	}
	snapshot := dbSnapshot{}
	for _, table := range tables {
		rows, err := database.Raw(`SELECT * FROM "` + table + `"`).Rows()
		if err != nil {
			t.Fatalf("select %s: %v", table, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatalf("columns of %s: %v", table, err)
		}
		var rendered []map[string]string
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range values {
				pointers[index] = &values[index]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				t.Fatalf("scan %s: %v", table, err)
			}
			row := make(map[string]string, len(columns))
			for index, column := range columns {
				switch value := values[index].(type) {
				case []byte:
					// Both the text and the raw bytes: a blob column is searched
					// for the needle as text, like any other.
					row[column] = string(value)
				default:
					row[column] = fmt.Sprint(value)
				}
			}
			rendered = append(rendered, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("iterate %s: %v", table, err)
		}
		_ = rows.Close()
		snapshot[table] = rendered
	}
	return snapshot
}

// locate returns "table.column" for every cell that contains needle.
func (snapshot dbSnapshot) locate(needle string) []string {
	var found []string
	for table, rows := range snapshot {
		for _, row := range rows {
			for column, value := range row {
				if strings.Contains(value, needle) {
					found = append(found, table+"."+column)
				}
			}
		}
	}
	sort.Strings(found)
	return found
}

// changedCells returns "table.column" for every cell that differs between two
// snapshots, plus "table#rows" when a table's row count changed. Rows are paired
// by position, which is stable here: the polls only UPDATE, and a new row shows
// as a count change.
func (snapshot dbSnapshot) changedCells(after dbSnapshot) []string {
	var changed []string
	for table, beforeRows := range snapshot {
		afterRows := after[table]
		if len(beforeRows) != len(afterRows) {
			changed = append(changed, table+"#rows")
			continue
		}
		for index, beforeRow := range beforeRows {
			for column, value := range beforeRow {
				if afterRows[index][column] != value {
					changed = append(changed, table+"."+column)
				}
			}
		}
	}
	for table := range after {
		if _, ok := snapshot[table]; !ok {
			changed = append(changed, table+"#new-table")
		}
	}
	sort.Strings(changed)
	return changed
}

// TestCalendarFeedPollRecordsNoAccessTelemetryBeyondTheDayMark holds the
// invariant that polling the feed stores no per-request access data: the one
// declared exception is users.calendar_feed_last_polled_on, a calendar DAY.
//
// The app is the production feed route behind a Fiber config that trusts the
// X-Forwarded-For header (the shape the proxy deployment runs), a request logger
// wired with the production format and SafeRequestLogPath tag, and a control
// logger that DOES print ${ip} and ${ua}. The control proves the distinctive
// client address and user agent really reached the request context, so a scan
// that finds neither in the database or the log is not green for a client that
// never arrived.
//
// By-design log content, per the production request-log format (time, status,
// latency, method, route template, redacted error): the operator access log
// carries NO client address and NO user agent at all, and the path is the route
// template ("/calendar/feed/:token.ics"), never the token. The test asserts the
// stronger no-IP/no-UA statement because the production format has none of those
// fields; if a field carrying them is ever added, this test is the place to
// decide it consciously.
func TestCalendarFeedPollRecordsNoAccessTelemetryBeyondTheDayMark(t *testing.T) {
	_, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-telemetry@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)
	selector, _ := mustSplitFeedToken(t, token)

	const userAgent = "TelemetryProbeCalendarClient/9.87.6 (zz-distinctive-ua-4f1c)"
	clientIPs := []string{"203.0.113.77", "198.51.100.23", "192.0.2.201"}

	// Mirrors the production request-log format (cmd/ovumcy/server.go).
	var accessLog, controlLog bytes.Buffer
	app := fiber.New(fiber.Config{
		ProxyHeader:        fiber.HeaderXForwardedFor,
		TrustProxy:         true,
		EnableIPValidation: true,
		TrustProxyConfig:   fiber.TrustProxyConfig{Proxies: []string{"0.0.0.0/0"}},
	})
	app.Use(logger.New(logger.Config{
		Stream: &accessLog,
		Format: "${time} | ${status} | ${latency} | ${method} | ${request_path} | ${safe_error}\n",
		CustomTags: map[string]logger.LogFunc{
			"request_path": func(buffer logger.Buffer, c fiber.Ctx, _ *logger.Data, _ string) (int, error) {
				return buffer.WriteString(SafeRequestLogPath(c))
			},
			"safe_error": func(buffer logger.Buffer, c fiber.Ctx, data *logger.Data, _ string) (int, error) {
				return buffer.WriteString(SafeLogError(data.ChainErr))
			},
		},
	}))
	app.Use(logger.New(logger.Config{Stream: &controlLog, Format: "${ip} ${ua}\n"}))
	app.Get(calendarFeedRoutePath, mustFeedHandler(t, database).ServeCalendarFeed)

	before := snapshotWholeDatabase(t, database)
	if hits := before.locate(userAgent); len(hits) != 0 {
		t.Fatalf("the user agent is in the database before any poll: %v", hits)
	}

	var firstBody string
	for index, ip := range clientIPs {
		request := httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil)
		request.Header.Set("X-Forwarded-For", ip)
		request.Header.Set("User-Agent", userAgent)
		response := mustAppResponse(t, app, request)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("poll %d answered %d; the scans below would measure a refused request", index, response.StatusCode)
		}
		body := mustReadBodyString(t, response.Body)
		if index == 0 {
			firstBody = body
		}
		if !strings.Contains(body, "BEGIN:VEVENT") {
			t.Fatalf("poll %d did not serve the owner's calendar:\n%s", index, body)
		}
		for _, needle := range append([]string{userAgent}, clientIPs...) {
			if strings.Contains(body, needle) {
				t.Fatalf("poll %d echoed %q in the feed body", index, needle)
			}
		}
	}
	if !strings.Contains(unfoldICS(firstBody), "not medical advice or a method of contraception") {
		t.Fatal("the polled body is not the owner's feed")
	}

	// Positive control on the transport: the client address and agent were seen.
	control := controlLog.String()
	for _, ip := range clientIPs {
		if !strings.Contains(control, ip+" "+userAgent) {
			t.Fatalf("the proxy config did not surface client %s with its user agent, so a clean scan proves nothing; control log:\n%s", ip, control)
		}
	}

	after := snapshotWholeDatabase(t, database)

	// (a) neither the address nor the agent is in any cell of any table.
	for _, needle := range append([]string{userAgent, "zz-distinctive-ua-4f1c"}, clientIPs...) {
		if hits := after.locate(needle); len(hits) != 0 {
			t.Fatalf("polling stored %q in %v; the feed may record no access telemetry", needle, hits)
		}
	}

	// (b) the only changed cell in the whole database is the day mark.
	changed := before.changedCells(after)
	if len(changed) != 1 || changed[0] != "users.calendar_feed_last_polled_on" {
		t.Fatalf("polling changed %v; the only permitted change is users.calendar_feed_last_polled_on", changed)
	}
	var reloaded models.User
	if err := database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	mark := reloaded.CalendarFeedLastPolledOn
	if mark == nil {
		t.Fatal("no poll reached the row: the day mark is empty, so the scans measured nothing")
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !mark.Equal(today) {
		t.Fatalf("the mark must be the owner-local DAY (%s at midnight), got %s", today.Format(time.RFC3339), mark.Format(time.RFC3339Nano))
	}

	// (c) the access log: route template only, no token, selector, address or agent.
	logText := accessLog.String()
	if strings.Count(logText, "\n") != len(clientIPs) {
		t.Fatalf("expected one access-log line per poll, got:\n%s", logText)
	}
	if !strings.Contains(logText, "| 200 |") || !strings.Contains(logText, "| /calendar/feed/:token.ics |") {
		t.Fatalf("the access log does not show the polls by route template, so absence checks are vacuous:\n%s", logText)
	}
	for _, secret := range []string{token, selector, userAgent, "zz-distinctive-ua-4f1c"} {
		if strings.Contains(logText, secret) {
			t.Fatalf("the access log carries %q:\n%s", secret, logText)
		}
	}
	for _, ip := range clientIPs {
		if strings.Contains(logText, ip) {
			t.Fatalf("the access log carries client address %s; the production format has no address field:\n%s", ip, logText)
		}
	}

	// Anti-vacuity of the scan itself: planted in a column, the same scan finds it.
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).
		Update("calendar_feed_key_epoch", reloaded.CalendarFeedKeyEpoch+userAgent).Error; err != nil {
		t.Fatalf("plant the user agent: %v", err)
	}
	planted := snapshotWholeDatabase(t, database).locate(userAgent)
	if len(planted) != 1 || planted[0] != "users.calendar_feed_key_epoch" {
		t.Fatalf("the scan did not find a planted user agent where it was planted: %v", planted)
	}
}
