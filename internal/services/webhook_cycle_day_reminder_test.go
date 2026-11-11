package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// --- decision fixtures -------------------------------------------------------

// cycleDayTestStart is the running cycle start of the decision cases: three
// earlier explicit starts one 28-day cycle apart give an average of 28, so the
// reference length the held rule measures against is 28.
var cycleDayTestStart = time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC)

func cycleDayStartLog(day time.Time) models.DailyLog {
	return models.DailyLog{UserID: 1, Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}
}

// cycleDayHistory returns start plus three explicit starts before it, 28 days
// apart.
func cycleDayHistory(start time.Time) []models.DailyLog {
	return []models.DailyLog{
		cycleDayStartLog(start.AddDate(0, 0, -84)),
		cycleDayStartLog(start.AddDate(0, 0, -56)),
		cycleDayStartLog(start.AddDate(0, 0, -28)),
		cycleDayStartLog(start),
	}
}

// cycleDayInstant is hour:00 UTC on cycle day n of the cycle opened by start.
func cycleDayInstant(start time.Time, n int, hour int) time.Time {
	day := start.AddDate(0, 0, n-1)
	return time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, time.UTC)
}

func cycleDayTestRule(id uint, day int, hour int) models.CycleDayReminder {
	return models.CycleDayReminder{ID: id, UserID: 1, CycleDay: day, RemindAtHour: hour}
}

func cycleDayTestUser() *models.User {
	return &models.User{ID: 1, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
}

func timePointer(value time.Time) *time.Time { return &value }

// --- decision ----------------------------------------------------------------

func TestCycleDayDecisionFiresOnlyOnTheRuleDay(t *testing.T) {
	rules := []models.CycleDayReminder{cycleDayTestRule(1, 10, 9)}
	logs := cycleDayHistory(cycleDayTestStart)

	for _, n := range []int{9, 11} {
		decision := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, logs, cycleDayInstant(cycleDayTestStart, n, 12), time.UTC)
		if len(decision.Due) != 0 || decision.Hold != nil {
			t.Fatalf("cycle day %d: a day-10 rule must not fire, got %+v", n, decision)
		}
	}

	now := cycleDayInstant(cycleDayTestStart, 10, 12)
	decision := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, logs, now, time.UTC)
	if len(decision.Due) != 1 {
		t.Fatalf("cycle day 10: expected the rule due, got %+v", decision)
	}
	due := decision.Due[0]
	if due.CycleDay != 10 || !due.CycleStart.Equal(cycleDayTestStart) || due.Rule.ID != 1 {
		t.Fatalf("unexpected due entry: %+v", due)
	}
	if got := due.Day.Format("2006-01-02"); got != "2026-03-07" {
		t.Fatalf("due day = %s, want 2026-03-07", got)
	}
	if decision.Hold != nil || decision.Watermarked != 0 {
		t.Fatalf("no hold and no watermark expected, got %+v", decision)
	}
}

func TestCycleDayDecisionWaitsForTheRuleHour(t *testing.T) {
	rules := []models.CycleDayReminder{cycleDayTestRule(1, 10, 9)}
	logs := cycleDayHistory(cycleDayTestStart)

	before := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, logs, cycleDayInstant(cycleDayTestStart, 10, 8), time.UTC)
	if len(before.Due) != 0 {
		t.Fatalf("08:00 is before the 09 rule hour, got %+v", before.Due)
	}
	for _, hour := range []int{9, 23} {
		decision := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, logs, cycleDayInstant(cycleDayTestStart, 10, hour), time.UTC)
		if len(decision.Due) != 1 {
			t.Fatalf("%02d:00 is at/after the rule hour, expected due, got %+v", hour, decision)
		}
	}
}

func TestCycleDayDecisionWithoutACycleStartDecidesNothing(t *testing.T) {
	rules := []models.CycleDayReminder{cycleDayTestRule(1, 1, 0), cycleDayTestRule(2, 10, 0)}
	now := cycleDayInstant(cycleDayTestStart, 10, 12)

	decision := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, nil, now, time.UTC)
	if len(decision.Due) != 0 || decision.Hold != nil || decision.Watermarked != 0 {
		t.Fatalf("no cycle start must decide nothing, got %+v", decision)
	}
	if got := DecideDueCycleDayReminders(nil, rules, nil, cycleDayHistory(cycleDayTestStart), now, time.UTC); len(got.Due) != 0 || got.Hold != nil {
		t.Fatalf("a nil user must decide nothing, got %+v", got)
	}
}

