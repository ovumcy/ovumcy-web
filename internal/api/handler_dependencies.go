package api

import "github.com/ovumcy/ovumcy-web/internal/apideps"

// Dependencies is the set of domain services the HTTP handler requires. The
// type and its workflow-service ports live in internal/apideps so the
// composition layer (internal/bootstrap) can build it without importing
// internal/api. The alias keeps api.Dependencies as the name used throughout
// the handler and its tests.
type Dependencies = apideps.Dependencies

func (handler *Handler) withDependencies(dependencies Dependencies) *Handler {
	handler.authService = dependencies.AuthService
	handler.registrationService = dependencies.RegistrationService
	handler.passwordResetSvc = dependencies.PasswordResetService
	handler.loginService = dependencies.LoginService
	handler.oidcService = dependencies.OIDCService
	handler.oidcLogoutStateSvc = dependencies.OIDCLogoutStateSvc
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
	handler.totpService = dependencies.TOTPService
	handler.registerPickupTokens = dependencies.RegisterPickupTokens
	handler.auditLogEnabled = dependencies.AuditLogEnabled
	return handler
}
