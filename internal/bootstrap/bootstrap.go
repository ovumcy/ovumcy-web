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
	// OutboundDeliveryEnabled says whether this instance runs the built-in
	// reminder pass at all (REMINDER_SCHEDULER_ENABLED, off by default). It is
	// wired here rather than read where it is rendered so the settings surface
	// receives a DOMAIN fact and not a configuration lookup: on a default
	// instance every webhook column is inert, and a surface that called such a
	// row "armed" would claim a capability the process does not have.
	OutboundDeliveryEnabled bool
}

// i18nDisclaimerProvider adapts the i18n Manager to services.NotifyCopyProvider:
// it answers any catalogue key for a language, and names the medical-safety
// disclaimer (i18n key medical.disclaimer — the single catalogue entry every
// predictive surface renders) through its own method, falling back to the
// manager's default language (Messages merges the default over the target). It
// is the seam the request-free egress passes use so every payload carries
// owner-localized copy — the reminder headline and sentence as well as the
// "estimates, not medical advice or a method of contraception" string — without
// importing the whole Manager into internal/services.
type i18nDisclaimerProvider struct {
	manager *i18n.Manager
}

const disclaimerMessageKey = "medical.disclaimer"

func (provider i18nDisclaimerProvider) Disclaimer(language string) string {
	return provider.Message(language, disclaimerMessageKey)
}

// Message returns the catalogue entry for key at language. A nil manager (never
// production, only a partially wired test) yields the empty string rather than
// panicking, matching Disclaimer's original behavior.
func (provider i18nDisclaimerProvider) Message(language string, key string) string {
	if provider.manager == nil {
		return ""
	}
	return provider.manager.Messages(language)[key]
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
	calendarFeedService := services.NewCalendarFeedService(repositories.Users, dayService, i18nDisclaimerProvider{manager: i18nManager}, secretKey)
	calendarFeedSettingsService := services.NewCalendarFeedSettingsService(repositories.Users, secretKey)
	dashboardViewService := services.NewDashboardViewService(statsService, viewerService, dayService)
	exportService := services.NewExportService(dayService, symptomService)
	importService := services.NewImportService(dailyLogs, repositories.Users, symptomService, dayLogTxRunner)
	settingsService := services.NewSettingsService(repositories.Users)
	// Attach the shared limiter and the secret key so the re-auth budget keys on
	// (client, account) like the other auth policies rather than on the client
	// alone. The budget itself is not operator-tunable: unlike the edge limiters
	// it guards a credential check, and the default matches totp.disable.
	settingsService.ConfigureReauthAttempts(
		secretKey,
		attemptLimiter,
		services.DefaultSettingsReauthAttemptsLimit,
		services.DefaultSettingsReauthAttemptsWindow,
	)
	webhookSettingsService := services.NewWebhookSettingsService(repositories.Users, secretKey)
	egressLedgerService := services.NewEgressLedgerService(webhookSettingsService, calendarFeedSettingsService, opts.OutboundDeliveryEnabled)
	totpService := services.NewTOTPService(repositories.Users, secretKey, attemptLimiter)
	// Every session-issuing path consults the same derived TOTP-verifiability
	// predicate instead of the raw TOTPEnabled column (see TOTPFactorVerifier,
	// internal/services/totp_service.go): wire it onto the local login service
	// and onto the concrete OIDC login service below, before the latter is
	// boxed into the apideps.OIDCWorkflowService interface.
	loginService.SetTOTPVerifier(totpService)
	oidcLogoutStateService := services.NewOIDCLogoutStateService(repositories.OIDCLogout)

	oidcLoginService := services.NewOIDCLoginService(
		security.NewOIDCClient(opts.OIDCConfig),
		repositories.OIDCIdentities,
		repositories.Users,
		registrationService,
	)
	oidcLoginService.SetTOTPVerifier(totpService)
	var oidcService apideps.OIDCWorkflowService = oidcLoginService
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
		SettingsViewService:    services.NewSettingsViewService(settingsService, exportService, symptomService, egressLedgerService),
		WebhookSettingsService: webhookSettingsService,
		OnboardingService:      services.NewOnboardingService(repositories.Users),
		SetupService:           services.NewSetupService(repositories.Users),
		TOTPService:            totpService,
		ReadinessService:       services.NewReadinessService(repositories.Health),
		RegisterPickupTokens:   repositories.RegisterPickupTokens,
	}
}
