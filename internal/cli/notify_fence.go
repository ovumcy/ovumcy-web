package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// confirmNotifyFenceContinuity is the gate `ovumcy notify` passes before it
// reads a single webhook endpoint (WEB-408). A restored backup brings back
// endpoints the owners removed after its backup point, and only the server's
// ContinuityBroken boot pass clears them; a notify run from cron or a sidecar
// before that boot would POST to them. So notify delivers only from a database
// the fence proves the server has already booted over, and refuses with nothing
// sent otherwise. It writes nothing in either half: arming and reconciling the
// fence stay the server's job.
//
// fenceOptOut is CALENDAR_FEED_FENCE_OPTOUT, the server's own acceptance of
// running without a usable fence. It waives only the states that carry no
// evidence either way — no path in this shell, an anchor this process cannot
// read, a fence that holds no marker in either half — because those are the
// shapes an opted-out instance is in on every start. A pair that disagrees, or
// a half present without the other, is evidence of a restore the server's next
// boot will answer by clearing endpoints; the opt-out never covers delivering
// to them first.
func confirmNotifyFenceContinuity(ctx context.Context, fencePath string, fence *services.CalendarFeedRestoreFence, fenceOptOut bool, output io.Writer) error {
	var state calendarFeedFenceConfirmState
	var cause error
	switch {
	case fencePath == "":
		state = calendarFeedFenceStateNotSet
	case !security.CalendarFeedFencePathRooted(fencePath):
		return notifyFenceRefusal(fencePath, calendarFeedFenceStateRelative, nil)
	case fence == nil:
		return errors.New("calendar-feed restore fence: no fence was wired for this command (internal error)")
	default:
		err := fence.CheckContinuity(ctx)
		if err == nil {
			return nil
		}
		var ok bool
		state, cause, ok = calendarFeedFenceStateForRefusal(err)
		if !ok {
			return fmt.Errorf("%w. Nothing was sent", err) // codecov:ignore -- CheckContinuity returns only the classified errors
		}
	}

	if fenceOptOut && notifyFenceOptOutWaives(state) {
		_, _ = fmt.Fprintf(output, "calendar-feed restore fence: continuity not confirmed; delivering anyway under %s=true, which accepts that a restored backup can still hold removed webhook endpoints until the server's next start\n", security.CalendarFeedFenceOptOutEnv)
		return nil
	}
	return notifyFenceRefusal(fencePath, state, cause)
}

// notifyFenceOptOutWaives names the states CALENDAR_FEED_FENCE_OPTOUT waives
// for notify; see confirmNotifyFenceContinuity.
func notifyFenceOptOutWaives(state calendarFeedFenceConfirmState) bool {
	switch state {
	case calendarFeedFenceStateNotSet, calendarFeedFenceStateUnreachable, calendarFeedFenceStateNeverArmed:
		return true
	}
	return false
}

func notifyFenceRefusal(fencePath string, state calendarFeedFenceConfirmState, cause error) error {
	return composeCalendarFeedFenceRefusal(fencePath, state, cause,
		"A restored database can still hold webhook endpoints the owners removed, and only the server's boot pass clears them, so the server must boot over this database before notify delivers from it.",
		"Nothing was sent")
}
