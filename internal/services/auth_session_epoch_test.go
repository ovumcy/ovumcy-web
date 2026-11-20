package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// testSessionEpoch and otherSessionEpoch are well-formed epochs (43 base64url
// characters, 32 bytes) the tests hand to services and tokens.
const (
	testSessionEpoch  = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	otherSessionEpoch = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
)

var epochTestSecret = []byte("0123456789abcdef0123456789abcdef")

type epochTestUsers struct {
	AuthUserRepository
	user models.User
}

func (users epochTestUsers) FindByIDOptional(_ context.Context, id uint) (models.User, bool, error) {
	if id != users.user.ID {
		return models.User{}, false, nil
	}
	return users.user, true, nil
}

func newEpochTestService(epoch string) *AuthService {
	service := NewAuthService(epochTestUsers{user: models.User{ID: 7, Role: models.RoleOwner, AuthSessionVersion: 1, LocalAuthEnabled: true}})
	service.ConfigureSessionEpoch(epoch)
	return service
}

type epochTestStore struct {
	values map[string]string
	getErr error
	setErr error
	racer  string
}

func (store *epochTestStore) Get(_ context.Context, key string) (string, bool, error) {
	value, found := store.values[key]
	return value, found, store.getErr
}

// SetIfAbsent models the repository's insert-if-absent. racer, when set, is a
// value another process commits between this boot's read and its insert.
func (store *epochTestStore) SetIfAbsent(_ context.Context, key string, value string) (bool, error) {
	if store.setErr != nil {
		return false, store.setErr
	}
	if store.racer != "" {
		store.values[key] = store.racer
	}
	if _, exists := store.values[key]; exists {
		return false, nil
	}
	store.values[key] = value
	return true, nil
}

func (store *epochTestStore) Set(_ context.Context, key string, value string) error {
	if store.setErr != nil {
		return store.setErr
	}
	store.values[key] = value
	return nil
}

func TestResolveAuthSessionAdmitsATokenMintedUnderTheEpochInForce(t *testing.T) {
	service := newEpochTestService(testSessionEpoch)
	token, _, err := service.BuildAuthSessionTokenWithSessionID(epochTestSecret, 7, models.RoleOwner, 1, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	user, claims, err := service.ResolveAuthSession(t.Context(), epochTestSecret, token, time.Now())
	if err != nil || user == nil || claims.Epoch != testSessionEpoch {
		t.Fatalf("a token under the epoch in force must resolve, got user %v, claims %+v, err %v", user, claims, err)
	}
}

func TestResolveAuthSessionRefusesATokenWithoutTheEpochClaim(t *testing.T) {
	service := newEpochTestService(testSessionEpoch)
	now := time.Now()
	claims := AuthSessionClaims{
		UserID: 7, Role: models.RoleOwner, SessionVersion: 1, SessionID: "legacy-session",
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), IssuedAt: jwt.NewNumericDate(now)},
	}
	token, err := signAuthSessionClaims(epochTestSecret, &claims)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, _, err := service.ResolveAuthSession(t.Context(), epochTestSecret, token, now); !errors.Is(err, ErrAuthSessionTokenInvalid) {
		t.Fatalf("a token without the ep claim must be refused at the parse as invalid, got %v", err)
	}
}

