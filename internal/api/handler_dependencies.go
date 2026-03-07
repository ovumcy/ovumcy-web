package api

import (
	"errors"
	"reflect"

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
	for _, requirement := range dependencies.requirements() {
		if requirement.missing() {
			return errors.New(requirement.message)
		}
	}
	return nil
}

type dependencyRequirement struct {
	value   any
	message string
}

func (dependencies Dependencies) requirements() []dependencyRequirement {
	return []dependencyRequirement{
		{value: dependencies.AuthService, message: "auth service is required"},
		{value: dependencies.RegistrationService, message: "registration service is required"},
		{value: dependencies.PasswordResetService, message: "password reset service is required"},
		{value: dependencies.LoginService, message: "login service is required"},
		{value: dependencies.DayService, message: "day service is required"},
		{value: dependencies.SymptomService, message: "symptom service is required"},
		{value: dependencies.ViewerService, message: "viewer service is required"},
		{value: dependencies.StatsService, message: "stats service is required"},
		{value: dependencies.CalendarViewService, message: "calendar view service is required"},
		{value: dependencies.DashboardViewService, message: "dashboard view service is required"},
		{value: dependencies.ExportService, message: "export service is required"},
		{value: dependencies.SettingsService, message: "settings service is required"},
		{value: dependencies.SettingsViewService, message: "settings view service is required"},
		{value: dependencies.OnboardingService, message: "onboarding service is required"},
		{value: dependencies.SetupService, message: "setup service is required"},
	}
}

func (requirement dependencyRequirement) missing() bool {
	if requirement.value == nil {
		return true
	}

	value := reflect.ValueOf(requirement.value)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
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
