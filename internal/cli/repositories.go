package cli

import (
	"fmt"
	"os"
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
// must resolve the SAME fence. The runbook reaches this CLI through
// `docker compose exec` on the running container, where that variable is
// already set; a shell that is not that one is the case the warning covers.
func buildRepositories(database *gorm.DB) *db.Repositories {
	fencePath := calendarFeedFencePath()
	if warning := calendarFeedFenceWarning(fencePath); warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}
	repositories, _ := bootstrap.BuildRepositories(database, fencePath)
	return repositories
}

func calendarFeedFencePath() string {
	return strings.TrimSpace(os.Getenv(security.CalendarFeedFencePathEnv))
}

// calendarFeedFenceWarning is what an operator sees when this process cannot
// reach the fence the server keeps. It does not refuse the command: `users
// delete` is a data subject's erasure and must never become conditional on a
// volume being mounted, and the outcome is already safe — the database half of
// the fence moves on alone, so the server's next boot disarms rather than
// missing the revocation. What it must not be is silent, because the operator
// would otherwise read that disarm as a backup restore nobody performed.
func calendarFeedFenceWarning(fencePath string) string {
	if fencePath != "" {
		return ""
	}
	return "warning: " + security.CalendarFeedFencePathEnv + " is not set in this shell, so a command that changes calendar-feed state records it only inside the database. " +
		"The server disarms every armed calendar feed on its next start as a result. Run the operator CLI where the server's fence is visible " +
		"(the runbook uses `docker compose exec`), or set the variable to the same path the server uses."
}
