package api

import "github.com/gofiber/fiber/v2"

func RegisterRoutes(app *fiber.App, handler *Handler) {
	registerPageRoutes(app, handler)
	registerAPIRoutes(app, handler)
}

func registerPageRoutes(app *fiber.App, handler *Handler) {
	app.Get("/healthz", handler.Health)
	app.Get("/favicon.ico", sendNoContent)
	app.Get("/lang/:lang", handler.SetLanguage)

	app.Get("/login", handler.ShowLoginPage)
	app.Get("/auth/oidc/start", handler.StartOIDCLogin)
	app.Get(oidcLogoutBridgePath, handler.ShowOIDCLogoutBridge)
	app.Get(oidcLogoutBridgeRedirectPath, handler.RedirectOIDCLogout)
	app.Get("/register", handler.ShowRegisterPage)
	app.Get("/recovery-code", handler.ShowRecoveryCodePage)
	app.Get("/forgot-password", handler.ShowForgotPasswordPage)
	app.Get("/reset-password", handler.ShowResetPasswordPage)
	app.Post("/auth/oidc/callback", handler.CompleteOIDCLogin)
	app.Post("/logout", handler.AuthRequired, handler.Logout)
	app.Get("/privacy", handler.ShowPrivacyPage)
	app.Get("/onboarding", handler.AuthRequired, handler.ShowOnboarding)
	app.Post("/onboarding/step1", handler.AuthRequired, handler.OnboardingStep1)
	app.Post("/onboarding/step2", handler.AuthRequired, handler.OnboardingStep2)
	app.Post("/onboarding/complete", handler.AuthRequired, handler.OnboardingComplete)
	app.Get("/", handler.AuthRequired, handler.ShowDashboard)
	app.Get("/dashboard", handler.AuthRequired, handler.ShowDashboard)
	app.Get("/calendar", handler.AuthRequired, handler.ShowCalendar)
	app.Get("/calendar/day/:date", handler.AuthRequired, handler.CalendarDayPanel)
	app.Get("/stats", handler.AuthRequired, handler.ShowStats)
	app.Get("/settings", handler.AuthRequired, handler.ShowSettings)
	app.Post("/settings/cycle", handler.AuthRequired, handler.OwnerOnly, handler.UpdateCycleSettings)
}

func registerAPIRoutes(app *fiber.App, handler *Handler) {
	api := app.Group("/api")

	auth := api.Group("/auth")
	auth.Post("/logout", handler.AuthRequired, handler.Logout)
	auth.Post("/register", handler.Register)
	auth.Post("/login", handler.Login)
	auth.Post("/forgot-password", handler.ForgotPassword)
	auth.Post("/reset-password", handler.ResetPassword)

	days := api.Group("/days", handler.AuthRequired)
	days.Get("", handler.GetDays)
	days.Get("/:date/exists", handler.OwnerOnly, handler.CheckDayExists)
	days.Get("/:date", handler.GetDay)
	days.Post("/:date", handler.OwnerOnly, handler.UpsertDay)
	days.Post("/:date/cycle-start", handler.OwnerOnly, handler.MarkCycleStart)
	days.Delete("/:date", handler.OwnerOnly, handler.DeleteDay)

	dailyLog := api.Group("/log", handler.AuthRequired, handler.OwnerOnly)
	dailyLog.Delete("/delete", handler.DeleteDailyLog)

	symptoms := api.Group("/symptoms", handler.AuthRequired)
	symptoms.Get("", handler.OwnerOnly, handler.GetSymptoms)
	symptoms.Post("", handler.OwnerOnly, handler.CreateSymptom)
	symptoms.Post("/:id", handler.OwnerOnly, handler.UpdateSymptom)
	symptoms.Post("/:id/archive", handler.OwnerOnly, handler.ArchiveSymptom)
	symptoms.Post("/:id/restore", handler.OwnerOnly, handler.RestoreSymptom)
	symptoms.Delete("/:id", handler.OwnerOnly, handler.DeleteSymptom)

	stats := api.Group("/stats", handler.AuthRequired)
	stats.Get("/overview", handler.GetStatsOverview)

	export := api.Group("/export", handler.AuthRequired, handler.OwnerOnly)
	export.Post("/summary", handler.ExportSummary)
	export.Post("/csv", handler.ExportCSV)
	export.Post("/json", handler.ExportJSON)
	export.Post("/pdf", handler.ExportPDF)

	settings := api.Group("/settings", handler.AuthRequired)
	settings.Post("/interface", handler.UpdateInterfaceSettings)
	settings.Post("/profile", handler.UpdateProfile)
	settings.Post("/tracking", handler.OwnerOnly, handler.UpdateTrackingSettings)
	settings.Post("/change-password", handler.ChangePassword)
	settings.Post("/regenerate-recovery-code", handler.RegenerateRecoveryCode)
	settings.Post("/clear-data/validate", handler.OwnerOnly, handler.ValidateClearDataPassword)
	settings.Post("/clear-data", handler.OwnerOnly, handler.ClearAllData)
	settings.Delete("/delete-account", handler.DeleteAccount)
}

func sendNoContent(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}