func TestCycleDayDecisionPregnancyPauseDecidesNothing(t *testing.T) {
	rules := []models.CycleDayReminder{cycleDayTestRule(1, 10, 9)}
	now := cycleDayInstant(cycleDayTestStart, 10, 12)

	paused := append(cycleDayHistory(cycleDayTestStart), models.DailyLog{
		UserID: 1, Date: cycleDayTestStart.AddDate(0, 0, 3), PregnancyTest: models.PregnancyTestPositive,
	})
	decision := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, paused, now, time.UTC)
	if len(decision.Due) != 0 || decision.Hold != nil {
		t.Fatalf("a positive pregnancy test after the start must pause every rule, got %+v", decision)
	}

	control := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, cycleDayHistory(cycleDayTestStart), now, time.UTC)
	if len(control.Due) != 1 {
		t.Fatalf("control: the same owner without the test must be due, got %+v", control)
	}
}

func TestCycleDayDecisionWithholdsDayOneAndTwoOnAnUnsettledLoneDay(t *testing.T) {
	today := time.Date(2026, time.March, 26, 0, 0, 0, 0, time.UTC)
	previous := []models.DailyLog{
		cycleDayStartLog(today.AddDate(0, 0, -84)),
		cycleDayStartLog(today.AddDate(0, 0, -56)),
		cycleDayStartLog(today.AddDate(0, 0, -28)),
	}
	bleeding := func(day time.Time) models.DailyLog {
		return models.DailyLog{UserID: 1, Date: day, IsPeriod: true, Flow: models.FlowMedium}
	}
	now := time.Date(2026, time.March, 26, 12, 0, 0, 0, time.UTC)
	dayOne := []models.CycleDayReminder{cycleDayTestRule(1, 1, 9)}
	dayTwo := []models.CycleDayReminder{cycleDayTestRule(2, 2, 9)}

	// A lone bleeding day logged today opens a provisional cycle; the rule for
	// day 1 waits for it to settle.
	loneToday := append(append([]models.DailyLog{}, previous...), bleeding(today))
	if stats := BuildCycleStatsFromLogs(cycleDayTestUser(), loneToday, now, time.UTC); !CalendarDay(stats.LastPeriodStart, time.UTC).Equal(today) {
		t.Fatalf("precondition: the lone day must be the running start, got %v", stats.LastPeriodStart)
	}
	if decision := DecideDueCycleDayReminders(cycleDayTestUser(), dayOne, nil, loneToday, now, time.UTC); len(decision.Due) != 0 || decision.Hold != nil {
		t.Fatalf("a lone bleeding day today must withhold the day-1 rule, got %+v", decision)
	}
	explicitToday := append(append([]models.DailyLog{}, previous...), cycleDayStartLog(today))
	if decision := DecideDueCycleDayReminders(cycleDayTestUser(), dayOne, nil, explicitToday, now, time.UTC); len(decision.Due) != 1 {
		t.Fatalf("an explicit start today is settled: the day-1 rule must fire, got %+v", decision)
	}

	// A lone day yesterday makes today a provisional day 2.
	loneYesterday := append(append([]models.DailyLog{}, previous...), bleeding(today.AddDate(0, 0, -1)))
	if stats := BuildCycleStatsFromLogs(cycleDayTestUser(), loneYesterday, now, time.UTC); !CalendarDay(stats.LastPeriodStart, time.UTC).Equal(today.AddDate(0, 0, -1)) {
		t.Fatalf("precondition: yesterday's lone day must be the running start, got %v", stats.LastPeriodStart)
	}
	if decision := DecideDueCycleDayReminders(cycleDayTestUser(), dayTwo, nil, loneYesterday, now, time.UTC); len(decision.Due) != 0 || decision.Hold != nil {
		t.Fatalf("a lone bleeding day yesterday must withhold the day-2 rule, got %+v", decision)
	}
	consecutive := append(append([]models.DailyLog{}, previous...), bleeding(today.AddDate(0, 0, -1)), bleeding(today))
	decision := DecideDueCycleDayReminders(cycleDayTestUser(), dayTwo, nil, consecutive, now, time.UTC)
	if len(decision.Due) != 1 || decision.Due[0].CycleDay != 2 {
		t.Fatalf("two consecutive bleeding days are a settled start: the day-2 rule must fire, got %+v", decision)
	}
}

