package api

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// TestResetPasswordRedeemLoserOfConcurrentRedeemGetsInvalidTokenAndClearedCookie
// pins the handler side of the reset compare-and-swap. Two submits of the same
// reset link both parse the token before either writes; the second write finds
// the password hash already moved by the first and affects no row, and the
// service answers ErrResetTokenAlreadyConsumed. The handler must answer that
// loser exactly as it answers a replay after the win — 400 "invalid reset
// token" — clear the sealed reset cookie, and mint no session; a 500, or a kept
// cookie inviting a retry of a spent token, is the defect.
//
// The race is made deterministic rather than raced: a GORM query callback fires
// once, right after the redeem has read the row it validates the token against,
// and commits the winning redeem's password hash on its own. The winner must
// commit outside the loser's write: the compare-and-swap runs in a transaction
// that rolls back when it matches no row, and a winner simulated inside it would
// roll back with it. The update callback anchors that the loser still reached
// its compare-and-swap — a winner landing before the read would be refused by
// the token's password fingerprint instead, and prove nothing about the CAS.
func TestResetPasswordRedeemLoserOfConcurrentRedeemGetsInvalidTokenAndClearedCookie(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-concurrent-loser@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	const winnerHash = "$2a$10$winner-of-the-concurrent-redeem-hash"
	const winnerCallback = "test:reset-cas-concurrent-winner"
	const casCallback = "test:reset-cas-concurrent-loser-write"
	var fired, reachedCAS atomic.Bool
	if err := database.Callback().Query().After("gorm:query").Register(winnerCallback, func(tx *gorm.DB) {
		loaded, ok := tx.Statement.Dest.(*models.User)
		if !ok || loaded.ID != user.ID || !fired.CompareAndSwap(false, true) {
			return
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE users SET password_hash = ? WHERE id = ?", winnerHash, user.ID).Error; err != nil {
			t.Errorf("simulate the winning redeem: %v", err)
		}
	}); err != nil {
		t.Fatalf("register winner callback: %v", err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove(winnerCallback) })
	if err := database.Callback().Update().Before("gorm:update").Register(casCallback, func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		_, rotatesCode := updates["recovery_code_hash"]
		_, rewritesPassword := updates["password_hash"]
		if rotatesCode && rewritesPassword {
			reachedCAS.Store(true)
		}
	}); err != nil {
		t.Fatalf("register CAS callback: %v", err)
	}
	t.Cleanup(func() { _ = database.Callback().Update().Remove(casCallback) })

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")

	if !fired.Load() || !reachedCAS.Load() {
		t.Fatalf("anchor: the winner must land after the redeem's read (fired=%v) and the loser must still reach its compare-and-swap write (reached=%v)", fired.Load(), reachedCAS.Load())
	}
	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "invalid reset token" {
		t.Fatalf("expected %q for the loser, got %q", "invalid reset token", got)
	}
	cleared := responseCookie(response.Cookies(), resetPasswordCookieName)
	if cleared == nil || cleared.Value != "" {
		t.Fatalf("expected the loser's reset cookie to be cleared, got %#v", cleared)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected the loser to mint no session, got %#v", authCookie)
	}

	var stored models.User
	if err := database.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.PasswordHash != winnerHash {
		t.Fatal("expected the loser to leave the winner's password hash in place")
	}
}
