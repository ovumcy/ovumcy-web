package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestListOwnerLutealPhaseRowsProjectsWhatTheRecomputeDecidesOn pins the read
// side of the one-shot luteal-phase recompute.
//
// The pass compares a recomputed estimate against the stored one and writes only
// on a difference, so a projection that dropped luteal_phase would still look
// correct from the outside — every row would read as zero, differ from its
// recomputed value, and be rewritten to the same number the pass would have
// written anyway. Only reading the column back proves it was selected. Timezone
// is the same shape of silent default: an empty zone resolves to the fallback
// and the answer would look right.
//
// The partner row is there because the pass is scoped to owners: the column
// CHECK still admits 'partner' even though both account-creating methods refuse
// it, and luteal_phase means nothing for a role this product does not have.
func TestListOwnerLutealPhaseRowsProjectsWhatTheRecomputeDecidesOn(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "luteal-rows.db"))
	repository := NewUserRepository(database)
	ctx := context.Background()

	first := &models.User{Email: "first@example.com", PasswordHash: "hash", Role: models.RoleOwner, Timezone: "America/New_York", LutealPhase: 15}
	second := &models.User{Email: "second@example.com", PasswordHash: "hash", Role: models.RoleOwner, LutealPhase: 12}
	excluded := &models.User{Email: "excluded@example.com", PasswordHash: "hash", Role: models.RoleOwner, LutealPhase: 19}
	for _, user := range []*models.User{first, second, excluded} {
		if err := repository.Create(ctx, user); err != nil {
			t.Fatalf("seed %s: %v", user.Email, err)
		}
	}
	// Written past the repository on purpose: Create refuses the role, and the
	// point here is that a row the database would still accept stays out of the
	// pass's population.
	if err := database.Exec("UPDATE users SET role = ? WHERE id = ?", "partner", excluded.ID).Error; err != nil {
		t.Fatalf("demote the excluded row: %v", err)
	}

	rows, err := repository.ListOwnerLutealPhaseRows(ctx)
	if err != nil {
		t.Fatalf("ListOwnerLutealPhaseRows: %v", err)
	}

	want := []models.LutealPhaseRecomputeRow{
		{ID: first.ID, Timezone: "America/New_York", LutealPhase: 15},
		{ID: second.ID, Timezone: "", LutealPhase: 12},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows (%+v), want %d — the non-owner row must not be listed", len(rows), rows, len(want))
	}
	for index, wantRow := range want {
		if rows[index] != wantRow {
			t.Fatalf("row %d = %+v, want %+v", index, rows[index], wantRow)
		}
	}
}

// TestListOwnerLutealPhaseRowsSurfacesAStorageFailure holds the query's error
// path open. The recompute treats an unreadable listing as a reason to abort
// without writing its done-marker, so the next boot retries the whole pass; a
// method that swallowed the error and returned an empty slice would instead read
// as "no owners to repair" and let the marker land on an instance where nothing
// was walked — the repair lost silently and for good.
func TestListOwnerLutealPhaseRowsSurfacesAStorageFailure(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "luteal-rows-error.db"))
	repository := NewUserRepository(database)

	if err := database.Exec("DROP TABLE users").Error; err != nil {
		t.Fatalf("drop users: %v", err)
	}

	rows, err := repository.ListOwnerLutealPhaseRows(context.Background())
	if err == nil {
		t.Fatalf("a query against a missing table must return an error, got rows=%+v", rows)
	}
	if rows != nil {
		t.Fatalf("a failed listing must return no rows, got %+v", rows)
	}
}
