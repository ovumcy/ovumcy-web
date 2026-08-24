package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubSettingsViewLoader struct {
	user models.User
	err  error

	// Captured userID argument — used to prove the read is owner-scoped.
	settingsUserID uint
}

func (stub *stubSettingsViewLoader) LoadSettings(ctx context.Context, userID uint) (models.User, error) {
	stub.settingsUserID = userID
	if stub.err != nil {
		return models.User{}, stub.err
	}
	return stub.user, nil
}

type stubSettingsViewExportBuilder struct {
	summary   ExportSummary
	responses []ExportSummary
	err       error
	called    bool
	calls     []settingsViewSummaryCall

	// Captured userID arguments, one per call — used to prove every summary
	// read is owner-scoped, not just the first.
	summaryUserIDs []uint
}

// The responses stay a pair — the whole history first, the window ending today
// second — now returned by one call rather than by two.
func (stub *stubSettingsViewExportBuilder) BuildSummaryHistoryAndWindow(ctx context.Context, userID uint, through time.Time, location *time.Location) (ExportSummary, ExportSummary, error) {
	stub.called = true
	stub.summaryUserIDs = append(stub.summaryUserIDs, userID)
	stub.calls = append(stub.calls, newSettingsViewSummaryCall(through, location))
	if stub.err != nil {
		return ExportSummary{}, ExportSummary{}, stub.err
	}

	history, window := stub.summary, stub.summary
	if len(stub.responses) > 0 {
		history = stub.responses[0]
	}
	if len(stub.responses) > 1 {
		window = stub.responses[1]
	}
	return history, window, nil
}

type settingsViewSummaryCall struct {
	Through string
}

func newSettingsViewSummaryCall(through time.Time, location *time.Location) settingsViewSummaryCall {
	return settingsViewSummaryCall{Through: through.In(location).Format("2006-01-02")}
}

type stubSettingsViewSymptomProvider struct {
	symptoms []models.SymptomType
	err      error
	called   bool

	// Captured userID argument — used to prove the read is owner-scoped.
	symptomsUserID uint
}

func (stub *stubSettingsViewSymptomProvider) FetchSymptoms(ctx context.Context, userID uint) ([]models.SymptomType, error) {
	stub.called = true
	stub.symptomsUserID = userID
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
}

// stubSettingsViewWebhookStatusBuilder records the owner id the view forwards
// into the webhook projection. The id is the AAD the ciphertext is bound to, so
// a hard-coded one would silently report every owner's webhook unconfigured.
type stubSettingsViewWebhookStatusBuilder struct {
	display WebhookURLDisplay

	// Captured userID argument — used to prove the read is owner-scoped.
	webhookUserID uint
}

func (stub *stubSettingsViewWebhookStatusBuilder) BuildWebhookURLDisplay(userID uint, _ string) WebhookURLDisplay {
	stub.webhookUserID = userID
	return stub.display
}

// stubSettingsViewCalendarFeedStatusBuilder records the owner id the view
// forwards into the .ics feed status, which reports whether a feed is armed.
type stubSettingsViewCalendarFeedStatusBuilder struct {
	status CalendarFeedStatus

	// Captured userID argument — used to prove the read is owner-scoped.
	feedUserID uint
}

func (stub *stubSettingsViewCalendarFeedStatusBuilder) BuildFeedStatus(ctx context.Context, userID uint) CalendarFeedStatus {
	stub.feedUserID = userID
	return stub.status
}

