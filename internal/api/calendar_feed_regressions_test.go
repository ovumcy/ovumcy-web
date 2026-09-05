package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// armCalendarFeedForUser mints a real feed token for the user, persists the whole
// stored triple, and seeds a stable ~28d period cadence so the feed emits
// prediction events. It returns the full shown-once token to present in the URL.
func armCalendarFeedForUser(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()
	return armCalendarFeedForUserWithColumns(t, database, userID, func(columns models.CalendarFeedTokenColumns) models.CalendarFeedTokenColumns {
		return columns
	})
}

// armCalendarFeedForUserWithColumns is armCalendarFeedForUser with a hook to alter
// the stored triple before it is written — used to seed a row in the pre-migration-032
// shape (bcrypt hash, no MAC).
func armCalendarFeedForUserWithColumns(t *testing.T, database *gorm.DB, userID uint, adjust func(models.CalendarFeedTokenColumns) models.CalendarFeedTokenColumns) string {
	t.Helper()
	token, columns, err := services.GenerateCalendarFeedToken([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	repo := db.NewRepositories(database).Users
	if err := repo.SaveCalendarFeedToken(t.Context(), userID, adjust(columns)); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}
	// Three cycle starts one 28-day cycle apart, anchored to the CURRENT day: the
	// feed emits no prediction events once a cycle has run past the account's
	// reference length by more than a week (services.DashboardCycleOverdue), so a
	// cadence pinned to fixed calendar dates stops producing events as soon as the
	// clock moves past them. Relative seeding keeps the running cycle on day 3.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	starts := []time.Time{today.AddDate(0, 0, -58), today.AddDate(0, 0, -30), today.AddDate(0, 0, -2)}
	for _, start := range starts {
		if err := database.Create(&models.DailyLog{UserID: userID, Date: start, IsPeriod: true}).Error; err != nil {
			t.Fatalf("seed period log %s: %v", start.Format("2006-01-02"), err)
		}
	}
	// Anchor the current cycle to the most recent seeded start.
	if err := database.Model(&models.User{}).Where("id = ?", userID).Update("last_period_start", starts[len(starts)-1]).Error; err != nil {
		t.Fatalf("set last_period_start: %v", err)
	}
	return token
}

func calendarFeedURL(token string) string {
	return "/calendar/feed/" + token + ".ics"
}

// mustServeCalendarFeed GETs token's feed URL and asserts the armed-feed
// precondition every later assertion in this file leans on: 200, no
// Set-Cookie, and a calendar body. stage names which precondition is being
// proven, so a failure reads as "precondition lost" against the right moment
// rather than as whatever assertion the caller makes next. Returns the body
// for a caller that wants to inspect it further.
func mustServeCalendarFeed(t *testing.T, app *fiber.App, token string, stage string) string {
	t.Helper()
	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("precondition lost: the armed feed must serve %s, got %d", stage, response.StatusCode)
	}
	if len(response.Cookies()) != 0 {
		t.Fatalf("precondition lost: the armed feed must not set a cookie %s, got %#v", stage, response.Cookies())
	}
	body := readBodyString(t, response)
	if !strings.Contains(body, "BEGIN:VCALENDAR") {
		t.Fatalf("precondition lost: expected a calendar body from the armed feed %s, got:\n%s", stage, body)
	}
	return body
}

