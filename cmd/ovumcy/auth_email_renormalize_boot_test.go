package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestMustRenormalizeAuthEmailsAcrossBoots drives the real boot wrapper
// against a migrated SQLite database seeded with one legacy decorated row: the
// first boot repairs it (and prints the startup line — the log branch is part
// of the wrapper's contract), writes the done-marker, and a second boot leaves
// the marker byte-identical. (Full row-level repair semantics are proven in
// internal/db; the pass logic in internal/services.)
func TestMustRenormalizeAuthEmailsAcrossBoots(t *testing.T) {
	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "email-renormalize-boot.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories := db.NewRepositories(database)

	legacy := models.User{
		Email:               "boot drill <boot-legacy@example.com>",
		PasswordHash:        "hash",
		RecoveryCodeHash:    "recovery",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
	}
	if err := repositories.Users.Create(context.Background(), &legacy); err != nil {
		t.Fatalf("seed legacy user: %v", err)
	}

	mustRenormalizeAuthEmails(repositories)

	repaired, err := repositories.Users.FindByNormalizedEmail(context.Background(), "boot-legacy@example.com")
	if err != nil || repaired.ID != legacy.ID {
		t.Fatalf("first boot must repair the legacy row (err=%v, id=%d, want %d)", err, repaired.ID, legacy.ID)
	}
	first, found, err := repositories.AppState.Get(context.Background(), models.AppStateKeyAuthEmailRenormalizeV1)
	if err != nil || !found || first == "" {
		t.Fatalf("first boot must record the done-marker (found=%v, err=%v, value=%q)", found, err, first)
	}

	mustRenormalizeAuthEmails(repositories)
	second, _, err := repositories.AppState.Get(context.Background(), models.AppStateKeyAuthEmailRenormalizeV1)
	if err != nil || second != first {
		t.Fatalf("second boot must keep the marker (err=%v, before=%q, after=%q)", err, first, second)
	}
}

// TestAuthEmailRenormalizeStartupMessage pins the operator-facing log line:
// silent for the routine outcomes, counts only (never addresses) otherwise.
func TestAuthEmailRenormalizeStartupMessage(t *testing.T) {
	if got := authEmailRenormalizeStartupMessage(services.AuthEmailRenormalizeOutcome{AlreadyDone: true}); got != "" {
		t.Fatalf("already-done must log nothing, got %q", got)
	}
	if got := authEmailRenormalizeStartupMessage(services.AuthEmailRenormalizeOutcome{}); got != "" {
		t.Fatalf("clean pass with nothing to do must log nothing, got %q", got)
	}
	rewrittenOnly := authEmailRenormalizeStartupMessage(services.AuthEmailRenormalizeOutcome{Renormalized: 3})
	if rewrittenOnly != "auth email repair: 3 stored email(s) rewritten to their bare address" {
		t.Fatalf("unexpected rewritten-only message: %q", rewrittenOnly)
	}
	withSkips := authEmailRenormalizeStartupMessage(services.AuthEmailRenormalizeOutcome{Renormalized: 1, SkippedConflicts: 1, SkippedUnrenormalizable: 1})
	if withSkips != "auth email repair: 1 stored email(s) rewritten to their bare address, 2 left for operator review (duplicate mailbox or unparseable) — see docs/self-hosted.md, Troubleshooting" {
		t.Fatalf("unexpected with-skips message: %q", withSkips)
	}
}