func TestResolveAuthSessionRefusesATokenFromAnotherEpoch(t *testing.T) {
	minted := newEpochTestService(otherSessionEpoch)
	token, _, err := minted.BuildAuthSessionTokenWithSessionID(epochTestSecret, 7, models.RoleOwner, 1, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	serving := newEpochTestService(testSessionEpoch)
	if _, _, err := serving.ResolveAuthSession(t.Context(), epochTestSecret, token, time.Now()); !errors.Is(err, ErrAuthSessionTokenRevoked) {
		t.Fatalf("a token minted under another epoch must be refused as revoked, got %v", err)
	}
}

func TestAuthServiceWithoutASessionEpochMintsAndResolvesNothing(t *testing.T) {
	configured := newEpochTestService(testSessionEpoch)
	token, _, err := configured.BuildAuthSessionTokenWithSessionID(epochTestSecret, 7, models.RoleOwner, 1, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	unconfigured := newEpochTestService("")
	if _, _, err := unconfigured.BuildAuthSessionTokenWithSessionID(epochTestSecret, 7, models.RoleOwner, 1, time.Hour, time.Now()); !errors.Is(err, ErrAuthSessionEpochUnavailable) {
		t.Fatalf("minting without an epoch must fail closed, got %v", err)
	}
	if _, err := unconfigured.BuildPasswordResetToken(epochTestSecret, 7, "$2a$10$testhashvaluefortokenclaims", 1, PasswordResetTokenPurposeRecovery, time.Hour, time.Now()); !errors.Is(err, ErrAuthSessionEpochUnavailable) {
		t.Fatalf("minting a reset grant without an epoch must fail closed, got %v", err)
	}
	if _, _, err := unconfigured.ResolveAuthSession(t.Context(), epochTestSecret, token, time.Now()); !errors.Is(err, ErrAuthSessionTokenRevoked) {
		t.Fatalf("resolving without an epoch must refuse, got %v", err)
	}
	if _, err := unconfigured.SessionEpoch(); !errors.Is(err, ErrAuthSessionEpochUnavailable) {
		t.Fatalf("SessionEpoch without a configured epoch must fail, got %v", err)
	}
	if unconfigured.SessionEpochCurrent("") || unconfigured.SessionEpochCurrent(testSessionEpoch) {
		t.Fatal("an unconfigured service must never call an epoch current, an empty claim included")
	}
	if _, _, err := BuildAuthSessionTokenWithVersionAndSessionID(epochTestSecret, 7, models.RoleOwner, 1, "", time.Hour, time.Now()); !errors.Is(err, ErrAuthSessionEpochUnavailable) {
		t.Fatalf("the package builder must refuse an empty epoch, got %v", err)
	}
}

func TestParsePasswordResetGrantRefusesAMissingOrMismatchedEpoch(t *testing.T) {
	hash := "$2a$10$testhashvaluefortokenclaims"
	now := time.Now()
	mint := func(epoch string) string {
		token, err := BuildPasswordResetToken(epochTestSecret, 7, hash, 1, epoch, PasswordResetTokenPurposeRecovery, time.Hour, now)
		if err != nil {
			t.Fatalf("mint grant: %v", err)
		}
		return token
	}
	service := newEpochTestService(testSessionEpoch)
	if _, err := service.ParsePasswordResetGrant(epochTestSecret, mint(testSessionEpoch), now); err != nil {
		t.Fatalf("a grant under the epoch in force must parse, got %v", err)
	}
	if _, err := service.ParsePasswordResetGrant(epochTestSecret, mint(otherSessionEpoch), now); !errors.Is(err, ErrPasswordResetTokenInvalidSessionEpoch) {
		t.Fatalf("a grant from another epoch must be refused, got %v", err)
	}

	claims := PasswordResetClaims{
		UserID: 7, Purpose: PasswordResetTokenPurposeRecovery, PasswordState: PasswordStateFingerprint(hash), SessionVersion: 1,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), IssuedAt: jwt.NewNumericDate(now)},
	}
	legacy, err := signPasswordResetClaims(epochTestSecret, &claims)
	if err != nil {
		t.Fatalf("sign legacy grant: %v", err)
	}
	if _, err := ParsePasswordResetToken(epochTestSecret, legacy, now); !errors.Is(err, ErrPasswordResetTokenInvalidSessionEpoch) {
		t.Fatalf("a grant without the ep claim must be refused at parse, got %v", err)
	}
	if _, err := service.ParsePasswordResetGrant(epochTestSecret, legacy, now); !errors.Is(err, ErrPasswordResetTokenInvalidSessionEpoch) {
		t.Fatalf("a grant without the ep claim must be refused, got %v", err)
	}
}

func TestLoadAuthSessionEpochCreatesTheKeyOnTheFirstBoot(t *testing.T) {
	store := &epochTestStore{values: map[string]string{}}
	epoch, created, err := LoadAuthSessionEpoch(t.Context(), store)
	if err != nil || !created || !security.ValidAuthSessionEpoch(epoch) {
		t.Fatalf("a boot with no key must create a well-formed epoch, got %q created=%t err=%v", epoch, created, err)
	}
	if store.values[models.AppStateKeyAuthSessionEpoch] != epoch {
		t.Fatalf("the created epoch must be stored, got %q", store.values[models.AppStateKeyAuthSessionEpoch])
	}
	again, created, err := LoadAuthSessionEpoch(t.Context(), store)
	if err != nil || created || again != epoch {
		t.Fatalf("a later boot must keep the stored epoch, got %q created=%t err=%v", again, created, err)
	}
}

