package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestImportServiceAcceptsExactlyMaxEntries pins the inclusive upper bound of the
// restore size cap. The documented contract (ErrImportTooLarge "marks a payload
// carrying more than MaxImportEntries") makes exactly MaxImportEntries a VALID
// payload that must import, not be rejected.
//
// The existing oversized test feeds MaxImportEntries+1 and both the correct `>`
// guard and the `>=` boundary mutant reject it, so it cannot see the boundary.
// This test feeds exactly MaxImportEntries records (all the same calendar day, so
// planEntries dedups to a single created day and the rest count as rejected
// duplicates — cheap) and asserts the payload is accepted: the `>=` mutant would
// instead return ErrImportTooLarge and create nothing.
func TestImportServiceAcceptsExactlyMaxEntries(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-cap-boundary@example.com")
	importService := newImportServiceIntegration(t, database, symptomService)

	entries := make([]ExportJSONEntry, MaxImportEntries)
	for i := range entries {
		entries[i] = ExportJSONEntry{Date: "2026-01-01", CycleFactors: []string{}}
	}
	raw, err := json.Marshal(importPayload{Entries: entries})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("exactly MaxImportEntries (%d) must import, got error %v", MaxImportEntries, err)
	}
	if result.Added != 1 {
		t.Fatalf("expected exactly one added day after dedup, got %+v", result)
	}
	if result.Rejected != MaxImportEntries-1 {
		t.Fatalf("expected %d rejected duplicates, got %+v", MaxImportEntries-1, result)
	}
}
