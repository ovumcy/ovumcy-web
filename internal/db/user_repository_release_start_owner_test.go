package db

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A zero owner id is refused before the old start's rows are read or written.
func TestReleaseOldPeriodStartRefusesAZeroOwner(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)
	move := models.PeriodStartMove{ReleaseStart: time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)}

	if err := releaseOldPeriodStart(repo.database, 0, move); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired, got %v", err)
	}
}
