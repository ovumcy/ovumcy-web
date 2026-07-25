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
	service := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey))

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

	// The verifier plaintext must NOT be stored in either verifier column: the full
	// token contains the verifier, and neither stored value may be a plaintext
	// slice of it.
	if repo.saved.VerifierHash == "" {
		t.Fatal("expected a stored verifier hash")
	}
	if repo.saved.VerifierMAC == "" {
		t.Fatal("expected a stored keyed verifier MAC — it is what the feed endpoint compares")
	}
	if strings.Contains(token, repo.saved.VerifierHash) {
		t.Fatal("stored verifier hash must not be a plaintext slice of the token")
	}
	if strings.Contains(token, repo.saved.VerifierMAC) {
		t.Fatal("stored verifier MAC must not be a plaintext slice of the token")
	}
	// The stored columns must verify the returned token (proves the stored values
	// are real derivations of this token's verifier, not garbage).
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), token, *repo.saved) {
		t.Fatal("expected the stored columns to verify the returned token")
	}
	// A different token must NOT verify against the stored columns.
	other, _ := mustGenerateFeedToken(t)
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), other, *repo.saved) {
		t.Fatal("an unrelated token must not verify against the stored columns")
	}
}

// TestGenerateFeedTokenRotationInvalidatesOldToken proves that a second
// GenerateFeedToken (rotation) yields a new token whose stored columns no longer
// verify the FIRST token — the old subscribe URL is dead immediately.
func TestGenerateFeedTokenRotationInvalidatesOldToken(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey))

	firstToken, err := service.GenerateFeedToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("first GenerateFeedToken: %v", err)
	}
	firstColumns := *repo.saved

	secondToken, err := service.GenerateFeedToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("rotate GenerateFeedToken: %v", err)
	}
	if secondToken == firstToken {
		t.Fatal("expected a fresh token on rotation")
	}
	if repo.saved.Selector == firstColumns.Selector {
		t.Fatal("expected a fresh selector on rotation")
	}
	if repo.saved.VerifierMAC == firstColumns.VerifierMAC {
		t.Fatal("expected a fresh verifier MAC on rotation")
	}
	// The OLD token must not verify against the NEW stored columns.
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), firstToken, *repo.saved) {
		t.Fatal("old token must not verify against rotated columns")
	}
	// The NEW token must verify against the NEW stored columns.
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), secondToken, *repo.saved) {
		t.Fatal("new token must verify against rotated columns")
	}
	// And the old columns (had they survived) still verify only the old token —
	// sanity that the two token triples are genuinely distinct.
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), firstToken, firstColumns) {
		t.Fatal("first token must verify against the first columns")
	}
}

// TestGenerateFeedTokenPropagatesPersistError proves a persistence failure is
// surfaced as ErrCalendarFeedTokenPersist and no token leaks back.
func TestGenerateFeedTokenPropagatesPersistError(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{saveErr: errors.New("write failed")}
	service := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey))

	token, err := service.GenerateFeedToken(context.Background(), 1)
	if !errors.Is(err, ErrCalendarFeedTokenPersist) {
		t.Fatalf("expected ErrCalendarFeedTokenPersist, got %v", err)
	}
	if token != "" {
		t.Fatalf("expected no token on persist failure, got %q", token)
	}
}

// TestGenerateFeedTokenFailsClosedWithoutSecretKey proves the mint refuses rather
// than persisting a token with no keyed MAC. An empty MAC column is not a neutral
// value — it is the marker for "minted before migration 032, verify via bcrypt" —
// so writing one would pin a fresh subscription to the ~265 ms path forever.
// Nothing may reach the repository, and no token may leak back.
func TestGenerateFeedTokenFailsClosedWithoutSecretKey(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo, nil)

	token, err := service.GenerateFeedToken(context.Background(), 1)
	if !errors.Is(err, ErrCalendarFeedTokenGenerate) {
		t.Fatalf("expected ErrCalendarFeedTokenGenerate, got %v", err)
	}
	if token != "" {
		t.Fatalf("expected no token when the MAC cannot be derived, got %q", token)
	}
	if repo.saved != nil {
		t.Fatalf("expected nothing persisted when the mint fails, got %+v", repo.saved)
	}
}

// TestRevokeFeedTokenClearsColumns proves revoke delegates to the clear path.
func TestRevokeFeedTokenClearsColumns(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey))

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
	service := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey))

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
	if got := NewCalendarFeedSettingsService(repoOff, []byte(calendarFeedTestSecretKey)).BuildFeedStatus(context.Background(), 3); got.Configured {
		t.Fatal("expected not-configured for an empty selector")
	}

	// Configured: non-empty selector.
	repoOn := &stubCalendarFeedSettingsRepo{findUser: models.User{ID: 3, CalendarFeedSelector: "SOMESELECTOR16XX"}}
	if got := NewCalendarFeedSettingsService(repoOn, []byte(calendarFeedTestSecretKey)).BuildFeedStatus(context.Background(), 3); !got.Configured {
		t.Fatal("expected configured for a non-empty selector")
	}

	// Load error: reports not-configured so the settings page still renders.
	repoErr := &stubCalendarFeedSettingsRepo{findErr: errors.New("db down")}
	if got := NewCalendarFeedSettingsService(repoErr, []byte(calendarFeedTestSecretKey)).BuildFeedStatus(context.Background(), 3); got.Configured {
		t.Fatal("expected not-configured on load error")
	}
}
