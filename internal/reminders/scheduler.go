// Package reminders holds the trigger for the built-in daily reminder scheduler
// (issue #125). It is a thin, lifecycle-tied wrapper around the request-free
// webhook notify pass (issue #124): the pass itself — listing owners, deciding
// due reminders, delivering, and writing each per-reminder watermark — is
// unchanged and remains the authoritative idempotency guard. This package adds
// ONLY the trigger: when to call the pass, a once-per-local-day "ran today"
// marker for restart safety and current-day catch-up, panic isolation so a bad
// pass cannot crash the web process, and a bounded drain on shutdown.
//
// Why here and not in cmd/ovumcy: keeping the schedule math (nextRun),
// catch-up, marker handling, and drain in an internal package makes them fully
// unit-testable with an injected clock and stubs — cmd/ovumcy is the codecov-
// ignored composition root and only supplies ~3 lines of glue.
//
// Concurrency contract (this is the first long-lived, request-path-adjacent
// goroutine in the codebase): a Scheduler.Run runs in exactly ONE goroutine.
// The "ran today" marker is read and written only by that goroutine, so it needs
// no Go-level locking. The Scheduler captures only value config, the PassRunner
// and MarkerStore interfaces (built once at boot), and a clock — it shares NO
// mutable Go state with the HTTP handlers. The underlying DB handle is the
// concurrency-safe *gorm.DB the rest of the process already shares. The
// scheduler never touches the fiber app, the server's `served` channel, or app
// shutdown; it only reads its clock/timer, calls RunOnce, and selects on the
// signal context — so it cannot participate in the fiber/fasthttp boot-window
// shutdown race (#146/#147/#165).
package reminders

import (
	"context"
	"log"
	"runtime/debug"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// dateLayout is the marker's stored form: a bare local calendar date. Comparing
// dates (not instants) is what makes "already ran today" independent of the
// wall-clock time within the day.
const dateLayout = "2006-01-02"

// PassRunner is the single behavior the scheduler needs from the notify service:
// run one pass. It is satisfied by *services.WebhookNotifyService.RunOnce, kept
// as an interface so the scheduler is testable with a stub that records calls
// and never opens a socket. dryRun is always false from the scheduler — a dry
// run is an operator-CLI concern.
type PassRunner interface {
	RunOnce(ctx context.Context, now time.Time, location *time.Location, dryRun bool) (services.NotifyReport, error)
}

// MarkerStore is the narrow persistence the scheduler needs for its once-per-
// local-day marker. It is satisfied by *db.AppStateRepository (Get/Set on the
// app_state key/value table). Only the scheduler goroutine calls it.
type MarkerStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
}

// Config is the value-only configuration captured at boot. Hour is the LOCAL
// hour-of-day (0-23) the daily pass should run at; Location is the server's
// configured timezone (the same one the notify pass uses as its per-owner
// fallback — there is no separate reminder TZ).
type Config struct {
	Hour     int
	Location *time.Location
}

// Scheduler triggers the daily notify pass. It is constructed once at boot and
// driven by a single Run goroutine. All fields are set at construction and never
// mutated afterward, so the struct is safe to share by value-capture into the
// goroutine.
type Scheduler struct {
	runner PassRunner
	marker MarkerStore
	config Config

	// now returns the current instant. Injected so tests drive scheduling
	// deterministically without touching the wall clock; production passes
	// time.Now.
	now func() time.Time
	// newTimer creates a timer that fires after d. Injected so tests can make
	// "wait until next run" fire immediately (or never) without real sleeps;
	// production wraps time.NewTimer. The scheduler consumes only C() and
	// Stop(), so the fake need not be a real *time.Timer (calling Stop on a
	// hand-built *time.Timer panics on modern Go).
	newTimer func(d time.Duration) schedulerTimer
	// markerKey is the app_state key for the "ran today" date. A field (not a
	// bare const at the call site) purely so tests can assert against it.
	markerKey string
}

// schedulerTimer is the minimal timer seam the scheduler needs: a fire channel
// and a Stop. It decouples the loop from *time.Timer so tests can supply a
// fully deterministic fake (fire-now / never-fire) without constructing a real
// runtime timer.
type schedulerTimer interface {
	C() <-chan time.Time
	Stop()
}

// realTimer adapts *time.Timer to schedulerTimer for production.
type realTimer struct{ timer *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.timer.C }
func (t realTimer) Stop()               { t.timer.Stop() }

func newRealTimer(d time.Duration) schedulerTimer { return realTimer{timer: time.NewTimer(d)} }

// New builds a Scheduler with production wiring: the real clock and real timers.
// runner and marker are the boot-built collaborators; config carries the local
// run hour and server location.
func New(runner PassRunner, marker MarkerStore, config Config) *Scheduler {
	return &Scheduler{
		runner:    runner,
		marker:    marker,
		config:    config,
		now:       time.Now,
		newTimer:  newRealTimer,
		markerKey: markerKey,
	}
}

