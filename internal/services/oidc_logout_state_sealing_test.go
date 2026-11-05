package services

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// WEB-489: the id_token_hint an oidc_logout_states row keeps for up to seven
// days is sealed under the instance secret and bound to the row's owner. A row
// that does not open for the owner asking — cleartext from before the change,
// sealed for another owner, or sealed without the owner binding — reads as not
// found, so the sign-out completes locally instead of handing the provider a
// token from the wrong place.

var testOIDCLogoutStateKey = []byte("oidc-logout-state-test-secret-key")

func sealOIDCLogoutHintForTest(t *testing.T, hint string, ownerID uint) string {
	t.Helper()
	sealed, err := security.EncryptField(hint, testOIDCLogoutStateKey, oidcLogoutIDTokenHintAAD(ownerID))
	if err != nil {
		t.Fatalf("seal the fixture hint: %v", err)
	}
	return sealed
}

// The label and a ciphertext sealed under it are pinned together: renaming the
// label, or changing what it binds, makes every live row unreadable at the next
// sign-out. Do not regenerate the fixture to make this pass.
const (
	goldenOIDCLogoutHintKey            = "golden-backcompat-secret-key-0123456789"
	goldenOIDCLogoutHintPlaintext      = "eyJhbGciOiJSUzI1NiJ9.golden.signature"
	goldenOIDCLogoutHintOwner     uint = 42
	goldenOIDCLogoutHintAAD            = "ovumcy.field.oidc_logout_id_token_hint:42"
	goldenOIDCLogoutHintSealed         = "lrgm--EZDVh9PfMzTzbqEOUSbL6wv-ON6ff_FoHofL-P380qdFRgNHU_i_0ixNXatJ4nav8g9IVxiC53aGKlz2o"
)

// The same plaintext sealed in the unbound no-aad format DecryptField still
// opens through its legacy fallback (built with the security package's own
// test helper, which has no production counterpart).
const goldenOIDCLogoutHintUnbound = "GwJK--lsl4pEmB-NT25hCRKgvdmbNSfLKT3MM8h1FN-As8Lotg5dVAeYlothoQXhcuWdYzyJ1o6KGMjtsoSYWv0"

// An unbound value opens under the right key for any user_id, so accepting it
// would drop the owner binding. It is refused, and the refusal is logged
// without the session id or the stored value. Not parallel: it captures the
// process-wide log output.
func TestOIDCLogoutStateServiceRefusesAnUnboundHintAndLogsTheDrop(t *testing.T) {
	key := []byte(goldenOIDCLogoutHintKey)
	if _, isLegacy, err := security.DecryptField(goldenOIDCLogoutHintUnbound, key, oidcLogoutIDTokenHintAAD(goldenOIDCLogoutHintOwner)); err != nil || !isLegacy {
		t.Fatalf("the fixture must open through the unbound fallback, or this test proves nothing (isLegacy=%t, err=%v)", isLegacy, err)
	}

	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(original) })

	const sessionID = "sess-unbound-hint"
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:             sessionID,
			UserID:                goldenOIDCLogoutHintOwner,
			EndSessionEndpoint:    "https://id.example.com/logout",
			IDTokenHint:           goldenOIDCLogoutHintUnbound,
			PostLogoutRedirectURL: "https://app.example.com/login",
			ExpiresAt:             time.Now().Add(time.Hour).UTC(),
		},
	}
	got, found, err := NewOIDCLogoutStateService(store, key).Load(context.Background(), sessionID, goldenOIDCLogoutHintOwner, time.Now())
	if err != nil || found || got.IDTokenHint != "" {
		t.Fatalf("an unbound hint must read as not found (found=%t, err=%v, hint=%q)", found, err, got.IDTokenHint)
	}
	if store.deleteByIDCalls != 1 {
		t.Fatalf("the unbound row was not dropped (calls=%d)", store.deleteByIDCalls)
	}

	logged := output.String()
	if !strings.Contains(logged, "id_token_hint did not open") || !strings.Contains(logged, "unbound=true") {
		t.Fatalf("the drop was not logged as an unbound value: %q", logged)
	}
	for _, secret := range []string{sessionID, goldenOIDCLogoutHintUnbound, goldenOIDCLogoutHintPlaintext} {
		if strings.Contains(logged, secret) {
			t.Fatalf("the drop log carries %q", secret)
		}
	}
}

func TestOIDCLogoutIDTokenHintAADLabelIsPinned(t *testing.T) {
	t.Parallel()

	if got := string(oidcLogoutIDTokenHintAAD(goldenOIDCLogoutHintOwner)); got != goldenOIDCLogoutHintAAD {
		t.Fatalf("the id_token_hint AAD label changed: got %q, want %q", got, goldenOIDCLogoutHintAAD)
	}
	plaintext, isLegacy, err := security.DecryptField(goldenOIDCLogoutHintSealed, []byte(goldenOIDCLogoutHintKey), oidcLogoutIDTokenHintAAD(goldenOIDCLogoutHintOwner))
	if err != nil || isLegacy {
		t.Fatalf("the golden sealed hint no longer opens under its label (isLegacy=%t, err=%v)", isLegacy, err)
	}
	if plaintext != goldenOIDCLogoutHintPlaintext {
		t.Fatalf("the golden sealed hint opened to %q, want %q", plaintext, goldenOIDCLogoutHintPlaintext)
	}
}

