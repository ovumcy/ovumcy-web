package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// stubCalendarFeedSettingsRepo records the last-saved feed columns and whether a
// clear happened, and can force errors, so the settings service's mint/persist/
// clear/status behavior can be asserted without a database.
type stubCalendarFeedSettingsRepo struct {
	saved   *models.CalendarFeedTokenColumns
	cleared bool
	// clearedUserID keeps the owner id the CLEAR was called with on its own
	// field. Reusing findUserID would make the assertion ambiguous — the save
	// and claim paths write that one too, so a revoke case could read an id no
	// revoke ever supplied.
	clearedUserID uint
	saveErr       error
	clearErr      error
	findErr       error
	claimErr      error
	findUser      models.User
	findUserID    uint
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
	// The real UPDATE NULLs calendar_feed_revealed_at in the same statement, so
	// a freshly minted token arrives with its one-time reveal armed.
	s.findUser.CalendarFeedRevealedAt = nil
	return nil
}

// ClaimCalendarFeedReveal models the real compare-and-set: the first call
// consumes the reveal, every later one loses because the mark is already set.
func (s *stubCalendarFeedSettingsRepo) ClaimCalendarFeedReveal(_ context.Context, userID uint, revealedAt time.Time) (bool, error) {
	if s.claimErr != nil {
		return false, s.claimErr
	}
	s.findUserID = userID
	if s.findUser.CalendarFeedRevealedAt != nil {
		return false, nil
	}
	claimedAt := revealedAt.UTC()
	s.findUser.CalendarFeedRevealedAt = &claimedAt
	return true, nil
}

func (s *stubCalendarFeedSettingsRepo) ClearCalendarFeedToken(_ context.Context, userID uint) error {
	if s.clearErr != nil {
		return s.clearErr
	}
	s.cleared = true
	s.clearedUserID = userID
	s.findUser.ID = userID
	s.findUser.CalendarFeedSelector = ""
	s.findUser.CalendarFeedVerifierHash = ""
	return nil
}

func (s *stubCalendarFeedSettingsRepo) LoadSettingsByID(_ context.Context, userID uint) (models.User, error) {
	s.findUserID = userID
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

// TestRevokeFeedTokenClearsColumnsForTheRequestedOwner proves revoke delegates
// to the clear path AND that it forwards the owner id it was asked to revoke. "The mock was called" is
// not the outcome: revoke is the containment action behind the one sanctioned
// secret-in-transport exception, so the operand it carries is the whole guard —
// a clear aimed at a constant row would still set this flag. The mint path is
// pinned the same way above; this closes the same seam on the revoke side.
func TestRevokeFeedTokenClearsColumnsForTheRequestedOwner(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey))

	if err := service.RevokeFeedToken(context.Background(), 9); err != nil {
		t.Fatalf("RevokeFeedToken: %v", err)
	}
	if !repo.cleared {
		t.Fatal("expected ClearCalendarFeedToken to be called")
	}
	if repo.clearedUserID != 9 {
		t.Fatalf("expected the clear scoped to user 9, got %d", repo.clearedUserID)
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

// TestClaimFeedRevealIsSingleUsePerMintAndOwnerBound pins the seam the reveal
// page gates on: a mint arms exactly one reveal, the replay of a spent one
// loses, and a claim naming no account is refused before it reaches the
// repository — an absent owner id is invalid input, never a claim that skips the
// comparison.
func TestClaimFeedRevealIsSingleUsePerMintAndOwnerBound(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{}
	service := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey))
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)

	if _, err := service.GenerateFeedToken(context.Background(), 7); err != nil {
		t.Fatalf("GenerateFeedToken: %v", err)
	}
	claimed, err := service.ClaimFeedReveal(context.Background(), 7, now)
	if err != nil {
		t.Fatalf("ClaimFeedReveal: %v", err)
	}
	if !claimed {
		t.Fatal("expected a freshly minted token to arm exactly one reveal")
	}

	replayed, err := service.ClaimFeedReveal(context.Background(), 7, now)
	if err != nil {
		t.Fatalf("ClaimFeedReveal replay: %v", err)
	}
	if replayed {
		t.Fatal("expected a replayed claim to lose against the mark already set")
	}

	// A rotate is a second mint, so it re-arms.
	if _, err := service.GenerateFeedToken(context.Background(), 7); err != nil {
		t.Fatalf("GenerateFeedToken rotate: %v", err)
	}
	rearmed, err := service.ClaimFeedReveal(context.Background(), 7, now)
	if err != nil {
		t.Fatalf("ClaimFeedReveal after rotate: %v", err)
	}
	if !rearmed {
		t.Fatal("expected a rotate to re-arm the reveal of the new subscribe URL")
	}

	unattributed := &stubCalendarFeedSettingsRepo{}
	if _, err := NewCalendarFeedSettingsService(unattributed, []byte(calendarFeedTestSecretKey)).
		ClaimFeedReveal(context.Background(), 0, now); !errors.Is(err, ErrCalendarFeedTokenPersist) {
		t.Fatalf("expected a claim naming no account to be refused, got %v", err)
	}
	if unattributed.findUser.CalendarFeedRevealedAt != nil {
		t.Fatal("a claim naming no account must never reach the repository")
	}
}

// TestClaimFeedRevealSurfacesStorageFailure keeps a storage failure
// distinguishable from "already claimed": the page refuses on either, but
// conflating them would hide a database outage behind an ordinary-looking
// replay.
func TestClaimFeedRevealSurfacesStorageFailure(t *testing.T) {
	repo := &stubCalendarFeedSettingsRepo{claimErr: errors.New("claim failed")}

	claimed, err := NewCalendarFeedSettingsService(repo, []byte(calendarFeedTestSecretKey)).
		ClaimFeedReveal(context.Background(), 7, time.Now())
	if !errors.Is(err, ErrCalendarFeedTokenPersist) {
		t.Fatalf("expected the storage failure to surface as a persist error, got %v", err)
	}
	if claimed {
		t.Fatal("a claim that could not be recorded must never report success")
	}
}
