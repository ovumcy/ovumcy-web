// Package bootstrap is the composition root that wires the db repositories into
// the domain services and assembles them into the apideps.Dependencies the HTTP
// handler consumes. It is the single source of that wiring, shared by the
// production binary (cmd/ovumcy) and the internal/api test helpers, so the two
// cannot drift. bootstrap sits above internal/api in the dependency graph and
// may import internal/db; internal/api itself must not.
package bootstrap

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/apideps"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// AttemptLimit configures a rate-limited attempt policy: at most Max attempts
// per Window.
type AttemptLimit struct {
	Max    int
	Window time.Duration
}

// Options carries the wiring knobs that differ between the production binary and
// the test helpers. The zero value is a valid, minimal test configuration.
type Options struct {
	// RegistrationMode selects open/invite/closed owner registration.
	RegistrationMode services.RegistrationMode
	// OIDCConfig configures the OIDC client. The zero value yields a disabled client.
	OIDCConfig security.OIDCConfig
	// OIDCServiceOverride, when non-nil, replaces the built OIDC login service.
	// Tests inject a stub through it; production leaves it nil.
	OIDCServiceOverride apideps.OIDCWorkflowService
	// LoginAttempts and RecoveryAttempts configure the login and
	// password-recovery attempt limiters. Both are always applied.
	LoginAttempts    AttemptLimit
	RecoveryAttempts AttemptLimit
	// LogoutAttempts, when non-nil, configures the logout attempt limiter.
	// Production sets it; tests leave it nil to keep the service default.
	LogoutAttempts *AttemptLimit
	// AuditLogEnabled gates the per-action security-event audit stream.
	AuditLogEnabled bool
}

// i18nDisclaimerProvider adapts the i18n Manager to services.DisclaimerProvider:
// it returns the localized medical-safety disclaimer (i18n key
// dashboard.prediction_disclaimer) for a language, falling back to the manager's
// default language (Messages merges the default over the target). It is the seam
// the request-free webhook notify pass uses so every payload carries the
// owner-localized "estimates, not medical advice or a method of contraception"
// string without importing the whole Manager into internal/services.
type i18nDisclaimerProvider struct {
	manager *i18n.Manager
}

const disclaimerMessageKey = "dashboard.prediction_disclaimer"

func (provider i18nDisclaimerProvider) Disclaimer(language string) string {
	if provider.manager == nil {
		return ""
	}
	return provider.manager.Messages(language)[disclaimerMessageKey]
}

// BuildNotifyService assembles the request-free webhook notify pass (issue #124,
// slice 3) from the SAME repositories and secret the web path uses, so a future
// in-process scheduler (#125) can reuse this exact recipe. secretKey decrypts
// each owner's stored webhook_url (aad-bound to the owner id); blockPrivateAddresses
// wires the off-by-default WEBHOOK_BLOCK_PRIVATE_ADDRESSES egress gate. The
// returned service reaches a real socket only through the hardened deliverer.
func BuildNotifyService(repositories *db.Repositories, secretKey []byte, i18nManager *i18n.Manager, blockPrivateAddresses bool) *services.WebhookNotifyService {
	webhookSettings := services.NewWebhookSettingsService(repositories.Users, secretKey)
	deliverer := services.NewWebhookDeliverer(blockPrivateAddresses)
	return services.NewWebhookNotifyService(
		repositories.Users,
		repositories.DailyLogs,
		webhookSettings,
		deliverer,
		i18nDisclaimerProvider{manager: i18nManager},
	)
}

