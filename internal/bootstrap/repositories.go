package bootstrap

import (
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// BuildRepositories is the ONE way a binary builds its repository set. It
// attaches the calendar-feed restore fence, so every write that arms, rotates
// or removes a feed records that change outside the database — which is what
// lets the boot pass tell a restored backup from the database it replaced.
//
// It returns the fence too, because the boot pass needs the same instance: one
// fence, one anchor path, one token, whether it is being advanced by a
// revocation or compared at startup.
//
// fencePath is CALENDAR_FEED_FENCE_PATH, and may be empty. The fence then
// records nothing outside the database and the boot pass disarms every armed
// feed on each start, which is the fail-closed answer to "this deployment has
// nowhere to keep a fence".
//
// A binary that reaches db.NewRepositories directly instead gets a set whose
// feed writes are invisible to the fence — a revocation recorded only inside
// the database is exactly the defect the fence exists for — so a guard refuses
// that call outside this package.
func BuildRepositories(database *gorm.DB, fencePath string) (*db.Repositories, *services.CalendarFeedRestoreFence) {
	repositories := db.NewRepositories(database)
	fence := services.NewCalendarFeedRestoreFence(
		repositories.AppState,
		repositories.Users,
		security.NewCalendarFeedFenceFile(fencePath),
	)
	return repositories.WithCalendarFeedFence(fence), fence
}
