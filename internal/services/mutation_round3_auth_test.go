package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// mr3authCASRepo is a minimal AuthUserRepository implementing the CAS surface.
// Its CAS handler succeeds without touching the *models.User pointer the
// service mutates, so the only thing that bumps the passed-in userSnap's
// AuthSessionVersion is the production line auth_service.go:460. This isolates
// the `+ 1` arithmetic on the in-memory user the service returns to its caller.
type mr3authCASRepo struct {
	stubAuthUserRepo
}

func (r *mr3authCASRepo) UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(
	_ context.Context, _ uint, _, _, _ string,
) error {
	// Succeed; deliberately do NOT mutate any user state here so the
	// assertion below pins the service's own mutation of userSnap.
	return nil
}

// TestMR3Auth_CASBumpsPassedUserSessionVersion pins auth_service.go:460:81
// `NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1`. The existing CAS
// regression test asserts on the STUB-mutated user object, not the userSnap
// pointer the production line writes. Here the stub's CAS is a no-op on user
// state, so AuthSessionVersion on userSnap can only reach 2 via line 460.
//
// Mutant kill: +→- makes Normalize(1)-1 == 0 (fails want 2); *→Normalize(1)*1
// == 1 (fails); /→1/1 == 1 (fails).
func TestMR3Auth_CASBumpsPassedUserSessionVersion(t *testing.T) {
	originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash original password: %v", err)
	}

	repo := &mr3authCASRepo{}
	service := NewAuthService(repo)

	userSnap := models.User{
		ID:                 7,
		PasswordHash:       string(originalHash),
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
	}

	recoveryCode, err := service.ResetPasswordAndRotateRecoveryCodeCAS(
		context.Background(), &userSnap, string(originalHash), "EvenStronger2",
	)
	if err != nil {
		t.Fatalf("ResetPasswordAndRotateRecoveryCodeCAS: unexpected error: %v", err)
	}
	if recoveryCode == "" {
		t.Fatal("expected a non-empty rotated recovery code")
	}

	// The service mutates the passed-in userSnap (production line 460). With
	// the starting version 1, NormalizeAuthSessionVersion(1)+1 == 2.
	if userSnap.AuthSessionVersion != 2 {
		t.Fatalf("expected userSnap.AuthSessionVersion == 2 after CAS bump, got %d", userSnap.AuthSessionVersion)
	}
}

// TestMR3Auth_ConfigureWindowBoundary pins auth_attempt_policy.go:43:12
// `if window >= time.Second`. A window of exactly time.Second must be ACCEPTED
// (kills BOUNDARY >=→>), while a window just below time.Second must be IGNORED
// (kills NEGATION).
func TestMR3Auth_ConfigureWindowBoundary(t *testing.T) {
	t.Parallel()

	// Seed a known baseline window via the constructor, then reconfigure.
	policy := NewAuthAttemptPolicy("mr3auth", nil, 5, 10*time.Minute)

	// Exactly time.Second is at the boundary and must be accepted.
	policy.Configure(5, time.Second)
	if policy.window != time.Second {
		t.Fatalf("window==time.Second must be accepted, got %v (kills >=→>)", policy.window)
	}

	// Just below time.Second must be rejected, leaving the window unchanged.
	policy.Configure(5, time.Second-time.Nanosecond)
	if policy.window != time.Second {
		t.Fatalf("window<time.Second must be ignored; window changed to %v (kills negation)", policy.window)
	}
}

// TestMR3Auth_AddFailureAllSweepCounterIncrements pins
// attempt_limiter.go:89:19 `limiter.addCallsN++` in AddFailureAll. With
// addCallsN one below the sweep threshold, a single AddFailureAll call must
// cross the threshold and trigger the stale-key sweep that removes the
// pre-seeded stale keys, leaving only the just-refreshed live key.
//
// Mutant kill: ++→-- leaves addCallsN below the threshold, so no sweep fires
// and the stale keys survive (map size != 1).
func TestMR3Auth_AddFailureAllSweepCounterIncrements(t *testing.T) {
	t.Parallel()

	window := time.Hour
	now := time.Now().UTC()
	past := now.Add(-2 * window) // most-recent failure well before the threshold

	limiter := NewAttemptLimiter()

	// One below the sweep threshold: the single AddFailureAll below must push
	// addCallsN to exactly evictEveryN and fire the sweep.
	limiter.addCallsN = evictEveryN - 1

	// Pre-seed stale keys directly so we don't perturb the call counter.
	staleCount := evictEveryN - 1
	for i := 0; i < staleCount; i++ {
		limiter.attempts[fmt.Sprintf("mr3auth-stale-%d", i)] = []time.Time{past}
	}

	liveKey := "mr3auth-live"
	limiter.AddFailureAll([]string{liveKey}, now, window)

	limiter.mu.Lock()
	mapLen := len(limiter.attempts)
	_, livePresent := limiter.attempts[liveKey]
	limiter.mu.Unlock()

	if mapLen != 1 {
		t.Fatalf("expected sweep to leave 1 key, got %d (kills ++→--, which skips the sweep)", mapLen)
	}
	if !livePresent {
		t.Fatal("expected the live key to survive the sweep")
	}
}

// TestMR3Auth_SweepSizeGuardBoundary pins the size term of the sweep-skip
// guard at attempt_limiter.go:100:62: `len(limiter.attempts) < evictAboveSize`.
// We construct map state where, at the guard check, addCallsN is low (so the
// counter term can never force a sweep) and len == evictAboveSize exactly.
// The original `<` makes `len < evictAboveSize` false → the guard is false →
// the sweep proceeds and removes the stale keys. The mutant `<=` makes it true
// → the whole guard is true → early return → stale keys survive.
func TestMR3Auth_SweepSizeGuardBoundary(t *testing.T) {
	t.Parallel()

	window := time.Hour
	now := time.Now().UTC()
	past := now.Add(-2 * window)

	limiter := NewAttemptLimiter()

	// Keep the call-counter term low so only the size term governs the guard.
	// AddFailureAll increments it to 1, still well below evictEveryN.
	limiter.addCallsN = 0

	liveKey := "mr3auth-live"
	// Seed exactly evictAboveSize keys: the live key (refreshed by the call
	// below, so it stays after the sweep) plus evictAboveSize-1 stale keys.
	// Because the live key already exists, AddFailureAll updates it in place
	// and does NOT grow the map, so len stays == evictAboveSize at the guard.
	limiter.attempts[liveKey] = []time.Time{past}
	staleCount := evictAboveSize - 1
	for i := 0; i < staleCount; i++ {
		limiter.attempts[fmt.Sprintf("mr3auth-stale-%05d", i)] = []time.Time{past}
	}

	if got := len(limiter.attempts); got != evictAboveSize {
		t.Fatalf("test setup invariant: expected %d seeded keys, got %d", evictAboveSize, got)
	}

	limiter.AddFailureAll([]string{liveKey}, now, window)

	limiter.mu.Lock()
	mapLen := len(limiter.attempts)
	_, livePresent := limiter.attempts[liveKey]
	limiter.mu.Unlock()

	// Original `<`: guard false at len==evictAboveSize → sweep removes all the
	// stale keys, leaving only the refreshed live key.
	if mapLen != 1 {
		t.Fatalf("expected the sweep to leave 1 key, got %d (kills <→<=, which early-returns at len==cap)", mapLen)
	}
	if !livePresent {
		t.Fatal("expected the refreshed live key to survive the sweep")
	}
}
