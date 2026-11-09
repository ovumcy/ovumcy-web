package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// switchableFenceAnchor is a real fence file behind a mount that can be taken
// away: while unreachable, every read and write fails, and the file keeps the
// token it held, exactly as an unmounted volume does for the bytes on it.
type switchableFenceAnchor struct {
	file        *security.CalendarFeedFenceFile
	unreachable bool
}

func (anchor *switchableFenceAnchor) Read() (string, bool, error) {
	if anchor.unreachable {
		return "", false, errors.New("fence volume is not mounted")
	}
	return anchor.file.Read()
}

func (anchor *switchableFenceAnchor) Write(value string) error {
	if anchor.unreachable {
		return errors.New("fence volume is not mounted")
	}
	return anchor.file.Write(value)
}

// TestOptOutResidualARestoreWithTheVolumeBackRevivesBackedUpFeeds pins the
// DECLARED residual of CALENDAR_FEED_FENCE_OPTOUT (WEB-518). The hypothesis
// held: the waiver records nothing, and no code can do better, because with
// the fence unreachable there is no place outside the database to record it.
// The closing step is the operator's — mount the fence, remove the opt-out,
// and after restoring such a backup remove the fence file and have owners
// re-generate their feeds — and the documentation says so. If this test
// starts failing, the residual changed: update that documentation with it.
//
// The sequence: a backup is taken while
// the fence works, the instance then boots under CALENDAR_FEED_FENCE_OPTOUT
// with the fence volume unmounted (the boot drops the database half of the
// marker), the owner arms and revokes a feed while the volume is still away
// (refusalWaiver waives both writes and records nothing), the old volume is
// mounted again with its old token, and the backup taken before the drop is
// restored. The next fenced boot must not leave the backup's feed armed.
//
// The credential "authenticates" when its selector still resolves to a row,
// which is the lookup the feed endpoint performs first and the only one a
// restore can change; the verifier columns come back with the row.
func TestOptOutResidualARestoreWithTheVolumeBackRevivesBackedUpFeeds(t *testing.T) {
	ctx := context.Background()
	database, repositories, _, fencePath := newOperatorRevocationFixture(t)
	anchor := &switchableFenceAnchor{file: security.NewCalendarFeedFenceFile(fencePath)}
	fence := NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, anchor)
	repositories.WithCalendarFeedFence(fence)

	if _, err := fence.EnforceRequired(ctx); err != nil {
		t.Fatalf("first boot: %v", err)
	}
	owner := createArmedOwner(t, ctx, repositories, "owner@example.com")
	backedUpSelector := loadCalendarFeedSelector(t, repositories, owner.ID)
	backedUpMarker, found, err := repositories.AppState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil || !found || backedUpSelector == "" {
		t.Fatalf("the backup point needs an armed feed and a marker: selector=%q found=%t err=%v", backedUpSelector, found, err)
	}

	// OPTOUT boot, volume away: drops the database half.
	anchor.unreachable = true
	outcome, err := fence.Enforce(ctx)
	if err != nil || !outcome.Unanchored {
		t.Fatalf("opt-out boot over an unreachable fence: outcome=%+v err=%v", outcome, err)
	}
	if _, found, _ := repositories.AppState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence); found {
		t.Fatal("the unanchored boot must drop the database half of the marker")
	}

	// The owner arms a new feed and revokes it while the volume is away.
	if err := repositories.Users.SaveCalendarFeedToken(ctx, owner.ID, models.CalendarFeedTokenColumns{
		Selector: "selector-after-optout", VerifierHash: "hash", VerifierMAC: "mac", KeyEpoch: "epoch",
	}); err != nil {
		t.Fatalf("arm while unreachable: %v", err)
	}
	if err := repositories.Users.ClearCalendarFeedToken(ctx, owner.ID); err != nil {
		t.Fatalf("revoke while unreachable: %v", err)
	}
	if selector := loadCalendarFeedSelector(t, repositories, owner.ID); selector != "" {
		t.Fatalf("the revocation must clear the row, got %q", selector)
	}

	// The old volume is back with its old token, and the pre-drop backup is
	// restored: its marker, its armed row, and no unanchored stamp (the stamp
	// was written after the backup point).
	anchor.unreachable = false
	// The row is written around the repository: a restore replaces rows
	// without advancing any fence.
	if err := database.WithContext(ctx).Model(&models.User{}).Where("id = ?", owner.ID).Updates(map[string]any{
		"calendar_feed_selector": backedUpSelector, "calendar_feed_verifier_hash": "hash", "calendar_feed_verifier_mac": "mac",
	}).Error; err != nil {
		t.Fatalf("restore the backed-up row: %v", err)
	}
	if err := repositories.AppState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, backedUpMarker); err != nil {
		t.Fatalf("restore the backed-up marker: %v", err)
	}
	if err := repositories.AppState.Delete(ctx, models.AppStateKeyCalendarFeedFenceUnanchored); err != nil {
		t.Fatalf("restore without the stamp: %v", err)
	}

	if _, err := fence.EnforceRequired(ctx); err != nil {
		t.Fatalf("boot after the restore: %v", err)
	}
	if _, found, err := repositories.Users.FindByCalendarFeedSelector(ctx, backedUpSelector); err != nil || !found {
		t.Fatalf("the declared residual changed: the backed-up credential no longer resolves after the restore boot (found=%t err=%v); update the opt-out documentation with it", found, err)
	}
}
