package reminders

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// hourlyRunner serves both passes; it records every cycle-day pass and can be
// told to panic on its first one.
type hourlyRunner struct {
	mu          sync.Mutex
	cycleDay    []time.Time
	dryRuns     []bool
	daily       int
	panicFirst  bool
	cycleDaySig chan struct{}
}

func newHourlyRunner() *hourlyRunner {
	return &hourlyRunner{cycleDaySig: make(chan struct{}, 16)}
}

func (r *hourlyRunner) RunOnce(context.Context, time.Time, *time.Location, bool) (services.NotifyReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.daily++
	return services.NotifyReport{}, nil
}

func (r *hourlyRunner) RunCycleDayOnce(_ context.Context, now time.Time, _ *time.Location, dryRun bool) (services.NotifyReport, error) {
	r.mu.Lock()
	r.cycleDay = append(r.cycleDay, now)
	r.dryRuns = append(r.dryRuns, dryRun)
	shouldPanic := r.panicFirst && len(r.cycleDay) == 1
	r.mu.Unlock()
	r.cycleDaySig <- struct{}{}
	if shouldPanic {
		panic("cycle-day pass blew up")
	}
	return services.NotifyReport{}, nil
}

func (r *hourlyRunner) cycleDayCalls() ([]time.Time, []bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.cycleDay...), append([]bool(nil), r.dryRuns...)
}

func waitForCycleDayCalls(t *testing.T, r *hourlyRunner, want int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		calls, _ := r.cycleDayCalls()
		if len(calls) >= want {
			return
		}
		select {
		case <-r.cycleDaySig:
		case <-deadline:
			t.Fatalf("timed out waiting for %d cycle-day pass(es); got %d", want, len(calls))
		}
	}
}

func TestStartRunsTheCycleDayPassAtOnceAndDrainsOnCancel(t *testing.T) {
	fixed := time.Date(2026, time.March, 7, 10, 20, 0, 0, time.UTC)
	runner := newHourlyRunner()
	scheduler := newTestScheduler(runner, newFakeMarker(), 23, time.UTC, func() time.Time { return fixed }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := scheduler.Start(ctx)

	waitForCycleDayCalls(t, runner, 1)
	calls, dryRuns := runner.cycleDayCalls()
	if !calls[0].Equal(fixed) || dryRuns[0] {
		t.Fatalf("the immediate pass must run at the injected now and not as a dry run, got now=%v dryRun=%v", calls[0], dryRuns[0])
	}

	select {
	case <-done:
		t.Fatal("done closed while ctx was still live")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("done did not close after ctx was cancelled")
	}
	if calls, _ := runner.cycleDayCalls(); len(calls) != 1 {
		t.Fatalf("a never-firing timer allows exactly the immediate pass, got %d", len(calls))
	}
}

func TestRunHourlyWaitsForTheNextWholeHourAndSurvivesAPanic(t *testing.T) {
	fixed := time.Date(2026, time.March, 7, 10, 20, 0, 0, time.UTC)
	runner := newHourlyRunner()
	runner.panicFirst = true

	var mu sync.Mutex
	var waits []time.Duration
	fireOnce := fireOnceTimerFactory()
	scheduler := newTestScheduler(runner, newFakeMarker(), 23, time.UTC, func() time.Time { return fixed }, func(d time.Duration) schedulerTimer {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		return fireOnce(d)
	})

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		scheduler.RunHourly(ctx, runner)
		close(finished)
	}()

	// The first pass panics; the loop must recover and run the timer-driven one.
	waitForCycleDayCalls(t, runner, 2)
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("RunHourly did not return after ctx was cancelled")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(waits) == 0 || waits[0] != 40*time.Minute {
		t.Fatalf("the first wait must be the gap to 11:00 (40m), got %v", waits)
	}
}

func TestStartWithoutACycleDayRunnerStartsNoHourlyLoop(t *testing.T) {
	runner := newFakeRunner()
	fixed := time.Date(2026, time.March, 7, 10, 20, 0, 0, time.UTC)
	scheduler := newTestScheduler(runner, newFakeMarker(), 23, time.UTC, func() time.Time { return fixed }, neverFireTimerFactory())

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Start(ctx)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("done did not close for a daily-only runner")
	}
}

func TestUntilNextHourIsTheGapToTheNextWholeHour(t *testing.T) {
	kolkata := time.FixedZone("IST", 5*3600+1800)
	cases := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{"on the hour", time.Date(2026, time.March, 7, 10, 0, 0, 0, time.UTC), time.Hour},
		{"one second before", time.Date(2026, time.March, 7, 10, 59, 59, 0, time.UTC), time.Second},
		{"mid hour with nanos", time.Date(2026, time.March, 7, 10, 15, 30, 500_000_000, time.UTC), 44*time.Minute + 29*time.Second + 500*time.Millisecond},
		{"before midnight", time.Date(2026, time.March, 7, 23, 45, 0, 0, time.UTC), 15 * time.Minute},
		// 10:15 at +05:30 is 04:45 UTC: the wait is to the absolute hour.
		{"half-hour zone", time.Date(2026, time.March, 7, 10, 15, 0, 0, kolkata), 15 * time.Minute},
	}
	for _, testCase := range cases {
		if got := untilNextHour(testCase.now); got != testCase.want {
			t.Fatalf("%s: untilNextHour(%v) = %v, want %v", testCase.name, testCase.now, got, testCase.want)
		}
	}
}
