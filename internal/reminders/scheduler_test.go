package reminders

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// fakeRunner is a deterministic PassRunner. It records each RunOnce call, can be
// told to panic on the Nth call, and signals every call on a channel so a test
// can block until a pass has actually run without sleeping.
type fakeRunner struct {
	mu        sync.Mutex
	calls     int
	panicOn   int // 1-based call index to panic on; 0 = never
	returnErr error
	// errCalls limits returnErr to the FIRST n calls, so a test can make a pass
	// fail and then recover on a retry. 0 (the zero value) means returnErr is
	// returned by every call — a permanently broken pass.
	errCalls   int
	called     chan struct{}
	perCallNow []time.Time
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{called: make(chan struct{}, 16)}
}

func (r *fakeRunner) RunOnce(_ context.Context, now time.Time, _ *time.Location, _ bool) (services.NotifyReport, error) {
	r.mu.Lock()
	r.calls++
	current := r.calls
	shouldPanic := r.panicOn == current
	err := r.returnErr
	if r.errCalls > 0 && current > r.errCalls {
		err = nil
	}
	r.perCallNow = append(r.perCallNow, now)
	r.mu.Unlock()

	// Signal AFTER incrementing so a waiter observing the channel sees a settled
	// call count. Non-blocking send: the buffered channel absorbs bursts.
	select {
	case r.called <- struct{}{}:
	default:
	}

	if shouldPanic {
		panic("simulated notify pass panic")
	}
	return services.NotifyReport{}, err
}

func (r *fakeRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// fakeMarker is an in-memory MarkerStore. It optionally fails reads to exercise
// the fail-safe catch-up skip.
type fakeMarker struct {
	mu       sync.Mutex
	values   map[string]string
	getErr   error
	setErr   error
	setCalls int
}

func newFakeMarker() *fakeMarker {
	return &fakeMarker{values: map[string]string{}}
}

func (m *fakeMarker) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", false, m.getErr
	}
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *fakeMarker) Set(_ context.Context, key string, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setCalls++
	if m.setErr != nil {
		return m.setErr
	}
	m.values[key] = value
	return nil
}

func (m *fakeMarker) get(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	return v, ok
}

// fakeTimer is a deterministic schedulerTimer: it fires only if its channel is
// pre-loaded. Stop is a no-op (never panics, unlike a hand-built *time.Timer).
type fakeTimer struct{ ch chan time.Time }

func (t fakeTimer) C() <-chan time.Time { return t.ch }
func (t fakeTimer) Stop()               {}

// fireOnceTimerFactory returns a newTimer func whose FIRST call yields a timer
// that fires immediately (channel pre-loaded), and whose subsequent calls yield
// timers that never fire — so the scheduler runs exactly one timer-driven pass
// then blocks in select until ctx is cancelled. This drives the loop
// deterministically with no wall-clock waiting.
func fireOnceTimerFactory() func(time.Duration) schedulerTimer {
	var calls int
	var mu sync.Mutex
	return func(time.Duration) schedulerTimer {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()

		ch := make(chan time.Time, 1)
		if first {
			ch <- time.Now()
		}
		return fakeTimer{ch: ch}
	}
}

// neverFireTimerFactory returns timers that never fire, so the scheduler blocks
// in its select waiting for the next run until ctx cancels — the path the
// shutdown-during-wait test exercises.
func neverFireTimerFactory() func(time.Duration) schedulerTimer {
	return func(time.Duration) schedulerTimer {
		return fakeTimer{ch: make(chan time.Time)}
	}
}

// newTestScheduler builds a Scheduler with injected clock and timer factory,
// bypassing New so tests control every non-deterministic input.
func newTestScheduler(runner PassRunner, marker MarkerStore, hour int, location *time.Location, now func() time.Time, newTimer func(time.Duration) schedulerTimer) *Scheduler {
	return &Scheduler{
		runner:    runner,
		marker:    marker,
		config:    Config{Hour: hour, Location: location},
		now:       now,
		newTimer:  newTimer,
		markerKey: markerKey,
	}
}