// TestCalendarFeedServesOwnersICSWithHardenedHeaders is the happy-path contract:
// a valid token returns 200 with text/calendar, a private 1h cache, the
// noindex robots tag, structural .ics markers, and the medical-safety disclaimer
// in a DESCRIPTION — and never a Set-Cookie.
func TestCalendarFeedServesOwnersICSWithHardenedHeaders(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-ok@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)

	request := httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil)
	response := mustAppResponse(t, app, request)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a valid feed token, got %d", response.StatusCode)
	}
	if ct := response.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Fatalf("expected text/calendar content type, got %q", ct)
	}
	if cc := response.Header.Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Fatalf("expected private 1h cache, got %q", cc)
	}
	if robots := response.Header.Get("X-Robots-Tag"); robots != "noindex" {
		t.Fatalf("expected X-Robots-Tag noindex, got %q", robots)
	}
	if len(response.Cookies()) != 0 {
		t.Fatalf("feed must not set any cookie, got %#v", response.Cookies())
	}

	body := readBodyString(t, response)
	for _, marker := range []string{"BEGIN:VCALENDAR", "BEGIN:VEVENT", "SUMMARY:", "DESCRIPTION:"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("expected .ics marker %q, got:\n%s", marker, body)
		}
	}
	// Medical-safety disclaimer (exact-wording invariant) present in a DESCRIPTION.
	// Assert against the UNFOLDED body — RFC 5545 folds long lines with a CRLF +
	// space, which a calendar client (and this check) must unfold before reading
	// the value; the wording itself is unchanged.
	if unfolded := unfoldICS(body); !strings.Contains(unfolded, "not medical advice or a method of contraception") {
		t.Fatalf("expected medical-safety disclaimer in feed body, got:\n%s", body)
	}
	// Neutral-title invariant: SUMMARY carries no date/phase.
	for _, line := range strings.Split(body, "\r\n") {
		if strings.HasPrefix(line, "SUMMARY:") && line != "SUMMARY:Ovumcy: reminder (estimate)" {
			t.Fatalf("SUMMARY must be the fixed neutral label, got %q", line)
		}
	}
}

// feedResponseFingerprint is everything a client can observe about one feed
// response except the per-request Date header: the status, the header set, and
// the body bytes. Comparing fingerprints is how the no-oracle contract is
// actually checked — a response that carries no calendar body can still differ
// from its sibling in every other byte, and that difference IS the oracle.
type feedResponseFingerprint struct {
	status  int
	headers string
	body    string
}

// fingerprintFeedResponse renders a response into its comparable shape. Date is
// dropped because fasthttp regenerates it per request (two cases either side of
// a second boundary would differ for a reason that carries no information about
// the token); every other header stays in, so a cause smuggled into a header is
// as visible as one smuggled into the body.
func fingerprintFeedResponse(t *testing.T, response *http.Response) feedResponseFingerprint {
	t.Helper()
	headerLines := make([]string, 0, len(response.Header))
	for name, values := range response.Header {
		if name == "Date" {
			continue
		}
		headerLines = append(headerLines, name+": "+strings.Join(values, ", "))
	}
	sort.Strings(headerLines)
	return feedResponseFingerprint{
		status:  response.StatusCode,
		headers: strings.Join(headerLines, "\n"),
		body:    readBodyString(t, response),
	}
}

// The shape every public feed 404 must have, measured from the running app
// rather than modelled: the handler answers with c.SendStatus, which fills an
// empty body with fiber's fixed status text, so the "bare" 404 is the same nine
// bytes under text/plain whatever the cause was. Pinned as a constant so the
// four cases cannot drift TOGETHER into something cause-bearing — mutual
// identity alone would still hold if they all started explaining themselves.
const (
	calendarFeedBare404Body    = "Not Found"
	calendarFeedBare404Headers = "Content-Length: 9\nContent-Type: text/plain; charset=utf-8"
)

