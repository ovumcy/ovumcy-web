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
	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "rotation-boot.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
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
		t.Fatalf("first boot must log nothing, got %q", got)
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