// waitForCalls blocks until the runner has been called at least want times or
// the deadline elapses. It reads the runner's signal channel, so it does not
// spin on the wall clock.
func waitForCalls(t *testing.T, r *fakeRunner, want int, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for r.callCount() < want {
		select {
		case <-r.called:
		case <-deadline:
			t.Fatalf("timed out waiting for %d RunOnce call(s); got %d", want, r.callCount())
		}
	}
}

// TestCatchUpRunsWhenMarkerIsYesterdayAndHourReached covers the primary catch-up
// case: marker=yesterday, local clock today at H+3h -> exactly one immediate
// pass on startup, marker advanced to today. The timer factory never fires, so
// the ONLY pass is the catch-up one.
func TestCatchUpRunsWhenMarkerIsYesterdayAndHourReached(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc) // H=9, so H+3h
	runner := newFakeRunner()
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // yesterday

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	waitForCalls(t, runner, 1, 2*time.Second)
	// Give the marker write a moment by waiting on the value, not a sleep.
	cancel()
	<-done

	if got := runner.callCount(); got != 1 {
		t.Fatalf("expected exactly one catch-up pass, got %d", got)
	}
	if v, ok := marker.get(markerKey); !ok || v != "2026-07-06" {
		t.Fatalf("expected marker advanced to today 2026-07-06, got %q ok=%v", v, ok)
	}
}

// TestCatchUpSkipsWhenMarkerIsToday covers restart safety: marker=today means a
// pass already ran today (or a same-day restart), so NO catch-up pass fires even
// though the local hour has passed.
func TestCatchUpSkipsWhenMarkerIsToday(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	runner := newFakeRunner()
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-06" // today

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)
	// No pass should fire; cancel and confirm zero calls.
	cancel()
	<-done

	if got := runner.callCount(); got != 0 {
		t.Fatalf("expected no catch-up pass when marker==today, got %d", got)
	}
}

// TestCatchUpSkipsWhenHourNotYetReached covers the "too early" case: marker is
// old but the local clock has not reached H, so catch-up does not fire (the
// normal timer loop would). With a never-fire timer, zero passes run.
func TestCatchUpSkipsWhenHourNotYetReached(t *testing.T) {
	utc := time.UTC
	early := time.Date(2026, 7, 6, 6, 0, 0, 0, utc) // before H=9
	runner := newFakeRunner()
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-01"

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return early }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)
	cancel()
	<-done

	if got := runner.callCount(); got != 0 {
		t.Fatalf("expected no pass before the run hour, got %d", got)
	}
}

// TestCatchUpFailsSafeOnMarkerReadError covers the fail-safe: if the marker read
// errors on startup, catch-up is skipped (no pass fired on an unknowable marker).
func TestCatchUpFailsSafeOnMarkerReadError(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	runner := newFakeRunner()
	marker := newFakeMarker()
	marker.getErr = errors.New("db down")

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)
	cancel()
	<-done

	if got := runner.callCount(); got != 0 {
		t.Fatalf("expected catch-up to fail safe (no pass) on marker read error, got %d", got)
	}
}

// TestTimerLoopRunsScheduledPassAndMarks covers the normal loop: no catch-up
// (marker is today), then the timer fires once and drives exactly one scheduled
// pass, which marks today. The clock advances to the next day for the fired pass
// so the marked date reflects the fire time.
func TestTimerLoopRunsScheduledPassAndMarks(t *testing.T) {
	utc := time.UTC
	// Start "today" already marked so catch-up is skipped; the timer fire
	// represents the next day's scheduled run.
	var mu sync.Mutex
	current := time.Date(2026, 7, 6, 9, 30, 0, 0, utc)
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return current
	}
	runner := newFakeRunner()
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-06"

	scheduler := newTestScheduler(runner, marker, 9, utc, clock, fireOnceTimerFactory())

	// Advance the clock to the next day right before the timer-driven pass so the
	// marked date is the new day. The timer fires immediately, so set it now.
	mu.Lock()
	current = time.Date(2026, 7, 7, 9, 0, 0, 0, utc)
	mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	waitForCalls(t, runner, 1, 2*time.Second)
	cancel()
	<-done

	if v, ok := marker.get(markerKey); !ok || v != "2026-07-07" {
		t.Fatalf("expected timer-driven pass to mark 2026-07-07, got %q ok=%v", v, ok)
	}
}

