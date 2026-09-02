package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"gorm.io/gorm"
)

// calendarFeedFencePath resolves the fence location once, so the value a
// subcommand warns about is provably the value its repositories were built
// with.
func calendarFeedFencePath() string {
	return strings.TrimSpace(os.Getenv(security.CalendarFeedFencePathEnv))
}

// buildRepositories is every CLI subcommand's repository set.
//
// It exists so no subcommand reaches db.NewRepositories directly: two of them
// remove calendar-feed access (`reset` force-clears the token, `users delete`
// takes the row and its feed with it), and a removal recorded only inside the
// database is undone by restoring a backup taken before it. Routing all four
// through one constructor keeps that from depending on which subcommand an
// operator happened to run.
//
// Whether this process can actually reach the fence is the subject of
// calendarFeedFenceWarning, which the two feed-affecting subcommands raise and
// the others deliberately do not.
func buildRepositories(database *gorm.DB) *db.Repositories {
	repositories, _ := bootstrap.BuildRepositories(database, calendarFeedFencePath())
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
// The probe is deliberately narrow. It answers "unset", and it answers an
// ABSOLUTE path whose directory is not there — which is what copying the
// container's /app/fence value into a host shell gives. It says nothing about a
// relative path: that resolves against the working directory, which is the
// server's and not this process's, so a missing directory here would be no
// evidence at all and the warning would cry wolf. Nor does it WRITE a probe:
// the fence file is the server's, and a subcommand has no business advancing it
// just to find out whether it could.
func calendarFeedFenceWarning(fencePath string) string {
	fencePath = strings.TrimSpace(fencePath)

	const consequence = ", so a command that changes calendar-feed state records it only inside the database. " +
		"The server disarms every armed calendar feed on its next start as a result. " +
		"Run the operator CLI where the server's fence is visible (the runbook uses `docker compose exec`), " +
		"or set the variable to the same path the server uses."

	switch {
	case fencePath == "":
		return "warning: " + security.CalendarFeedFencePathEnv + " is not set in this shell" + consequence
	case rootedPath(fencePath) && !directoryExists(filepath.Dir(filepath.Clean(fencePath))):
		return "warning: " + security.CalendarFeedFencePathEnv + " points at " + fencePath + ", whose directory does not exist in this process" + consequence
	}
	return ""
}

// rootedPath reports whether the path names a location independent of the
// working directory. filepath.IsAbs is not enough on its own: this CLI is
// developed on Windows, where it demands a drive letter and therefore calls
// `/app/fence/calendar-feed.fence` relative — which is precisely the value an
// operator copies out of a compose file, and precisely the case the probe
// exists to catch. A leading separator settles it on either platform.
func rootedPath(path string) bool {
	return filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`)
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// warnAboutAnUnreachableCalendarFeedFence prints that warning, if there is one,
// for a subcommand about to remove calendar-feed access. Subcommands that
// cannot change feed state stay silent on purpose — a line that appears on
// every `notify` run from cron is a line nobody reads beside the `reset` that
// will actually cause a disarm.
func warnAboutAnUnreachableCalendarFeedFence(errOutput io.Writer) {
	if warning := calendarFeedFenceWarning(calendarFeedFencePath()); warning != "" {
		_, _ = io.WriteString(errOutput, warning+"\n")
	}
}
