package services

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

var opaqueCalendarFeedUID = regexp.MustCompile(`^[0-9a-f]{32}@ovumcy$`)

// buildTestFeedICS renders under the package's test SECRET_KEY unless the input
// names its own: with none the builder withholds every event. A fixture owner
// with no id would be withheld the same way, so an assertion that the feed
// lacks an event would pass for the wrong reason — it is refused here instead.
func buildTestFeedICS(input CalendarFeedICSInput) []byte {
	if input.User != nil && input.User.ID == 0 {
		panic("buildTestFeedICS: the fixture owner has no id, so the feed would carry no event")
	}
	if input.SecretKey == nil {
		input.SecretKey = []byte(calendarFeedTestSecretKey)
	}
	return BuildCalendarFeedICS(input)
}

// feedTestUID is the UID buildTestFeedICS gives ownerID's event of this kind on
// date (YYYYMMDD).
func feedTestUID(t *testing.T, ownerID uint, kind string, date string) string {
	t.Helper()
	return feedTestUIDs(t, ownerID).UID(kind, date)
}

func feedTestUIDs(t *testing.T, ownerID uint) *security.CalendarFeedEventUIDs {
	t.Helper()
	uids, err := security.NewCalendarFeedEventUIDs([]byte(calendarFeedTestSecretKey), ownerID)
	if err != nil {
		t.Fatalf("derive UID key: %v", err)
	}
	return uids
}

// feedCarriesKind reports whether renderConfirmedFeed's input resolves an event
// of kind: the body no longer names it, so a test asking which kinds ship
// asks the resolver the body is rendered from.
func feedCarriesKind(user *models.User, logs []models.DailyLog, now time.Time, kind string) bool {
	for _, event := range calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: time.UTC}) {
		if event.kind == kind {
			return true
		}
	}
	return false
}

func renderUIDTestFeed(t *testing.T, user *models.User, now string, secretKey string) []string {
	t.Helper()
	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       predictableFeedLogs(t),
		Now:        mustParseDashboardDay(t, now),
		Location:   time.UTC,
		Disclaimer: "disclaimer",
		SecretKey:  []byte(secretKey),
	}))
	uids := extractICSUIDs(t, body)
	if len(uids) == 0 {
		t.Fatalf("fixture: expected prediction events, got:\n%s", body)
	}
	return uids
}

func assertNoSharedUID(t *testing.T, first []string, second []string, what string) {
	t.Helper()
	seen := map[string]bool{}
	for _, uid := range first {
		seen[uid] = true
	}
	for _, uid := range second {
		if seen[uid] {
			t.Fatalf("UID %q is shared by %s for the same event", uid, what)
		}
	}
}

// TestCalendarFeedUIDNamesNoEventKind (WEB-234): the SUMMARY is neutral so a
// calendar viewer learns nothing about cycle phase, and the UID — shown in
// event details and exports — must keep that promise too.
func TestCalendarFeedUIDNamesNoEventKind(t *testing.T) {
	for _, uid := range renderUIDTestFeed(t, predictableFeedUser(t, "2026-03-02"), "2026-03-20", calendarFeedTestSecretKey) {
		lower := strings.ToLower(uid)
		if strings.Contains(lower, "period") || strings.Contains(lower, "ovulation") || strings.Contains(lower, "window") {
			t.Fatalf("UID %q names the event kind", uid)
		}
		if !opaqueCalendarFeedUID.MatchString(uid) {
			t.Fatalf("UID %q is not the opaque 32-hex form", uid)
		}
	}
}

// TestCalendarFeedUIDIsStableForTheSameDay: a client updates an event in place
// only while its UID holds, so two polls of the same owner give the same UIDs,
// whether on the same day or a later one.
func TestCalendarFeedUIDIsStableForTheSameDay(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	first := renderUIDTestFeed(t, user, "2026-03-20", calendarFeedTestSecretKey)
	again := renderUIDTestFeed(t, user, "2026-03-20", calendarFeedTestSecretKey)
	later := renderUIDTestFeed(t, user, "2026-03-21", calendarFeedTestSecretKey)
	if diff := cmpStringSets(first, again); diff != "" {
		t.Fatalf("same-day renders minted different UIDs: %s", diff)
	}
	if diff := cmpStringSets(first, later); diff != "" {
		t.Fatalf("next-day render minted different UIDs: %s", diff)
	}
}

// TestCalendarFeedUIDDiffersPerOwner: two owners on the same cycle get the same
// event dates, and their UIDs must still not match — a shared calendar holding
// both feeds otherwise links them, and an unkeyed UID is a dictionary away
// from naming the kind.
func TestCalendarFeedUIDDiffersPerOwner(t *testing.T) {
	first := predictableFeedUser(t, "2026-03-02")
	second := predictableFeedUser(t, "2026-03-02")
	second.ID = first.ID + 1
	assertNoSharedUID(t,
		renderUIDTestFeed(t, first, "2026-03-20", calendarFeedTestSecretKey),
		renderUIDTestFeed(t, second, "2026-03-20", calendarFeedTestSecretKey),
		"two owners")
}

// TestCalendarFeedUIDFollowsTheSecretKey: the UID is keyed by SECRET_KEY, so a
// rotation renames every event — the rotation already disarms the feed, and
// the new key never reproduces a UID minted under the old one.
func TestCalendarFeedUIDFollowsTheSecretKey(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	assertNoSharedUID(t,
		renderUIDTestFeed(t, user, "2026-03-20", calendarFeedTestSecretKey),
		renderUIDTestFeed(t, user, "2026-03-20", calendarFeedTestSecretKey+"-rotated"),
		"two SECRET_KEYs")
}

// TestCalendarFeedWithoutAUIDKeyCarriesNoEvent: with no SECRET_KEY or no owner
// id there is nothing to key the UID with, and the builder withholds the events
// rather than name one by its kind. A zero id is invalid input, never a
// reason to key every ownerless render alike.
func TestCalendarFeedWithoutAUIDKeyCarriesNoEvent(t *testing.T) {
	ownerless := predictableFeedUser(t, "2026-03-02")
	ownerless.ID = 0
	for name, input := range map[string]CalendarFeedICSInput{
		"no secret key": {User: predictableFeedUser(t, "2026-03-02")},
		"zero owner id": {User: ownerless, SecretKey: []byte(calendarFeedTestSecretKey)},
	} {
		input.Logs = predictableFeedLogs(t)
		input.Now = mustParseDashboardDay(t, "2026-03-20")
		input.Location = time.UTC
		if len(calendarFeedEvents(input)) == 0 {
			t.Fatalf("%s: fixture must resolve events", name)
		}
		body := string(BuildCalendarFeedICS(input))
		if strings.Contains(body, "BEGIN:VEVENT") || !strings.Contains(body, "END:VCALENDAR") {
			t.Fatalf("%s: want a well-formed feed with no event, got:\n%s", name, body)
		}
	}
}