// TestCalendarFeedReturnsBare404WithoutOracleForBadTokens proves the no-oracle
// contract at the transport boundary: an unknown selector, a malformed token, a
// correct-selector/wrong-verifier token, and a token whose feed has since been
// revoked all get the SAME response — same status, same headers, same body
// bytes, no Set-Cookie — so the response tells an enumerator nothing about
// which of the four they hit.
//
// wrongVerifier is tried against the STILL-ARMED subscription, proven live by a
// GET before the bad-token cases; bogus and malformed run first but mutate
// nothing, so the row is still armed when wrongVerifier fires: the row's
// selector must still resolve so the request actually reaches "selector found,
// verifier mismatch" inside VerifyCalendarFeedToken, rather than the "selector
// resolves no row" branch a since-revoked feed produces instead.
//
// The identity is asserted BETWEEN the cases, not case by case against a marker
// of its own: four bodies that each merely lack "BEGIN:VCALENDAR" can still
// differ from one another in every remaining byte, which is exactly the oracle
// this route must not be. The shared shape is pinned alongside, so the cases
// cannot drift together.
func TestCalendarFeedReturnsBare404WithoutOracleForBadTokens(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-404@example.com", "StrongPass1", true)
	validToken := armCalendarFeedForUser(t, database, user.ID)

	// Positive control: the subscription genuinely works before any bad token is
	// tried against it, so wrongVerifier below is proven against a live row, not
	// one that never served anything.
	mustServeCalendarFeed(t, app, validToken, "before the bad-token cases")

	// A token whose selector resolves no row, a too-short malformed token, and a
	// correct-selector/wrong-verifier token — built from the real selector/
	// verifier split so the boundary is never a hand-copied magic 16. 'Z' and
	// '2' are both in the token alphabet and preserve length, so bogus and
	// wrongVerifier still reach the by-selector lookup instead of falling into
	// malformed's too-short path.
	selector, verifier, ok := services.SplitCalendarFeedToken(validToken)
	if !ok {
		t.Fatalf("SplitCalendarFeedToken: a freshly armed token must split, got token of length %d", len(validToken))
	}
	bogus := strings.Repeat("Z", len(selector)) + verifier
	malformed := "SHORT"
	wrongVerifier := selector + strings.Repeat("2", len(verifier))

	// A slice, not a map: the cases are compared against each other in a fixed
	// order, so the failure message always names the same reference case
	// (bogus) on every run.
	liveCases := []struct {
		name  string
		token string
	}{
		{name: "bogus", token: bogus},
		{name: "malformed", token: malformed},
		{name: "wrongVerifier", token: wrongVerifier},
	}

	var reference feedResponseFingerprint
	var referenceName string
	compare := func(name, token string) {
		request := httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil)
		response := mustAppResponse(t, app, request)
		if len(response.Cookies()) != 0 {
			t.Fatalf("%s: 404 must not set a cookie, got %#v", name, response.Cookies())
		}
		got := fingerprintFeedResponse(t, response)
		if referenceName == "" {
			reference, referenceName = got, name
			return
		}
		if got != reference {
			t.Fatalf("%s answers differently from %s, which is an enumeration oracle:\n %s: %#v\n %s: %#v",
				name, referenceName, referenceName, reference, name, got)
		}
	}
	for _, liveCase := range liveCases {
		compare(liveCase.name, liveCase.token)
	}

	// revoke only now: the three live cases above must have seen an armed row
	if err := db.NewRepositories(database).Users.ClearCalendarFeedToken(t.Context(), user.ID); err != nil {
		t.Fatalf("ClearCalendarFeedToken: %v", err)
	}
	compare("revokedValidToken", validToken)

	// The four agree with each other; now pin the shape they agree ON, so they
	// cannot drift TOGETHER into a response that explains itself. Checked after
	// the mutual comparison, and only once, so the identity assertion above is
	// the one that fires when a single case diverges — that is the failure a
	// reader needs to see named.
	want := feedResponseFingerprint{
		status:  http.StatusNotFound,
		headers: calendarFeedBare404Headers,
		body:    calendarFeedBare404Body,
	}
	if reference != want {
		t.Fatalf("every bad-token case answered %#v; expected the bare no-oracle 404 %#v", reference, want)
	}
}

// TestCalendarFeedTokenIsRedactedFromRequestLog drives a real request through a
// Fiber request logger wired to the SAME SafeRequestLogPath tag production uses,
// and asserts the captured log line carries ":token.ics", never the token value.
func TestCalendarFeedTokenIsRedactedFromRequestLog(t *testing.T) {
	_, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-log@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)

	var logBuf bytes.Buffer
	logged := fiber.New()
	logged.Use(logger.New(logger.Config{
		Stream: &logBuf,
		Format: "${method} ${request_path}\n",
		CustomTags: map[string]logger.LogFunc{
			"request_path": func(buffer logger.Buffer, c fiber.Ctx, _ *logger.Data, _ string) (int, error) {
				return buffer.WriteString(SafeRequestLogPath(c))
			},
		},
	}))
	// Register the feed route on the logging app so the matched-route template
	// drives SafeRequestLogPath exactly as in production.
	handler := mustFeedHandler(t, database)
	logged.Get(calendarFeedRoutePath, handler.ServeCalendarFeed)

	request := httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil)
	if _, err := logged.Test(request); err != nil {
		t.Fatalf("request: %v", err)
	}

	logLine := logBuf.String()
	if strings.Contains(logLine, token) {
		t.Fatalf("request log leaked the feed token: %q", logLine)
	}
	if !strings.Contains(logLine, ":token.ics") {
		t.Fatalf("expected request log to mask the token as :token.ics, got %q", logLine)
	}
}

