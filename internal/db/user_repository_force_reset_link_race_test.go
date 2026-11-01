package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// TestForceResetFullDeletesAnIdentityLinkedConcurrentlyOnPostgres: an OIDC
// link and `reset-password --full` queued on the same owner row. A link queued
// first commits its identity while the reset waits; the reset's identity
// DELETE must run after the row UPDATE that waited for it, or it sees no
// identity yet and the link survives a compromise recovery. A link queued
// behind the reset must fail its session-version compare-and-set.
//
// A third transaction holds the owner row so that both writers queue on it in
// a fixed order before it lets them go.
func TestForceResetFullDeletesAnIdentityLinkedConcurrentlyOnPostgres(t *testing.T) {
	database := openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
	repos := NewRepositories(database)
	ctx := context.Background()

	for index, linkFirst := range []bool{true, false} {
		user := &models.User{
			Email:            []string{"link-first@example.com", "reset-first@example.com"}[index],
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

		linkErr, resetErr := raceLinkAgainstFullReset(t, database, repos, &stored, linkFirst)
		if resetErr != nil {
			t.Fatalf("linkFirst=%v: reset: %v", linkFirst, resetErr)
		}
		switch {
		case linkFirst && errors.Is(linkErr, models.ErrAuthSessionVersionChanged):
			t.Fatal("the link did not take the owner row ahead of the reset: the test never reached the race it targets")
		case linkFirst && linkErr != nil:
			t.Fatalf("link: %v", linkErr)
		case !linkFirst && !errors.Is(linkErr, models.ErrAuthSessionVersionChanged):
			t.Fatalf("a link queued behind the reset must fail its session-version compare-and-set, got %v", linkErr)
		}

		var identities int64
		if err := database.Model(&models.OIDCIdentity{}).Where("user_id = ?", user.ID).Count(&identities).Error; err != nil {
			t.Fatalf("count identities: %v", err)
		}
		if identities != 0 {
			t.Fatalf("linkFirst=%v: %d identity(ies) linked while reset-password --full ran outlived it", linkFirst, identities)
		}
		after, err := repos.Users.FindByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("reload reset user: %v", err)
		}
		if after.TOTPEnabled || after.TOTPSecret != "" || after.RecoveryCodeHash != "" {
			t.Fatalf("linkFirst=%v: reset-password --full left a second factor standing (totp_enabled=%v)", linkFirst, after.TOTPEnabled)
		}
	}
}

func raceLinkAgainstFullReset(t *testing.T, database *gorm.DB, repos *Repositories, user *models.User, linkFirst bool) (linkErr error, resetErr error) {
	t.Helper()
	ctx := context.Background()

	blocker := database.Begin()
	if blocker.Error != nil {
		t.Fatalf("begin blocker: %v", blocker.Error)
	}
	released := false
	defer func() {
		if !released {
			blocker.Rollback()
		}
	}()
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
	link := func() {
		go func() {
			linked <- repos.OIDCIdentities.CreateAndRevokeSessions(ctx, &models.OIDCIdentity{
				UserID: user.ID, Issuer: "https://idp.example.com", Subject: user.Email, CreatedAt: time.Now().UTC(),
			}, user.AuthSessionVersion)
		}()
	}
	reset := make(chan error, 1)
	forceReset := func() {
		go func() {
			reset <- repos.Users.ForceResetPasswordAndRevokeSessions(ctx, user.ID, "operator-hash", true)
		}()
	}
	first, second := link, forceReset
	if !linkFirst {
		first, second = forceReset, link
	}
	first()
	waitForLockWaiters(1)
	second()
	waitForLockWaiters(2)

	if err := blocker.Rollback().Error; err != nil {
		t.Fatalf("release owner row: %v", err)
	}
	released = true

	for _, wait := range []struct {
		name string
		done chan error
		into *error
	}{{"link", linked, &linkErr}, {"reset", reset, &resetErr}} {
		select {
		case *wait.into = <-wait.done:
		case <-time.After(15 * time.Second):
			t.Fatalf("%s did not finish after the owner row was released", wait.name)
		}
	}
	return linkErr, resetErr
}
