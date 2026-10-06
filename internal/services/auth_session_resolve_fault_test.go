package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestResolveAuthSessionTellsAStorageFaultFromAMissingAccount: a lookup that
// fails says nothing about the session, so it must not come back as the
// invalid-credentials verdict that makes the caller clear the cookie; an
// account that is genuinely gone still does.
func TestResolveAuthSessionTellsAStorageFaultFromAMissingAccount(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	storageFault := errors.New("database is locked")

	cases := map[string]struct {
		repo    *stubAuthUserRepo
		want    error
		notWant error
	}{
		"storage fault":   {repo: &stubAuthUserRepo{findByIDOptionalErr: storageFault}, want: ErrAuthSessionLookupFailed, notWant: ErrAuthInvalidCreds},
		"missing account": {repo: &stubAuthUserRepo{}, want: ErrAuthInvalidCreds, notWant: ErrAuthSessionLookupFailed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			service := NewAuthService(c.repo)
			token, _, err := service.BuildAuthSessionTokenWithSessionID(secret, 42, models.RoleOwner, 1, 30*time.Minute, now)
			if err != nil {
				t.Fatalf("BuildAuthSessionTokenWithSessionID() unexpected error: %v", err)
			}
			_, _, err = service.ResolveAuthSession(context.Background(), secret, token, now.Add(time.Minute))
			if !errors.Is(err, c.want) || errors.Is(err, c.notWant) {
				t.Fatalf("ResolveAuthSession() error = %v, want %v and not %v", err, c.want, c.notWant)
			}
			if name == "storage fault" && !errors.Is(err, storageFault) {
				t.Fatalf("ResolveAuthSession() error = %v, want it to wrap the storage fault", err)
			}
		})
	}
}
