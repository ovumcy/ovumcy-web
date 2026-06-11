package apideps

import (
	"errors"
	"reflect"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Dependencies is the full set of domain services the HTTP handler requires.
// The composition layer (internal/bootstrap) builds it from the db
// repositories; internal/api consumes it via a type alias.
type Dependencies struct {
	AuthService          *services.AuthService
	RegistrationService  RegistrationWorkflowService
	PasswordResetService *services.PasswordResetService
	LoginService         LoginWorkflowService
	OIDCService          OIDCWorkflowService
	OIDCLogoutStateSvc   *services.OIDCLogoutStateService
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
	TOTPService          *services.TOTPService
	RegisterPickupTokens RegisterPickupTokenStore
}

// Validate reports the first missing dependency, so handler construction fails
// fast with a clear message instead of nil-panicking on the first request.
func (dependencies Dependencies) Validate() error {
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
		{value: dependencies.OIDCService, message: "oidc service is required"},
		{value: dependencies.OIDCLogoutStateSvc, message: "oidc logout state service is required"},
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
		{value: dependencies.TOTPService, message: "totp service is required"},
		{value: dependencies.RegisterPickupTokens, message: "register pickup token store is required"},
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