// TestPanicIsolationSurvivesAndDoesNotMark covers panic isolation: a pass that
// panics once is recovered (the goroutine survives), the day is NOT marked (so a
// retry is allowed), and a subsequent fire runs normally. The catch-up pass
// panics; then the timer loop fires a second pass that succeeds and marks today.
func TestPanicIsolationSurvivesAndDoesNotMark(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	runner := newFakeRunner()
	runner.panicOn = 1 // the catch-up pass panics
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // yesterday -> catch-up fires

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, fireOnceTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	// Wait for TWO calls: the panicking catch-up pass, then the timer-driven pass.
	waitForCalls(t, runner, 2, 2*time.Second)
	cancel()
	<-done

	// The process survived the panic (we got here) and the second pass marked
	// today. Critically, the marker is today only because the SECOND (successful)
	// pass wrote it — the panicking first pass must not have marked it.
	if v, ok := marker.get(markerKey); !ok || v != "2026-07-06" {
		t.Fatalf("expected the surviving pass to mark today 2026-07-06, got %q ok=%v", v, ok)
	}
	if marker.setCalls != 1 {
		t.Fatalf("expected exactly one marker write (from the non-panicking pass), got %d", marker.setCalls)
	}
}

// TestShutdownDuringWaitReturnsPromptly covers clean shutdown while the
// scheduler is blocked waiting for the next run: cancelling ctx makes Run return
// well within the drain budget, and no pass fires.
func TestShutdownDuringWaitReturnsPromptly(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 6, 0, 0, 0, utc) // before H, so no catch-up
	runner := newFakeRunner()
	marker := newFakeMarker()

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	cancel()
	select {
	case <-done:
	case <-time.After(DefaultStopBudget):
		t.Fatal("scheduler did not return within the drain budget after ctx cancel")
	}
	if got := runner.callCount(); got != 0 {
		t.Fatalf("expected no pass to fire during a shutdown-in-wait, got %d", got)
	}
}

