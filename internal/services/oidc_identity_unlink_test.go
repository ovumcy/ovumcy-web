package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

func newUnlinkTestService(loginMode security.OIDCLoginMode, identities *stubOIDCIdentityStore) *OIDCLoginService {
	return NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config:  security.OIDCConfig{Enabled: true, LoginMode: loginMode},
	}, identities, &stubOIDCUserStore{}, nil)
}

func unlinkTestOwner(withPassword bool) models.User {
	user := models.User{ID: 7, Role: models.RoleOwner}
	if withPassword {
		user.LocalAuthEnabled = true
		user.PasswordHash = "$2a$10$placeholderplaceholderplaceholderplaceholderplaceholde"
	}
	return user
}

func TestOIDCUnlinkIdentityRemovesTheOwnersIdentityAndRevokesSessions(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{listed: []models.OIDCIdentity{
		{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "a"},
	}}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)

	if _, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(true), 1); err != nil {
		t.Fatalf("UnlinkIdentity() unexpected error: %v", err)
	}
	if identities.deletedID != 1 || identities.deleteCalls != 1 {
		t.Fatalf("expected exactly one revoking delete of identity 1, got calls=%d id=%d", identities.deleteCalls, identities.deletedID)
	}
}

// An identity id belonging to another owner reads exactly like a missing one,
// and nothing is deleted: the id from the request is always combined with the
// session's user id.
func TestOIDCUnlinkIdentityRefusesAnotherOwnersIdentity(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{listed: []models.OIDCIdentity{
		{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "mine"},
		{ID: 2, UserID: 8, Issuer: "https://id.example.com", Subject: "theirs"},
	}}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)

	for _, identityID := range []uint{2, 0, 99} {
		_, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(true), identityID)
		if !errors.Is(err, ErrOIDCIdentityNotFound) {
			t.Fatalf("identity %d: expected ErrOIDCIdentityNotFound, got %v", identityID, err)
		}
	}
	if identities.deleteCalls != 0 {
		t.Fatalf("expected no delete for a foreign, zero or missing id, got %d", identities.deleteCalls)
	}
}

// The account must keep a way in. The last identity goes only when local
// password sign-in is both set up and allowed on this instance.
func TestOIDCUnlinkIdentityRefusesToRemoveTheLastSignInMethod(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mode         security.OIDCLoginMode
		withPassword bool
		want         error
	}{
		"no local password":                 {mode: security.OIDCLoginModeHybrid, withPassword: false, want: ErrOIDCUnlinkLastSignIn},
		"password but local sign-in closed": {mode: security.OIDCLoginModeOIDCOnly, withPassword: true, want: ErrOIDCUnlinkLastSignIn},
		"password and local sign-in open":   {mode: security.OIDCLoginModeHybrid, withPassword: true, want: nil},
	} {
		identities := &stubOIDCIdentityStore{listed: []models.OIDCIdentity{
			{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "only"},
		}}
		service := newUnlinkTestService(tc.mode, identities)
		_, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(tc.withPassword), 1)
		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Fatalf("%s: expected %v, got %v", name, tc.want, err)
		}
		wantDeletes := 0
		if tc.want == nil {
			wantDeletes = 1
		}
		if identities.deleteCalls != wantDeletes {
			t.Fatalf("%s: expected %d deletes, got %d", name, wantDeletes, identities.deleteCalls)
		}
	}
}

// With a second identity left, an OIDC-only account may drop one of them.
func TestOIDCUnlinkIdentityAllowsRemovingOneOfTwoWithoutAPassword(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{listed: []models.OIDCIdentity{
		{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "a"},
		{ID: 2, UserID: 7, Issuer: "https://id.example.com", Subject: "b"},
	}}
	service := newUnlinkTestService(security.OIDCLoginModeOIDCOnly, identities)
	if _, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(false), 2); err != nil {
		t.Fatalf("UnlinkIdentity() unexpected error: %v", err)
	}
	if identities.deletedID != 2 {
		t.Fatalf("expected identity 2 deleted, got %d", identities.deletedID)
	}
}

// The service's own read can be stale: a concurrent unlink of the other
// identity commits between it and the delete. The store's in-transaction
// refusal must surface as the same last-sign-in error, not as a storage fault,
// and the store must be told whether this instance accepts password sign-in.
func TestOIDCUnlinkIdentitySurfacesTheStoresLastSignInRefusal(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mode     security.OIDCLoginMode
		wantOpen bool
	}{
		"oidc_only": {mode: security.OIDCLoginModeOIDCOnly, wantOpen: false},
		"hybrid":    {mode: security.OIDCLoginModeHybrid, wantOpen: true},
	} {
		identities := &stubOIDCIdentityStore{
			listed: []models.OIDCIdentity{
				{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "a"},
				{ID: 2, UserID: 7, Issuer: "https://id.example.com", Subject: "b"},
			},
			deleteErr: models.ErrOIDCUnlinkLastSignIn,
		}
		service := newUnlinkTestService(tc.mode, identities)
		if _, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(false), 2); !errors.Is(err, ErrOIDCUnlinkLastSignIn) {
			t.Fatalf("%s: expected ErrOIDCUnlinkLastSignIn from the store's refusal, got %v", name, err)
		}
		if identities.deleteLocalSignInOpen != tc.wantOpen {
			t.Fatalf("%s: expected localSignInOpen=%v passed to the store, got %v", name, tc.wantOpen, identities.deleteLocalSignInOpen)
		}
	}
}

