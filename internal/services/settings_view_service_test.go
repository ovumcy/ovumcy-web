package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type stubSettingsViewLoader struct {
	user models.User
	err  error
}

func (stub *stubSettingsViewLoader) LoadSettings(_ uint) (models.User, error) {
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
	callIndex int
	calls     []settingsViewSummaryCall
}

func (stub *stubSettingsViewExportBuilder) BuildSummary(_ uint, from *time.Time, to *time.Time, location *time.Location) (ExportSummary, error) {
	stub.called = true
	stub.calls = append(stub.calls, newSettingsViewSummaryCall(from, to, location))
	if stub.err != nil {
		return ExportSummary{}, stub.err
	}
	if stub.callIndex < len(stub.responses) {
		response := stub.responses[stub.callIndex]
		stub.callIndex++
		return response, nil
	}
	return stub.summary, nil
}

type settingsViewSummaryCall struct {
	HasFrom bool
	HasTo   bool
	From    string
	To      string
}

func newSettingsViewSummaryCall(from *time.Time, to *time.Time, location *time.Location) settingsViewSummaryCall {
	call := settingsViewSummaryCall{
		HasFrom: from != nil,
		HasTo:   to != nil,
	}
	if from != nil {
		call.From = from.In(location).Format("2006-01-02")
	}
	if to != nil {
		call.To = to.In(location).Format("2006-01-02")
	}
	return call
}

type stubSettingsViewSymptomProvider struct {
	symptoms []models.SymptomType
	err      error
	called   bool
}

func (stub *stubSettingsViewSymptomProvider) FetchSymptoms(_ uint) ([]models.SymptomType, error) {
	stub.called = true
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
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
	service := NewSettingsViewService(settingsLoader, NewNotificationService(), nil, nil)

	user := &models.User{ID: 1, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(user, "en", SettingsViewInput{
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
	service := NewSettingsViewService(settingsLoader, NewNotificationService(), exportBuilder, symptomProvider)

	user := &models.User{ID: 2, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(user, "ru", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC)
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
		{HasFrom: false, HasTo: false},
		{HasFrom: true, HasTo: true, From: "2026-02-01", To: "2026-02-21"},
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

	service := NewSettingsViewService(settingsLoader, NewNotificationService(), exportBuilder, nil)
	user := &models.User{ID: 5, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(user, "ru", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-03-12"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	assertSettingsViewOwnerSummaryCalls(t, exportBuilder.calls, []settingsViewSummaryCall{
		{HasFrom: false, HasTo: false},
		{HasFrom: true, HasTo: true, From: "2026-03-12", To: "2026-03-12"},
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

	service := NewSettingsViewService(settingsLoader, NewNotificationService(), nil, nil)
	user := &models.User{ID: 6, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(user, "ru", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-03-12"), time.UTC)
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
	service := NewSettingsViewService(settingsLoader, NewNotificationService(), exportBuilder, symptomProvider)

	user := &models.User{ID: 3, Role: models.RolePartner}
	viewData, err := service.BuildSettingsPageViewData(user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}
	if exportBuilder.called {
		t.Fatalf("did not expect BuildSummary call for partner")
	}
	if symptomProvider.called {
		t.Fatalf("did not expect FetchSymptoms call for partner")
	}
	if viewData.HasOwnerExportViewState {
		t.Fatalf("expected no owner export state for partner")
	}
	if viewData.HasOwnerSymptomsView {
		t.Fatalf("expected no owner symptoms view state for partner")
	}
}

func TestBuildSettingsPageViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 4, Role: models.RoleOwner}

	settingsErrService := NewSettingsViewService(
		&stubSettingsViewLoader{err: errors.New("settings fail")},
		NewNotificationService(),
		nil,
		nil,
	)
	if _, err := settingsErrService.BuildSettingsPageViewData(user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadSettings) {
		t.Fatalf("expected ErrSettingsViewLoadSettings, got %v", err)
	}

	exportErrService := NewSettingsViewService(
		&stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5, AutoPeriodFill: true}},
		NewNotificationService(),
		&stubSettingsViewExportBuilder{err: errors.New("export fail")},
		nil,
	)
	if _, err := exportErrService.BuildSettingsPageViewData(user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadExport) {
		t.Fatalf("expected ErrSettingsViewLoadExport, got %v", err)
	}

	symptomErrService := NewSettingsViewService(
		&stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5, AutoPeriodFill: true}},
		NewNotificationService(),
		nil,
		&stubSettingsViewSymptomProvider{err: errors.New("symptom fail")},
	)
	if _, err := symptomErrService.BuildSettingsPageViewData(user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadSymptoms) {
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
