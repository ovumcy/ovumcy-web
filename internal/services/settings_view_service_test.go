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
	summary ExportSummary
	err     error
	called  bool
}

func (stub *stubSettingsViewExportBuilder) BuildSummary(_ uint, _ *time.Time, _ *time.Time, _ *time.Location) (ExportSummary, error) {
	stub.called = true
	if stub.err != nil {
		return ExportSummary{}, stub.err
	}
	return stub.summary, nil
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
	service := NewSettingsViewService(settingsLoader, NewNotificationService(), nil)

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
		summary: ExportSummary{
			TotalEntries: 2,
			HasData:      true,
			DateFrom:     "2026-02-01",
			DateTo:       "2026-02-21",
		},
	}
	service := NewSettingsViewService(settingsLoader, NewNotificationService(), exportBuilder)

	user := &models.User{ID: 2, Role: models.RoleOwner}
	viewData, err := service.BuildSettingsPageViewData(user, "ru", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}

	if !exportBuilder.called {
		t.Fatalf("expected BuildSummary to be called for owner")
	}
	if !viewData.HasOwnerExportViewState || !viewData.Export.HasSummaryForOwner {
		t.Fatalf("expected owner export state in view data")
	}
	if viewData.Export.DateFromDisplay != "01.02.2026" {
		t.Fatalf("expected localized from display, got %q", viewData.Export.DateFromDisplay)
	}
	if viewData.Export.DateToDisplay != "21.02.2026" {
		t.Fatalf("expected localized to display, got %q", viewData.Export.DateToDisplay)
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
	service := NewSettingsViewService(settingsLoader, NewNotificationService(), exportBuilder)

	user := &models.User{ID: 3, Role: models.RolePartner}
	viewData, err := service.BuildSettingsPageViewData(user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC)
	if err != nil {
		t.Fatalf("BuildSettingsPageViewData() unexpected error: %v", err)
	}
	if exportBuilder.called {
		t.Fatalf("did not expect BuildSummary call for partner")
	}
	if viewData.HasOwnerExportViewState {
		t.Fatalf("expected no owner export state for partner")
	}
}

func TestBuildSettingsPageViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 4, Role: models.RoleOwner}

	settingsErrService := NewSettingsViewService(
		&stubSettingsViewLoader{err: errors.New("settings fail")},
		NewNotificationService(),
		nil,
	)
	if _, err := settingsErrService.BuildSettingsPageViewData(user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadSettings) {
		t.Fatalf("expected ErrSettingsViewLoadSettings, got %v", err)
	}

	exportErrService := NewSettingsViewService(
		&stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5, AutoPeriodFill: true}},
		NewNotificationService(),
		&stubSettingsViewExportBuilder{err: errors.New("export fail")},
	)
	if _, err := exportErrService.BuildSettingsPageViewData(user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-02-21"), time.UTC); !errors.Is(err, ErrSettingsViewLoadExport) {
		t.Fatalf("expected ErrSettingsViewLoadExport, got %v", err)
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
