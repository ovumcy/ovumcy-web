package api

import "github.com/gofiber/fiber/v3"

func RegisterRoutes(app *fiber.App, handler *Handler) {
	// Registered here, alongside the routes it protects, rather than in the
	// composition root's middleware chain: it must cover every route this
	// function registers that can carry a body a handler reads — page and
	// /api/v1 alike — and an app assembled without it would answer an over-limit
	// compressed body with a domain error or fiber's bare text. Mounted app-wide
	// rather than on the body-reading groups so a route added outside them still
	// inherits it; the guard itself decides, from the request method, whether the
	// decode probe is owed. See requestBodyLimitGuard.
	// Registered before the body-limit guard so the decode probe below runs
	// under the deadline too — the probe is the decompression, and an inflate
	// of a BodyLimit-sized stream is exactly the kind of work that must not
	// outlive the caller. See RequestDeadlineGuard.
	app.Use(RequestDeadlineGuard(RequestBudget))
	app.Use(requestBodyLimitGuard)
	registerPageRoutes(app, handler)
	registerV1APIRoutes(app, handler)
}

func registerV1APIRoutes(app *fiber.App, handler *Handler) {
	v1 := app.Group("/api/v1")

	users := v1.Group("/users")
	users.Post("", handler.Register)

	usersCurrent := users.Group("/current", handler.AuthRequired)
	usersCurrent.Get("", handler.OwnerOnly, handler.GetCurrentUser)
	usersCurrent.Delete("", handler.OwnerOnly, handler.DeleteAccount)
	usersCurrent.Patch("/profile", handler.OwnerOnly, handler.UpdateProfile)
	usersCurrent.Patch("/interface", handler.OwnerOnly, handler.UpdateInterfaceSettings)
	usersCurrent.Patch("/tracking", handler.OwnerOnly, handler.UpdateTrackingSettings)
	usersCurrent.Post("/webhook", handler.OwnerOnly, handler.UpdateWebhookSettings)
	usersCurrent.Post("/calendar-feed", handler.OwnerOnly, handler.GenerateCalendarFeed)
	usersCurrent.Post("/calendar-feed/rotate", handler.OwnerOnly, handler.RotateCalendarFeed)
	usersCurrent.Delete("/calendar-feed", handler.OwnerOnly, handler.RevokeCalendarFeed)
	usersCurrent.Post("/timezone", handler.OwnerOnly, handler.UpdateTimezone)
	usersCurrent.Patch("/cycle", handler.OwnerOnly, handler.UpdateCycleSettings)
	usersCurrent.Patch("/reminders", handler.OwnerOnly, handler.UpdateReminderSettings)
	usersCurrent.Put("/password", handler.OwnerOnly, handler.ChangePassword)
	usersCurrent.Post("/password/step-up", handler.OwnerOnly, handler.StartLocalPasswordSetupReauth)
	usersCurrent.Post("/recovery-code", handler.OwnerOnly, handler.RegenerateRecoveryCode)
	usersCurrent.Put("/2fa", handler.OwnerOnly, handler.VerifyTOTP2FAEnrollment)
	usersCurrent.Delete("/2fa", handler.OwnerOnly, handler.DisableTOTP2FA)
	usersCurrent.Post("/data-wipe/validate", handler.OwnerOnly, handler.ValidateClearDataPassword)
	usersCurrent.Post("/data-wipe", handler.OwnerOnly, handler.ClearAllData)
	usersCurrent.Post("/data-wipe/step-up", handler.OwnerOnly, handler.StartClearDataStepupReauth)
	usersCurrent.Post("/deletion/step-up", handler.OwnerOnly, handler.StartDeleteAccountStepupReauth)

	onboarding := v1.Group("/onboarding", handler.AuthRequired)
	onboarding.Post("/steps/1", handler.OwnerOnly, handler.OnboardingStep1)
	onboarding.Post("/steps/2", handler.OwnerOnly, handler.OnboardingStep2)
	onboarding.Post("/complete", handler.OwnerOnly, handler.OnboardingComplete)

	sessions := v1.Group("/sessions")
	sessions.Post("", handler.Login)
	sessions.Post("/2fa-challenge", handler.VerifyTOTPLogin)
	sessions.Delete("/current", handler.AuthRequired, handler.OwnerOnly, handler.Logout)

	passwordResets := v1.Group("/password-resets")
	passwordResets.Post("", handler.ForgotPassword)
	passwordResets.Post("/redeem", handler.ResetPassword)

	days := v1.Group("/days", handler.AuthRequired)
	days.Get("", handler.OwnerOnly, handler.GetDays)
	days.Head("/:date", handler.OwnerOnly, handler.CheckDayExists)
	days.Get("/:date", handler.OwnerOnly, handler.GetDay)
	days.Put("/:date", handler.OwnerOnly, handler.UpsertDay)
	days.Delete("/:date", handler.OwnerOnly, handler.DeleteDay)
	days.Post("/:date/cycle-start", handler.OwnerOnly, handler.MarkCycleStart)

	symptoms := v1.Group("/symptoms", handler.AuthRequired)
	symptoms.Get("", handler.OwnerOnly, handler.GetSymptoms)
	symptoms.Post("", handler.OwnerOnly, handler.CreateSymptom)
	symptoms.Patch("/:id", handler.OwnerOnly, handler.UpdateSymptom)
	symptoms.Delete("/:id", handler.OwnerOnly, handler.DeleteSymptom)
	symptoms.Post("/:id/restore", handler.OwnerOnly, handler.RestoreSymptom)

	stats := v1.Group("/stats", handler.AuthRequired)
	stats.Get("/overview", handler.OwnerOnly, handler.GetStatsOverview)

	exports := v1.Group("/exports", handler.AuthRequired, handler.OwnerOnly)
	exports.Get("/summary", handler.ExportSummary)
	exports.Get("/csv", handler.ExportCSV)
	exports.Get("/json", handler.ExportJSON)

	imports := v1.Group("/imports", handler.AuthRequired)
	imports.Post("/json", handler.OwnerOnly, handler.ImportJSON)
}

