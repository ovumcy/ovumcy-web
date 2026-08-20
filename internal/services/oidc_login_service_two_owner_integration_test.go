package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"gorm.io/gorm"
)

const (
	twoOwnerIssuerA = "https://id-a.example.com"
	twoOwnerIssuerB = "https://id-b.example.com"
	// One subject, two providers. The unique index is on (issuer, subject)
	// (migration 014), so this is a shape a real deployment can hold: two
	// self-hosting owners whose IdPs each mint the same opaque subject — "1",
	// "user", an email local part. Resolution that drops the issuer predicate
	// then hands one owner the other's account.
	twoOwnerSharedSubject = "shared-subject"
)

// TestOIDCLoginServiceResolvesTheActingOwnersIdentity drives OIDC login and the
// erasure step-up re-auth through the REAL identity and user repositories with
// two owners on one database.
//
// The stub assertions elsewhere in this package observe which (issuer, subject)
// the service FORWARDS. They cannot observe what the store does with it: a
// FindByIssuerSubject whose WHERE clause lost its issuer predicate is handed
// exactly the right arguments and still returns the wrong row, and the db-layer
// test seeds a single identity, so nothing in the tree sees that today. Here
// each owner has an identity under a different issuer with the SAME subject,
// and owner A is created first — so it holds id 1 and its identity sorts first,
// which is what a lookup degraded to subject-only would return.
//
// The IdP itself stays a double: only the code exchange is replaced, and the
// property under test is which stored identity the claims resolve to, not how
// the claims were obtained.
func TestOIDCLoginServiceResolvesTheActingOwnersIdentity(t *testing.T) {
	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-oidc-two-owner")
	repositories := db.NewRepositories(database)

	ownerA := createTwoOwnerUser(t, database, "oidc-owner-a@example.com", nil)
	ownerB := createTwoOwnerUser(t, database, "oidc-owner-b@example.com", nil)
	requireDistinctTwoOwnerFixture(t, ownerA, ownerB)

	identityA := createTwoOwnerOIDCIdentity(t, database, ownerA.ID, twoOwnerIssuerA, twoOwnerSharedSubject)
	identityB := createTwoOwnerOIDCIdentity(t, database, ownerB.ID, twoOwnerIssuerB, twoOwnerSharedSubject)

	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)

	// Login as owner B must resolve owner B's identity, not the same-subject
	// identity owner A holds at another provider.
	serviceB := newTwoOwnerOIDCLoginService(t, repositories, twoOwnerIssuerB, ownerB.Email, now)
	resultB, err := serviceB.Authenticate(context.Background(), "code", "verifier", "nonce", now)
	if err != nil {
		t.Fatalf("Authenticate() as owner B: unexpected error: %v", err)
	}
	if resultB.User.ID != ownerB.ID {
		t.Fatalf("login under issuer %q resolved owner %d, want owner %d — the identity lookup is not scoped by issuer", twoOwnerIssuerB, resultB.User.ID, ownerB.ID)
	}
	if resultB.NewlyLinked {
		t.Fatal("did not expect an existing identity link to be created again")
	}

	// Positive anchor: the same flow under owner A's issuer must resolve owner
	// A, so the assertion above cannot be satisfied by a lookup that resolves
	// nobody or always the caller.
	serviceA := newTwoOwnerOIDCLoginService(t, repositories, twoOwnerIssuerA, ownerA.Email, now)
	resultA, err := serviceA.Authenticate(context.Background(), "code", "verifier", "nonce", now)
	if err != nil {
		t.Fatalf("Authenticate() as owner A: unexpected error: %v", err)
	}
	if resultA.User.ID != ownerA.ID {
		t.Fatalf("login under issuer %q resolved owner %d, want owner %d", twoOwnerIssuerA, resultA.User.ID, ownerA.ID)
	}

	// The erasure step-up: owner B's re-auth must accept only owner B's own
	// identity at owner B's own issuer.
	if err := serviceB.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", ownerB.ID, 5*time.Minute, now); err != nil {
		t.Fatalf("ValidateReauthExchange() for owner B: unexpected error: %v", err)
	}
	if err := serviceA.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", ownerB.ID, 5*time.Minute, now); !errors.Is(err, ErrOIDCReauthIdentityMismatch) {
		t.Fatalf("a step-up under owner A's issuer must not satisfy owner B's re-auth; got %v", err)
	}

	// The touch is the one write on this path: it must land on the acting
	// owner's identity row and leave the other owner's alone.
	if readTwoOwnerOIDCIdentity(t, database, identityB.ID).LastUsedAt == nil {
		t.Fatal("expected owner B's identity to be touched by their own login")
	}
	if touched := readTwoOwnerOIDCIdentity(t, database, identityA.ID).LastUsedAt; touched == nil {
		t.Fatal("expected owner A's identity to be touched by their own login")
	}
}

// newTwoOwnerOIDCLoginService wires the real repositories behind a provider
// client that returns fixed, verified claims for one issuer.
func newTwoOwnerOIDCLoginService(t *testing.T, repositories *db.Repositories, issuer string, email string, now time.Time) *OIDCLoginService {
	t.Helper()

	client := &stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        issuer,
				Subject:       twoOwnerSharedSubject,
				Email:         email,
				EmailVerified: true,
				AuthTime:      now.Add(-2 * time.Minute),
			},
		},
	}
	return NewOIDCLoginService(client, repositories.OIDCIdentities, repositories.Users, nil)
}

func createTwoOwnerOIDCIdentity(t *testing.T, database *gorm.DB, userID uint, issuer string, subject string) models.OIDCIdentity {
	t.Helper()

	identity := models.OIDCIdentity{
		UserID:    userID,
		Issuer:    issuer,
		Subject:   subject,
		CreatedAt: time.Now().UTC(),
	}
	if err := database.Create(&identity).Error; err != nil {
		t.Fatalf("create oidc identity for user %d: %v", userID, err)
	}
	return identity
}

func readTwoOwnerOIDCIdentity(t *testing.T, database *gorm.DB, identityID uint) models.OIDCIdentity {
	t.Helper()

	var identity models.OIDCIdentity
	if err := database.First(&identity, identityID).Error; err != nil {
		t.Fatalf("read oidc identity %d: %v", identityID, err)
	}
	return identity
}