func TestCycleDayDecisionHoldsPastTheReferenceLength(t *testing.T) {
	logs := cycleDayHistory(cycleDayTestStart)
	label := models.CycleDayReminder{ID: 3, UserID: 1, CycleDay: 30, RemindAtHour: 9, Label: "x", SendLabel: true}

	atReference := DecideDueCycleDayReminders(cycleDayTestUser(), []models.CycleDayReminder{cycleDayTestRule(1, 28, 9)}, nil, logs, cycleDayInstant(cycleDayTestStart, 28, 10), time.UTC)
	if len(atReference.Due) != 1 || atReference.Hold != nil {
		t.Fatalf("day 28 of a 28-day reference is not past it, expected due, got %+v", atReference)
	}

	now := cycleDayInstant(cycleDayTestStart, 30, 10)
	held := DecideDueCycleDayReminders(cycleDayTestUser(), []models.CycleDayReminder{label, cycleDayTestRule(4, 30, 9)}, nil, logs, now, time.UTC)
	if len(held.Due) != 0 {
		t.Fatalf("a rule past the reference length must not be sent, got %+v", held.Due)
	}
	if held.Hold == nil || !held.Hold.CycleStart.Equal(cycleDayTestStart) || held.Hold.Day.Format("2006-01-02") != "2026-03-27" {
		t.Fatalf("one hold notice keyed by the cycle start expected, got %+v", held.Hold)
	}

	watermarked := DecideDueCycleDayReminders(cycleDayTestUser(), []models.CycleDayReminder{label}, timePointer(cycleDayTestStart), logs, now, time.UTC)
	if watermarked.Hold != nil || len(watermarked.Due) != 0 {
		t.Fatalf("a hold watermark equal to the start owes no second notice, got %+v", watermarked)
	}
	if watermarked.Watermarked != 1 {
		t.Fatalf("the covered hold counts as one idempotent skip, got %d", watermarked.Watermarked)
	}

	older := DecideDueCycleDayReminders(cycleDayTestUser(), []models.CycleDayReminder{label}, timePointer(cycleDayTestStart.AddDate(0, 0, -28)), logs, now, time.UTC)
	if older.Hold == nil {
		t.Fatalf("a hold watermark of the previous cycle owes a new notice, got %+v", older)
	}
}

func TestCycleDayDecisionSendsOncePerCycle(t *testing.T) {
	// The rule was sent on 2026-03-02 (day 5 of a cycle starting 02-26); the
	// start was then corrected forward to 02-28, which makes 03-04 day 5 again.
	corrected := time.Date(2026, time.February, 28, 0, 0, 0, 0, time.UTC)
	logs := cycleDayHistory(corrected)
	now := cycleDayInstant(corrected, 5, 12)

	sentInCycle := cycleDayTestRule(1, 5, 9)
	sentInCycle.LastSentOn = timePointer(time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC))
	decision := DecideDueCycleDayReminders(cycleDayTestUser(), []models.CycleDayReminder{sentInCycle}, nil, logs, now, time.UTC)
	if len(decision.Due) != 0 || decision.Watermarked != 1 {
		t.Fatalf("a send dated inside the running cycle must withhold the rule, got %+v", decision)
	}

	sentOnStart := cycleDayTestRule(2, 5, 9)
	sentOnStart.LastSentOn = timePointer(corrected)
	if got := DecideDueCycleDayReminders(cycleDayTestUser(), []models.CycleDayReminder{sentOnStart}, nil, logs, now, time.UTC); len(got.Due) != 0 {
		t.Fatalf("a send dated on the start belongs to this cycle, got %+v", got)
	}

	sentLastCycle := cycleDayTestRule(3, 5, 9)
	sentLastCycle.LastSentOn = timePointer(corrected.AddDate(0, 0, -24))
	fresh := DecideDueCycleDayReminders(cycleDayTestUser(), []models.CycleDayReminder{sentLastCycle}, nil, logs, now, time.UTC)
	if len(fresh.Due) != 1 || fresh.Watermarked != 0 {
		t.Fatalf("a send of the previous cycle must not withhold this one, got %+v", fresh)
	}
}

