package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// stubCalendarFeedSettingsRepo records the last-saved feed columns and whether a
// clear happened, and can force errors, so the settings service's mint/persist/
// clear/status behavior can be asserted without a database.
type stubCalendarFeedSettingsRepo struct {
	saved      *models.CalendarFeedTokenColumns
	cleared    bool
	saveErr    error
	clearErr   error
	findErr    error
	findUser   models.User
	findUserID uint
}

func (s *stubCalendarFeedSettingsRepo) SaveCalendarFeedToken(_ context.Context, userID uint, columns models.CalendarFeedTokenColumns) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.findUserID = userID
	copied := columns
	s.saved = &copied
	// Reflect the write into the row the status/find path returns.
	s.findUser.ID = userID
	s.findUser.CalendarFeedSelector = columns.Selector
	s.findUser.CalendarFeedVerifierHash = columns.VerifierHash
	return nil
}

func (s *stubCalendarFeedSettingsRepo) ClearCalendarFeedToken(_ context.Context, userID uint) error {
	if s.clearErr != nil {
		return s.clearErr
	}
	s.cleared = true
	s.findUser.ID = userID
	s.findUser.CalendarFeedSelector = ""
	s.findUser.CalendarFeedVerifierHash = ""
	return nil
}

func (s *stubCalendarFeedSettingsRepo) FindByID(_ context.Context, userID uint) (models.User, error) {
	if s.findErr != nil {
		return models.User{}, s.findErr
	}
	return s.findUser, nil
}

// TestGenerateFeedTokenPersistsHashedTokenAndReturnsFullTokenOnce proves the
// core secret contract: the caller gets the full shown-once token, while what is
// PERSISTED is the non-secret selector plus a bcrypt HASH of the verifier — never
// the verifier plaintext — and that the persisted columns verify the returned
// token via the real constant-time verifier.
func TestGenerateFeedTokenPersistsHashedTokenAndReturnsFullTokenOnce(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo)

	token, err := service.GenerateFeedToken(context.Background(), 42)
	if err != nil {
		t.Fatalf("GenerateFeedToken: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatal("expected a non-empty shown-once token")
	}
	if repo.saved == nil {
		t.Fatal("expected the token to be persisted")
	}
	if repo.findUserID != 42 {
		t.Fatalf("expected persistence scoped to user 42, got %d", repo.findUserID)
	}

	// The verifier plaintext must NOT be stored: the full token contains the
	// verifier, and the stored hash must not equal any plaintext slice of it.
	if repo.saved.VerifierHash == "" {
		t.Fatal("expected a stored verifier hash")
	}
	if strings.Contains(token, repo.saved.VerifierHash) {
		t.Fatal("stored verifier hash must not be a plaintext slice of the token")
	}
	// The stored columns must verify the returned token (proves the hash is a
	// real bcrypt hash of this token's verifier, not garbage).
	if !VerifyCalendarFeedToken(token, repo.saved.Selector, repo.saved.VerifierHash) {
		t.Fatal("expected stored selector+hash to verify the returned token")
	}
	// A different token must NOT verify against the stored columns.
	other, _, _, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("mint other token: %v", err)
	}
	if VerifyCalendarFeedToken(other, repo.saved.Selector, repo.saved.VerifierHash) {
		t.Fatal("an unrelated token must not verify against the stored columns")
	}
}