// A disabled OIDC provider refuses unlink before touching storage.
func TestOIDCUnlinkIdentityRequiresEnabledProvider(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)
	if _, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(true), 1); !errors.Is(err, ErrOIDCDisabled) {
		t.Fatalf("expected ErrOIDCDisabled, got %v", err)
	}
}

// A storage fault resolving the owner's identities, or deleting the named
// one, surfaces as ErrOIDCIdentityResolveFailed — the caller must not need to
// know the store's own error types, only that the unlink did not happen.
func TestOIDCUnlinkIdentityMapsStorageFaultsToResolveFailed(t *testing.T) {
	t.Parallel()

	listFault := &stubOIDCIdentityStore{listErr: errors.New("db fault")}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, listFault)
	if _, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(true), 1); !errors.Is(err, ErrOIDCIdentityResolveFailed) {
		t.Fatalf("list fault: expected ErrOIDCIdentityResolveFailed, got %v", err)
	}

	deleteFault := &stubOIDCIdentityStore{
		listed:    []models.OIDCIdentity{{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "a"}},
		deleteErr: errors.New("db fault"),
	}
	service = newUnlinkTestService(security.OIDCLoginModeHybrid, deleteFault)
	if _, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(true), 1); !errors.Is(err, ErrOIDCIdentityResolveFailed) {
		t.Fatalf("delete fault: expected ErrOIDCIdentityResolveFailed, got %v", err)
	}
}

// A delete that finds nothing — the identity vanished between the
// owner-scoped read above and the delete itself — reports the same not-found
// the caller sees for a foreign or missing id, never a silent success.
func TestOIDCUnlinkIdentityReportsNotFoundWhenTheDeleteLosesARace(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{
		listed:         []models.OIDCIdentity{{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "a"}},
		deleteNotFound: true,
	}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)
	if _, err := service.UnlinkIdentity(context.Background(), unlinkTestOwner(true), 1); !errors.Is(err, ErrOIDCIdentityNotFound) {
		t.Fatalf("expected ErrOIDCIdentityNotFound for a delete that finds nothing, got %v", err)
	}
}

// A storage fault listing identities surfaces the same way to the settings
// page as it does to unlink: ErrOIDCIdentityResolveFailed, not a raw DB error.
func TestOIDCListLinkedIdentitiesMapsStorageFaultToResolveFailed(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{listErr: errors.New("db fault")}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)
	if _, err := service.ListLinkedIdentities(context.Background(), 7); !errors.Is(err, ErrOIDCIdentityResolveFailed) {
		t.Fatalf("expected ErrOIDCIdentityResolveFailed, got %v", err)
	}
}

// A disabled OIDC provider refuses a link confirmation before touching storage.
func TestOIDCConfirmAndLinkIdentityRequiresEnabledProvider(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{}
	service := NewOIDCLoginService(&stubOIDCProviderClient{}, identities, &stubOIDCUserStore{}, nil)
	claims := security.OIDCClaims{Issuer: "https://id.example.com", Subject: "fresh-sub"}
	if _, err := service.ConfirmAndLinkIdentity(context.Background(), 7, 1, claims, time.Now()); !errors.Is(err, ErrOIDCDisabled) {
		t.Fatalf("expected ErrOIDCDisabled, got %v", err)
	}
	if identities.lastSubject != "" {
		t.Fatalf("expected no identity lookup, got one for subject %q", identities.lastSubject)
	}
}

// A storage fault resolving whether the pair is already linked surfaces as
// ErrOIDCIdentityResolveFailed and links nothing.
func TestOIDCConfirmAndLinkIdentityMapsALookupFaultToResolveFailed(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{findErr: errors.New("db fault")}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)
	claims := security.OIDCClaims{Issuer: "https://id.example.com", Subject: "fresh-sub"}
	if _, err := service.ConfirmAndLinkIdentity(context.Background(), 7, 1, claims, time.Now()); !errors.Is(err, ErrOIDCIdentityResolveFailed) {
		t.Fatalf("expected ErrOIDCIdentityResolveFailed, got %v", err)
	}
	if identities.createCallSeen {
		t.Fatal("expected no link write after a failed lookup")
	}
}