func TestCycleDayDecisionStillFiresForAnUnpredictableCycle(t *testing.T) {
	user := cycleDayTestUser()
	user.UnpredictableCycle = true
	user.IrregularCycle = true
	decision := DecideDueCycleDayReminders(user, []models.CycleDayReminder{cycleDayTestRule(1, 10, 9)}, nil, cycleDayHistory(cycleDayTestStart), cycleDayInstant(cycleDayTestStart, 10, 12), time.UTC)
	if len(decision.Due) != 1 {
		t.Fatalf("an observed cycle-day count does not depend on predictability, got %+v", decision)
	}
}

func TestCycleDayDecisionUsesTheOwnerLocationDay(t *testing.T) {
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Skipf("tzdata: %v", err)
	}
	losAngeles, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("tzdata: %v", err)
	}
	start := time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)
	logs := cycleDayHistory(start)
	// 2026-03-12 08:00 in Kiritimati (UTC+14) is 2026-03-11 11:00 in Los Angeles
	// (PDT): cycle day 10 at hour 11 there, day 11 at hour 8 in Kiritimati.
	now := time.Date(2026, time.March, 12, 8, 0, 0, 0, kiritimati)
	rules := []models.CycleDayReminder{cycleDayTestRule(1, 10, 9), cycleDayTestRule(2, 11, 0)}

	decision := DecideDueCycleDayReminders(cycleDayTestUser(), rules, nil, logs, now, losAngeles)
	if len(decision.Due) != 1 || decision.Due[0].Rule.ID != 1 || decision.Due[0].CycleDay != 10 {
		t.Fatalf("the decision must use the location's day and hour (day 10, 11:00), got %+v", decision)
	}
	if got := decision.Due[0].Day.Format("2006-01-02"); got != "2026-03-11" {
		t.Fatalf("due day = %s, want the Los Angeles day 2026-03-11", got)
	}
}

// --- notify fixtures ---------------------------------------------------------

const cycleDayLabelSentinel = "LABEL-SENTINEL-7f3"

// cycleDayCopy answers the cycle-day keys on top of the prediction copy.
type cycleDayCopy struct{ disclaimer string }

func (stub cycleDayCopy) Disclaimer(string) string { return stub.disclaimer }

func (stub cycleDayCopy) Message(language string, key string) string {
	switch key {
	case reminderCycleDayTitleKey:
		return "Cycle-day reminder"
	case reminderCycleDayMessageKey:
		return "Today is cycle day %d."
	case reminderCycleDayLabelMessageKey:
		return "Today is cycle day %d: %s"
	case reminderCycleDayHeldTitleKey:
		return "Cycle-day reminders paused"
	case reminderCycleDayHeldMessageKey:
		return "Your cycle has run past its usual length, so cycle-day reminders are paused."
	}
	return stubDisclaimer{}.Message(language, key)
}

type cycleDayStoreCall struct {
	userID        uint
	ruleID        uint
	day           time.Time
	previous      *time.Time
	configVersion int
}

type stubCycleDayStore struct {
	mu        sync.Mutex
	rules     map[uint][]models.CycleDayReminder
	claimLost bool
	claims    []cycleDayStoreCall
	releases  []cycleDayStoreCall
}

func (stub *stubCycleDayStore) ListByUser(_ context.Context, userID uint) ([]models.CycleDayReminder, error) {
	return stub.rules[userID], nil
}

func (stub *stubCycleDayStore) ClaimSend(_ context.Context, userID uint, ruleID uint, day time.Time, previous *time.Time, configVersion int) (bool, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.claims = append(stub.claims, cycleDayStoreCall{userID: userID, ruleID: ruleID, day: day, previous: previous, configVersion: configVersion})
	return !stub.claimLost, nil
}

func (stub *stubCycleDayStore) ReleaseSend(_ context.Context, userID uint, ruleID uint, day time.Time, previous *time.Time) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.releases = append(stub.releases, cycleDayStoreCall{userID: userID, ruleID: ruleID, day: day, previous: previous})
	return nil
}

// cycleDayNotifyRecord is an owner with delivery on and both prediction kinds
// off, so only cycle-day reminders can leave.
func cycleDayNotifyRecord(url string) models.WebhookNotifyRecord {
	return models.WebhookNotifyRecord{
		ID:                   1,
		CycleLength:          28,
		PeriodLength:         5,
		LutealPhase:          14,
		WebhookEnabled:       true,
		WebhookURL:           url,
		ReminderLeadDays:     3,
		WebhookConfigVersion: 7,
	}
}

