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
	//
	// This LINE is what the guard's own behaviour tests cannot see: they mount
	// it themselves and pass against an app that never does. Deleting it is
	// pinned by TestRegisterRoutesMountsTheDeadlineGuardDownToTheRepository,
	// which observes the context at the repository, not at the handler.
	app.Use(RequestDeadlineGuard(RequestBudget))
	app.Use(requestBodyLimitGuard)
	registerPageRoutes(app, handler)
	registerV1APIRoutes(app, handler)
	registerHEADTwins(app)
}

// registerHEADTwins gives every GET route registered above a HEAD route with
// the same handler chain, so a HEAD request is answered by the route's own
// handlers — same status, same headers, no body, since fasthttp drops the body
// of a HEAD response on the wire.
//
// Fiber makes these copies on its own, but only at startup (App.startupProcess
// → ensureAutoHeadRoutes), and it appends them to the END of the HEAD stack.
// The deployed app registers its terminal catch-all with app.Use AFTER this
// function runs, and a Use route joins EVERY method stack at the moment it is
// registered — so a copy appended later sits behind the catch-all and is never
// reached: HEAD to any page or /api/v1 route answered 404 while GET answered
// the route. Registering the twins here, while route registration is still
// running, puts them ahead of whatever the composition root mounts afterwards,
// and leaves fiber's own pass with nothing to add (it skips a GET path that
// already carries a HEAD route).
//
// A GET path that already carries its own HEAD route keeps it: HEAD
// /api/v1/days/:date answers a different question from GET — whether the day
// holds any data at all — and must not be replaced by a copy of the reader.
func registerHEADTwins(app *fiber.App) {
	covered := make(map[string]struct{})
	for _, route := range app.GetRoutes(true) {
		if route.Method == fiber.MethodHead {
			covered[route.Path] = struct{}{}
		}
	}

	for _, route := range app.GetRoutes(true) {
		if route.Method != fiber.MethodGet || len(route.Handlers) == 0 {
			continue // codecov:ignore -- a route with no handler cannot exist here: fiber panics on one
		}
		if _, done := covered[route.Path]; done {
			continue
		}
		covered[route.Path] = struct{}{}

		chain := make([]any, 0, len(route.Handlers))
		for _, routeHandler := range route.Handlers {
			chain = append(chain, routeHandler)
		}
		app.Add([]string{fiber.MethodHead}, route.Path, chain[0], chain[1:]...)
	}
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
	usersCurrent.Delete("/webhook", handler.OwnerOnly, handler.RemoveWebhookDestination)
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
	// Authenticated settings path for linking a NEW OIDC identity (issue #701):
	// the public /auth/oidc/link-confirm route below stays closed, and this is
	// the replacement — a fresh provider re-authentication gates the same
	// permanent binding ConfirmAndLinkIdentity performs.
	usersCurrent.Post("/oidc/link/step-up", handler.OwnerOnly, handler.StartOIDCIdentityLinkStepup)

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
	app.Post(LanguageSwitchPath, handler.SetLanguage)

	app.Get("/login", handler.ShowLoginPage)
	app.Get("/auth/oidc/start", handler.refuseHEADOnShownOnceSurface, handler.StartOIDCLogin)
	app.Get(oidcLogoutBridgePath, handler.ShowOIDCLogoutBridge)
	app.Get(oidcLogoutBridgeRedirectPath, handler.refuseHEADOnShownOnceSurface, handler.RedirectOIDCLogout)
	app.Get("/register", handler.ShowRegisterPage)
	app.Get(registerPickupNextPath, handler.refuseHEADOnShownOnceSurface, requireFirstPartyRequest(handler.refuseRegisterPickupRequest), handler.PickupRegister)
	app.Get("/recovery-code", handler.refuseHEADOnShownOnceSurface, handler.ShowRecoveryCodePage)
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
		app.Get("/auth/oidc/callback", handler.refuseHEADOnShownOnceSurface, handler.CompleteOIDCLogin)
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
	app.Get(calendarFeedRevealPath, handler.refuseHEADOnShownOnceSurface, handler.AuthRequired, requireFirstPartyRequest(handler.refuseCalendarFeedRevealRequest), handler.ShowCalendarFeedRevealPage)
}

// shownOnceGETRoutes names every GET route whose chain starts with
// refuseHEADOnShownOnceSurface: the routes whose GET spends or mints one-time
// auth material, and which therefore refuse the HEAD twin registerHEADTwins
// would otherwise let run that chain for a body the protocol discards.
//
// The set is declared rather than derived for the same reason
// firstPartyGuardedRoutes is: nothing in the route table says which GET spends
// something. Membership is decided by reading the handler — /register/welcome
// consumes the pickup token, /recovery-code and /settings/calendar-feed claim
// their reveal marks, /auth/oidc/logout/redirect consumes the end-session
// state, the query-mode OIDC callback consumes the one-time state cookie
// together with the provider's authorization code, and /auth/oidc/start mints
// that state cookie and starts the provider handshake before it redirects —
// a HEAD can neither follow that redirect nor use the cookie, and would only
// overwrite whatever state a concurrent GET login already staged in the same
// cookie jar. TestShownOnceGETRoutesAreExactlyTheDeclaredSet refuses both
// drifts: a refusal dropped from a route named here, and a route that
// acquires one without being named.
//
// The OIDC callback is registered only under OIDC_RESPONSE_MODE=query, which is
// why that test builds a query-mode app as well as the default one.
//
// What counts as spending or minting is one-time SECRET or AUTH material — a
// consumption mark, a pickup nonce, a sealed one-time state cookie. The flash
// cookie every page pops is not: it carries a notice, not a secret, and
// treating it as one would refuse HEAD on nearly every page in the app, which
// is the answer this change exists to remove. popFlashCookie itself leaves the
// cookie alone on HEAD — it returns an empty payload without reading or
// clearing it — so no route here needs to guard the flash on the route's
// account.
//
// This is not the whole set of surfaces that spend something on a GET: the
// inline recovery-code reveal shares GET /register with the anonymous signup
// page, so a route-wide refusal there would answer 404 to an ordinary probe of
// a page that exists. That one is refused inside claimRecoveryCodeReveal, where
// the first-party rule sits and for the same reason — see its comment. A route
// belongs HERE when its GET spends or mints something on every visit; a route
// whose spend depends on the request's own state refuses at the spend.
var shownOnceGETRoutes = []string{
	fiber.MethodGet + " /auth/oidc/start",
	fiber.MethodGet + " /auth/oidc/callback",
	fiber.MethodGet + " " + oidcLogoutBridgeRedirectPath,
	fiber.MethodGet + " " + registerPickupNextPath,
	fiber.MethodGet + " /recovery-code",
	fiber.MethodGet + " " + calendarFeedRevealPath,
}

// firstPartyGuardedRoutes names every route that carries
// requireFirstPartyRequest. The set is declared rather than derived because
// nothing in the route table says which GET mutates state — so a route joining
// the class has to be written down here, and
// TestFirstPartyGuardedRoutesAreExactlyTheDeclaredSet refuses both drifts: a
// guard silently dropped from a route, and a route silently guarded without
// being named.
var firstPartyGuardedRoutes = []string{
	fiber.MethodGet + " " + registerPickupNextPath,
	fiber.MethodGet + " " + calendarFeedRevealPath,
}

func sendNoContent(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}
