package db

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestForceResetFullDeletesAnIdentityLinkedConcurrentlyOnPostgres: an OIDC
// link queued on the owner row ahead of `reset-password --full` commits its
// identity while the reset waits. The reset's identity DELETE must run after
// the row UPDATE that waited for the link; run first, it sees no identity
// yet, and the link committed behind it survives a compromise recovery.
//
// A third transaction holds the owner row so that both writers queue on it in
// a fixed order — the link first, then the reset — before it lets them go.
func TestForceResetFullDeletesAnIdentityLinkedConcurrentlyOnPostgres(t *testing.T) {
	database := openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
	repos := NewRepositories(database)
	ctx := context.Background()

	user := &models.User{
		Email:            "force-reset-link-race@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		TOTPEnabled:      true,
		TOTPSecret:       "secret",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	stored, err := repos.Users.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}

	blocker := database.Begin()
	if blocker.Error != nil {
		t.Fatalf("begin blocker: %v", blocker.Error)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			blocker.Rollback()
		}
	})
	if err := blocker.Exec(`SELECT id FROM users WHERE id = ? FOR UPDATE`, user.ID).Error; err != nil {
		t.Fatalf("lock owner row: %v", err)
	}

	waitForLockWaiters := func(want int64) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			var waiting int64
			if err := database.Raw(`SELECT count(*) FROM pg_stat_activity
				WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting).Error; err != nil {
				t.Fatalf("read pg_stat_activity: %v", err)
			}
			if waiting >= want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("fewer than %d writer(s) queued on the owner row", want)
	}

	linked := make(chan error, 1)
	go func() {
		linked <- repos.OIDCIdentities.CreateAndRevokeSessions(ctx, &models.OIDCIdentity{
			UserID: user.ID, Issuer: "https://idp.example.com", Subject: "attacker", CreatedAt: time.Now().UTC(),
		}, stored.AuthSessionVersion)
	}()
	waitForLockWaiters(1)

	reset := make(chan error, 1)
	go func() {
		reset <- repos.Users.ForceResetPasswordAndRevokeSessions(ctx, user.ID, "operator-hash", true)
	}()
	waitForLockWaiters(2)

	if err := blocker.Rollback().Error; err != nil {
		t.Fatalf("release owner row: %v", err)
	}
	released = true

	for name, done := range map[string]chan error{"link": linked, "reset": reset} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("%s did not finish after the owner row was released", name)
		}
	}

	var identities int64
	if err := database.Model(&models.OIDCIdentity{}).Where("user_id = ?", user.ID).Count(&identities).Error; err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identities != 0 {
		t.Fatalf("%d identity(ies) linked while reset-password --full ran outlived it", identities)
	}
}