func newCycleDayNotifyService(record models.WebhookNotifyRecord, logs []models.DailyLog, rules []models.CycleDayReminder, deliverer WebhookDeliverer, copyProvider NotifyCopyProvider) (*WebhookNotifyService, *stubNotifyRepo, *stubCycleDayStore) {
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	store := &stubCycleDayStore{rules: map[uint][]models.CycleDayReminder{record.ID: rules}}
	service := NewWebhookNotifyService(repo, stubLogReader{byUser: map[uint][]models.DailyLog{record.ID: logs}}, stubDecryptor{}, deliverer, copyProvider).
		WithCycleDayReminders(store)
	return service, repo, store
}

var cycleDayTestCopy = cycleDayCopy{disclaimer: "Estimates only, not medical advice."}

// --- notify ------------------------------------------------------------------

func TestCycleDayReminderPayloadOmitsTheLabelWithoutTheOptIn(t *testing.T) {
	rule := models.CycleDayReminder{ID: 5, UserID: 1, CycleDay: 10, RemindAtHour: 9, Label: cycleDayLabelSentinel, SendLabel: false}
	logs := cycleDayHistory(cycleDayTestStart)
	now := cycleDayInstant(cycleDayTestStart, 10, 12)

	// JSON envelope.
	deliverer := &stubDeliverer{}
	service, _, _ := newCycleDayNotifyService(cycleDayNotifyRecord("https://hook.example/a"), logs, []models.CycleDayReminder{rule}, deliverer, cycleDayTestCopy)
	report, err := service.RunCycleDayOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunCycleDayOnce: %v", err)
	}
	deliveries := deliverer.deliveries()
	if report.Sent != 1 || len(deliveries) != 1 {
		t.Fatalf("expected one delivered cycle-day reminder, report=%+v deliveries=%d", report, len(deliveries))
	}
	payload := deliveries[0].payload
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), cycleDayLabelSentinel) {
		t.Fatalf("the label left without the opt-in: %s", encoded)
	}
	if payload.Type != "cycle-day" || payload.CycleDay != 10 || !strings.Contains(string(encoded), `"cycle_day":10`) {
		t.Fatalf("payload must carry type cycle-day and cycle_day 10: %s", encoded)
	}
	if payload.Message != "Today is cycle day 10." || payload.Disclaimer == "" {
		t.Fatalf("unexpected message/disclaimer: %s", encoded)
	}

	// ntfy plain-text body.
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw)+"\n"+r.Header.Get("X-Title")+"\n"+r.Header.Get("X-Tags"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ntfyService, _, _ := newCycleDayNotifyService(cycleDayNotifyRecord(server.URL+"/topic?format=ntfy"), logs, []models.CycleDayReminder{rule}, NewWebhookDeliverer(false), cycleDayTestCopy)
	ntfyReport, err := ntfyService.RunCycleDayOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunCycleDayOnce (ntfy): %v", err)
	}
	mu.Lock()
	captured := append([]string(nil), bodies...)
	mu.Unlock()
	if ntfyReport.Sent != 1 || len(captured) != 1 {
		t.Fatalf("expected one ntfy delivery, report=%+v bodies=%d", ntfyReport, len(captured))
	}
	if strings.Contains(captured[0], cycleDayLabelSentinel) {
		t.Fatalf("the label left in the ntfy request without the opt-in: %q", captured[0])
	}
	if !strings.Contains(captured[0], "Today is cycle day 10.") {
		t.Fatalf("control: the ntfy body must carry the message, got %q", captured[0])
	}

	// Dry-run preview.
	dryService, _, dryStore := newCycleDayNotifyService(cycleDayNotifyRecord("https://hook.example/a"), logs, []models.CycleDayReminder{rule}, &stubDeliverer{}, cycleDayTestCopy)
	dryReport, err := dryService.RunCycleDayOnce(context.Background(), now, time.UTC, true)
	if err != nil {
		t.Fatalf("RunCycleDayOnce (dry run): %v", err)
	}
	if len(dryReport.DryRunPreview) != 1 || dryReport.DryRunPreview[0].Type != "cycle-day" {
		t.Fatalf("expected one cycle-day preview line, got %+v", dryReport.DryRunPreview)
	}
	if strings.Contains(fmt.Sprintf("%+v", dryReport), cycleDayLabelSentinel) {
		t.Fatalf("the label reached the dry-run report: %+v", dryReport)
	}
	if len(dryStore.claims) != 0 {
		t.Fatalf("a dry run must claim nothing, got %+v", dryStore.claims)
	}
}

