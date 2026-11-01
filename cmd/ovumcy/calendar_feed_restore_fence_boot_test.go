package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestMustEnforceCalendarFeedRestoreFenceAcrossBoots drives the real boot
// wrapper against a real migrated SQLite database. The two facts it pins are
// the ones the wrapper alone can be wrong about: a restart with the fence in
// place keeps the marker byte-identical, and a restart whose fence file was
// replaced (what a restored database looks like from the fence's side) mints a
// new one. The disarm semantics are proven in internal/db, the decision table
// in internal/services.
func TestMustEnforceCalendarFeedRestoreFenceAcrossBoots(t *testing.T) {
	fencePath := filepath.Join(t.TempDir(), "calendar-feed.fence")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "fence-boot.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories, fence := bootstrap.BuildRepositories(database, fencePath)

	mustEnforceCalendarFeedRestoreFence(fence, false)
	first := storedFenceMarker(t, repositories)
	if first == "" {
		t.Fatal("first boot must record a non-empty fence marker")
	}
	onDisk, err := os.ReadFile(fencePath)
	if err != nil {
		t.Fatalf("the fence file must exist after the first boot: %v", err)
	}
	if strings.TrimSpace(string(onDisk)) != first {
		t.Fatalf("both halves must hold the same token; file %q, app_state %q", strings.TrimSpace(string(onDisk)), first)
	}

	mustEnforceCalendarFeedRestoreFence(fence, false)
	if second := storedFenceMarker(t, repositories); second != first {
		t.Fatalf("an ordinary restart must keep the marker (before=%q, after=%q)", first, second)
	}

	// A database that came back from a backup carries a marker this fence file
	// never wrote. Rewriting the file is the same disagreement seen from the
	// other side, and is what a test can stage without a second database.
	if err := os.WriteFile(fencePath, []byte("a-different-generation\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mustEnforceCalendarFeedRestoreFence(fence, false)
	third := storedFenceMarker(t, repositories)
	if third == first || third == "a-different-generation" || third == "" {
		t.Fatalf("a disagreeing fence must mint a fresh marker (before=%q, after=%q)", first, third)
	}
}

// TestMustEnforceCalendarFeedRestoreFenceWithoutAFenceDoesNotStopTheBoot pins
// the deployment decision this fix rests on: an operator who pulls the new
// image without adding the mount, and who set CALENDAR_FEED_FENCE_OPTOUT to
// accept the residual, gets a booting instance with its feeds disarmed, not a
// container that will not start. It also pins that nothing is recorded, so the
// next unanchored boot cannot read as agreement.
func TestMustEnforceCalendarFeedRestoreFenceWithoutAFenceDoesNotStopTheBoot(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "fence-unanchored.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories, fence := bootstrap.BuildRepositories(database, "")

	mustEnforceCalendarFeedRestoreFence(fence, true)

	if marker := storedFenceMarker(t, repositories); marker != "" {
		t.Fatalf("an unanchored boot must record no marker, got %q", marker)
	}
}

// TestEnforceCalendarFeedRestoreFenceRefusesTheBootWithoutAFence pins the v2.0
// deploy contract (WEB-399): with no usable fence and no opt-out the boot is
// refused with an error naming the variable, the docs section and the
// opt-out, and the refused boot writes nothing — no disarm stamp that would
// cost every owner a sign-in once the operator mounts the fence.
func TestEnforceCalendarFeedRestoreFenceRefusesTheBootWithoutAFence(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "fence-refused.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories, fence := bootstrap.BuildRepositories(database, "")

	_, err = enforceCalendarFeedRestoreFence(context.Background(), fence, false)
	if !errors.Is(err, errCalendarFeedFenceRequired) {
		t.Fatalf("a boot without a fence must be refused, got %v", err)
	}
	for _, want := range []string{"CALENDAR_FEED_FENCE_PATH", "Calendar Feed Restore Fence", calendarFeedFenceOptOutEnv + "=true"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name %q, got %q", want, err)
		}
	}
	if _, found, err := repositories.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedFenceUnanchored); err != nil || found {
		t.Fatalf("a refused boot must not stamp the database (found=%v, err=%v)", found, err)
	}
}

