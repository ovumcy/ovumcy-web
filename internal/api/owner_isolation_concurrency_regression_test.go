package api

// owner_isolation_concurrency_regression_test.go drives the privacy boundary —
// "every read of per-user data is scoped to the authenticated session's
// user_id" — under genuine request concurrency, on the household shape where
// two independent owners share one deployment.
//
// The static half of that guarantee is already covered: the persistence sweep
// scopes every query, and the IDOR regressions pin the handler boundary. What
// none of them exercise is two owners' requests running *at the same time*
// through one app, where a shared object could carry one owner's data into the
// other's response. The product holds exactly three places where two requests
// can touch the same memory — the `responseHeaderBaselines` pool the deadline
// guard rolls headers back through, GORM session handles, and two mutexes that
// carry no health data — and each is safe by construction; this test is the
// behavioural proof standing behind that reading, so a future change that
// breaks one of them fails here rather than in production.
//
// Both owners write and read the SAME dates, with per-owner note markers, so a
// scope that broke would return a populated row belonging to the other account
// instead of a harmless 404. Three assertions run on every response: the
// payload's own `user_id`/identity, the absence of the other owner's marker,
// email and cookie values anywhere in the body or in Set-Cookie, and — the
// positive anchor a negative-only probe needs — that every read actually
// returned this owner's own data, counted and compared against the expected
// total at the end. Without that anchor a suite of empty 401s would pass as
// "no leakage observed".

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	// Six workers per owner keeps twelve requests genuinely in flight against
	// the single SQLite writer without pushing the day-save queue past the
	// request budget; the iterations then give every worker repeated passes
	// over the same rows, which is where a shared object would show up.
	isolationWorkersPerOwner = 6
	isolationIterations      = 8
	isolationWindowDays      = 4
)

// raceOwner is one authenticated owner plus everything that must never appear
// in its responses: the other owner's note marker, email and cookie values.
type raceOwner struct {
	ctx        settingsSecurityTestContext
	marker     string
	foreign    []string // substrings that must never surface on this owner's session
	ownReads   int64    // successful reads that returned this owner's own data
	windowFrom string
	windowTo   string
}

// raceDay is the slice of the day DTO this probe reads. The full shape is
// pinned by the day regressions; here only ownership and the marker matter.
type raceDay struct {
	UserID uint   `json:"user_id"`
	Date   string `json:"date"`
	Notes  string `json:"notes"`
}

type raceCurrentUser struct {
	ID    uint   `json:"id"`
	Email string `json:"email"`
}

func TestOwnerIsolationHoldsUnderConcurrentCrossOwnerRequests(t *testing.T) {
	ownerACtx := newSettingsSecurityTestContext(t, "race-isolation-a@example.com")
	ownerBCtx := signInSecondOwner(t, ownerACtx, "race-isolation-b@example.com")

	baseDay := time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)
	window := make([]string, 0, isolationWindowDays)
	for offset := range isolationWindowDays {
		window = append(window, baseDay.AddDate(0, 0, offset).Format("2006-01-02"))
	}
	windowFrom := window[0]
	windowTo := window[len(window)-1]

	ownerA := &raceOwner{
		ctx:        ownerACtx,
		marker:     "MARKER-OWNER-ALPHA",
		foreign:    foreignSecrets(ownerBCtx, "MARKER-OWNER-BRAVO"),
		windowFrom: windowFrom,
		windowTo:   windowTo,
	}
	ownerB := &raceOwner{
		ctx:        ownerBCtx,
		marker:     "MARKER-OWNER-BRAVO",
		foreign:    foreignSecrets(ownerACtx, "MARKER-OWNER-ALPHA"),
		windowFrom: windowFrom,
		windowTo:   windowTo,
	}

	// Seed both owners' rows serially, so every concurrent read below has its
	// own data to find: a read that legitimately returns nothing cannot serve
	// as the positive anchor.
	for _, owner := range []*raceOwner{ownerA, ownerB} {
		for _, date := range window {
			if failure := owner.saveDay(date, owner.marker+"-seed"); failure != "" {
				t.Fatalf("seed %s for %s: %s", date, owner.ctx.user.Email, failure)
			}
		}
	}

	var waitGroup sync.WaitGroup
	start := make(chan struct{})
	for _, owner := range []*raceOwner{ownerA, ownerB} {
		for worker := range isolationWorkersPerOwner {
			waitGroup.Add(1)
			go func(owner *raceOwner, worker int) {
				defer waitGroup.Done()
				<-start
				for iteration := range isolationIterations {
					date := window[(worker+iteration)%len(window)]
					if failure := owner.runOnePass(worker, iteration, date); failure != "" {
						// One report per worker: the first crossing already
						// answers the question, and a flood of identical lines
						// would bury it.
						t.Errorf("owner %s worker %d iteration %d: %s", owner.ctx.user.Email, worker, iteration, failure)
						return
					}
				}
			}(owner, worker)
		}
	}
	close(start)
	waitGroup.Wait()

	// Positive anchor. Every pass performs three reads that must return this
	// owner's own data (day, range, identity); a run that leaked nothing
	// because it read nothing must not pass.
	wantReads := int64(isolationWorkersPerOwner * isolationIterations * 3)
	for _, owner := range []*raceOwner{ownerA, ownerB} {
		if got := atomic.LoadInt64(&owner.ownReads); got != wantReads {
			t.Errorf("owner %s completed %d own-data reads, want %d — the isolation assertions above ran on fewer responses than the probe issued",
				owner.ctx.user.Email, got, wantReads)
		}
	}

	// Persisted state is the second half of the question: a response can look
	// correct while a write landed on the other owner's row.
	assertPersistedRowsStayOwnScoped(t, ownerA, ownerB)
}