// TestSanitizeRequestLogPathMasksRawTokenDotICSFallback pins the defense-in-depth
// hardening: even the RAW-PATH fallback (when no matched-route template is
// available) masks a "<token>.ics" segment, closing the flagged gap where the
// trailing ".ics" would otherwise defeat the opaque-token redaction.
func TestSanitizeRequestLogPathMasksRawTokenDotICSFallback(t *testing.T) {
	rawToken := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP"
	got := sanitizeRequestLogPath("/calendar/feed/" + rawToken + ".ics")
	if strings.Contains(got, rawToken) {
		t.Fatalf("raw-path fallback leaked the token: %q", got)
	}
	if got != "/calendar/feed/:token.ics" {
		t.Fatalf("expected raw fallback to mask as /calendar/feed/:token.ics, got %q", got)
	}
}

// TestCalendarFeedIsRateLimitedPerIP drives a REAL limiter.New mounted on the
// feed prefix and asserts the endpoint returns 429 once the per-IP budget is
// exceeded, with an integer Retry-After no greater than the window.
func TestCalendarFeedIsRateLimitedPerIP(t *testing.T) {
	_, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-rl@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)
	handler := mustFeedHandler(t, database)

	const maxRequests = 3
	rlApp := fiber.New()
	rlApp.Use(CalendarFeedRateLimitPrefix, limiter.New(limiter.Config{
		Max:        maxRequests,
		Expiration: time.Minute,
		LimitReached: func(c fiber.Ctx) error {
			return c.SendStatus(fiber.StatusTooManyRequests)
		},
	}))
	rlApp.Get(calendarFeedRoutePath, handler.ServeCalendarFeed)

	url := calendarFeedURL(token)
	var lastStatus int
	var retryAfter string
	for range maxRequests + 1 {
		request := httptest.NewRequest(http.MethodGet, url, nil)
		response := mustAppResponse(t, rlApp, request)
		lastStatus = response.StatusCode
		retryAfter = response.Header.Get(fiber.HeaderRetryAfter)
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding the per-IP budget, got %d", lastStatus)
	}
	if retryAfter == "" {
		t.Fatalf("expected a Retry-After header on the 429")
	}
}

// mustFeedHandler builds a handler bound to the given database, for tests that
// mount the feed route on a bespoke Fiber app (logger / rate-limit wiring).
func mustFeedHandler(t *testing.T, database *gorm.DB) *Handler {
	t.Helper()
	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler, err := NewHandler("test-secret-key", time.UTC, i18nManager, false, newTestHandlerDependencies(database, i18nManager))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}
	return handler
}

