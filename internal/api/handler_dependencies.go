package api

import (
	"errors"

	"github.com/terraincognita07/ovumcy/internal/services"
)

type Dependencies struct {
	AuthService          *services.AuthService
	RegistrationService  RegistrationWorkflowService
	PasswordResetService *services.PasswordResetService
	LoginService         LoginWorkflowService
	DayService           *services.DayService
	SymptomService       *services.SymptomService
	ViewerService        *services.ViewerService
	StatsService         *services.StatsService
	CalendarViewService  *services.CalendarViewService
	DashboardViewService *services.DashboardViewService
	ExportService        *services.ExportService
	SettingsService      *services.SettingsService
	SettingsViewService  *services.SettingsViewService
	OnboardingService    *services.OnboardingService
	SetupService         *services.SetupService
}

func (dependencies Dependencies) validate() error {
	switch {
	case dependencies.AuthService == nil:
		return errors.New("auth service is required")
	case dependencies.RegistrationService == nil:
		return errors.New("registration service is required")
	case dependencies.PasswordResetService == nil:
		return errors.New("password reset service is required")
	case dependencies.LoginService == nil:
		return errors.New("login service is required")
	case dependencies.DayService == nil:
		return errors.New("day service is required")
	case dependencies.SymptomService == nil:
		return errors.New("symptom service is required")
	case dependencies.ViewerService == nil:
		return errors.New("viewer service is required")
	case dependencies.StatsService == nil:
		return errors.New("stats service is required")
	case dependencies.CalendarViewService == nil:
		return errors.New("calendar view service is required")
	case dependencies.DashboardViewService == nil:
		return errors.New("dashboard view service is required")
	case dependencies.ExportService == nil:
		return errors.New("export service is required")
	case dependencies.SettingsService == nil:
		return errors.New("settings service is required")
	case dependencies.SettingsViewService == nil:
		return errors.New("settings view service is required")
	case dependencies.OnboardingService == nil:
		return errors.New("onboarding service is required")
	case dependencies.SetupService == nil:
		return errors.New("setup service is required")
	default:
		return nil
	}
}

func (handler *Handler) withDependencies(dependencies Dependencies) *Handler {
	handler.authService = dependencies.AuthService
	handler.registrationService = dependencies.RegistrationService
	handler.passwordResetSvc = dependencies.PasswordResetService
	handler.loginService = dependencies.LoginService
	handler.dayService = dependencies.DayService
	handler.symptomService = dependencies.SymptomService
	handler.viewerService = dependencies.ViewerService
	handler.statsService = dependencies.StatsService
	handler.calendarViewService = dependencies.CalendarViewService
	handler.dashboardViewService = dependencies.DashboardViewService
	handler.exportService = dependencies.ExportService
	handler.settingsService = dependencies.SettingsService
	handler.settingsViewService = dependencies.SettingsViewService
	handler.onboardingSvc = dependencies.OnboardingService
	handler.setupService = dependencies.SetupService
	return handler
}