// TestBuildSettingsPageViewDataReadsTheOwnersEntriesOnce pins the cost of the
// settings render against the real ExportService rather than a summary stub.
// The page needs two aggregates — everything the owner has, for the export
// panel's selectable bounds, and everything up to today, for its default window
// — and it used to ask for them with two calls that each fetched EVERY
// daily_logs row and walked it in Go. Two full reads of the owner's whole
// history, on every settings render, for three numbers, on a page that shows
// none of them until the export panel is opened; the shape hid it, because the
// second call reads as a cheap narrowing of the first.
//
// The figures are asserted beside the count: one read must not become one read
// of the wrong thing.
// TestCompareISODateComparesTheSameOperandsInBothArms pins one comparison to
// one reading of its operands. The equality arm trimmed and the ordering arm
// did not, so a padded value was "not equal" and then ordered by the space:
// 0x20 sorts below every digit, which made a padded LATER date read as earlier.
// Both callers pass canonical values today — the trim in front of the raw
// comparison is exactly what made a padded one look safe to pass — and the
// function decides the export panel's selectable bounds, so the wrong branch
// would silently offer a range that excludes the owner's own data.
func TestCompareISODateComparesTheSameOperandsInBothArms(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "canonical earlier", left: "2024-01-02", right: "2024-01-05", want: -1},
		{name: "canonical later", left: "2024-01-05", right: "2024-01-02", want: 1},
		{name: "canonical equal", left: "2024-01-05", right: "2024-01-05", want: 0},
		{name: "padded later left", left: " 2024-01-05", right: "2024-01-02", want: 1},
		{name: "padded earlier left", left: " 2024-01-02", right: "2024-01-05", want: -1},
		{name: "padded right", left: "2024-01-05", right: "2024-01-02 ", want: 1},
		{name: "padded equal", left: " 2024-01-05 ", right: "2024-01-05", want: 0},
		{name: "empty against a date", left: "", right: "2024-01-05", want: -1},
	} {
		if got := compareISODate(testCase.left, testCase.right); got != testCase.want {
			t.Fatalf("%s: compareISODate(%q, %q) = %d, want %d", testCase.name, testCase.left, testCase.right, got, testCase.want)
		}
	}
}

func TestBuildSettingsPageViewDataReadsTheOwnersEntriesOnce(t *testing.T) {
	days := &stubExportDayReader{logs: []models.DailyLog{
		{Date: mustParseSettingsViewDay(t, "2026-02-01"), Notes: "first"},
		{Date: mustParseSettingsViewDay(t, "2026-02-21"), Notes: "today"},
		// A future-dated entry: inside the owner's history, outside the
		// default export window that stops at today.
		{Date: mustParseSettingsViewDay(t, "2026-03-05"), Notes: "ahead"},
	}}
	exportService := NewExportService(days, &stubExportSymptomReader{})
	settingsLoader := &stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5}}
	service := NewSettingsViewService(settingsLoader, exportService, nil, nil, nil)

	owner := &models.User{ID: 77, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(
		context.Background(), owner, "en", SettingsViewInput{},
		mustParseSettingsViewDay(t, "2026-02-21"), time.UTC,
	)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	if len(days.ownerIDs) != 1 {
		t.Fatalf("one settings render fetched the owner's entries %d times; the whole history is materialized on each", len(days.ownerIDs))
	}
	if days.ownerIDs[0] != owner.ID {
		t.Fatalf("the read must stay scoped to the acting owner %d, got %d", owner.ID, days.ownerIDs[0])
	}

	if viewData.Export.SelectableDateMax != "2026-03-05" {
		t.Fatalf("the selectable range covers everything the owner has, got max %q", viewData.Export.SelectableDateMax)
	}
	if viewData.Export.SelectableDateMin != "2026-02-01" {
		t.Fatalf("the selectable range starts at the earliest entry, got min %q", viewData.Export.SelectableDateMin)
	}
	if viewData.Export.DefaultDateTo != "2026-02-21" {
		t.Fatalf("the default window still ends today, got %q", viewData.Export.DefaultDateTo)
	}
	if viewData.Export.SummaryTotalEntries != 2 {
		t.Fatalf("the default window summarizes the two entries up to today, got %d", viewData.Export.SummaryTotalEntries)
	}
	if viewData.Export.SummaryDateFrom != "2026-02-01" || viewData.Export.SummaryDateTo != "2026-02-21" {
		t.Fatalf("expected the default window summary 2026-02-01..2026-02-21, got %q..%q", viewData.Export.SummaryDateFrom, viewData.Export.SummaryDateTo)
	}
}

