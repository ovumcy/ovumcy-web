package services

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"gorm.io/gorm"
)

// newOperatorRevocationFixture wires a real, migrated SQLite database and the
// SERVER's own repository set and fence at fencePath. database is returned
// too so a test can build a SEPARATE repository set over the same connection
// — an operator CLI process and the server share a database, never a Go
// value, and WithCalendarFeedFence mutates the *UserRepository it is given.
func newOperatorRevocationFixture(t *testing.T) (database *gorm.DB, serverRepositories *db.Repositories, serverFence *CalendarFeedRestoreFence, fencePath string) {
	t.Helper()

	fencePath = filepath.Join(t.TempDir(), "calendar-feed.fence")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "operator-revocation.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	serverRepositories = db.NewRepositories(database)
	serverFence = NewCalendarFeedRestoreFence(serverRepositories.AppState, serverRepositories.Users, security.NewCalendarFeedFenceFile(fencePath))
	serverRepositories.WithCalendarFeedFence(serverFence)
	return database, serverRepositories, serverFence, fencePath
}

// createArmedOwner creates one owner account and arms its calendar feed
// through the real repository write path, so arming advances whichever fence
// is attached to repositories.Users exactly as a live request would.
func createArmedOwner(t *testing.T, ctx context.Context, repositories *db.Repositories, email string) models.User {
	t.Helper()

	user := models.User{
		Email:               email,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		AutoPeriodFill:      true,
	}
	if err := repositories.Users.Create(ctx, &user); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := repositories.Users.SaveCalendarFeedToken(ctx, user.ID, models.CalendarFeedTokenColumns{
		Selector: "selector-" + email, VerifierHash: "hash", VerifierMAC: "mac", KeyEpoch: "epoch",
	}); err != nil {
		t.Fatalf("arm calendar feed: %v", err)
	}
	return user
}

func loadCalendarFeedSelector(t *testing.T, repositories *db.Repositories, userID uint) string {
	t.Helper()

	user, found, err := repositories.Users.FindByIDOptional(context.Background(), userID)
	if err != nil {
		t.Fatalf("FindByIDOptional: %v", err)
	}
	if !found {
		return ""
	}
	return user.CalendarFeedSelector
}

// TestOperatorRevocationWithoutAConfirmedFenceIsRefusedRatherThanLost is F-03,
// reproduced with real objects — a real migrated SQLite database, the real
// repository write path, the real file-backed anchor — and no Docker.
//
// The scenario is exactly buildRepositories's pre-fix wiring: an operator CLI
// process whose fence shares the server's app_state but has no anchor of its
// own (CALENDAR_FEED_FENCE_PATH unset in an operator's shell is the common
// case, not the exception). Before AdvanceConfirmed existed, the only
// available method was Advance, which degrades an unconfigured anchor into a
// silent no-op — the caller proceeds to delete the row regardless, and a
// restore of the backup taken moments before that deletion brings the feed
// back, because neither fence half ever moved. AdvanceConfirmed must instead
// refuse before anything is written, leaving the row and both fence halves
// exactly where the server's own boot left them, so the CLI never reaches the
// delete at all.
func TestOperatorRevocationWithoutAConfirmedFenceIsRefusedRatherThanLost(t *testing.T) {
	ctx := context.Background()
	_, serverRepositories, serverFence, _ := newOperatorRevocationFixture(t)

	if _, err := serverFence.Enforce(ctx); err != nil {
		t.Fatalf("first-boot Enforce: %v", err)
	}
	owner := createArmedOwner(t, ctx, serverRepositories, "owner@example.com")

	backupMarker, backupFound, err := serverRepositories.AppState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil || !backupFound {
		t.Fatalf("AppState.Get after arming: found=%t err=%v", backupFound, err)
	}

	// The operator CLI: same app_state, no anchor of its own.
	cliFence := NewCalendarFeedRestoreFence(serverRepositories.AppState, serverRepositories.Users, security.NewCalendarFeedFenceFile(""))

	err = cliFence.AdvanceConfirmed(ctx)
	if !errors.Is(err, ErrCalendarFeedFenceUnreachable) {
		t.Fatalf("AdvanceConfirmed over an unconfigured anchor must return ErrCalendarFeedFenceUnreachable, got %v", err)
	}

	// Nothing may have moved: the row is still there and still armed, and the
	// database half is exactly where the server's own boot left it.
	if selector := loadCalendarFeedSelector(t, serverRepositories, owner.ID); selector == "" {
		t.Fatal("a refused revocation must leave the row and its armed feed untouched")
	}
	afterMarker, afterFound, err := serverRepositories.AppState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil || !afterFound || afterMarker != backupMarker {
		t.Fatalf("a refused revocation must not move the database half; before=%q after=%q (found=%t)", backupMarker, afterMarker, afterFound)
	}
}

// TestOperatorRevocationAfterAnUnbootedRestoreDoesNotMaskTheDiscontinuity is
// F-03's second shape: a restore has already landed in the database — the
// stored marker is an older generation than the fence file — but the SERVER
// has not booted since, so Enforce has not yet had the chance to read that
// disagreement and disarm. An operator who runs the CLI in THIS window, with
// a fully reachable anchor pointed at the same file the server uses, must not
// be able to erase the evidence: before AdvanceConfirmed existed, Advance
// would mint a token that resynchronizes the two halves, and the very next
// Enforce would then read like an ordinary restart that never saw a restore
// at all. AdvanceConfirmed refuses instead, so the discontinuity survives to
// the next boot.
func TestOperatorRevocationAfterAnUnbootedRestoreDoesNotMaskTheDiscontinuity(t *testing.T) {
	ctx := context.Background()
	_, serverRepositories, serverFence, fencePath := newOperatorRevocationFixture(t)

	if _, err := serverFence.Enforce(ctx); err != nil {
		t.Fatalf("first-boot Enforce: %v", err)
	}
	owner := createArmedOwner(t, ctx, serverRepositories, "owner@example.com")

	// Simulate a restore that landed in the database but has not been booted
	// against yet: only the STORED half rolls back, exactly as a database
	// backup replaces app_state while the fence file — outside any backup by
	// contract — keeps the value the instance minted after the backup was
	// taken.
	if err := serverRepositories.AppState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, "an-older-generation"); err != nil {
		t.Fatalf("simulate restored app_state: %v", err)
	}

	// The operator CLI, this time with a WORKING anchor at the server's own
	// path — the case the whole mechanism is supposed to make safe.
	cliFence := NewCalendarFeedRestoreFence(serverRepositories.AppState, serverRepositories.Users, security.NewCalendarFeedFenceFile(fencePath))

	err := cliFence.AdvanceConfirmed(ctx)
	var continuity *CalendarFeedFenceContinuityError
	if !errors.As(err, &continuity) {
		t.Fatalf("AdvanceConfirmed over disagreeing halves must return *CalendarFeedFenceContinuityError, got %v", err)
	}

	// The very next boot must still see the discontinuity and disarm.
	outcome, err := serverFence.Enforce(ctx)
	if err != nil {
		t.Fatalf("Enforce after the refused revocation: %v", err)
	}
	if !outcome.ContinuityBroken || outcome.DisarmedFeeds == 0 {
		t.Fatalf("a restore that landed before any confirmed revocation must still be caught on the next boot, got %+v", outcome)
	}
	if selector := loadCalendarFeedSelector(t, serverRepositories, owner.ID); selector != "" {
		t.Fatalf("the feed the discontinuity check disarms must actually be cleared, got selector %q", selector)
	}
}
