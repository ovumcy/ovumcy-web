package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

func TestNewOIDCStepupStateValid(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	state, err := newOIDCStepupState(now, oidcStepupPurposeLocalPasswordSetup, 7, "hashedpw")
	if err != nil {
		t.Fatalf("newOIDCStepupState() unexpected error: %v", err)
	}
	if state.Purpose != oidcStepupPurposeLocalPasswordSetup {
		t.Fatalf("expected purpose %q, got %q", oidcStepupPurposeLocalPasswordSetup, state.Purpose)
	}
	if state.UserID != 7 {
		t.Fatalf("expected user ID 7, got %d", state.UserID)
	}
	if state.PasswordHash != "hashedpw" {
		t.Fatalf("expected password hash preserved, got %q", state.PasswordHash)
	}
	if state.State == "" || state.Nonce == "" || state.CodeVerifier == "" {
		t.Fatalf("expected random state/nonce/verifier to be populated, got %+v", state)
	}
	if !state.validAt(now) {
		t.Fatal("expected freshly created state to be valid")
	}
}

func TestNewOIDCStepupStateZeroUserID(t *testing.T) {
	t.Parallel()

	if _, err := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, 0, "hashedpw"); err == nil {
		t.Fatal("expected error for userID=0")
	}
}

func TestNewOIDCStepupStateEmptyPasswordHash(t *testing.T) {
	t.Parallel()

	if _, err := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, 5, ""); err == nil {
		t.Fatal("expected error for empty password hash")
	}
	if _, err := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, 5, "   "); err == nil {
		t.Fatal("expected error for whitespace-only password hash")
	}
}

func TestOIDCStepupStateValidAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)

	fresh, err := newOIDCStepupState(now, oidcStepupPurposeLocalPasswordSetup, 1, "h")
	if err != nil {
		t.Fatalf("create fresh state: %v", err)
	}
	if !fresh.validAt(now) {
		t.Fatal("expected fresh state to be valid")
	}
	if fresh.validAt(now.Add(oidcStepupCookieTTL + time.Second)) {
		t.Fatal("expected state past TTL to be invalid")
	}

	empty := oidcStepupState{}
	if empty.validAt(now) {
		t.Fatal("expected empty state to be invalid")
	}

	// Every purpose needs the same core fields, so a payload missing one is
	// invalid before its purpose is consulted at all. Each case below carries a
	// LIVE expiry on purpose: the empty state above is rejected by its
	// unparseable ExpiresAt, which returns before the field check runs and would
	// leave that check untested while looking covered.
	for _, missing := range []struct {
		name   string
		mutate func(*oidcStepupState)
	}{
		{name: "no owner id", mutate: func(s *oidcStepupState) { s.UserID = 0 }},
		{name: "no state", mutate: func(s *oidcStepupState) { s.State = "" }},
		{name: "no nonce", mutate: func(s *oidcStepupState) { s.Nonce = "" }},
		{name: "no pkce verifier", mutate: func(s *oidcStepupState) { s.CodeVerifier = "" }},
	} {
		incomplete := fresh
		missing.mutate(&incomplete)
		if incomplete.validAt(now) {
			t.Fatalf("expected a state with %s to be invalid", missing.name)
		}
	}

	noPurpose := oidcStepupState{
		UserID:       1,
		State:        "s",
		Nonce:        "n",
		CodeVerifier: "v",
		PasswordHash: "h",
		ExpiresAt:    now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	if noPurpose.validAt(now) {
		t.Fatal("expected state with empty purpose to be invalid")
	}

	// Decision (2026-07-28): an unknown purpose is now rejected here, where it
	// used to pass on the grounds that purpose validation was the caller's
	// concern. That held while one purpose existed and one completion handler
	// compared against it. With a second purpose the payload SHAPE became
	// purpose-dependent — password setup carries a prepared hash, erasure
	// carries an operation — so "valid" cannot be decided without reading the
	// purpose, and a payload naming none of the known ones satisfies no arm.
	// The completion handlers still check the purpose they dispatch on; this is
	// the earlier of two gates, not a replacement for either.
	unknownPurpose := noPurpose
	unknownPurpose.Purpose = "unknown_purpose"
	if unknownPurpose.validAt(now) {
		t.Fatal("expected state with unknown purpose to be invalid: it satisfies no purpose's field requirements")
	}

	// Each purpose requires its own fields and refuses the other's, so a
	// payload cannot be reinterpreted as the opposite flow.
	erasure, err := newOIDCErasureStepupState(now, 1, oidcStepupErasureClearData)
	if err != nil {
		t.Fatalf("create erasure state: %v", err)
	}
	if !erasure.validAt(now) {
		t.Fatal("expected fresh erasure state to be valid")
	}
	if erasure.validAt(now.Add(oidcStepupCookieTTL + time.Second)) {
		t.Fatal("expected erasure state past TTL to be invalid")
	}

	erasureWithHash := erasure
	erasureWithHash.PasswordHash = "h"
	if erasureWithHash.validAt(now) {
		t.Fatal("expected an erasure payload carrying a password hash to be invalid: no constructor mints that shape")
	}

	erasureWithoutOperation := erasure
	erasureWithoutOperation.Operation = ""
	if erasureWithoutOperation.validAt(now) {
		t.Fatal("expected an erasure payload naming no operation to be invalid")
	}

	erasureUnknownOperation := erasure
	erasureUnknownOperation.Operation = "wipe_everything"
	if erasureUnknownOperation.validAt(now) {
		t.Fatal("expected an erasure payload naming an unknown operation to be invalid")
	}

	passwordSetupWithOperation := fresh
	passwordSetupWithOperation.Operation = oidcStepupErasureDeleteAccount
	if passwordSetupWithOperation.validAt(now) {
		t.Fatal("expected a password-setup payload carrying an erasure operation to be invalid")
	}
}