func TestBuildSettingsPageViewDataClassifiesChangePasswordError(t *testing.T) {
	settingsLoader := &stubSettingsViewLoader{
		user: models.User{
			CycleLength:     28,
			PeriodLength:    5,
			AutoPeriodFill:  true,
			LastPeriodStart: nil,
		},
	}
	service := NewSettingsViewService(settingsLoader, nil, nil, nil, nil)

	user := &models.User{ID: 1, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{
		FlashError: "invalid current password",
	}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	if viewData.ChangePasswordErrorKey != "settings.error.invalid_current_password" {
		t.Fatalf("expected change-password error key, got %q", viewData.ChangePasswordErrorKey)
	}
	if viewData.ErrorKey != "" {
		t.Fatalf("expected empty general ErrorKey, got %q", viewData.ErrorKey)
	}
}

func TestBuildSettingsPageViewDataOwnerLoadsExportSummary(t *testing.T) {
	settingsLoader := &stubSettingsViewLoader{
		user: models.User{
			CycleLength:    28,
			PeriodLength:   5,
			AutoPeriodFill: true,
		},
	}
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 2, HasData: true, DateFrom: "2026-02-01", DateTo: "2026-02-21"},
			{TotalEntries: 2, HasData: true, DateFrom: "2026-02-01", DateTo: "2026-02-21"},
		},
	}
	symptomProvider := &stubSettingsViewSymptomProvider{
		symptoms: []models.SymptomType{
			{ID: 1, Name: "Headache", IsBuiltin: true},
			{ID: 2, Name: "Joint stiffness"},
			{ID: 3, Name: "Caffeine crash", ArchivedAt: ptrSettingsViewTime(mustParseSettingsViewDay(t, "2026-02-01"))},
		},
	}
	service := NewSettingsViewService(settingsLoader, exportBuilder, symptomProvider, nil, nil)

	user := &models.User{ID: 2, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(context.Background(), user, "ru", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	if !exportBuilder.called {
		t.Fatalf("expected BuildSummary to be called for owner")
	}
	if !symptomProvider.called {
		t.Fatalf("expected FetchSymptoms to be called for owner")
	}
	assertSettingsViewOwnerSummaryCalls(t, exportBuilder.calls, []settingsViewSummaryCall{
		{Through: "2026-02-21"},
	})
	assertOwnerSymptomsViewData(t, viewData)
	assertOwnerExportViewData(t, viewData, ownerExportViewExpectation{
		defaultFrom:        "2026-02-01",
		defaultTo:          "2026-02-21",
		selectableMin:      "2026-02-01",
		selectableMax:      "2026-02-21",
		summaryFromDisplay: "01.02.2026",
		summaryToDisplay:   "21.02.2026",
	})
}

// The settings page reads five separate stores for the acting owner: the
// persisted settings row, the export summary, the custom symptom catalogue, the
// webhook URL projection and the .ics feed status. Each read must carry the
// authenticated owner's id, so this pins every operand against a non-zero owner
// id no fixture supplies by default — a hard-coded owner would otherwise render
// one owner's health settings, symptom catalogue, export summary or feed state
// to another. The two status builders are covered here rather than left nil
// precisely because a nil collaborator returns early and observes nothing.
func TestBuildSettingsPageViewDataScopesEveryOwnerReadToTheAuthenticatedOwner(t *testing.T) {
	settingsLoader := &stubSettingsViewLoader{
		user: models.User{
			CycleLength:    28,
			PeriodLength:   5,
			AutoPeriodFill: true,
		},
	}
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 2, HasData: true, DateFrom: "2026-02-01", DateTo: "2026-02-21"},
			{TotalEntries: 2, HasData: true, DateFrom: "2026-02-01", DateTo: "2026-02-21"},
		},
	}
	symptomProvider := &stubSettingsViewSymptomProvider{
		symptoms: []models.SymptomType{{ID: 2, Name: "Joint stiffness"}},
	}
	webhookStatus := &stubSettingsViewWebhookStatusBuilder{
		display: WebhookURLDisplay{Configured: true, Host: "hooks.example.test"},
	}
	calendarFeedStatus := &stubSettingsViewCalendarFeedStatusBuilder{
		status: CalendarFeedStatus{Configured: true},
	}
	service := NewSettingsViewService(settingsLoader, exportBuilder, symptomProvider, webhookStatus, calendarFeedStatus)

	owner := &models.User{ID: 4242, Role: models.RoleOwner}
	if _, err := service.BuildSettingsPageViewData(context.Background(), owner, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	if settingsLoader.settingsUserID != owner.ID {
		t.Fatalf("expected settings read scoped to owner id %d, got %d", owner.ID, settingsLoader.settingsUserID)
	}
	if len(exportBuilder.summaryUserIDs) == 0 {
		t.Fatalf("expected at least one export summary read for the owner")
	}
	for index, summaryUserID := range exportBuilder.summaryUserIDs {
		if summaryUserID != owner.ID {
			t.Fatalf("expected export summary read %d scoped to owner id %d, got %d", index, owner.ID, summaryUserID)
		}
	}
	if symptomProvider.symptomsUserID != owner.ID {
		t.Fatalf("expected symptom read scoped to owner id %d, got %d", owner.ID, symptomProvider.symptomsUserID)
	}
	if webhookStatus.webhookUserID != owner.ID {
		t.Fatalf("expected webhook projection scoped to owner id %d, got %d", owner.ID, webhookStatus.webhookUserID)
	}
	if calendarFeedStatus.feedUserID != owner.ID {
		t.Fatalf("expected calendar feed status scoped to owner id %d, got %d", owner.ID, calendarFeedStatus.feedUserID)
	}
}