func TestCycleDayReminderPayloadCarriesTheLabelWithTheOptIn(t *testing.T) {
	rule := models.CycleDayReminder{ID: 5, UserID: 1, CycleDay: 10, RemindAtHour: 9, Label: cycleDayLabelSentinel, SendLabel: true}
	deliverer := &stubDeliverer{}
	service, _, _ := newCycleDayNotifyService(cycleDayNotifyRecord("https://hook.example/a"), cycleDayHistory(cycleDayTestStart), []models.CycleDayReminder{rule}, deliverer, cycleDayTestCopy)
	if _, err := service.RunCycleDayOnce(context.Background(), cycleDayInstant(cycleDayTestStart, 10, 12), time.UTC, false); err != nil {
		t.Fatalf("RunCycleDayOnce: %v", err)
	}
	deliveries := deliverer.deliveries()
	if len(deliveries) != 1 {
		t.Fatalf("expected one delivery, got %d", len(deliveries))
	}
	if got := deliveries[0].payload.Message; got != "Today is cycle day 10: "+cycleDayLabelSentinel {
		t.Fatalf("an opted-in label must reach the message, got %q", got)
	}
}

func TestCycleDayHoldPayloadCarriesNoCycleDayOrLabel(t *testing.T) {
	rule := models.CycleDayReminder{ID: 5, UserID: 1, CycleDay: 30, RemindAtHour: 9, Label: cycleDayLabelSentinel, SendLabel: true}
	deliverer := &stubDeliverer{}
	service, repo, store := newCycleDayNotifyService(cycleDayNotifyRecord("https://hook.example/a"), cycleDayHistory(cycleDayTestStart), []models.CycleDayReminder{rule}, deliverer, cycleDayTestCopy)
	report, err := service.RunCycleDayOnce(context.Background(), cycleDayInstant(cycleDayTestStart, 30, 12), time.UTC, false)
	if err != nil {
		t.Fatalf("RunCycleDayOnce: %v", err)
	}
	deliveries := deliverer.deliveries()
	if report.Sent != 1 || len(deliveries) != 1 {
		t.Fatalf("expected the single hold notice, report=%+v deliveries=%d", report, len(deliveries))
	}
	payload := deliveries[0].payload
	encoded, _ := json.Marshal(payload)
	if payload.Type != "cycle-day-held" || payload.CycleDay != 0 || strings.Contains(string(encoded), "cycle_day") {
		t.Fatalf("hold payload must be type cycle-day-held without cycle_day: %s", encoded)
	}
	if strings.Contains(string(encoded), cycleDayLabelSentinel) {
		t.Fatalf("hold payload must never carry the label: %s", encoded)
	}
	if len(store.claims) != 0 {
		t.Fatalf("a held rule must not be claimed, got %+v", store.claims)
	}
	writes := repo.writes()
	if len(writes) != 1 || writes[0].reminderType != "cycle-day-held" || !writes[0].anchor.Equal(cycleDayTestStart) {
		t.Fatalf("the hold watermark must be claimed at the cycle start, got %+v", writes)
	}
	if len(repo.claimEpochs) != 1 || repo.claimEpochs[0] != 7 {
		t.Fatalf("the hold claim must pin the snapshot's config version, got %v", repo.claimEpochs)
	}
}

func TestCycleDayReminderWithheldOnABlankDisclaimer(t *testing.T) {
	deliverer := &stubDeliverer{}
	service, repo, store := newCycleDayNotifyService(cycleDayNotifyRecord("https://hook.example/a"), cycleDayHistory(cycleDayTestStart),
		[]models.CycleDayReminder{cycleDayTestRule(5, 10, 9)}, deliverer, cycleDayCopy{disclaimer: "  "})
	report, err := service.RunCycleDayOnce(context.Background(), cycleDayInstant(cycleDayTestStart, 10, 12), time.UTC, false)
	if err != nil {
		t.Fatalf("RunCycleDayOnce: %v", err)
	}
	if report.WithheldDisclaimer != 1 || report.Due != 0 || report.Sent != 0 {
		t.Fatalf("a blank disclaimer must withhold the reminder, got %+v", report)
	}
	if len(deliverer.deliveries()) != 0 || len(store.claims) != 0 || len(repo.writes()) != 0 {
		t.Fatalf("nothing may be delivered or claimed: deliveries=%d claims=%+v writes=%+v", len(deliverer.deliveries()), store.claims, repo.writes())
	}
}