// TestGenerateFeedTokenRotationInvalidatesOldToken proves that a second
// GenerateFeedToken (rotation) yields a new token whose stored columns no longer
// verify the FIRST token — the old subscribe URL is dead immediately.
func TestGenerateFeedTokenRotationInvalidatesOldToken(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo)

	firstToken, err := service.GenerateFeedToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("first GenerateFeedToken: %v", err)
	}
	firstSelector := repo.saved.Selector
	firstHash := repo.saved.VerifierHash

	secondToken, err := service.GenerateFeedToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("rotate GenerateFeedToken: %v", err)
	}
	if secondToken == firstToken {
		t.Fatal("expected a fresh token on rotation")
	}
	if repo.saved.Selector == firstSelector {
		t.Fatal("expected a fresh selector on rotation")
	}
	// The OLD token must not verify against the NEW stored columns.
	if VerifyCalendarFeedToken(firstToken, repo.saved.Selector, repo.saved.VerifierHash) {
		t.Fatal("old token must not verify against rotated columns")
	}
	// The NEW token must verify against the NEW stored columns.
	if !VerifyCalendarFeedToken(secondToken, repo.saved.Selector, repo.saved.VerifierHash) {
		t.Fatal("new token must verify against rotated columns")
	}
	// And the old columns (had they survived) still verify only the old token —
	// sanity that the two token pairs are genuinely distinct.
	if !VerifyCalendarFeedToken(firstToken, firstSelector, firstHash) {
		t.Fatal("first token must verify against the first columns")
	}
}

// TestGenerateFeedTokenPropagatesPersistError proves a persistence failure is
// surfaced as ErrCalendarFeedTokenPersist and no token leaks back.
func TestGenerateFeedTokenPropagatesPersistError(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{saveErr: errors.New("write failed")}
	service := NewCalendarFeedSettingsService(repo)

	token, err := service.GenerateFeedToken(context.Background(), 1)
	if !errors.Is(err, ErrCalendarFeedTokenPersist) {
		t.Fatalf("expected ErrCalendarFeedTokenPersist, got %v", err)
	}
	if token != "" {
		t.Fatalf("expected no token on persist failure, got %q", token)
	}
}

// TestRevokeFeedTokenClearsColumns proves revoke delegates to the clear path.
func TestRevokeFeedTokenClearsColumns(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo)

	if err := service.RevokeFeedToken(context.Background(), 9); err != nil {
		t.Fatalf("RevokeFeedToken: %v", err)
	}
	if !repo.cleared {
		t.Fatal("expected ClearCalendarFeedToken to be called")
	}
}

// TestRevokeFeedTokenPropagatesError proves a clear failure surfaces as
// ErrCalendarFeedTokenPersist.
func TestRevokeFeedTokenPropagatesError(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{clearErr: errors.New("clear failed")}
	service := NewCalendarFeedSettingsService(repo)

	if err := service.RevokeFeedToken(context.Background(), 9); !errors.Is(err, ErrCalendarFeedTokenPersist) {
		t.Fatalf("expected ErrCalendarFeedTokenPersist, got %v", err)
	}
}

// TestBuildFeedStatusReportsConfiguredOnlyFromSelector proves the status
// projection reports Configured strictly from the presence of a stored selector,
// and never carries the token or the URL.
func TestBuildFeedStatusReportsConfiguredOnlyFromSelector(t *testing.T) {
	// Not configured: empty selector.
	repoOff := &stubCalendarFeedSettingsRepo{findUser: models.User{ID: 3}}
	if got := NewCalendarFeedSettingsService(repoOff).BuildFeedStatus(context.Background(), 3); got.Configured {
		t.Fatal("expected not-configured for an empty selector")
	}

	// Configured: non-empty selector.
	repoOn := &stubCalendarFeedSettingsRepo{findUser: models.User{ID: 3, CalendarFeedSelector: "SOMESELECTOR16XX"}}
	if got := NewCalendarFeedSettingsService(repoOn).BuildFeedStatus(context.Background(), 3); !got.Configured {
		t.Fatal("expected configured for a non-empty selector")
	}

	// Load error: reports not-configured so the settings page still renders.
	repoErr := &stubCalendarFeedSettingsRepo{findErr: errors.New("db down")}
	if got := NewCalendarFeedSettingsService(repoErr).BuildFeedStatus(context.Background(), 3); got.Configured {
		t.Fatal("expected not-configured on load error")
	}
}