// A storage fault persisting the confirmed link surfaces as ErrOIDCLinkFailed,
// the same verdict a lost unique-constraint race reports: the caller cannot
// tell the two apart and must not need to.
func TestOIDCConfirmAndLinkIdentitySurfacesAStorageFaultAsLinkFailed(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{createErr: errors.New("db fault")}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)
	claims := security.OIDCClaims{Issuer: "https://id.example.com", Subject: "fresh-sub"}
	if _, err := service.ConfirmAndLinkIdentity(context.Background(), 7, 1, claims, time.Now()); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed, got %v", err)
	}
}

// linkIdentity is the auto-provision sign-in path's persistence step. Its own
// zero-owner and missing-claim-key guards mirror checks Authenticate and
// resolveUserForClaims already make before calling it, but stay here as the
// function's own floor rather than trusting the caller.
func TestOIDCLinkIdentityRefusesAZeroOwnerOrAMissingClaimKey(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)
	valid := security.OIDCClaims{Issuer: "https://id.example.com", Subject: "sub"}

	if err := service.linkIdentity(context.Background(), 0, valid, time.Now()); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("zero owner: expected ErrOIDCLinkFailed, got %v", err)
	}
	if err := service.linkIdentity(context.Background(), 7, security.OIDCClaims{Issuer: "https://id.example.com"}, time.Now()); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("missing subject: expected ErrOIDCLinkFailed, got %v", err)
	}
	if identities.createCallSeen {
		t.Fatal("did not expect Create() for either refused call")
	}
}

func TestOIDCListLinkedIdentitiesIsOwnerScoped(t *testing.T) {
	t.Parallel()

	linkedAt := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{listed: []models.OIDCIdentity{
		{ID: 1, UserID: 7, Issuer: "https://id.example.com", Subject: "a", CreatedAt: linkedAt},
		{ID: 2, UserID: 8, Issuer: "https://id.example.com", Subject: "b", CreatedAt: linkedAt},
	}}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)

	linked, err := service.ListLinkedIdentities(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListLinkedIdentities() unexpected error: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != 1 || !linked[0].LinkedAt.Equal(linkedAt) {
		t.Fatalf("expected only the owner's identity, got %+v", linked)
	}
	if none, _ := service.ListLinkedIdentities(context.Background(), 0); len(none) != 0 {
		t.Fatalf("a zero user id must list nothing, got %+v", none)
	}
}

// An explicit link to an existing account bumps AuthSessionVersion in the
// same write: the store call that persists it must be the revoking one.
func TestOIDCConfirmAndLinkIdentityRevokesSessionsInTheSameWrite(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{}
	service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)
	claims := security.OIDCClaims{Issuer: "https://id.example.com", Subject: "fresh-sub"}
	if _, err := service.ConfirmAndLinkIdentity(context.Background(), 7, 1, claims, time.Now()); err != nil {
		t.Fatalf("ConfirmAndLinkIdentity() unexpected error: %v", err)
	}
	if !identities.revokedOnCreate {
		t.Fatal("expected the link to be written through CreateAndRevokeSessions")
	}
}

// A blank subject or issuer identifies nobody: it is refused before any lookup
// or write, on the link path and on sign-in.
func TestOIDCBlankSubjectIsRefusedAtLinkAndSignIn(t *testing.T) {
	t.Parallel()

	for name, claims := range map[string]security.OIDCClaims{
		"empty subject": {Issuer: "https://id.example.com", Subject: "", Email: "owner@example.com", EmailVerified: true},
		"blank subject": {Issuer: "https://id.example.com", Subject: "  ", Email: "owner@example.com", EmailVerified: true},
		"empty issuer":  {Issuer: "", Subject: "sub", Email: "owner@example.com", EmailVerified: true},
	} {
		identities := &stubOIDCIdentityStore{}
		service := newUnlinkTestService(security.OIDCLoginModeHybrid, identities)
		if _, err := service.ConfirmAndLinkIdentity(context.Background(), 7, 1, claims, time.Now()); !errors.Is(err, ErrOIDCLinkFailed) {
			t.Fatalf("%s: link expected ErrOIDCLinkFailed, got %v", name, err)
		}

		client := &stubOIDCProviderClient{
			enabled:  true,
			config:   security.OIDCConfig{Enabled: true, LoginMode: security.OIDCLoginModeHybrid, AutoProvision: true},
			exchange: security.OIDCExchangeResult{Claims: claims},
		}
		signIn := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)
		if _, err := signIn.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now()); !errors.Is(err, ErrOIDCAuthenticationFailed) {
			t.Fatalf("%s: sign-in expected ErrOIDCAuthenticationFailed, got %v", name, err)
		}
		if identities.createCallSeen || identities.lastSubject != "" || identities.lastIssuer != "" {
			t.Fatalf("%s: expected no lookup or write, got create=%v lookup=(%q,%q)", name, identities.createCallSeen, identities.lastIssuer, identities.lastSubject)
		}
	}
}