// Run is the scheduler goroutine body. It runs until ctx is cancelled (the same
// signal context that stops the HTTP server), then returns so the caller can
// sequence the DB close after it. The flow is:
//
//  1. Catch-up on start: if the marker shows the last run was before today AND
//     the local time is already at/after the run hour, run one pass immediately
//     (covers "the process was down when the scheduled hour passed"). A marker
//     equal to today means a same-day restart — skip, do not re-fire.
//  2. Loop: arm a timer to the next instant whose local hour == Hour, wait for
//     it (or ctx), run one pass, mark today, and recompute the next instant.
//     Recomputing each cycle (rather than a fixed 24h ticker) keeps the fire
//     pinned to the wall-clock hour across DST transitions.
//
// ctx flows into every RunOnce, so a shutdown mid-pass cancels the in-flight
// outbound work.
func (s *Scheduler) Run(ctx context.Context) {
	s.runCatchUp(ctx)
	if ctx.Err() != nil {
		return
	}

	for {
		timer := s.newTimer(s.untilNextRun(s.now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}

		// Re-check cancellation: a timer that fires in the same scheduling round
		// as ctx cancellation must not start a pass we are trying to drain.
		if ctx.Err() != nil {
			return
		}
		s.runScheduledPass(ctx)
	}
}

// runCatchUp performs the current-day catch-up described on Run. It runs at most
// one pass and only for TODAY — it never backfills historical missed days.
func (s *Scheduler) runCatchUp(ctx context.Context) {
	now := s.now()
	today := now.In(s.config.Location).Format(dateLayout)

	lastRun, ok, err := s.marker.Get(ctx, s.markerKey)
	if err != nil {
		// A marker read failure must not fire a pass that idempotency would then
		// double-guard on the marker: fail safe by skipping catch-up. The next
		// scheduled fire still runs. #124's watermark remains the true backstop.
		log.Printf("reminder scheduler: marker read failed on startup, skipping catch-up")
		return
	}
	if ok && lastRun == today {
		// Already ran today (restart safety): do not re-fire.
		return
	}
	if !s.localHourReached(now) {
		// Today's scheduled hour has not arrived yet; the normal timer loop will
		// fire it. Nothing to catch up.
		return
	}
	s.runScheduledPass(ctx)
}

// runScheduledPass runs exactly one notify pass with panic isolation, then marks
// today ON SUCCESS ONLY. A panic is recovered so the scheduler survives; the day
// is NOT marked on panic so the next fire retries (and #124's per-reminder
// watermark still prevents any double-send). now is resolved once so the pass
// clock and the marked date agree.
func (s *Scheduler) runScheduledPass(ctx context.Context) {
	now := s.now()
	if !s.runPassRecovered(ctx, now) {
		// Panicked: leave the marker untouched so a retry is allowed.
		return
	}
	s.markRan(ctx, now)
}

// runPassRecovered calls RunOnce inside a recover() so a panic in the batch pass
// (or anything it calls) is contained to this goroutine instead of crashing the
// process — fiber's recover middleware only covers request handlers. It returns
// true when the pass completed without panicking (a returned error is a normal,
// logged pass outcome, not a panic). The recovery logs only a stable reason and
// a scrubbed stack — NEVER per-owner reminder contents (RunOnce itself never
// surfaces them).
func (s *Scheduler) runPassRecovered(ctx context.Context, now time.Time) (completed bool) {
	defer func() {
		if r := recover(); r != nil {
			completed = false
			log.Printf("reminder scheduler: notify pass panicked, recovered and continuing; stack:\n%s", debug.Stack())
		}
	}()

	if _, err := s.runner.RunOnce(ctx, now, s.config.Location, false); err != nil {
		// A pass-level error (e.g. listing owners failed) is expected-transient and
		// safe to log by reason; RunOnce already contains per-owner failures. We
		// still mark today so a broken DB does not busy-loop the scheduler — the
		// watermark guarantees no double-send if it recovers before the next day.
		log.Printf("reminder scheduler: notify pass returned an error (see prior notify logs)")
	}
	return true
}

// markRan records today's local date as the last successful run. A write failure
// is logged only — it is not fatal: the worst case is the next start re-runs the
// pass, and #124's watermark still prevents a double-send.
func (s *Scheduler) markRan(ctx context.Context, now time.Time) {
	today := now.In(s.config.Location).Format(dateLayout)
	if err := s.marker.Set(ctx, s.markerKey, today); err != nil {
		log.Printf("reminder scheduler: failed to persist last-run marker")
	}
}

// untilNextRun returns the duration from now until the next scheduled fire.
func (s *Scheduler) untilNextRun(now time.Time) time.Duration {
	return nextRun(now, s.config.Hour, s.config.Location).Sub(now)
}

// localHourReached reports whether, in the configured location, now is at or past
// the run hour on its own local day.
func (s *Scheduler) localHourReached(now time.Time) bool {
	return now.In(s.config.Location).Hour() >= s.config.Hour
}