func TestOIDCLogoutStateServiceStoresTheHintSealed(t *testing.T) {
	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-oidc-logout-sealed")
	repositories := db.NewRepositories(database)
	ctx := context.Background()
	now := time.Now().UTC()
	owner := createTwoOwnerUser(t, database, "oidc-logout-sealed@example.com", nil)

	const (
		sessionID = "sess-sealed-hint"
		hint      = "eyJhbGciOiJSUzI1NiJ9.sealed.signature"
	)
	service := NewOIDCLogoutStateService(repositories.OIDCLogout, testOIDCLogoutStateKey)
	if err := service.Save(ctx, sessionID, OIDCLogoutState{
		UserID:                owner.ID,
		EndSessionEndpoint:    "https://id.example.com/logout",
		IDTokenHint:           hint,
		PostLogoutRedirectURL: "https://app.example.com/login",
	}, now); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	var stored models.OIDCLogoutState
	if err := database.Where("session_id = ?", sessionID).First(&stored).Error; err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	if stored.IDTokenHint == "" || stored.IDTokenHint == hint {
		t.Fatalf("the stored id_token_hint column is %q: it must be a sealed value, never the token", stored.IDTokenHint)
	}

	state, found, err := service.Consume(ctx, sessionID, owner.ID, now)
	if err != nil || !found {
		t.Fatalf("the owner must read the sealed hint back (found=%t, err=%v)", found, err)
	}
	if state.IDTokenHint != hint {
		t.Fatalf("Consume() opened the hint to %q, want the original", state.IDTokenHint)
	}

	// A service under another secret is the restored-backup-on-a-new-key case:
	// the row exists, the hint does not open, and the sign-out stays local.
	if err := service.Save(ctx, sessionID, OIDCLogoutState{
		UserID:                owner.ID,
		EndSessionEndpoint:    "https://id.example.com/logout",
		IDTokenHint:           hint,
		PostLogoutRedirectURL: "https://app.example.com/login",
	}, now); err != nil {
		t.Fatalf("re-Save(): %v", err)
	}
	otherKey := NewOIDCLogoutStateService(repositories.OIDCLogout, []byte("a-different-instance-secret-key"))
	if _, found, err := otherKey.Load(ctx, sessionID, owner.ID, now); err != nil || found {
		t.Fatalf("a hint sealed under another secret must read as not found (found=%t, err=%v)", found, err)
	}
}

func TestOIDCLogoutStateServiceTreatsAnUnopenableHintAsNotFound(t *testing.T) {
	t.Parallel()

	const (
		owner uint = 11
		other uint = 12
		hint       = "eyJhbGciOiJSUzI1NiJ9.header.signature"
	)
	cases := []struct {
		name   string
		stored string
	}{
		{name: "cleartext row written before the hint was sealed", stored: hint},
		{name: "hint sealed for another owner", stored: sealOIDCLogoutHintForTest(t, hint, other)},
		{name: "garbage", stored: "not-a-sealed-value"},
	}
	for _, testCase := range cases {
		for _, consume := range []bool{false, true} {
			store := &oidclogoutstateserviceCovStore{
				findFound: true,
				findRecord: models.OIDCLogoutState{
					SessionID:             "sess",
					UserID:                owner,
					EndSessionEndpoint:    "https://id.example.com/logout",
					IDTokenHint:           testCase.stored,
					PostLogoutRedirectURL: "https://app.example.com/login",
					ExpiresAt:             time.Now().Add(time.Hour).UTC(),
				},
			}
			service := NewOIDCLogoutStateService(store, testOIDCLogoutStateKey)
			read := service.Load
			if consume {
				read = service.Consume
			}
			got, found, err := read(context.Background(), "sess", owner, time.Now())
			if err != nil {
				t.Fatalf("%s (consume=%t): unexpected error %v", testCase.name, consume, err)
			}
			if found || got.IDTokenHint != "" {
				t.Fatalf("%s (consume=%t): an unopenable hint was returned as usable state %#v", testCase.name, consume, got)
			}
			if store.deleteByIDCalls != 1 || store.deleteBySessionID != "sess" || store.deleteByUserID != owner {
				t.Fatalf("%s (consume=%t): the unusable row was not dropped for its owner (calls=%d)", testCase.name, consume, store.deleteByIDCalls)
			}
		}
	}

	// Positive anchor: the same fixture sealed for the owner opens.
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:             "sess",
			UserID:                owner,
			EndSessionEndpoint:    "https://id.example.com/logout",
			IDTokenHint:           sealOIDCLogoutHintForTest(t, hint, owner),
			PostLogoutRedirectURL: "https://app.example.com/login",
			ExpiresAt:             time.Now().Add(time.Hour).UTC(),
		},
	}
	got, found, err := NewOIDCLogoutStateService(store, testOIDCLogoutStateKey).Load(context.Background(), "sess", owner, time.Now())
	if err != nil || !found || got.IDTokenHint != hint {
		t.Fatalf("a hint sealed for its owner must open (found=%t, err=%v, hint=%q)", found, err, got.IDTokenHint)
	}
}

func TestOIDCLogoutStateServiceSaveFailsClosedWithoutASecret(t *testing.T) {
	t.Parallel()

	const hint = "plain-hint"
	store := &oidclogoutstateserviceCovStore{}
	state := OIDCLogoutState{UserID: 7}
	state.IDTokenHint = hint
	err := NewOIDCLogoutStateService(store, nil).Save(context.Background(), "sess", state, time.Now())
	if err == nil {
		t.Fatal("Save() without a secret must refuse rather than store the hint")
	}
	if store.saved != nil {
		t.Fatalf("Save() without a secret reached the store with %#v", store.saved)
	}
}