// Two first boots converge on one stored value: the one that lost the insert
// serves the winner's epoch, not the one it generated.
func TestLoadAuthSessionEpochServesTheWinnerOfAFirstBootRace(t *testing.T) {
	store := &epochTestStore{values: map[string]string{}, racer: otherSessionEpoch}
	epoch, created, err := LoadAuthSessionEpoch(t.Context(), store)
	if err != nil || created || epoch != otherSessionEpoch {
		t.Fatalf("the losing boot must serve the stored winner, got %q created=%t err=%v", epoch, created, err)
	}
	if store.values[models.AppStateKeyAuthSessionEpoch] != otherSessionEpoch {
		t.Fatalf("the winner must stay stored, got %q", store.values[models.AppStateKeyAuthSessionEpoch])
	}
}

func TestLoadAuthSessionEpochReplacesAMalformedStoredValueAndFailsOnStorageErrors(t *testing.T) {
	for _, stored := range []string{"", "short", "not base64 url !!!"} {
		store := &epochTestStore{values: map[string]string{models.AppStateKeyAuthSessionEpoch: stored}}
		epoch, created, err := LoadAuthSessionEpoch(t.Context(), store)
		if err != nil || !created || !security.ValidAuthSessionEpoch(epoch) || epoch == stored {
			t.Fatalf("stored %q must be replaced by a well-formed epoch, got %q created=%t err=%v", stored, epoch, created, err)
		}
	}
	if _, _, err := LoadAuthSessionEpoch(t.Context(), &epochTestStore{values: map[string]string{}, getErr: errors.New("read refused")}); err == nil {
		t.Fatal("a failed read must fail the load")
	}
	if _, _, err := LoadAuthSessionEpoch(t.Context(), &epochTestStore{values: map[string]string{}, setErr: errors.New("write refused")}); err == nil {
		t.Fatal("a failed store must fail the load, never serve an unstored epoch")
	}
}

func TestCalendarFeedRestoreFenceHandsEachRestoreBootAFreshSessionEpoch(t *testing.T) {
	var epochs []string
	for range 2 {
		appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation"}}
		users := &stubFenceUserStore{}
		anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
		outcome, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background())
		if err != nil || !outcome.ContinuityBroken {
			t.Fatalf("expected a restore boot, got %+v, %v", outcome, err)
		}
		if len(users.revokeEpochs) != 1 || !security.ValidAuthSessionEpoch(users.revokeEpochs[0]) {
			t.Fatalf("a restore boot must hand the revocation one well-formed epoch, got %v", users.revokeEpochs)
		}
		epochs = append(epochs, users.revokeEpochs[0])
	}
	if epochs[0] == epochs[1] {
		t.Fatal("each restore boot must rotate to a new epoch")
	}

	appState := &stubFenceAppState{values: map[string]string{}}
	users := &stubFenceUserStore{}
	if _, err := NewCalendarFeedRestoreFence(appState, users, &stubFenceAnchor{}).Enforce(context.Background()); err != nil || len(users.revokeEpochs) != 0 {
		t.Fatalf("a first boot is not a restore and must not rotate, got %v, %v", users.revokeEpochs, err)
	}
}

func TestAuthSessionEpochsEqualRefusesAnEmptyOperand(t *testing.T) {
	for _, pair := range [][2]string{{"", ""}, {"", testSessionEpoch}, {testSessionEpoch, ""}, {testSessionEpoch, otherSessionEpoch}} {
		if security.AuthSessionEpochsEqual(pair[0], pair[1]) {
			t.Fatalf("%q vs %q must not be equal", pair[0], pair[1])
		}
	}
	if !security.AuthSessionEpochsEqual(testSessionEpoch, testSessionEpoch) {
		t.Fatal("the same epoch must be equal")
	}
}