// TestNewOIDCErasureStepupStateRefusesIncompletePayloads pins the constructor
// half: the state that authorizes an erasure must name both the owner it was
// minted for and which erasure was confirmed, and it never carries a password
// hash — there is no credential to commit on this path.
func TestNewOIDCErasureStepupStateRefusesIncompletePayloads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)

	if _, err := newOIDCErasureStepupState(now, 0, oidcStepupErasureClearData); err == nil {
		t.Fatal("expected an erasure state with no owner id to be refused")
	}
	if _, err := newOIDCErasureStepupState(now, 5, ""); err == nil {
		t.Fatal("expected an erasure state with no operation to be refused")
	}
	if _, err := newOIDCErasureStepupState(now, 5, "drop_tables"); err == nil {
		t.Fatal("expected an erasure state with an unknown operation to be refused")
	}

	for _, operation := range []oidcStepupErasureOperation{oidcStepupErasureClearData, oidcStepupErasureDeleteAccount} {
		state, err := newOIDCErasureStepupState(now, 5, operation)
		if err != nil {
			t.Fatalf("create erasure state for %q: %v", operation, err)
		}
		if state.Purpose != oidcStepupPurposeErasure {
			t.Fatalf("expected purpose %q, got %q", oidcStepupPurposeErasure, state.Purpose)
		}
		if state.Operation != operation {
			t.Fatalf("expected operation %q, got %q", operation, state.Operation)
		}
		if state.PasswordHash != "" {
			t.Fatal("expected an erasure state to carry no password hash")
		}
		if state.State == "" || state.Nonce == "" || state.CodeVerifier == "" {
			t.Fatal("expected an erasure state to carry state, nonce and PKCE verifier")
		}
	}
}

func TestOIDCStepupStateMatchesState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	state, err := newOIDCStepupState(now, oidcStepupPurposeLocalPasswordSetup, 1, "h")
	if err != nil {
		t.Fatalf("create state: %v", err)
	}

	if !state.matchesState(state.State) {
		t.Fatal("expected state to match itself")
	}
	if state.matchesState("wrong-state") {
		t.Fatal("expected wrong state to not match")
	}
	if state.matchesState("") {
		t.Fatal("expected empty candidate to not match")
	}
}

func newStepupTestHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
}