// BuildDependencies wires the repositories and configuration into the domain
// services the HTTP handler requires. Both the production binary and the
// internal/api test helpers call it so their wiring stays identical.
func BuildDependencies(repositories *db.Repositories, secretKey []byte, i18nManager *i18n.Manager, opts Options) apideps.Dependencies {
	authService := services.NewAuthService(repositories.Users)
	if opts.LogoutAttempts != nil {
		authService.ConfigureLogoutAttemptLimits(opts.LogoutAttempts.Max, opts.LogoutAttempts.Window)
	}
	attemptLimiter := services.NewAttemptLimiter()
	passwordResetService := services.NewPasswordResetService(authService, attemptLimiter)
	passwordResetService.ConfigureRecoveryAttemptLimits(opts.RecoveryAttempts.Max, opts.RecoveryAttempts.Window)
	loginService := services.NewLoginService(authService, passwordResetService, attemptLimiter)
	loginService.ConfigureAttemptLimits(opts.LoginAttempts.Max, opts.LoginAttempts.Window)
	dailyLogs := repositories.DailyLogs
	dayLogTxRunner := func(ctx context.Context, fn func(services.DayLogRepository) error) error {
		return dailyLogs.WithinTransaction(ctx, func(tx *db.DailyLogRepository) error {
			return fn(tx)
		})
	}
	dayService := services.NewDayServiceWithTx(dailyLogs, repositories.Users, dayLogTxRunner)
	var reservedSymptomNames []string
	if i18nManager != nil {
		reservedSymptomNames = services.BuiltinSymptomReservedNames(i18nManager)
	}
	symptomService := services.NewSymptomService(repositories.Symptoms, reservedSymptomNames...)
	registrationService := services.NewRegistrationService(authService, repositories.Users, opts.RegistrationMode)
	viewerService := services.NewViewerService(dayService, symptomService)
	statsService := services.NewStatsService(dayService, symptomService)
	calendarViewService := services.NewCalendarViewService(dayService, statsService)
	calendarFeedService := services.NewCalendarFeedService(repositories.Users, dayService, i18nDisclaimerProvider{manager: i18nManager})
	calendarFeedSettingsService := services.NewCalendarFeedSettingsService(repositories.Users)
	dashboardViewService := services.NewDashboardViewService(statsService, viewerService, dayService)
	exportService := services.NewExportService(dayService, symptomService)
	importService := services.NewImportService(dailyLogs, repositories.Users, symptomService, dayLogTxRunner)
	settingsService := services.NewSettingsService(repositories.Users)
	webhookSettingsService := services.NewWebhookSettingsService(repositories.Users, secretKey)
	totpService := services.NewTOTPService(repositories.Users, secretKey, attemptLimiter)
	oidcLogoutStateService := services.NewOIDCLogoutStateService(repositories.OIDCLogout)

	var oidcService apideps.OIDCWorkflowService = services.NewOIDCLoginService(
		security.NewOIDCClient(opts.OIDCConfig),
		repositories.OIDCIdentities,
		repositories.Users,
		registrationService,
	)
	if opts.OIDCServiceOverride != nil {
		oidcService = opts.OIDCServiceOverride
	}

	return apideps.Dependencies{
		AuditLogEnabled:        opts.AuditLogEnabled,
		AuthService:            authService,
		RegistrationService:    registrationService,
		PasswordResetService:   passwordResetService,
		LoginService:           loginService,
		OIDCService:            oidcService,
		OIDCLogoutStateSvc:     oidcLogoutStateService,
		DayService:             dayService,
		SymptomService:         symptomService,
		ViewerService:          viewerService,
		StatsService:           statsService,
		CalendarViewService:    calendarViewService,
		CalendarFeedService:    calendarFeedService,
		CalendarFeedSettings:   calendarFeedSettingsService,
		DashboardViewService:   dashboardViewService,
		ExportService:          exportService,
		ImportService:          importService,
		SettingsService:        settingsService,
		SettingsViewService:    services.NewSettingsViewService(settingsService, exportService, symptomService, webhookSettingsService, calendarFeedSettingsService),
		WebhookSettingsService: webhookSettingsService,
		OnboardingService:      services.NewOnboardingService(repositories.Users),
		SetupService:           services.NewSetupService(repositories.Users),
		TOTPService:            totpService,
		RegisterPickupTokens:   repositories.RegisterPickupTokens,
	}
}