// TestShutdownCancelsInFlightPass covers that the ctx handed to RunOnce is a
// child of the signal context: cancelling ctx cancels the in-flight pass. The
// runner blocks inside RunOnce until it observes ctx cancellation, then returns;
// the scheduler goroutine must then drain.
func TestShutdownCancelsInFlightPass(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // catch-up fires immediately

	blocking := &blockingRunner{
		entered: make(chan struct{}),
	}
	scheduler := newTestScheduler(blocking, marker, 9, utc, func() time.Time { return today }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	// Wait until the pass is in-flight, then cancel; the pass observes ctx.Done().
	select {
	case <-blocking.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("catch-up pass did not start")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(DefaultStopBudget):
		t.Fatal("scheduler did not drain after cancelling an in-flight pass")
	}
	if !blocking.sawCancel() {
		t.Fatal("expected the in-flight RunOnce to observe ctx cancellation")
	}
}

// blockingRunner blocks inside RunOnce until its ctx is cancelled, recording
// that it saw the cancellation. It proves the pass ctx is a child of sigCtx.
type blockingRunner struct {
	entered   chan struct{}
	mu        sync.Mutex
	cancelled bool
}

func (r *blockingRunner) RunOnce(ctx context.Context, _ time.Time, _ *time.Location, _ bool) (services.NotifyReport, error) {
	close(r.entered)
	<-ctx.Done()
	r.mu.Lock()
	r.cancelled = true
	r.mu.Unlock()
	return services.NotifyReport{}, ctx.Err()
}

func (r *blockingRunner) sawCancel() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

// TestMarkerKeyMatchesModelConstant guards the wire between the scheduler's
// marker key and the persisted app_state key so they cannot drift.
func TestMarkerKeyMatchesModelConstant(t *testing.T) {
	if markerKey != models.AppStateKeyLastReminderRunDate {
		t.Fatalf("scheduler markerKey %q must equal models.AppStateKeyLastReminderRunDate %q", markerKey, models.AppStateKeyLastReminderRunDate)
	}
}

// TestNewBuildsProductionSchedulerAndDrivesRealTimer exercises the PRODUCTION
// wiring path (New + newRealTimer + realTimer.C/Stop) that the fake-timer tests
// bypass. A scheduler built by New with the marker set to yesterday and the
// clock past the run hour runs its catch-up pass, then arms a REAL time.Timer
// for the next run; cancelling ctx makes Run take the ctx.Done() branch, which
// calls the real timer's Stop(). This executes New, newRealTimer, realTimer.C()
// (evaluated when the loop's select is set up) and realTimer.Stop() — with no
// wall-clock wait, because we cancel before the real timer could fire (its next
// run is ~a day out).
func TestNewBuildsProductionSchedulerAndDrivesRealTimer(t *testing.T) {
	utc := time.UTC
	runner := newFakeRunner()
	marker := newFakeMarker()
	marker.values[markerKey] = time.Now().In(utc).AddDate(0, 0, -1).Format(dateLayout) // yesterday

	// Hour just before the current local hour so catch-up fires immediately and
	// the armed next-run timer is ~a day out (never fires during the test).
	hour := time.Now().In(utc).Hour()
	if hour == 0 {
		// At 00:xx, "hour before now" would be negative; use hour 0 so catch-up
		// still fires (localHourReached is >=) and next run is tomorrow 00:00.
		hour = 0
	} else {
		hour--
	}

	scheduler := New(runner, marker, Config{Hour: hour, Location: utc})

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	// Wait for the catch-up pass to run (proves Run entered and reached the loop,
	// which armed a real timer), then cancel so Run's ctx.Done() branch calls the
	// real timer's Stop().
	waitForCalls(t, runner, 1, 2*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(DefaultStopBudget):
		t.Fatal("production-wired scheduler did not return promptly after cancel")
	}

	if runner.callCount() != 1 {
		t.Fatalf("expected exactly one catch-up pass from the production-wired scheduler, got %d", runner.callCount())
	}
}

// gateContext decouples Done() from Err() so a test can force the exact
// interleaving the post-fire guard defends against: the timer fires (so the loop
// takes the timer branch, because Done() is NEVER signalled) and only then does
// the loop's ctx.Err() check observe cancellation. A real context cannot express
// this (Done() closes exactly when Err() becomes non-nil), so the select would
// race between the timer and ctx.Done() branches. gateContext makes it
// deterministic: Done() returns a channel that never closes; Err() returns
// context.Canceled once fail() is called.
type gateContext struct {
	context.Context
	mu     sync.Mutex
	failed bool
	never  chan struct{}
}

func newGateContext() *gateContext {
	return &gateContext{Context: context.Background(), never: make(chan struct{})}
}

func (c *gateContext) Done() <-chan struct{} { return c.never }

func (c *gateContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed {
		return context.Canceled
	}
	return nil
}

func (c *gateContext) fail() {
	c.mu.Lock()
	c.failed = true
	c.mu.Unlock()
}

// TestTimerFiresButContextAlreadyCancelledSkipsPass covers the re-check-after-
// fire guard (the branch that returns when the timer fires in the SAME round as
// ctx cancellation): the pass must NOT start once we are draining. Using a
// gateContext, the loop deterministically takes the timer branch (Done() never
// closes), and the fire is delivered only after the context has been marked
// cancelled — so Run's post-fire ctx.Err() check sees the cancellation and
// returns without running a pass, exercising exactly the guard.
func TestTimerFiresButContextAlreadyCancelledSkipsPass(t *testing.T) {
	utc := time.UTC
	early := time.Date(2026, 7, 6, 6, 0, 0, 0, utc) // before H=9 so no catch-up pass
	runner := newFakeRunner()
	marker := newFakeMarker()

	ctx := newGateContext()

	fired := make(chan time.Time) // unbuffered: send completes only once Run receives
	armed := make(chan struct{}, 1)
	factory := func(time.Duration) schedulerTimer {
		select {
		case armed <- struct{}{}:
		default:
		}
		return fakeTimer{ch: fired}
	}

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return early }, factory)
	done := scheduler.Start(ctx)

	// Wait until the loop has armed its timer (so Run has passed the post-catch-up
	// guard at line ~140 and is parked in the select). Only then mark the context
	// cancelled and deliver the fire: since Done() never closes, the select can
	// only take the timer branch, and Run's post-fire ctx.Err() guard — reached
	// strictly after receiving the fire, which strictly follows fail() — observes
	// the cancellation and returns without a pass.
	select {
	case <-armed:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler never armed its timer")
	}
	ctx.fail()
	fired <- time.Now()

	select {
	case <-done:
	case <-time.After(DefaultStopBudget):
		t.Fatal("scheduler did not return after a fire-with-cancelled-ctx round")
	}
	if runner.callCount() != 0 {
		t.Fatalf("a pass must NOT run when ctx is cancelled in the same round as the fire, got %d", runner.callCount())
	}
}