// foreignSecrets lists the values of another owner's session that must never
// appear in this owner's responses: its note marker, its account email and the
// raw values of both session cookies.
func foreignSecrets(other settingsSecurityTestContext, otherMarker string) []string {
	secrets := []string{otherMarker, other.user.Email}
	if _, value, found := strings.Cut(other.authCookie, "="); found && strings.TrimSpace(value) != "" {
		secrets = append(secrets, value)
	}
	if other.csrfCookie != nil && strings.TrimSpace(other.csrfCookie.Value) != "" {
		secrets = append(secrets, other.csrfCookie.Value)
	}
	return secrets
}

// runOnePass performs one write and three reads on this owner's session and
// returns the first isolation failure it sees, or "" when everything held.
func (owner *raceOwner) runOnePass(worker int, iteration int, date string) string {
	notes := fmt.Sprintf("%s-w%d-i%d", owner.marker, worker, iteration)
	if failure := owner.saveDay(date, notes); failure != "" {
		return failure
	}
	if failure := owner.readDay(date, notes); failure != "" {
		return failure
	}
	if failure := owner.readWindow(); failure != "" {
		return failure
	}
	return owner.readIdentity()
}

func (owner *raceOwner) saveDay(date string, notes string) string {
	body, err := json.Marshal(map[string]any{
		"is_period":   false,
		"flow":        models.FlowNone,
		"symptom_ids": []uint{},
		"notes":       notes,
	})
	if err != nil {
		return fmt.Sprintf("marshal day payload: %v", err)
	}

	response, payload, failure := owner.request(http.MethodPut, "/api/v1/days/"+date, string(body))
	if failure != "" {
		return failure
	}
	var saved raceDay
	if err := json.Unmarshal(payload, &saved); err != nil {
		return fmt.Sprintf("decode day upsert response: %v", err)
	}
	if saved.UserID != owner.ctx.user.ID {
		return fmt.Sprintf("day upsert answered with user_id %d, want %d (%s)", saved.UserID, owner.ctx.user.ID, response.Status)
	}
	if saved.Notes != notes {
		return fmt.Sprintf("day upsert echoed notes %q, want %q", saved.Notes, notes)
	}
	return ""
}

func (owner *raceOwner) readDay(date string, notes string) string {
	_, payload, failure := owner.request(http.MethodGet, "/api/v1/days/"+date, "")
	if failure != "" {
		return failure
	}
	var day raceDay
	if err := json.Unmarshal(payload, &day); err != nil {
		return fmt.Sprintf("decode day read response: %v", err)
	}
	if day.UserID != owner.ctx.user.ID {
		return fmt.Sprintf("day read answered with user_id %d, want %d", day.UserID, owner.ctx.user.ID)
	}
	// The row is shared with the other owner's concurrent writes only in the
	// sense that both own a row for this date; this session must see its own,
	// carrying its own marker. Which iteration's note won the race is not
	// asserted — that is ordinary last-write-wins on this owner's own row.
	if !strings.HasPrefix(day.Notes, owner.marker) {
		return fmt.Sprintf("day read returned notes %q, which do not carry this owner's marker %q (own write was %q)", day.Notes, owner.marker, notes)
	}
	atomic.AddInt64(&owner.ownReads, 1)
	return ""
}