func TestCycleDayReminderFailedDeliveryReleasesTheClaim(t *testing.T) {
	previous := cycleDayTestStart.AddDate(0, 0, -19)
	rule := cycleDayTestRule(5, 10, 9)
	rule.LastSentOn = &previous
	deliverer := &stubDeliverer{failEvery: true}
	service, _, store := newCycleDayNotifyService(cycleDayNotifyRecord("https://hook.example/a"), cycleDayHistory(cycleDayTestStart), []models.CycleDayReminder{rule}, deliverer, cycleDayTestCopy)
	report, err := service.RunCycleDayOnce(context.Background(), cycleDayInstant(cycleDayTestStart, 10, 12), time.UTC, false)
	if err != nil {
		t.Fatalf("RunCycleDayOnce: %v", err)
	}
	if report.Failed != 1 || report.Sent != 0 {
		t.Fatalf("expected one failed delivery, got %+v", report)
	}
	if len(store.claims) != 1 || store.claims[0].ruleID != 5 || store.claims[0].configVersion != 7 ||
		store.claims[0].previous == nil || !store.claims[0].previous.Equal(previous) {
		t.Fatalf("the claim must compare against the snapshot's last_sent_on and version, got %+v", store.claims)
	}
	if got := store.claims[0].day.Format("2006-01-02"); got != "2026-03-07" {
		t.Fatalf("the claim must write the due day, got %s", got)
	}
	if len(store.releases) != 1 || store.releases[0].ruleID != 5 || store.releases[0].previous == nil || !store.releases[0].previous.Equal(previous) {
		t.Fatalf("a failed delivery must release the claim back to the previous value, got %+v", store.releases)
	}
}

func TestCycleDayReminderLostClaimSkipsWithoutDelivering(t *testing.T) {
	deliverer := &stubDeliverer{}
	service, _, store := newCycleDayNotifyService(cycleDayNotifyRecord("https://hook.example/a"), cycleDayHistory(cycleDayTestStart),
		[]models.CycleDayReminder{cycleDayTestRule(5, 10, 9)}, deliverer, cycleDayTestCopy)
	store.claimLost = true
	report, err := service.RunCycleDayOnce(context.Background(), cycleDayInstant(cycleDayTestStart, 10, 12), time.UTC, false)
	if err != nil {
		t.Fatalf("RunCycleDayOnce: %v", err)
	}
	if report.SkippedIdempotent != 1 || report.Sent != 0 || len(deliverer.deliveries()) != 0 || len(store.releases) != 0 {
		t.Fatalf("a lost claim is a skip with no delivery and no release, report=%+v", report)
	}
}

func TestRunCycleDayOnceSendsNoPredictionReminder(t *testing.T) {
	now := time.Date(2026, time.March, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://hook.example/a", now, 26)
	record.WebhookConfigVersion = 7
	logs := completedCycleStartLogs(1, *record.LastPeriodStart)
	rules := []models.CycleDayReminder{cycleDayTestRule(5, 27, 9)}

	hourlyDeliverer := &stubDeliverer{}
	hourly, hourlyRepo, _ := newCycleDayNotifyService(record, logs, rules, hourlyDeliverer, cycleDayTestCopy)
	if _, err := hourly.RunCycleDayOnce(context.Background(), now, time.UTC, false); err != nil {
		t.Fatalf("RunCycleDayOnce: %v", err)
	}
	deliveries := hourlyDeliverer.deliveries()
	if len(deliveries) != 1 || deliveries[0].payload.Type != "cycle-day" {
		t.Fatalf("the hourly pass must send the cycle-day reminder only, got %+v", deliveries)
	}
	if len(hourlyRepo.writes()) != 0 {
		t.Fatalf("the hourly pass must claim no prediction watermark, got %+v", hourlyRepo.writes())
	}

	// Control: the daily pass sends both, so the period reminder was due.
	dailyDeliverer := &stubDeliverer{}
	daily, _, _ := newCycleDayNotifyService(record, logs, rules, dailyDeliverer, cycleDayTestCopy)
	if _, err := daily.RunOnce(context.Background(), now, time.UTC, false); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	types := map[string]bool{}
	for _, delivery := range dailyDeliverer.deliveries() {
		types[delivery.payload.Type] = true
	}
	if !types[DueReminderTypePeriod] || !types["cycle-day"] {
		t.Fatalf("control: RunOnce must send the period and the cycle-day reminder, got %v", types)
	}
}