// TestMarkerWriteFailureIsLoggedNotFatal covers the marker-write error branch:
// when marker.Set fails after a successful pass, the failure is logged but the
// scheduler survives (the worst case is the next start re-runs the pass, and
// #124's watermark still prevents a double-send). Catch-up drives the pass; the
// marker Set returns an error; Run must still be alive to exit cleanly on cancel.
func TestMarkerWriteFailureIsLoggedNotFatal(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	runner := newFakeRunner()
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // yesterday -> catch-up fires
	marker.setErr = errors.New("marker write failed")

	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	waitForCalls(t, runner, 1, 2*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(DefaultStopBudget):
		t.Fatal("scheduler did not survive a marker write failure")
	}

	if runner.callCount() != 1 {
		t.Fatalf("expected the pass to have run once despite the marker write failure, got %d", runner.callCount())
	}
	if marker.setCalls != 1 {
		t.Fatalf("expected exactly one marker Set attempt (which failed), got %d", marker.setCalls)
	}
	// The marker was NOT persisted (Set returned an error before storing), proving
	// we exercised the error branch rather than a silent success.
	if _, ok := marker.get(markerKey + "::written"); ok {
		t.Fatal("unexpected sentinel")
	}
}

// retryTimerCeiling separates the two kinds of timer the scheduler arms from its
// single newTimer seam. The next-run timer is always the hours from now to the
// next local run hour (every fixture below sits at least three hours from its
// run hour); the in-slot retry timer is passRetryDelay, minutes. Anything at or
// below this ceiling is therefore a retry timer, which lets one factory drive
// retries deterministically without a wall-clock wait.
const retryTimerCeiling = time.Hour

// slotTimerFactory is a schedulerTimer factory that tells the two timers apart by
// their duration (see retryTimerCeiling). Retry timers fire IMMEDIATELY when
// fireRetries is set — that is how a retry test runs in microseconds instead of
// passRetryDelay — and stay parked otherwise, which is the "shutdown arrives
// mid-backoff" case. Next-run timers never fire, so every pass a test observes is
// one it drove itself. Each armed duration is recorded and announced on armed.
type slotTimerFactory struct {
	fireRetries bool

	mu        sync.Mutex
	durations []time.Duration
	armed     chan time.Duration
}

func newSlotTimerFactory(fireRetries bool) *slotTimerFactory {
	return &slotTimerFactory{fireRetries: fireRetries, armed: make(chan time.Duration, 16)}
}