func TestBuildSettingsPageViewDataOwnerClampsExportDefaultToRequestLocalToday(t *testing.T) {
	settingsLoader := &stubSettingsViewLoader{
		user: models.User{
			CycleLength:    28,
			PeriodLength:   5,
			AutoPeriodFill: true,
		},
	}

	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 2, HasData: true, DateFrom: "2026-03-12", DateTo: "2026-03-16"},
			{TotalEntries: 1, HasData: true, DateFrom: "2026-03-12", DateTo: "2026-03-12"},
		},
	}

	service := NewSettingsViewService(settingsLoader, exportBuilder, nil, nil, nil)
	user := &models.User{ID: 5, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(context.Background(), user, "ru", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-03-12"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	// The window the page asks for ends at request-local today, not at the
	// server's day: that cutoff is the whole point of this case.
	assertSettingsViewOwnerSummaryCalls(t, exportBuilder.calls, []settingsViewSummaryCall{
		{Through: "2026-03-12"},
	})
	if viewData.Export.DefaultDateTo != "2026-03-12" {
		t.Fatalf("expected export default to date to use request-local today, got %q", viewData.Export.DefaultDateTo)
	}
	if viewData.Export.SelectableDateMax != "2026-03-16" {
		t.Fatalf("expected selectable max date to keep future export bound, got %q", viewData.Export.SelectableDateMax)
	}
	if viewData.Export.SummaryTotalEntries != 1 {
		t.Fatalf("expected default summary total entries 1, got %d", viewData.Export.SummaryTotalEntries)
	}
	if viewData.Export.SummaryDateToDisplay != "12.03.2026" {
		t.Fatalf("expected localized summary display to use today, got %q", viewData.Export.SummaryDateToDisplay)
	}
}

func TestBuildSettingsPageViewDataSanitizesFutureLastPeriodStartForForm(t *testing.T) {
	futureStart := mustParseSettingsViewDay(t, "2026-04-05")
	settingsLoader := &stubSettingsViewLoader{
		user: models.User{
			CycleLength:     28,
			PeriodLength:    5,
			AutoPeriodFill:  true,
			LastPeriodStart: &futureStart,
		},
	}

	service := NewSettingsViewService(settingsLoader, nil, nil, nil, nil)
	user := &models.User{ID: 6, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(context.Background(), user, "ru", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-03-12"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	if viewData.LastPeriodStart != "2026-03-12" {
		t.Fatalf("expected sanitized last_period_start=2026-03-12, got %q", viewData.LastPeriodStart)
	}
	if viewData.CurrentUser.LastPeriodStart == nil || viewData.CurrentUser.LastPeriodStart.Format("2006-01-02") != "2026-03-12" {
		t.Fatalf("expected sanitized current user last_period_start, got %#v", viewData.CurrentUser.LastPeriodStart)
	}
}

func TestBuildSettingsPageViewDataPartnerSkipsExportSummary(t *testing.T) {
	settingsLoader := &stubSettingsViewLoader{
		user: models.User{
			CycleLength:    28,
			PeriodLength:   5,
			AutoPeriodFill: true,
		},
	}
	exportBuilder := &stubSettingsViewExportBuilder{}
	symptomProvider := &stubSettingsViewSymptomProvider{}
	service := NewSettingsViewService(settingsLoader, exportBuilder, symptomProvider, nil, nil)

	user := &models.User{ID: 3, Role: "legacy_viewer"}
	viewData, err := service.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}
	if exportBuilder.called {
		t.Fatalf("did not expect BuildSummary call for unsupported role")
	}
	if symptomProvider.called {
		t.Fatalf("did not expect FetchSymptoms call for unsupported role")
	}
	if viewData.HasOwnerExportViewState {
		t.Fatalf("expected no owner export state for unsupported role")
	}
	if viewData.HasOwnerSymptomsView {
		t.Fatalf("expected no owner symptoms view state for unsupported role")
	}
}

func TestBuildSettingsPageViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 4, Role: models.RoleOwner}

	settingsErrService := NewSettingsViewService(
		&stubSettingsViewLoader{err: errors.New("settings fail")},
		nil,
		nil,
		nil,
		nil,
	)
	if _, err := settingsErrService.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadSettings) {
		t.Fatalf("expected ErrSettingsViewLoadSettings, got %v", err)
	}

	exportErrService := NewSettingsViewService(
		&stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5, AutoPeriodFill: true}},
		&stubSettingsViewExportBuilder{err: errors.New("export fail")},
		nil,
		nil,
		nil,
	)
	if _, err := exportErrService.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadExport) {
		t.Fatalf("expected ErrSettingsViewLoadExport, got %v", err)
	}

	symptomErrService := NewSettingsViewService(
		&stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5, AutoPeriodFill: true}},
		nil,
		&stubSettingsViewSymptomProvider{err: errors.New("symptom fail")},
		nil,
		nil,
	)
	if _, err := symptomErrService.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadSymptoms) {
		t.Fatalf("expected ErrSettingsViewLoadSymptoms, got %v", err)
	}
}

func mustParseSettingsViewDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

func ptrSettingsViewTime(value time.Time) *time.Time {
	return &value
}

type ownerExportViewExpectation struct {
	defaultFrom        string
	defaultTo          string
	selectableMin      string
	selectableMax      string
	summaryFromDisplay string
	summaryToDisplay   string
}

func assertSettingsViewOwnerSummaryCalls(t *testing.T, got []settingsViewSummaryCall, want []settingsViewSummaryCall) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %d export summary calls, got %#v", len(want), got)
	}

	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected export summary call %d: got %#v want %#v", index, got[index], want[index])
		}
	}
}

func assertOwnerSymptomsViewData(t *testing.T, viewData SettingsPageViewData) {
	t.Helper()

	if !viewData.HasOwnerSymptomsView {
		t.Fatalf("expected owner symptoms view state")
	}
	if len(viewData.Symptoms.ActiveCustomSymptoms) != 1 || viewData.Symptoms.ActiveCustomSymptoms[0].Name != "Joint stiffness" {
		t.Fatalf("expected one active custom symptom, got %#v", viewData.Symptoms.ActiveCustomSymptoms)
	}
	if len(viewData.Symptoms.ArchivedCustomSymptoms) != 1 || viewData.Symptoms.ArchivedCustomSymptoms[0].Name != "Caffeine crash" {
		t.Fatalf("expected one archived custom symptom, got %#v", viewData.Symptoms.ArchivedCustomSymptoms)
	}
}

func assertOwnerExportViewData(t *testing.T, viewData SettingsPageViewData, expected ownerExportViewExpectation) {
	t.Helper()

	if !viewData.HasOwnerExportViewState || !viewData.Export.HasSummaryForOwner {
		t.Fatalf("expected owner export state in view data")
	}
	if viewData.Export.DefaultDateFrom != expected.defaultFrom {
		t.Fatalf("expected default from date %q, got %q", expected.defaultFrom, viewData.Export.DefaultDateFrom)
	}
	if viewData.Export.DefaultDateTo != expected.defaultTo {
		t.Fatalf("expected default to date %q, got %q", expected.defaultTo, viewData.Export.DefaultDateTo)
	}
	if viewData.Export.SelectableDateMin != expected.selectableMin {
		t.Fatalf("expected selectable min date %q, got %q", expected.selectableMin, viewData.Export.SelectableDateMin)
	}
	if viewData.Export.SelectableDateMax != expected.selectableMax {
		t.Fatalf("expected selectable max date %q, got %q", expected.selectableMax, viewData.Export.SelectableDateMax)
	}
	if viewData.Export.SummaryDateFromDisplay != expected.summaryFromDisplay {
		t.Fatalf("expected localized summary from display %q, got %q", expected.summaryFromDisplay, viewData.Export.SummaryDateFromDisplay)
	}
	if viewData.Export.SummaryDateToDisplay != expected.summaryToDisplay {
		t.Fatalf("expected localized summary to display %q, got %q", expected.summaryToDisplay, viewData.Export.SummaryDateToDisplay)
	}
}
