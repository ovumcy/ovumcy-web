package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestMustEnforceCalendarFeedKeyRotationAcrossBoots drives the real boot
// wrapper against a real migrated SQLite database across three boots: first
// boot records the epoch marker, a same-key reboot leaves it byte-identical,
// and a rotated-key boot moves it. (The row-level disarm semantics are proven
// in internal/db; the sentinel's ordering contract in internal/services.)
func TestMustEnforceCalendarFeedKeyRotationAcrossBoots(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "rotation-boot.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories := db.NewRepositories(database)

	keyA := []byte("boot-drill-secret-key-A-0123456789")
	keyB := []byte("boot-drill-secret-key-B-0123456789")

	mustEnforceCalendarFeedKeyRotation(repositories, keyA)
	first, found, err := repositories.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedKeyEpoch)
	if err != nil || !found || first == "" {
		t.Fatalf("first boot must record a non-empty epoch (found=%v, err=%v, value=%q)", found, err, first)
	}

	mustEnforceCalendarFeedKeyRotation(repositories, keyA)
	second, _, err := repositories.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedKeyEpoch)
	if err != nil || second != first {
		t.Fatalf("same-key reboot must keep the epoch (err=%v, before=%q, after=%q)", err, first, second)
	}

	mustEnforceCalendarFeedKeyRotation(repositories, keyB)
	third, _, err := repositories.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedKeyEpoch)
	if err != nil || third == first || third == "" {
		t.Fatalf("rotated-key boot must record a new epoch (err=%v, before=%q, after=%q)", err, first, third)
	}
}

// TestCalendarFeedRotationStartupMessage pins the operator-facing log line:
// silent for the routine outcomes, explicit (with the count) for a rotation.
func TestCalendarFeedRotationStartupMessage(t *testing.T) {
	if got := calendarFeedRotationStartupMessage(services.CalendarFeedRotationOutcome{}); got != "" {
		t.Fatalf("unchanged key must log nothing, got %q", got)
	}
	if got := calendarFeedRotationStartupMessage(services.CalendarFeedRotationOutcome{FirstBoot: true}); got != "" {
		t.Fatalf("a first boot with nothing to disarm must log nothing, got %q", got)
	}
	// A first boot that DID disarm is an upgrade whose legacy feeds were
	// cleared. Silent is exactly wrong there: a subscribe URL just stopped
	// answering and the owner has to be told why.
	upgraded := calendarFeedRotationStartupMessage(services.CalendarFeedRotationOutcome{FirstBoot: true, DisarmedFeeds: 3})
	if upgraded != "calendar-feed key epoch recorded for the first time: 3 legacy calendar feed(s) predating the keyed MAC disarmed, because nothing records which SECRET_KEY minted them; owners re-generate subscribe URLs from settings" {
		t.Fatalf("unexpected first-boot disarm message: %q", upgraded)
	}
	rotatedNone := calendarFeedRotationStartupMessage(services.CalendarFeedRotationOutcome{RotationDetected: true})
	if rotatedNone != "SECRET_KEY rotation detected: no legacy calendar feeds to disarm (armed feeds with a keyed MAC stop verifying on their own)" {
		t.Fatalf("unexpected zero-disarm rotation message: %q", rotatedNone)
	}
	rotatedSome := calendarFeedRotationStartupMessage(services.CalendarFeedRotationOutcome{RotationDetected: true, DisarmedFeeds: 2})
	if rotatedSome != "SECRET_KEY rotation detected: 2 legacy calendar feed(s) disarmed; owners re-generate subscribe URLs from settings" {
		t.Fatalf("unexpected disarm rotation message: %q", rotatedSome)
	}
}

// TestFirstBootAfterAnUpgradeDisarmsTheLegacyFeedItInherits drives the same
// boot wrapper against a database that arrived from a pre-sentinel release: no
// stored epoch, and an armed feed row with no verifier MAC. Recording the epoch
// beside such a row would adopt it as the baseline and leave the first poll
// free to backfill a MAC under today's key — reviving a subscribe URL a
// rotation in that same window was meant to kill. The row must be cleared on
// this boot, before the listener exists.
func TestFirstBootAfterAnUpgradeDisarmsTheLegacyFeedItInherits(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "rotation-upgrade-boot.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories := db.NewRepositories(database)

	// Written raw: the current mint always derives a MAC, so this shape can
	// only come from a release that predates migration 032.
	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at, calendar_feed_selector, calendar_feed_verifier_hash, calendar_feed_verifier_mac)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?)`,
		"legacy-feed@example.com", "hash", models.RoleOwner, "SELECTOR16CHARSXX", "legacy-bcrypt-hash", "",
	).Error; err != nil {
		t.Fatalf("seed a pre-032 armed feed: %v", err)
	}

	// Anchor: the row really is armed going in, so the emptiness below is the
	// disarm and not the seed failing to take.
	readSelector := func() string {
		t.Helper()
		var selector string
		if err := database.Raw(
			`SELECT COALESCE(calendar_feed_selector, '') FROM users WHERE email = ?`, "legacy-feed@example.com",
		).Scan(&selector).Error; err != nil {
			t.Fatalf("read back the feed row: %v", err)
		}
		return selector
	}
	if readSelector() == "" {
		t.Fatal("the seeded pre-032 feed is not armed, so the disarm below would prove nothing")
	}

	mustEnforceCalendarFeedKeyRotation(repositories, []byte("boot-drill-secret-key-A-0123456789"))

	if selector := readSelector(); selector != "" {
		t.Fatalf("the inherited MAC-less feed must be disarmed on the first boot, still armed as %q", selector)
	}
	if _, found, err := repositories.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedKeyEpoch); err != nil || !found {
		t.Fatalf("the epoch must be recorded after the disarm (found=%v, err=%v)", found, err)
	}
}
