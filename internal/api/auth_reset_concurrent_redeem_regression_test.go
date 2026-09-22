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
// The race is made deterministic rather than raced: a GORM callback fires once,
// inside the redeem's own compare-and-swap UPDATE and before it runs, and moves
// the password hash the way the winning redeem would have.
func TestResetPasswordRedeemLoserOfConcurrentRedeemGetsInvalidTokenAndClearedCookie(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-concurrent-loser@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	const winnerHash = "$2a$10$winner-of-the-concurrent-redeem-hash"
	const callbackName = "test:reset-cas-concurrent-winner"
	var fired atomic.Bool
	if err := database.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		if _, isResetCAS := updates["recovery_code_hash"]; !isResetCAS {
			return
		}
		if _, isResetCAS := updates["password_hash"]; !isResetCAS || !fired.CompareAndSwap(false, true) {
			return
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE users SET password_hash = ? WHERE id = ?", winnerHash, user.ID).Error; err != nil {
			t.Errorf("simulate the winning redeem: %v", err)
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	t.Cleanup(func() { _ = database.Callback().Update().Remove(callbackName) })

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")

	if !fired.Load() {
		t.Fatal("anchor: the redeem never reached the compare-and-swap write")
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