func (owner *raceOwner) readWindow() string {
	path := fmt.Sprintf("/api/v1/days?from=%s&to=%s", owner.windowFrom, owner.windowTo)
	_, payload, failure := owner.request(http.MethodGet, path, "")
	if failure != "" {
		return failure
	}
	var days []raceDay
	if err := json.Unmarshal(payload, &days); err != nil {
		return fmt.Sprintf("decode day range response: %v", err)
	}
	if len(days) == 0 {
		return "day range read returned no rows; this owner seeded the whole window, so an empty answer means the read never reached its own data"
	}
	for _, day := range days {
		if day.UserID != owner.ctx.user.ID {
			return fmt.Sprintf("day range read returned a row owned by user_id %d on %s, want only %d", day.UserID, day.Date, owner.ctx.user.ID)
		}
		if day.Notes != "" && !strings.HasPrefix(day.Notes, owner.marker) {
			return fmt.Sprintf("day range read returned notes %q on %s, which do not carry this owner's marker %q", day.Notes, day.Date, owner.marker)
		}
	}
	atomic.AddInt64(&owner.ownReads, 1)
	return ""
}

func (owner *raceOwner) readIdentity() string {
	_, payload, failure := owner.request(http.MethodGet, "/api/v1/users/current", "")
	if failure != "" {
		return failure
	}
	var current raceCurrentUser
	if err := json.Unmarshal(payload, &current); err != nil {
		return fmt.Sprintf("decode current user response: %v", err)
	}
	if current.ID != owner.ctx.user.ID || current.Email != owner.ctx.user.Email {
		return fmt.Sprintf("session resolved to id %d / %q, want %d / %q", current.ID, current.Email, owner.ctx.user.ID, owner.ctx.user.Email)
	}
	atomic.AddInt64(&owner.ownReads, 1)
	return ""
}

// request issues one request on this owner's session and screens the whole
// response — status, Set-Cookie values and body — for anything belonging to
// the other owner, before the caller inspects the payload.
func (owner *raceOwner) request(method string, path string, body string) (*http.Response, []byte, string) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	}
	request.Header.Set("Accept", fiber.MIMEApplicationJSON)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("X-Ovumcy-Timezone", "UTC")
	request.Header.Set("X-CSRF-Token", owner.ctx.csrfToken)
	request.Header.Set("Cookie", joinCookieHeader(owner.ctx.authCookie, cookiePair(owner.ctx.csrfCookie), "ovumcy_tz=UTC"))

	response, err := owner.ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		return nil, nil, fmt.Sprintf("%s %s failed: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Sprintf("%s %s: read body: %v", method, path, err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Sprintf("%s %s answered %d, want 200: %s", method, path, response.StatusCode, strings.TrimSpace(string(payload)))
	}

	for _, secret := range owner.foreign {
		if strings.Contains(string(payload), secret) {
			return nil, nil, fmt.Sprintf("%s %s body carries the other owner's value %q", method, path, secret)
		}
		for _, cookie := range response.Cookies() {
			if cookie.Value == secret {
				return nil, nil, fmt.Sprintf("%s %s set cookie %s to the other owner's value", method, path, cookie.Name)
			}
		}
	}
	return response, payload, ""
}

// assertPersistedRowsStayOwnScoped reads the two owners' rows straight out of
// the database. A response-level check cannot see a write that landed on the
// other owner's row and was answered from the writer's own in-memory entry, so
// the stored state is asserted separately: every row carries its owner's
// marker, and both owners hold a row for every date in the shared window.
func assertPersistedRowsStayOwnScoped(t *testing.T, owners ...*raceOwner) {
	t.Helper()

	for _, owner := range owners {
		var rows []models.DailyLog
		if err := owner.ctx.database.Where("user_id = ?", owner.ctx.user.ID).Find(&rows).Error; err != nil {
			t.Fatalf("load persisted rows for %s: %v", owner.ctx.user.Email, err)
		}
		if len(rows) < isolationWindowDays {
			t.Errorf("owner %s persisted %d day rows, want at least %d (the seeded window)", owner.ctx.user.Email, len(rows), isolationWindowDays)
		}
		for _, row := range rows {
			if row.Notes == "" {
				continue // auto-fill may add markerless rows; they carry no cross-owner signal
			}
			if !strings.HasPrefix(row.Notes, owner.marker) {
				t.Errorf("owner %s holds a persisted row on %s with notes %q, which do not carry its own marker %q",
					owner.ctx.user.Email, row.Date.Format("2006-01-02"), row.Notes, owner.marker)
			}
		}
	}
}