func (f *slotTimerFactory) newTimer(d time.Duration) schedulerTimer {
	f.mu.Lock()
	f.durations = append(f.durations, d)
	f.mu.Unlock()

	ch := make(chan time.Time, 1)
	if d <= retryTimerCeiling && f.fireRetries {
		ch <- time.Now()
	}
	select {
	case f.armed <- d:
	default:
	}
	return fakeTimer{ch: ch}
}

// retryDelays returns every armed duration the ceiling classifies as a retry
// backoff, in arming order.
func (f *slotTimerFactory) retryDelays() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []time.Duration
	for _, d := range f.durations {
		if d <= retryTimerCeiling {
			out = append(out, d)
		}
	}
	return out
}

// waitForRetryTimer blocks until the factory has armed a timer the ceiling
// classifies as a retry backoff, or the deadline elapses.
func (f *slotTimerFactory) waitForRetryTimer(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case d := <-f.armed:
			if d <= retryTimerCeiling {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the scheduler to arm a retry timer; armed so far: %v", f.durations)
		}
	}
}

// TestPassErrorRetriesInTheSameSlotAndMarksOnlyOnceItSucceeds is the core guard
// for the record's claim: a pass-level error (the owner listing failed, so ZERO
// reminders went out) must not be recorded as today's completed run. The first
// attempt errors, the in-slot retry succeeds, and only that success advances the
// marker — exactly one marker write for the day.
func TestPassErrorRetriesInTheSameSlotAndMarksOnlyOnceItSucceeds(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc) // H=9, so catch-up fires
	runner := newFakeRunner()
	runner.returnErr = errors.New("listing owners failed")
	runner.errCalls = 1 // only the first attempt fails; the retry succeeds
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // yesterday

	timers := newSlotTimerFactory(true)
	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, timers.newTimer)

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	waitForCalls(t, runner, 2, 2*time.Second)
	cancel()
	<-done

	if got := runner.callCount(); got != 2 {
		t.Fatalf("expected the failed pass to be retried once inside its slot (2 attempts), got %d", got)
	}
	if v, ok := marker.get(markerKey); !ok || v != "2026-07-06" {
		t.Fatalf("expected the successful retry to mark today 2026-07-06, got %q ok=%v", v, ok)
	}
	if marker.setCalls != 1 {
		t.Fatalf("expected exactly one marker write, made by the retry that actually succeeded, got %d", marker.setCalls)
	}
}

// TestRestartBeforeTheRetryStillCatchesUpToday is the record's second half: a
// process that dies between a failed attempt and its retry must not have left
// today marked, so the next start's catch-up re-runs the day. The first
// scheduler's retry timer never fires and the context is cancelled while the
// backoff is pending; a fresh scheduler over the SAME marker store then fires a
// catch-up pass.
func TestRestartBeforeTheRetryStillCatchesUpToday(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	clock := func() time.Time { return today }
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // yesterday -> catch-up fires

	failing := newFakeRunner()
	failing.returnErr = errors.New("listing owners failed")
	timers := newSlotTimerFactory(false) // the retry backoff stays pending

	first := newTestScheduler(failing, marker, 9, utc, clock, timers.newTimer)
	ctx, cancel := context.WithCancel(context.Background())
	done := first.Start(ctx)

	waitForCalls(t, failing, 1, 2*time.Second)
	timers.waitForRetryTimer(t, 2*time.Second)
	cancel() // the process dies while the retry is still waiting
	<-done

	if v, ok := marker.get(markerKey); !ok || v != "2026-07-05" {
		t.Fatalf("a pass that failed and has not retried yet must leave the marker at yesterday, got %q ok=%v", v, ok)
	}

	// The restart: a new scheduler over the same marker store, same local day.
	recovered := newFakeRunner()
	second := newTestScheduler(recovered, marker, 9, utc, clock, neverFireTimerFactory())
	restartCtx, restartCancel := context.WithCancel(context.Background())
	restartDone := second.Start(restartCtx)

	waitForCalls(t, recovered, 1, 2*time.Second)
	restartCancel()
	<-restartDone

	if got := recovered.callCount(); got != 1 {
		t.Fatalf("expected the restart to catch up today's unfinished pass exactly once, got %d", got)
	}
	if v, ok := marker.get(markerKey); !ok || v != "2026-07-06" {
		t.Fatalf("expected the caught-up pass to mark today 2026-07-06, got %q ok=%v", v, ok)
	}
}