// TestEnforceCalendarFeedRestoreFenceRefusesAnUnwritableFence covers the
// second refusal: a fence that reads but cannot be written is only found
// inside Enforce, and is refused there too.
func TestEnforceCalendarFeedRestoreFenceRefusesAnUnwritableFence(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "fence-unwritable.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	// A parent directory that does not exist: the read finds no token yet on
	// every platform, and the write, which never creates the parent, fails.
	fencePath := filepath.Join(t.TempDir(), "absent", "calendar-feed.fence")
	_, fence := bootstrap.BuildRepositories(database, fencePath)

	if err := fence.CheckAnchor(); err != nil {
		t.Fatalf("this fence must pass the read-only pre-check so the refusal comes from Enforce, got %v", err)
	}
	if _, err := enforceCalendarFeedRestoreFence(context.Background(), fence, false); !errors.Is(err, errCalendarFeedFenceRequired) {
		t.Fatalf("an unwritable fence must refuse the boot, got %v", err)
	}
}

// TestCalendarFeedRestoreFenceStartupMessage pins the operator-facing log line.
// The unanchored line is the one that has to carry a remedy: it is the only
// signal an operator gets that their feeds are being cleared on every start,
// and it has to name the variable and say the mount must be outside the backup.
func TestCalendarFeedRestoreFenceStartupMessage(t *testing.T) {
	if got := calendarFeedRestoreFenceStartupMessage(services.CalendarFeedRestoreFenceOutcome{}); got != "" {
		t.Fatalf("an agreeing fence must log nothing, got %q", got)
	}

	firstBoot := calendarFeedRestoreFenceStartupMessage(services.CalendarFeedRestoreFenceOutcome{FirstBoot: true})
	if !strings.Contains(firstBoot, "restore fence armed") {
		t.Fatalf("first boot must confirm the fence armed, got %q", firstBoot)
	}

	broken := calendarFeedRestoreFenceStartupMessage(services.CalendarFeedRestoreFenceOutcome{ContinuityBroken: true, DisarmedFeeds: 2})
	if !strings.Contains(broken, "2 armed calendar feed(s) disarmed") || !strings.Contains(broken, "backup restore") {
		t.Fatalf("a broken fence must name the count and the cause, got %q", broken)
	}

	// UnanchoredHistory qualifies ContinuityBroken rather than replacing it, so
	// both flags are set together on this outcome. The switch must still read
	// it as the unanchored-history case, not the plain backup-restore one: the
	// two share a cause (ContinuityBroken) but need different operator text,
	// and a case-order slip would silently produce the wrong line.
	unanchoredHistory := calendarFeedRestoreFenceStartupMessage(services.CalendarFeedRestoreFenceOutcome{
		ContinuityBroken:  true,
		UnanchoredHistory: true,
		DisarmedFeeds:     5,
	})
	if strings.Contains(unanchoredHistory, "backup restore") {
		t.Fatalf("an unanchored-history fence must not read as a backup restore, got %q", unanchoredHistory)
	}
	for _, want := range []string{"armed for the first time on a database that had already run without one", "5 armed calendar feed(s) disarmed", "not a restore", "Later starts disarm nothing"} {
		if !strings.Contains(unanchoredHistory, want) {
			t.Fatalf("the unanchored-history line must contain %q, got %q", want, unanchoredHistory)
		}
	}

	unanchored := calendarFeedRestoreFenceStartupMessage(services.CalendarFeedRestoreFenceOutcome{
		Unanchored:      true,
		UnanchoredCause: errors.New("read-only file system"),
		DisarmedFeeds:   3,
	})
	for _, want := range []string{"CALENDAR_FEED_FENCE_PATH", "NOT part of any database backup", "every start will disarm again", "3 armed calendar feed(s) disarmed", "database half of the fence marker dropped"} {
		if !strings.Contains(unanchored, want) {
			t.Fatalf("the unanchored line must contain %q, got %q", want, unanchored)
		}
	}
}

func storedFenceMarker(t *testing.T, repositories *db.Repositories) string {
	t.Helper()
	value, found, err := repositories.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil {
		t.Fatalf("AppState.Get: %v", err)
	}
	if !found {
		return ""
	}
	return value
}