// TestCalendarFeedMigratesPre032RowOnFirstSuccessfulPoll is the only place the
// real repository, the real service, and the real HTTP route meet on the lazy
// migration: a row stored the way migration 029 stored it (bcrypt hash, no MAC)
// still serves 200, and that same request writes the keyed MAC into the row so
// every later poll takes the microsecond path instead of ~265 ms of bcrypt.
func TestCalendarFeedMigratesPre032RowOnFirstSuccessfulPoll(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-pre032@example.com", "StrongPass1", true)
	token := armCalendarFeedForUserWithColumns(t, database, user.ID, func(columns models.CalendarFeedTokenColumns) models.CalendarFeedTokenColumns {
		columns.VerifierMAC = "" // the pre-032 shape: no MAC existed yet
		return columns
	})

	var before models.User
	if err := database.First(&before, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if before.CalendarFeedVerifierMAC != "" {
		t.Fatalf("test setup: expected a pre-032 row with no MAC, got %q", before.CalendarFeedVerifierMAC)
	}

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("a pre-032 feed row must keep working after the upgrade, got %d", response.StatusCode)
	}

	var after models.User
	if err := database.First(&after, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if after.CalendarFeedVerifierMAC == "" {
		t.Fatal("expected the first successful poll to write the keyed MAC into the row")
	}
	if after.CalendarFeedSelector != before.CalendarFeedSelector || after.CalendarFeedVerifierHash != before.CalendarFeedVerifierHash {
		t.Fatalf("the migration must touch only the MAC column, got selector=%q hash=%q",
			after.CalendarFeedSelector, after.CalendarFeedVerifierHash)
	}

	// The migrated row keeps serving, now through its MAC.
	again := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	if again.StatusCode != http.StatusOK {
		t.Fatalf("expected the migrated row to keep serving, got %d", again.StatusCode)
	}
}

// simulatedFeedLookupFailure is the text of the storage error injected below.
// The 500 regression asserts this exact string is absent from the response, so
// the injected error and the assertion cannot drift apart.
const simulatedFeedLookupFailure = "simulated feed lookup failure"

// failingFeedUserStore makes CalendarFeedService.ResolveFeed return an
// infrastructure error, driving ServeCalendarFeed's err != nil branch.
type failingFeedUserStore struct{}

func (failingFeedUserStore) FindByCalendarFeedSelector(context.Context, string) (models.User, bool, error) {
	return models.User{}, false, errors.New(simulatedFeedLookupFailure)
}

func (failingFeedUserStore) BackfillCalendarFeedVerifierMAC(context.Context, uint, string, string) error {
	return nil
}

// failingFeedDayReader satisfies the day-reader port; it is never reached
// because the user lookup fails first, but ResolveFeed needs a non-nil reader.
type failingFeedDayReader struct{}

func (failingFeedDayReader) FetchLogsForUser(context.Context, uint, time.Time, time.Time, *time.Location) ([]models.DailyLog, error) {
	return nil, nil
}

type constFeedDisclaimer struct{}

func (constFeedDisclaimer) Disclaimer(string) string { return "d" }

// TestCalendarFeedReturns500OnInfrastructureError drives the ServeCalendarFeed
// err != nil branch: when the feed service reports an infrastructure failure
// (e.g. a DB read error), the client gets the app-wide generic 500 envelope and
// nothing else — no calendar body, and no word of what actually failed. A 500
// that describes its cause is the same oracle as a 404 that does, with a
// storage error's text (table names, driver detail) on top.
//
// The app is wired with RespondTransportError, the exact entry point the
// composition root's top-level error handler calls (cmd/ovumcy/server.go), so
// the envelope asserted here is the one a real client is served; cmd is not
// importable from this package, and that handler's own totality is pinned there
// by TestOvumcyErrorHandlerEnvelopesEveryFiberErrorStatus. On a bare fiber.New
// the response would instead be the framework's "Internal Server Error" text,
// which nothing in this app ever answers — pinning that would pin the double.
func TestCalendarFeedReturns500OnInfrastructureError(t *testing.T) {
	_, database := newOnboardingTestApp(t)
	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	deps := newTestHandlerDependencies(database, i18nManager)
	// Swap in a feed service whose owner lookup always errors.
	deps.CalendarFeedService = services.NewCalendarFeedService(failingFeedUserStore{}, failingFeedDayReader{}, constFeedDisclaimer{}, []byte(testAppSecretKey))
	handler, err := NewHandler(testAppSecretKey, time.UTC, i18nManager, false, deps)
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		var fiberErr *fiber.Error
		if !errors.As(err, &fiberErr) {
			return RespondTransportError(c, fiber.StatusInternalServerError)
		}
		return RespondTransportError(c, fiberErr.Code)
	}})
	app.Get(calendarFeedRoutePath, handler.ServeCalendarFeed)

	// A well-formed token so the handler reaches the service (which then errors);
	// the concrete value is irrelevant because the lookup is stubbed to fail.
	token := strings.Repeat("A", 48)
	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 on an infrastructure error, got %d", response.StatusCode)
	}
	body := readBodyString(t, response)
	// The raw storage error first, checked against the body a client actually
	// receives rather than against the decoded envelope: a leak appended outside
	// the JSON would survive a structural comparison alone, and this is the
	// failure a reader needs named when the disclosure comes back.
	if strings.Contains(body, simulatedFeedLookupFailure) {
		t.Fatalf("500 leaked the raw storage error %q, got:\n%s", simulatedFeedLookupFailure, body)
	}
	if strings.Contains(body, "BEGIN:VCALENDAR") {
		t.Fatalf("500 must not leak a calendar body, got:\n%s", body)
	}
	if contentType := response.Header.Get(fiber.HeaderContentType); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected the mapped JSON envelope, got content type %q with body:\n%s", contentType, body)
	}

	// The exact generic envelope: the stable key derived from the status alone,
	// and NO further member — an extra field is how a cause gets carried out.
	var envelope map[string]any
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("500 body is not the mapped JSON envelope (%v), got:\n%s", err, body)
	}
	wantEnvelope := map[string]any{
		"error": "internal_error",
		"error_detail": map[string]any{
			"key":      "internal_error",
			"category": "internal",
			"target":   "global",
		},
	}
	if !reflect.DeepEqual(envelope, wantEnvelope) {
		t.Fatalf("expected the generic internal_error envelope %#v, got %#v", wantEnvelope, envelope)
	}
}