func TestSetAndPopOIDCStepupCookieRoundTrip(t *testing.T) {
	t.Parallel()

	handler := newStepupTestHandler(t)
	now := time.Now().UTC()
	original, err := newOIDCStepupState(now, oidcStepupPurposeLocalPasswordSetup, 42, "bcrypt-hash")
	if err != nil {
		t.Fatalf("create state: %v", err)
	}

	var cookieValue string
	setApp := fiber.New()
	setApp.Get("/set", func(c fiber.Ctx) error {
		if err := handler.setOIDCStepupCookie(c, original); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	setResp, err := setApp.Test(httptest.NewRequest("GET", "/set", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("set request: %v", err)
	}
	defer func() { _ = setResp.Body.Close() }()
	for _, c := range setResp.Cookies() {
		if c.Name == oidcStepupCookieName {
			cookieValue = c.Value
		}
	}
	if cookieValue == "" {
		t.Fatal("expected stepup cookie to be set")
	}

	popApp := fiber.New()
	var popped oidcStepupState
	popApp.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		popped = handler.popOIDCStepupCookie(c)
		return c.SendStatus(fiber.StatusNoContent)
	})
	popReq := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	popReq.Header.Set("Cookie", oidcStepupCookieName+"="+cookieValue)
	popResp, err := popApp.Test(popReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("pop request: %v", err)
	}
	defer func() { _ = popResp.Body.Close() }()

	if popped.UserID != original.UserID {
		t.Fatalf("expected user ID %d, got %d", original.UserID, popped.UserID)
	}
	if popped.Purpose != original.Purpose {
		t.Fatalf("expected purpose %q, got %q", original.Purpose, popped.Purpose)
	}
	if popped.PasswordHash != original.PasswordHash {
		t.Fatalf("expected password hash preserved, got %q", popped.PasswordHash)
	}
	if popped.State != original.State {
		t.Fatalf("expected state %q, got %q", original.State, popped.State)
	}
}

func TestPopOIDCStepupCookieWrongKey(t *testing.T) {
	t.Parallel()

	signer := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	verifier := &Handler{
		secretKey:    []byte("fedcba9876543210fedcba9876543210"),
		cookieSecure: true,
	}

	now := time.Now().UTC()
	state, err := newOIDCStepupState(now, oidcStepupPurposeLocalPasswordSetup, 1, "h")
	if err != nil {
		t.Fatalf("create state: %v", err)
	}

	var cookieValue string
	setApp := fiber.New()
	setApp.Get("/set", func(c fiber.Ctx) error {
		if err := signer.setOIDCStepupCookie(c, state); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	setResp, err := setApp.Test(httptest.NewRequest("GET", "/set", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("set request: %v", err)
	}
	defer func() { _ = setResp.Body.Close() }()
	for _, c := range setResp.Cookies() {
		if c.Name == oidcStepupCookieName {
			cookieValue = c.Value
		}
	}

	popApp := fiber.New()
	var popped oidcStepupState
	popApp.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		popped = verifier.popOIDCStepupCookie(c)
		return c.SendStatus(fiber.StatusNoContent)
	})
	popReq := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	popReq.Header.Set("Cookie", oidcStepupCookieName+"="+cookieValue)
	popResp, err := popApp.Test(popReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("pop request: %v", err)
	}
	defer func() { _ = popResp.Body.Close() }()

	if popped.UserID != 0 || popped.State != "" {
		t.Fatalf("expected empty state when key mismatches, got %+v", popped)
	}
}

func TestPopOIDCStepupCookieExpiredPayload(t *testing.T) {
	t.Parallel()

	handler := newStepupTestHandler(t)
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}

	expired := oidcStepupState{
		Purpose:      oidcStepupPurposeLocalPasswordSetup,
		UserID:       5,
		State:        "s",
		Nonce:        "n",
		CodeVerifier: "v",
		PasswordHash: "h",
		ExpiresAt:    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}
	payload, err := json.Marshal(expired)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sealed, err := codec.seal(oidcStepupCookieName, payload)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	app := fiber.New()
	var popped oidcStepupState
	app.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		popped = handler.popOIDCStepupCookie(c)
		return c.SendStatus(fiber.StatusNoContent)
	})
	req := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	req.Header.Set("Cookie", oidcStepupCookieName+"="+sealed)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if popped.UserID != 0 || popped.State != "" {
		t.Fatalf("expected empty state for expired cookie, got %+v", popped)
	}
}

func TestClearOIDCStepupCookieExpiresInPast(t *testing.T) {
	t.Parallel()

	handler := newStepupTestHandler(t)
	app := fiber.New()
	app.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		handler.clearOIDCStepupCookie(c)
		return c.SendStatus(fiber.StatusNoContent)
	})
	req := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	for _, c := range resp.Cookies() {
		if c.Name == oidcStepupCookieName {
			if c.Value != "" {
				t.Fatalf("expected cleared cookie to have empty value, got %q", c.Value)
			}
			if !c.Expires.Before(time.Now()) {
				t.Fatalf("expected cleared cookie expiry in the past, got %v", c.Expires)
			}
			return
		}
	}
	t.Fatal("expected ovumcy_oidc_stepup cookie in response")
}
