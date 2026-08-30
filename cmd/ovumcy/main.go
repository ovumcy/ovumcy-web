package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/reminders"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// codecov:ignore:start -- main() composition root: this whole function body is
// sequencing/wiring, never business logic. Every function it calls
// (mustLoadLocation, mustLoadRuntimeConfig, mustOpenDatabase,
// mustNewI18nManager, mustNewHandler, newFiberApp, installGracefulShutdown,
// startReminderScheduler, logStartup, runServer, closeDatabase, ...) already
// carries its own direct unit tests or its own narrower codecov:ignore with a
// reason (bootstrap.BuildDependencies is exercised via the internal/api test
// helper; startReminderScheduler's own glue is covered in internal/reminders).
// main() itself is invoked only by the built binary and is exercised by
// image-smoke/e2e, never `go test` — there is no seam to unit-test "does main
// call these in the right order" other than running the real process. Do NOT
// add a per-line ignore here for a new call in this body; it is already
// covered by this region. If you add a CONDITIONAL or a decision (anything
// beyond "call the next already-tested helper") to main(), move that logic
// into its own tested helper function instead of leaving it here uncovered.
func main() {
	handled, err := tryRunCLICommand()
	if err != nil {
		log.Fatal(err)
	}
	if handled {
		return
	}

	location := mustLoadLocation(getEnv("TZ", "Local"))
	time.Local = location

	config := mustLoadRuntimeConfig(location)
	database := mustOpenDatabase(config.DatabaseConfig)
	i18nManager := mustNewI18nManager(config.DefaultLanguage)
	repositories := db.NewRepositories(database)
	// All three boot passes must run after mustOpenDatabase (migrations applied)
	// and before any listener exists: no feed poll can race the revocation, no
	// request can observe a half-repaired identity, and no surface can render a
	// prediction from a luteal-phase estimate the upgrade has not corrected yet.
	// The third does not fail the boot — see recomputeDerivedLutealPhases.
	mustEnforceCalendarFeedKeyRotation(repositories, []byte(config.SecretKey))
	mustRenormalizeAuthEmails(repositories)
	recomputeDerivedLutealPhases(repositories, location)
	dependencies := bootstrap.BuildDependencies(repositories, []byte(config.SecretKey), i18nManager, bootstrapOptions(config))
	handler := mustNewHandler(config, i18nManager, dependencies)
	app := newFiberApp(config, handler)
	served := make(chan struct{})
	sigCtx, stopSignals := installGracefulShutdown(app, served)
	defer stopSignals()

	// Optional built-in reminder scheduler (issue #125). Started AFTER the
	// signal context is wired and BEFORE runServer, gated on config (default
	// OFF). It observes sigCtx — the same context that stops the server — and is
	// launched with `go` inside Start, so it can neither delay app.Listen nor
	// touch served/app shutdown. schedulerDone closes when it has fully drained
	// (an already-closed channel when the scheduler is disabled).
	schedulerDone := startReminderScheduler(sigCtx, config, database, i18nManager)

	logStartup(config)
	err = runServer(app, ":"+config.Port)
	close(served)
	// The server has stopped. If the scheduler is running, wait (bounded) for any
	// in-flight pass to finish reading/writing the DB, THEN close the DB — so the
	// database outlives the last reminder access on this single exit path.
	reminders.Drain(schedulerDone, reminders.DefaultStopBudget)
	closeDatabase(database)
	if err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

// codecov:ignore:end

// runServer blocks in app.Listen until the listener fails or a graceful stop
// completes, then returns the listen error. It no longer closes the database:
// the optional reminder scheduler may still be draining an in-flight pass that
// reads/writes the DB, so closeDatabase is sequenced by main AFTER the scheduler
// drain (see main). The database is still closed on BOTH exit paths — there is
// one main return path and main always closes it — SQLite still checkpoints its
// WAL and releases the file before the process exits.
func runServer(app *fiber.App, address string) error {
	// Fiber v3 moved DisableStartupMessage out of fiber.Config and into the
	// per-listen ListenConfig; keep the banner suppressed as before.
	return app.Listen(address, fiber.ListenConfig{DisableStartupMessage: true})
}

// startReminderScheduler builds and launches the optional built-in reminder
// scheduler (issue #125) when REMINDER_SCHEDULER_ENABLED is on, returning a
// channel that closes once the scheduler goroutine has fully drained. When the
// scheduler is OFF (the default) it returns an already-closed channel so the
// caller's drain is an instant no-op and no goroutine, timer, or outbound
// component is created. The scheduler reuses the SAME notify service recipe the
// `ovumcy notify` CLI uses (bootstrap.BuildNotifyService) plus the app_state
// marker repository, and observes sigCtx for shutdown.
//
// codecov:ignore:start -- main() composition-root wiring; the scheduler logic
// (nextRun, catch-up, marker, drain) is unit-tested in internal/reminders and
// this glue only assembles boot-built collaborators.
func startReminderScheduler(sigCtx context.Context, config runtimeConfig, database *gorm.DB, i18nManager *i18n.Manager) <-chan struct{} {
	if !config.ReminderScheduler.Enabled {
		closed := make(chan struct{})
		close(closed)
		return closed
	}

	repositories := db.NewRepositories(database)
	notifyService := bootstrap.BuildNotifyService(repositories, []byte(config.SecretKey), i18nManager, config.WebhookBlockPrivate)
	scheduler := reminders.New(notifyService, repositories.AppState, reminders.Config{
		Hour:     config.ReminderScheduler.Hour,
		Location: config.Location,
	})
	return scheduler.Start(sigCtx)
}

// codecov:ignore:end

func closeDatabase(database *gorm.DB) {
	sqlDB, err := database.DB()
	if err == nil {
		err = sqlDB.Close()
	}
	if err != nil {
		log.Printf("database close: %v", err)
	}
}

func mustOpenDatabase(databaseConfig db.Config) *gorm.DB {
	database, err := db.OpenDatabase(databaseConfig)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	return database
}

// mustEnforceCalendarFeedKeyRotation runs the boot-time calendar-feed
// key-rotation sentinel: when SECRET_KEY (or the feed-MAC label set) changed
// since the previous boot, it disarms the legacy pre-032 feed rows whose
// key-independent bcrypt hashes would otherwise keep a leaked subscribe URL
// alive across the rotation. The decision logic lives (tested) in
// services.CalendarFeedRotationSentinel; this wrapper only wires repositories,
// fails the boot hard on an error, and prints the operator-facing line.
func mustEnforceCalendarFeedKeyRotation(repositories *db.Repositories, secretKey []byte) {
	sentinel := services.NewCalendarFeedRotationSentinel(repositories.AppState, repositories.Users, secretKey)
	outcome, err := sentinel.Enforce(context.Background())
	if err != nil {
		log.Fatalf("calendar-feed key-rotation check failed: %v", err) // codecov:ignore -- reachable only when the DB fails between migration and this first read
	}
	if message := calendarFeedRotationStartupMessage(outcome); message != "" {
		log.Print(message)
	}
}

// mustRenormalizeAuthEmails runs the one-shot boot pass that rewrites auth
// emails stored by the pre-strict normalizer (whole display-name-decorated
// inputs) down to their bare parsed address, so those accounts keep signing in
// under the strict addr-spec rule. The decision logic lives (tested) in
// services.AuthEmailRenormalizer; this wrapper wires repositories, fails the
// boot hard on a storage error, and prints the operator-facing line.
func mustRenormalizeAuthEmails(repositories *db.Repositories) {
	renormalizer := services.NewAuthEmailRenormalizer(repositories.AppState, repositories.Users)
	outcome, err := renormalizer.Run(context.Background())
	if err != nil {
		log.Fatalf("auth email renormalization failed: %v", err) // codecov:ignore -- reachable only when the DB fails between migration and this first read
	}
	if message := authEmailRenormalizeStartupMessage(outcome); message != "" {
		log.Print(message)
	}
}

// recomputeDerivedLutealPhases runs the one-shot boot pass that rewrites the
// derived users.luteal_phase cache under the corrected personalized inference,
// so an account whose logs no longer support an inference stops predicting
// ovulation a day early. The decision logic lives (tested) in
// services.LutealPhaseRecomputer; this wrapper wires repositories and prints the
// operator-facing line.
//
// Deliberately NOT a must* wrapper, unlike the two passes above: the column is a
// derived cache with a safe fallback, so a storage error must not turn into an
// instance that will not start. It is logged, the marker stays unwritten, and the
// next boot retries.
func recomputeDerivedLutealPhases(repositories *db.Repositories, location *time.Location) {
	recomputer := services.NewLutealPhaseRecomputer(repositories.AppState, repositories.Users, repositories.DailyLogs, location)
	outcome, err := recomputer.Run(context.Background())
	if err != nil {
		log.Printf("luteal-phase recompute failed: %v (retried on the next start)", err)
	}
	if message := lutealPhaseRecomputeStartupMessage(outcome); message != "" {
		log.Print(message)
	}
}

// calendarFeedRotationStartupMessage renders the operator-facing startup line
// for a detected rotation, and stays quiet ("") for the routine cases — first
// boot and an unchanged key — so the startup banner is stable run to run.
func calendarFeedRotationStartupMessage(outcome services.CalendarFeedRotationOutcome) string {
	if !outcome.RotationDetected {
		return ""
	}
	if outcome.DisarmedFeeds == 0 {
		return "SECRET_KEY rotation detected: no legacy calendar feeds to disarm (armed feeds with a keyed MAC stop verifying on their own)"
	}
	return fmt.Sprintf("SECRET_KEY rotation detected: %d legacy calendar feed(s) disarmed; owners re-generate subscribe URLs from settings", outcome.DisarmedFeeds)
}

// authEmailRenormalizeStartupMessage renders the operator-facing startup line
// for the one-shot email repair, and stays quiet ("") when there was nothing
// to do — the startup banner must be stable run to run. Counts only, never
// addresses: emails must not reach logs.
func authEmailRenormalizeStartupMessage(outcome services.AuthEmailRenormalizeOutcome) string {
	if outcome.AlreadyDone {
		return ""
	}
	skipped := outcome.SkippedConflicts + outcome.SkippedUnrenormalizable
	if outcome.Renormalized == 0 && skipped == 0 {
		return ""
	}
	if skipped == 0 {
		return fmt.Sprintf("auth email repair: %d stored email(s) rewritten to their bare address", outcome.Renormalized)
	}
	return fmt.Sprintf("auth email repair: %d stored email(s) rewritten to their bare address, %d left for operator review (duplicate mailbox or unparseable) — see docs/self-hosted.md, Troubleshooting", outcome.Renormalized, skipped)
}

// lutealPhaseRecomputeStartupMessage renders the operator-facing startup line
// for the one-shot luteal-phase recompute, and stays quiet ("") when there was
// nothing to do — the startup banner must be stable run to run. Counts only:
// a per-account estimate is health data and must not reach logs.
func lutealPhaseRecomputeStartupMessage(outcome services.LutealPhaseRecomputeOutcome) string {
	if outcome.AlreadyDone {
		return ""
	}
	if outcome.Corrected == 0 && outcome.Failed == 0 {
		return ""
	}
	if outcome.Failed == 0 {
		return fmt.Sprintf("luteal-phase recompute: %d stored estimate(s) corrected", outcome.Corrected)
	}
	return fmt.Sprintf("luteal-phase recompute: %d stored estimate(s) corrected, %d account(s) could not be read or written — retried on the next start", outcome.Corrected, outcome.Failed)
}

func mustNewI18nManager(defaultLanguage string) *i18n.Manager {
	i18nManager, err := i18n.NewManager(defaultLanguage) // codecov:ignore -- main() composition-root wiring; runs only in the binary (exercised by e2e).
	if err != nil {
		log.Fatalf("i18n init failed: %v", err)
	}
	return i18nManager
}

func mustNewHandler(config runtimeConfig, i18nManager *i18n.Manager, dependencies api.Dependencies) *api.Handler {
	handler, err := api.NewHandler(config.SecretKey, config.Location, i18nManager, config.CookieSecure, dependencies) // codecov:ignore -- main() composition-root wiring; runs only in the binary (exercised by e2e).
	if err != nil {
		log.Fatalf("handler init failed: %v", err)
	}
	// Cache-bust versioned static asset URLs (?v=<token>) so a new build
	// invalidates stale JS/CSS without operator action; resolveAssetVersion
	// falls back ldflags → VCS revision → process start, so even a `go run`
	// deployment never serves a shared constant token across builds.
	handler.SetAssetVersion(resolveAssetVersion()) // codecov:ignore -- main() composition-root wiring; runs only in the binary (exercised by e2e).
	return handler
}

// installGracefulShutdown wires SIGINT/SIGTERM to a graceful stop. served
// must be closed once app.Listen (inside runServer) returns, for any reason —
// it bounds retryShutdown so a signal arriving after the server already
// exited doesn't spin.
//
// It returns both the stop function (to release the signal registration on
// exit) AND the signal context. The context is observed by the optional
// reminder scheduler so it stops on the SAME SIGINT/SIGTERM that stops the
// server. The scheduler only reads that context — it NEVER references served,
// the fiber app, or app shutdown, so it stays entirely clear of the boot-window
// shutdown race that retryShutdown/served exist to bridge.
func installGracefulShutdown(app *fiber.App, served <-chan struct{}) (context.Context, context.CancelFunc) {
	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		retryShutdown(app, shutdownCtx, served)
	}()
	return sigCtx, stopSignals
}

