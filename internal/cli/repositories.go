package cli

import (
	"os"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"gorm.io/gorm"
)

// calendarFeedFencePathEnv is the same variable the server reads. The operator
// CLI runs against the SAME database and volumes as the server — the runbook
// invokes it through `docker compose exec` on the running container — so it
// sees the value already in that environment without anything extra to set.
const calendarFeedFencePathEnv = "CALENDAR_FEED_FENCE_PATH"

// buildRepositories is every CLI subcommand's repository set.
//
// It exists so no subcommand reaches db.NewRepositories directly: two of them
// remove calendar-feed access (`reset` force-clears the token, `users delete`
// takes the row and its feed with it), and a removal that is recorded only
// inside the database is undone by restoring a backup taken before it. Routing
// all four through one constructor is what keeps that from depending on which
// subcommand an operator happened to use.
func buildRepositories(database *gorm.DB) *db.Repositories {
	repositories, _ := bootstrap.BuildRepositories(database, strings.TrimSpace(os.Getenv(calendarFeedFencePathEnv)))
	return repositories
}