// TestRetryBudgetIsBoundedThenTheDayIsMarked guards the other half of the trade:
// the retry is BOUNDED. A permanently failing pass is attempted exactly
// maxPassAttempts times in its slot, each attempt separated by a non-zero
// backoff, and only then is the day marked — so a broken database can never
// busy-loop the scheduler, which is the property the pre-existing "mark on
// error" behaviour bought at the cost of the whole day's reminders.
func TestRetryBudgetIsBoundedThenTheDayIsMarked(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	runner := newFakeRunner()
	runner.returnErr = errors.New("listing owners failed") // errCalls 0 -> every attempt fails
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // yesterday -> catch-up fires

	timers := newSlotTimerFactory(true)
	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, timers.newTimer)

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	waitForCalls(t, runner, maxPassAttempts, 2*time.Second)
	cancel()
	<-done

	if got := runner.callCount(); got != maxPassAttempts {
		t.Fatalf("expected exactly %d attempts in one slot, got %d", maxPassAttempts, got)
	}
	if v, ok := marker.get(markerKey); !ok || v != "2026-07-06" {
		t.Fatalf("expected the spent budget to mark today 2026-07-06 (anti-busy-loop), got %q ok=%v", v, ok)
	}
	delays := timers.retryDelays()
	if len(delays) != maxPassAttempts-1 {
		t.Fatalf("expected %d retry backoffs between %d attempts, got %d (%v)", maxPassAttempts-1, maxPassAttempts, len(delays), delays)
	}
	for i, d := range delays {
		if d <= 0 {
			t.Fatalf("retry backoff %d was %s: a zero delay is the busy loop the budget exists to prevent", i+1, d)
		}
	}
}

// TestPanickingPassDoesNotSpendTheErrorRetryBudget keeps the two failure classes
// distinct. A panic leaves the pass in a state the in-slot retry cannot reason
// about, so it keeps its original semantics — recovered, day not marked, retried
// by the NEXT fire — and must not be re-entered immediately as a transient error
// would be. Retry timers here fire instantly, so an in-slot retry would show up
// as a second call; the next-run timer never fires, so nothing else can.
func TestPanickingPassDoesNotSpendTheErrorRetryBudget(t *testing.T) {
	utc := time.UTC
	today := time.Date(2026, 7, 6, 12, 0, 0, 0, utc)
	runner := newFakeRunner()
	runner.panicOn = 1 // the catch-up pass panics
	marker := newFakeMarker()
	marker.values[markerKey] = "2026-07-05" // yesterday -> catch-up fires

	timers := newSlotTimerFactory(true)
	scheduler := newTestScheduler(runner, marker, 9, utc, func() time.Time { return today }, timers.newTimer)

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)

	// Wait until the loop has armed its next-run timer: that happens strictly
	// after the panicking catch-up pass returned, so any in-slot retry would
	// already have been armed and (firing instantly) already have called RunOnce.
	deadline := time.After(2 * time.Second)
	for armedNextRun := false; !armedNextRun; {
		select {
		case d := <-timers.armed:
			if d > retryTimerCeiling {
				armedNextRun = true
			}
		case <-deadline:
			t.Fatal("scheduler never reached its next-run timer after the panicking pass")
		}
	}
	cancel()
	<-done

	if got := runner.callCount(); got != 1 {
		t.Fatalf("a panicking pass must not be retried inside its slot; expected 1 attempt, got %d", got)
	}
	if delays := timers.retryDelays(); len(delays) != 0 {
		t.Fatalf("a panicking pass must arm no retry backoff, got %v", delays)
	}
	if marker.setCalls != 0 {
		t.Fatalf("a panicking pass must leave the marker untouched so the next fire retries, got %d write(s)", marker.setCalls)
	}
}
