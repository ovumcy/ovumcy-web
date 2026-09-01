package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"gorm.io/gorm"
)

// buildRepositories is every CLI subcommand's repository set.
//
// It exists so no subcommand reaches db.NewRepositories directly: two of them
// remove calendar-feed access (`reset` force-clears the token, `users delete`
// takes the row and its feed with it), and a removal recorded only inside the
// database is undone by restoring a backup taken before it. Routing all four
// through one constructor keeps that from depending on which subcommand an
// operator happened to run.
//
// The fence path comes from the same variable the server reads, because both
// must resolve the SAME fence. Whether this process can actually reach it is
// the subject of calendarFeedFenceWarning, which the two feed-affecting
// subcommands raise and the others deliberately do not.
func buildRepositories(database *gorm.DB) *db.Repositories {
	repositories, _ := bootstrap.BuildRepositories(database, strings.TrimSpace(os.Getenv(security.CalendarFeedFencePathEnv)))
	return repositories
}

// calendarFeedFenceWarning is what an operator sees before a subcommand that
// removes calendar-feed access runs in a process that cannot reach the fence.
//
// It does not refuse: `users delete` is a data subject's erasure and must never
// become conditional on a volume being mounted, and the outcome is already
// safe — the database half of the fence moves on alone, so the server's next
// boot disarms rather than missing the revocation. What it must not be is
// silent, because the operator would otherwise read that disarm as a backup
// restore nobody performed.
//
// It answers two shapes, because both produce that disarm and only one of them
// looks wrong: the variable unset, and the variable set to a path whose
// directory does not exist here — which is what copying the container's
// /app/fence value into a host shell gives. It deliberately does not try to
// WRITE a probe: the fence file is the server's, and a subcommand has no
// business advancing it just to find out whether it could.
func calendarFeedFenceWarning(fencePath string) string {
	fencePath = strings.TrimSpace(fencePath)
	if fencePath == "" {
		return "warning: " + security.CalendarFeedFencePathEnv + " is not set in this shell, so a command that changes calendar-feed state records it only inside the database. The server disarms every armed calendar feed on its next start as a result. Run the operator CLI where the server's fence is visible (the runbook uses `docker compose exec`), or set the variable to the same path the server uses."
	}
	if info, err := os.Stat(filepath.Dir(filepath.Clean(fencePath))); err != nil || !info.IsDir() {
		return "warning: " + security.CalendarFeedFencePathEnv + " points at " + fencePath + ", whose directory does not exist in this process, so a command that changes calendar-feed state records it only inside the database. The server disarms every armed calendar feed on its next start as a result. A container path copied into a host shell is the usual cause; run the operator CLI through `docker compose exec` instead."
	}
	return ""
}

// warnAboutAnUnreachableCalendarFeedFence prints that warning, if there is one,
// for a subcommand that is about to remove calendar-feed access. Subcommands
// that cannot change feed state stay silent on purpose — a line that appears on
// every `notify` run from cron is a line nobody reads beside the `reset` that
// will actually cause a disarm.
func warnAboutAnUnreachableCalendarFeedFence(errOutput *os.File) {
	if warning := calendarFeedFenceWarning(os.Getenv(security.CalendarFeedFencePathEnv)); warning != "" {
		_, _ = errOutput.WriteString(warning + "\n")
	}
}
