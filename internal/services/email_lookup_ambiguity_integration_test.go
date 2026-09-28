package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// TestWebSignInRefusesAMailboxTwoAccountsShare drives every web path that
// resolves an account by email through the REAL repository, on a database
// whose idx_users_email_normalized is gone — the migration refuses to build it
// over duplicates, so only a drop or restore outside the app leaves this — with
// two owners on one mailbox that differ only in case and padding. Both accounts
// carry the same password and recovery code, so a lookup that took the first
// row would sign the caller in — the refusal below can only come from the
// ambiguity check.
//
// Each path must answer exactly what it answers where nothing is ambiguous —
// login and recovery as for an address nobody holds, with the same bcrypt
// spend; SSO as for a single account the identity is not linked to — and act
// on neither account.
func TestWebSignInRefusesAMailboxTwoAccountsShare(t *testing.T) {
	const (
		password     = "CorrectHorse1!"
		recoveryCode = "OVUM-LEGACY-DUPE-0001"
		shared       = "shared@example.com"
		unknown      = "nobody@example.com"
	)
	ctx := context.Background()
	now := time.Date(2026, time.September, 27, 10, 0, 0, 0, time.UTC)

	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-ambiguous-email")
	if err := database.Exec("DROP INDEX IF EXISTS idx_users_email_normalized").Error; err != nil {
		t.Fatalf("drop the normalized-email index: %v", err)
	}
	passwordHash := mintBcryptHashAtCost(t, password, passwordHashCost)
	codeHash := mintBcryptHashAtCost(t, NormalizeRecoveryCode(recoveryCode), passwordHashCost)
	localAccount := func(user *models.User) {
		user.PasswordHash = passwordHash
		user.RecoveryCodeHash = codeHash
		user.LocalAuthEnabled = true
	}
	first := createTwoOwnerUser(t, database, "Shared@Example.com", localAccount)
	second := createTwoOwnerUser(t, database, " shared@example.com ", localAccount)

	repositories := db.NewRepositories(database)
	if matches, err := repositories.Users.FindAllByNormalizedEmail(ctx, shared); err != nil || len(matches) != 2 {
		t.Fatalf("fixture must hold two accounts on %s, got %d (err=%v)", shared, len(matches), err)
	}
	auth := NewAuthService(repositories.Users)

	t.Run("password sign-in", func(t *testing.T) {
		ledger := withLoginWorkLedger(t)
		_, unknownErr := auth.AuthenticateCredentials(ctx, unknown, password)
		unknownUnits := ledger.drain()

		if !errors.Is(unknownErr, ErrAuthInvalidCreds) {
			t.Fatalf("anchor: an unknown address must be refused with ErrAuthInvalidCreds, got %v", unknownErr)
		}

		user, err := auth.AuthenticateCredentials(ctx, shared, password)
		if !errors.Is(err, ErrAuthInvalidCreds) || err.Error() != unknownErr.Error() || user.ID != 0 {
			t.Fatalf("an ambiguous address must be refused as an unknown one (%v), got user %d, err %v", unknownErr, user.ID, err)
		}
		if units := ledger.drain(); units != unknownUnits {
			t.Fatalf("the ambiguous refusal spends %d bcrypt units, an unknown address %d: the difference tells the duplicate apart", units, unknownUnits)
		}
	})

	t.Run("recovery reset", func(t *testing.T) {
		ledger := &bcryptWorkLedger{}
		withEqualizerCompareLedger(t, ledger)
		withTopUpCompareLedger(t, ledger)
		_, unknownErr := auth.FindUserByEmailRecoveryCodeAndPassword(ctx, unknown, recoveryCode, password)
		unknownUnits := ledger.drain()

		if !errors.Is(unknownErr, ErrRecoveryCodeNotFound) {
			t.Fatalf("anchor: an unknown address must be refused with ErrRecoveryCodeNotFound, got %v", unknownErr)
		}

		user, err := auth.FindUserByEmailRecoveryCodeAndPassword(ctx, shared, recoveryCode, password)
		if !errors.Is(err, ErrRecoveryCodeNotFound) || err.Error() != unknownErr.Error() || user != nil {
			t.Fatalf("an ambiguous address must be refused as an unknown one (%v), got user %v, err %v", unknownErr, user, err)
		}
		if units := ledger.drain(); units != unknownUnits {
			t.Fatalf("the ambiguous refusal spends %d bcrypt units, an unknown address %d: the difference tells the duplicate apart", units, unknownUnits)
		}
	})

	provisioner := &stubOIDCAutoProvisioner{}
	ssoAs := func(email string) (OIDCLoginResult, error) {
		client := &stubOIDCProviderClient{
			enabled: true,
			config:  security.OIDCConfig{Enabled: true, AutoProvision: true},
			exchange: security.OIDCExchangeResult{Claims: security.OIDCClaims{
				Issuer:        twoOwnerIssuerA,
				Subject:       "fresh-subject-" + email,
				Email:         email,
				EmailVerified: true,
				AuthTime:      now.Add(-time.Minute),
			}},
		}
		return NewOIDCLoginService(client, repositories.OIDCIdentities, repositories.Users, provisioner).Authenticate(ctx, "code", "verifier", "nonce", now)
	}

	t.Run("sso sign-in", func(t *testing.T) {
		solo := createTwoOwnerUser(t, database, "solo@example.com", nil)
		_, soloErr := ssoAs(solo.Email)
		if !errors.Is(soloErr, ErrOIDCLinkRequiresConfirmation) {
			t.Fatalf("anchor: a single unlinked account must be refused for confirmation, got %v", soloErr)
		}
		result, err := ssoAs(shared)
		if !errors.Is(err, ErrOIDCLinkRequiresConfirmation) || result.User.ID != 0 || result.PendingLinkClaims != nil {
			t.Fatalf("an ambiguous address must be refused as a single unlinked account is (%v), got user %d, err %v", soloErr, result.User.ID, err)
		}
		if provisioner.called {
			t.Fatal("an ambiguous address must never auto-provision a third account beside the two")
		}
		var linked int64
		if err := database.Model(&models.OIDCIdentity{}).Count(&linked).Error; err != nil || linked != 0 {
			t.Fatalf("no identity may be linked to either account, found %d (err=%v)", linked, err)
		}
	})

	// Positive anchor: move the second row off the mailbox and the same inputs
	// resolve the first account on every path, so the refusals above came from
	// the ambiguity and not from a fixture no path could accept.
	if err := database.Model(&models.User{}).Where("id = ?", second.ID).Update("email", "moved@example.com").Error; err != nil {
		t.Fatalf("disambiguate the fixture: %v", err)
	}
	if user, err := auth.AuthenticateCredentials(ctx, shared, password); err != nil || user.ID != first.ID {
		t.Fatalf("anchor: the now-unique address must sign in account %d, got %d (err=%v)", first.ID, user.ID, err)
	}
	if user, err := auth.FindUserByEmailRecoveryCodeAndPassword(ctx, shared, recoveryCode, password); err != nil || user == nil || user.ID != first.ID {
		t.Fatalf("anchor: the now-unique address must resolve account %d for recovery, got %v (err=%v)", first.ID, user, err)
	}
	if result, err := ssoAs(shared); !errors.Is(err, ErrOIDCLinkRequiresConfirmation) || result.User.ID != first.ID {
		t.Fatalf("anchor: SSO on the now-unique address must offer account %d for link confirmation, got %d (err=%v)", first.ID, result.User.ID, err)
	}
}
