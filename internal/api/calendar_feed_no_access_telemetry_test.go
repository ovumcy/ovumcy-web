package api

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// dbSnapshot is every row of every table, each rendered as one string per
// column: table -> row key -> column -> value. A row is keyed by its primary
// key (all of its values when the table has none), so the pairing of a row with
// itself across two snapshots does not depend on the order SELECT * returns.
// It is built by enumerating the live schema, so a table or column added later
// is covered without touching this test.
type dbSnapshot map[string]map[string]map[string]string

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
		columnTypes, err := database.Migrator().ColumnTypes(table)
		if err != nil {
			t.Fatalf("column types of %s: %v", table, err)
		}
		primaryKey := map[string]bool{}
		for _, columnType := range columnTypes {
			if isPrimary, ok := columnType.PrimaryKey(); ok && isPrimary {
				primaryKey[columnType.Name()] = true
			}
		}
		rows, err := database.Raw(`SELECT * FROM "` + table + `"`).Rows()
		if err != nil {
			t.Fatalf("select %s: %v", table, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatalf("columns of %s: %v", table, err)
		}
		rendered := map[string]map[string]string{}
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
			var keyParts, allParts []string
			for index, column := range columns {
				var cell string
				if raw, ok := values[index].([]byte); ok {
					// A blob is kept as its raw bytes, so a needle (binary IP
					// forms included) is matched byte for byte.
					cell = string(raw)
				} else {
					cell = fmt.Sprint(values[index])
				}
				row[column] = cell
				allParts = append(allParts, column+"="+cell)
				if primaryKey[column] {
					keyParts = append(keyParts, column+"="+cell)
				}
			}
			key := strings.Join(keyParts, "|")
			if key == "" {
				key = strings.Join(allParts, "|")
			}
			rendered[key] = row
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

// tableRowCounts returns the true number of rows of every table, counted by the
// database, so rows a snapshot key collapses are still counted.
func tableRowCounts(t *testing.T, database *gorm.DB) map[string]int64 {
	t.Helper()
	tables, err := database.Migrator().GetTables()
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var count int64
		if err := database.Raw(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
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

// changedCells names every difference between two snapshots: "table.column" for
// a cell that changed (or a column present on one side only), "table#rows" when
// a row appeared or disappeared, "table#table" for a table on one side only.
func (snapshot dbSnapshot) changedCells(after dbSnapshot) []string {
	changed := map[string]bool{}
	for table, beforeRows := range snapshot {
		afterRows, ok := after[table]
		if !ok {
			changed[table+"#table"] = true
			continue
		}
		for key, beforeRow := range beforeRows {
			afterRow, ok := afterRows[key]
			if !ok {
				changed[table+"#rows"] = true
				continue
			}
			for column, value := range beforeRow {
				if afterValue, ok := afterRow[column]; !ok || afterValue != value {
					changed[table+"."+column] = true
				}
			}
			for column := range afterRow {
				if _, ok := beforeRow[column]; !ok {
					changed[table+"."+column] = true
				}
			}
		}
		for key := range afterRows {
			if _, ok := beforeRows[key]; !ok {
				changed[table+"#rows"] = true
			}
		}
	}
	for table := range after {
		if _, ok := snapshot[table]; !ok {
			changed[table+"#table"] = true
		}
	}
	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestDBSnapshotDiffSeesEveryKindOfChange keeps the diff honest: a changed cell,
// a row that appears, a column only one side has, and a table only one side has
// each show up, and a reordering of identical rows shows nothing.
func TestDBSnapshotDiffSeesEveryKindOfChange(t *testing.T) {
	base := func() dbSnapshot {
		return dbSnapshot{"users": {"id=1": {"id": "1", "a": "x"}, "id=2": {"id": "2", "a": "y"}}}
	}
	cases := map[string]struct {
		mutate func(dbSnapshot)
		want   []string
	}{
		"identical":      {func(dbSnapshot) {}, nil},
		"cell":           {func(s dbSnapshot) { s["users"]["id=1"]["a"] = "z" }, []string{"users.a"}},
		"new row":        {func(s dbSnapshot) { s["users"]["id=3"] = map[string]string{"id": "3", "a": "q"} }, []string{"users#rows"}},
		"removed row":    {func(s dbSnapshot) { delete(s["users"], "id=2") }, []string{"users#rows"}},
		"after-only col": {func(s dbSnapshot) { s["users"]["id=1"]["ua"] = "agent" }, []string{"users.ua"}},
		"new table":      {func(s dbSnapshot) { s["telemetry"] = map[string]map[string]string{} }, []string{"telemetry#table"}},
		"dropped table":  {func(s dbSnapshot) { delete(s, "users") }, []string{"users#table"}},
	}
	for name, tc := range cases {
		after := base()
		tc.mutate(after)
		if got := base().changedCells(after); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("%s: changedCells = %v, want %v", name, got, tc.want)
		}
	}
}

// TestCalendarFeedPollRecordsNoAccessTelemetryBeyondTheDayMark holds the
// invariant that polling the feed stores no per-request access data: the one
// declared exception is users.calendar_feed_last_polled_on, a calendar DAY.
//
// The app is the real route table (RegisterRoutes behind the language and
// method-override middleware, the not-found handler last, the audit stream
// switched on, though a poll emits no audit event) under a Fiber config that
// trusts X-Forwarded-For, the shape the proxy deployment runs. The edge rate
// limiters and the CSRF middleware are mounted by the composition root in
// cmd/ovumcy and are not part of this app; the feed limiter keeps per-window
// counters keyed by client address in process memory, and nothing it holds
// reaches the database. A request logger carries a hand-written mirror of the
// production format and SafeRequestLogPath tag (the production logger itself is
// driven by a test in cmd/ovumcy), and a control logger that does print ${ip}
// and ${ua} proves the distinctive client address and user agent reached the
// request context, so a clean scan is not green for a client that never arrived.
//
// By-design log content, per the production request-log format (time, status,
// latency, method, route template, redacted error): the operator access log
// carries NO client address and NO user agent at all, and the path is the route
// template ("/calendar/feed/:token.ics"), never the token. The test asserts the
// stronger no-IP/no-UA statement. The standard-library log is scanned the same
// way and must hold nothing but its anchor line. The GORM query logger writes
// through its own writer to stdout, outside both captured logs; the database
// scan is what covers what a query could have stored.
//
// Two rows are polled: a current one, and a pre-032 one with no verifier MAC,
// whose first poll legitimately also backfills calendar_feed_verifier_mac.
func TestCalendarFeedPollRecordsNoAccessTelemetryBeyondTheDayMark(t *testing.T) {
	t.Run("current row", func(t *testing.T) {
		checkFeedPollTelemetry(t, false)
	})
	t.Run("pre-032 row, MAC backfilled by the poll", func(t *testing.T) {
		checkFeedPollTelemetry(t, true)
	})
}

func checkFeedPollTelemetry(t *testing.T, clearMAC bool) {
	t.Helper()
	_, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-telemetry@example.com", "StrongPass1", true)
	// The day mark is the owner-local day; with the zone fixed at UTC it is the UTC
	// day the window check below compares against.
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("timezone", "UTC").Error; err != nil {
		t.Fatalf("set the owner timezone: %v", err)
	}
	token := armCalendarFeedForUserWithColumns(t, database, user.ID, func(columns models.CalendarFeedTokenColumns) models.CalendarFeedTokenColumns {
		if clearMAC {
			columns.VerifierMAC = ""
		}
		return columns
	})
	selector, verifier := mustSplitFeedToken(t, token)
	badToken := strings.Repeat("Z", len(selector)) + verifier
	permittedChange := []string{"users.calendar_feed_last_polled_on"}
	if clearMAC {
		permittedChange = []string{"users.calendar_feed_last_polled_on", "users.calendar_feed_verifier_mac"}
	}

	// The distinctive marker leads the agent string, so a log or column that
	// truncates the agent to its first bytes still carries it and is caught.
	const uaMarker = "zz-distinctive-ua-4f1c"
	const userAgent = uaMarker + " TelemetryProbeCalendarClient/9.87.6"
	clientIPs := []string{"203.0.113.77", "198.51.100.23", "192.0.2.201"}
	// The forms an address is likely to be stored in: dotted text, the 4- and
	// 16-byte binary forms a blob column would hold, and the 4-byte form as hex.
	// A decimal-integer or other encoding is not searched for.
	needles := []string{userAgent, uaMarker}
	for _, ip := range clientIPs {
		parsed := net.ParseIP(ip)
		needles = append(needles, ip, string(parsed.To4()), string(parsed.To16()), hex.EncodeToString(parsed.To4()))
	}

	var stdLog bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&stdLog)
	originalFlags := log.Flags()
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(originalWriter); log.SetFlags(originalFlags) })
	log.Printf("audit-capture anchor")

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler, err := NewHandler(testAppSecretKey, time.UTC, i18nManager, false,
		newTestHandlerDependencies(database, i18nManager, onboardingTestAppOptions{auditLogEnabled: true}))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

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
	app.Use(MethodOverride(handler))
	app.Use(handler.LanguageMiddleware)
	RegisterRoutes(app, handler)
	app.Use(handler.NotFound)

	before := snapshotWholeDatabase(t, database)
	rowsBefore := tableRowCounts(t, database)
	if hits := before.locate(userAgent); len(hits) != 0 {
		t.Fatalf("the user agent is in the database before any poll: %v", hits)
	}

	// The poll may straddle UTC midnight: the mark is either day.
	dayBefore := time.Now().UTC().Truncate(24 * time.Hour)

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
		for _, needle := range needles {
			if strings.Contains(body, needle) {
				t.Fatalf("poll %d echoed %q in the feed body", index, needle)
			}
		}
	}
	if !strings.Contains(unfoldICS(firstBody), "not medical advice or a method of contraception") {
		t.Fatal("the polled body is not the owner's feed")
	}
	// A HEAD poll is a poll too: it serves the headers and takes the same path.
	headRequest := httptest.NewRequest(http.MethodHead, calendarFeedURL(token), nil)
	headRequest.Header.Set("X-Forwarded-For", clientIPs[1])
	headRequest.Header.Set("User-Agent", userAgent)
	if status := mustAppResponse(t, app, headRequest).StatusCode; status != http.StatusOK {
		t.Fatalf("the HEAD poll answered %d, want 200", status)
	}
	// A refused poll is telemetry-free too.
	badRequest := httptest.NewRequest(http.MethodGet, calendarFeedURL(badToken), nil)
	badRequest.Header.Set("X-Forwarded-For", clientIPs[0])
	badRequest.Header.Set("User-Agent", userAgent)
	if status := mustAppResponse(t, app, badRequest).StatusCode; status != http.StatusNotFound {
		t.Fatalf("the unknown token answered %d, want 404", status)
	}
	dayAfter := time.Now().UTC().Truncate(24 * time.Hour)

	// Positive control on the transport: the client address and agent were seen.
	control := controlLog.String()
	for _, ip := range clientIPs {
		if !strings.Contains(control, ip+" "+userAgent) {
			t.Fatalf("the proxy config did not surface client %s with its user agent, so a clean scan proves nothing; control log:\n%s", ip, control)
		}
	}
	// A poll legitimately writes nothing to the standard log: after the requests
	// it holds exactly the anchor line, so any line a poll adds is a finding.
	if got := stdLog.String(); got != "audit-capture anchor\n" {
		t.Fatalf("the standard log must hold exactly the anchor line (a poll writes nothing there), got %q", got)
	}
	if lines := strings.Count(stdLog.String(), "\n"); lines != 1 {
		t.Fatalf("a poll wrote to the standard log; it should hold only the anchor, got:\n%s", stdLog.String())
	}

	after := snapshotWholeDatabase(t, database)

	// (a) neither the address nor the agent is in any cell of any table.
	for _, needle := range needles {
		if hits := after.locate(needle); len(hits) != 0 {
			t.Fatalf("polling stored %q in %v; the feed may record no access telemetry", needle, hits)
		}
	}

	// (b) the only changed cell in the whole database is the day mark.
	changed := before.changedCells(after)
	if strings.Join(changed, ",") != strings.Join(permittedChange, ",") {
		t.Fatalf("polling changed %v; the only permitted change is %v", changed, permittedChange)
	}
	// A table without a primary key keys its rows by their whole content, which
	// collapses duplicates; the true row counts catch a duplicate row appended.
	rowsAfter := tableRowCounts(t, database)
	for table, count := range rowsBefore {
		if rowsAfter[table] != count {
			t.Fatalf("polling changed the row count of %s from %d to %d", table, count, rowsAfter[table])
		}
	}
	var reloaded models.User
	if err := database.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	mark := reloaded.CalendarFeedLastPolledOn
	if mark == nil {
		t.Fatal("no poll reached the row: the day mark is empty, so the scans measured nothing")
	}
	if !mark.Equal(dayBefore) && !mark.Equal(dayAfter) {
		t.Fatalf("the mark must be the owner-local DAY (%s or %s at midnight), got %s",
			dayBefore.Format(time.RFC3339), dayAfter.Format(time.RFC3339), mark.Format(time.RFC3339Nano))
	}

	// (c) the access log: route template only, no token, selector, address or agent.
	logText := accessLog.String()
	// the GET polls, the HEAD poll and the refused poll: one line each.
	if strings.Count(logText, "\n") != len(clientIPs)+2 {
		t.Fatalf("expected one access-log line per request, got:\n%s", logText)
	}
	if strings.Count(logText, "| 200 |") != len(clientIPs)+1 || strings.Count(logText, "| 404 |") != 1 ||
		strings.Count(logText, "| /calendar/feed/:token.ics |") != len(clientIPs)+2 {
		t.Fatalf("the access log does not show the requests by route template, so absence checks are vacuous:\n%s", logText)
	}
	// Both logs: the access log and the standard log the audit stream writes to.
	for name, text := range map[string]string{"access log": logText, "standard log": stdLog.String()} {
		for _, secret := range append([]string{token, badToken, selector, verifier}, needles...) {
			if strings.Contains(text, secret) {
				t.Fatalf("the %s carries %q:\n%s", name, secret, text)
			}
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
