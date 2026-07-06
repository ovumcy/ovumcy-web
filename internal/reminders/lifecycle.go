package reminders

import (
	"context"
	"log"
	"time"
)

// DefaultStopBudget bounds how long the caller waits for an in-flight pass to
// unwind after the signal context is cancelled. It sits comfortably under the
// server's 10s graceful-shutdown budget so the scheduler drain never dominates
// shutdown; if a pass does not finish in time the caller logs and proceeds (the
// leaked run is bounded by #124's outbound timeout plus the cancelled ctx that
// already flowed into RunOnce).
const DefaultStopBudget = 5 * time.Second

// Start launches the scheduler goroutine bound to ctx and returns a channel that
// closes when Run has fully returned (ctx cancelled and any in-flight pass
// unwound). Keeping the goroutine launch + the completion signal here lets
// cmd/ovumcy stay a ~3-line glue call and keeps the drain logic unit-testable.
//
// ctx MUST be the server's signal context so the scheduler stops on the same
// SIGINT/SIGTERM that stops the HTTP server. The launched goroutine never
// touches the fiber app or its shutdown, so it cannot delay app.Listen or
// interfere with the boot-window shutdown retry.
func (s *Scheduler) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	return done
}

// Drain waits up to budget for done to close (the scheduler goroutine returning),
// then returns. It is called AFTER the HTTP server has stopped and BEFORE the DB
// is closed, so the database stays open until the last reminder read/write
// completes. On timeout it logs and returns anyway; the caller then closes the
// DB regardless, keeping "DB closes on both exit paths" true.
func Drain(done <-chan struct{}, budget time.Duration) {
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Printf("reminder scheduler: drain timed out after %s, proceeding with shutdown", budget)
	}
}
