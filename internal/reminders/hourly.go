package reminders

import (
	"context"
	"log"
	"runtime/debug"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// CycleDayPassRunner runs the cycle-day-only notify pass (WEB-383). Each rule
// is due from its own owner-local hour, so the pass runs every hour rather than
// once a day.
type CycleDayPassRunner interface {
	RunCycleDayOnce(ctx context.Context, now time.Time, location *time.Location, dryRun bool) (services.NotifyReport, error)
}

// RunHourly runs one cycle-day pass at once — a restart later in a rule's day
// still delivers it, since the decision accepts any hour at or after the rule's
// own — and then one at the top of every hour until ctx is cancelled. It keeps
// no marker: every send is a compare-and-set claim, so an extra pass sends
// nothing twice. A failed or panicking pass is logged and the next hour retries.
func (s *Scheduler) RunHourly(ctx context.Context, runner CycleDayPassRunner) {
	s.runCycleDayPass(ctx, runner)
	for {
		timer := s.newTimer(untilNextHour(s.now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}
		if ctx.Err() != nil {
			return
		}
		s.runCycleDayPass(ctx, runner)
	}
}

func (s *Scheduler) runCycleDayPass(ctx context.Context, runner CycleDayPassRunner) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("reminder scheduler: cycle-day pass panicked, recovered and continuing; stack:\n%s", debug.Stack())
		}
	}()
	if _, err := runner.RunCycleDayOnce(ctx, s.now(), s.config.Location, false); err != nil {
		log.Printf("reminder scheduler: cycle-day pass returned an error (see prior notify logs), retrying next hour")
	}
}

// untilNextHour is the wait to the next whole hour of absolute time. An owner
// in a zone offset by a fraction of an hour is reached within that fraction of
// their chosen hour.
func untilNextHour(now time.Time) time.Duration {
	return now.Truncate(time.Hour).Add(time.Hour).Sub(now)
}