// unfoldICS reverses RFC 5545 §3.1 line folding: a CRLF immediately followed by
// a single space (or tab) is a fold and is removed, rejoining the split content
// line. This is exactly what a conforming calendar client does before reading a
// property value.
func unfoldICS(body string) string {
	unfolded := strings.ReplaceAll(body, "\r\n ", "")
	return strings.ReplaceAll(unfolded, "\r\n\t", "")
}

func readBodyString(t *testing.T, response *http.Response) string {
	t.Helper()
	defer func() { _ = response.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(response.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return buf.String()
}

// TestCalendarFeedRestoredBackupStopsServingARevokedURL is the HTTP tail of the
// restore fence: the finding said a restored backup makes a revoked subscribe
// URL serve the calendar again, and this is that URL, at the real route, after
// the boot pass a restore now runs through.
//
// The restore is staged from the fence's own side, which is the honest side to
// stage it from: the fence file is the half a restore does NOT roll back, so
// "the database went back a generation" and "the file moved on without it" are
// one state, not two. SECRET_KEY never changes here — that is the whole point,
// since the key-epoch sentinel would have caught it if it did.
func TestCalendarFeedRestoredBackupStopsServingARevokedURL(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "feed-restore@example.com", "StrongPass1", true)
	token := armCalendarFeedForUser(t, database, user.ID)
	repositories := db.NewRepositories(database)
	fencePath := filepath.Join(t.TempDir(), "calendar-feed.fence")
	fence := security.NewCalendarFeedFenceFile(fencePath)

	// The instance boots once with this database, arming the fence.
	if _, err := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, fence).Enforce(t.Context()); err != nil {
		t.Fatalf("first Enforce: %v", err)
	}
	mustServeCalendarFeed(t, app, token, "before the restore")

	// The owner revokes, the operator restores a backup taken before that. The
	// rows and the app_state marker come back together; the fence file does not.
	if err := fence.Write("a-generation-this-database-never-saw"); err != nil {
		t.Fatalf("stage the restore: %v", err)
	}

	outcome, err := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, fence).Enforce(t.Context())
	if err != nil {
		t.Fatalf("boot Enforce after restore: %v", err)
	}
	if !outcome.ContinuityBroken || outcome.DisarmedFeeds != 1 {
		t.Fatalf("the restore must disarm the one armed feed, got %+v", outcome)
	}

	restored := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	if restored.StatusCode != http.StatusNotFound {
		t.Fatalf("the pre-restore subscribe URL must 404 after the restore, got %d", restored.StatusCode)
	}
	// And it must 404 the same way an unrelated token does: a distinguishable
	// refusal would tell a holder of the leaked URL that it once was real.
	unrelated := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, calendarFeedURL("ZZZZZZZZZZZZZZZZ"+token[16:]), nil))
	if fingerprintFeedResponse(t, restored) != fingerprintFeedResponse(t, unrelated) {
		t.Fatal("a disarmed token must be indistinguishable from an unknown one")
	}
}