// shutdownRetryInterval is how often retryShutdown re-attempts the graceful
// stop while it is still being silently no-oped in the boot window. It is a
// named constant (not an inline literal) so the retry loop and the tests that
// drive it deterministically share one source of truth.
const shutdownRetryInterval = 20 * time.Millisecond

// retryShutdown calls app.ShutdownWithContext until it takes effect.
// fasthttp's ShutdownWithContext silently no-ops ("if s.ln == nil { return
// nil }") when called before Serve has registered the listener — the boot
// window between fiber's net.Listen and fasthttp registering it, which fiber
// v3's own OnListen hook fires strictly *before* (listen.go: runOnListenHooks
// precedes app.server.Serve(ln)). A single call can silently lose the stop
// request in that window; retrying until served closes (Listen has returned,
// so either the stop already landed or there's nothing left to stop) bridges
// it without slowing down the common, non-racing case.
func retryShutdown(app *fiber.App, ctx context.Context, served <-chan struct{}) {
	retryShutdownFunc(app.ShutdownWithContext, ctx, served, shutdownRetryInterval)
}

// retryShutdownFunc is the interval-driven retry loop behind retryShutdown,
// with the shutdown call and tick interval injected. Production passes
// app.ShutdownWithContext and shutdownRetryInterval, so retryShutdown's
// behavior is byte-for-byte unchanged; the seam exists purely so tests can
// exercise the retry/log/terminate contract deterministically. A stub
// shutdown func lets the error-branch test force a persistent failure without
// depending on real fasthttp accept timing (whether the raw connection is
// counted as open at the instant of the stop call) — the race that made
// TestRetryShutdownLogsPersistentShutdownError flaky under load.
func retryShutdownFunc(shutdown func(context.Context) error, ctx context.Context, served <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := shutdown(ctx); err != nil {
			log.Printf("server shutdown failed: %v", err)
			return
		}
		select {
		case <-served:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// bootstrapOptions maps the runtime configuration onto the dependency-build
// options. It is its own function so the mapping is testable: the two logout
// budgets read from two different configuration pairs, and wiring the
// per-account limiter from the per-IP pair is exactly the defect this
// separation closes (docs/security/auth-policy-and-rate-limits.md).
func bootstrapOptions(config runtimeConfig) bootstrap.Options {
	return bootstrap.Options{
		RegistrationMode: config.RegistrationMode,
		OIDCConfig:       config.OIDC,
		LoginAttempts:    bootstrap.AttemptLimit{Max: config.RateLimits.LoginMax, Window: config.RateLimits.LoginWindow},
		RecoveryAttempts: bootstrap.AttemptLimit{Max: config.RateLimits.ForgotPasswordMax, Window: config.RateLimits.ForgotPasswordWindow},
		// The ACCOUNT pair, never LogoutMax/LogoutWindow: those size the per-IP
		// edge limiter in server.go.
		LogoutAttempts:  &bootstrap.AttemptLimit{Max: config.RateLimits.LogoutAccountMax, Window: config.RateLimits.LogoutAccountWindow},
		AuditLogEnabled: config.AuditLogEnabled,
	}
}