func registerPageRoutes(app *fiber.App, handler *Handler) {
	// Liveness and readiness are separate probes with the same public,
	// unauthenticated posture: neither sits under /api (so neither inherits the
	// API rate-limit budget), both are GET (so the CSRF middleware never
	// validates them), and both cost one cheap answer. /healthz never touches
	// storage; /readyz does exactly one trivial storage probe.
	app.Get("/healthz", handler.Health)
	app.Get("/readyz", handler.Ready)
	app.Get("/favicon.ico", sendNoContent)
	app.Post("/lang", handler.SetLanguage)

	app.Get("/login", handler.ShowLoginPage)
	app.Get("/auth/oidc/start", handler.StartOIDCLogin)
	app.Get(oidcLogoutBridgePath, handler.ShowOIDCLogoutBridge)
	app.Get(oidcLogoutBridgeRedirectPath, handler.RedirectOIDCLogout)
	app.Get("/register", handler.ShowRegisterPage)
	app.Get("/register/welcome", handler.PickupRegister)
	app.Get("/recovery-code", handler.ShowRecoveryCodePage)
	app.Get("/forgot-password", handler.ShowForgotPasswordPage)
	app.Get("/reset-password", handler.ShowResetPasswordPage)
	app.Get("/auth/2fa", handler.ShowTOTPChallengePage)
	app.Post("/auth/oidc/callback", handler.CompleteOIDCLogin)
	// In query response mode the provider returns the code via a GET redirect,
	// so the callback must also answer GET. GET is a safe method and is not
	// CSRF-validated by the middleware; like the POST callback it is guarded by
	// the sealed one-time state cookie (matchesState + validAt), which reads the
	// state from the query in this mode. form_post deployments keep POST-only.
	if handler.oidcResponseModeQuery() {
		app.Get("/auth/oidc/callback", handler.CompleteOIDCLogin)
	}
	app.Get(oidcLinkConfirmPath, handler.ShowOIDCLinkConfirmPage)
	app.Post(oidcLinkConfirmPath, handler.CompleteOIDCLinkConfirmation)
	app.Post("/logout", handler.AuthRequired, handler.OwnerOnly, handler.Logout)
	app.Get("/privacy", handler.ShowPrivacyPage)
	app.Get("/onboarding", handler.AuthRequired, handler.ShowOnboarding)
	app.Get("/", handler.AuthRequired, handler.ShowDashboard)
	app.Get("/dashboard", handler.AuthRequired, handler.ShowDashboard)
	app.Get("/calendar", handler.AuthRequired, handler.ShowCalendar)
	app.Get("/calendar/day/:date", handler.AuthRequired, handler.CalendarDayPanel)
	// Calendar (.ics) feed: authenticated by the path token ALONE (no cookie),
	// so it is deliberately NOT behind AuthRequired/OwnerOnly. Shaped as
	// ":token.ics" so the token binds as a clean :token param that
	// SafeRequestLogPath masks. Per-IP rate-limited in cmd/ovumcy/main.go.
	app.Get(calendarFeedRoutePath, handler.ServeCalendarFeed)
	app.Get("/stats", handler.AuthRequired, handler.ShowStats)
	app.Get("/settings", handler.AuthRequired, handler.ShowSettings)
	app.Get("/settings/2fa", handler.AuthRequired, handler.ShowTOTPSetupPage)
	// One-time reveal of the freshly generated/rotated .ics subscribe URL. The
	// URL (a secret) is read from the sealed one-time cookie, shown once, then
	// the cookie is cleared; a refresh redirects back to /settings.
	app.Get("/settings/calendar-feed", handler.AuthRequired, handler.ShowCalendarFeedRevealPage)
}

func sendNoContent(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}
