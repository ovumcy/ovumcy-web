package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestOIDCIdentityRepositoryUsesCanonicalTableName(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "oidc-identities.db"))

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at, local_auth_enabled) VALUES (?, ?, ?, CURRENT_TIMESTAMP, 1)`,
		"oidc-owner@example.com",
		"hash",
		"owner",
	).Error; err != nil {
		t.Fatalf("insert owner user: %v", err)
	}

	repository := NewOIDCIdentityRepository(database)
	createdAt := time.Now().UTC()
	identity := models.OIDCIdentity{
		UserID:    1,
		Issuer:    "https://id.example.com",
		Subject:   "subject-1",
		CreatedAt: createdAt,
	}

	if err := repository.Create(context.Background(), &identity); err != nil {
		t.Fatalf("create oidc identity: %v", err)
	}
	if identity.ID == 0 {
		t.Fatal("expected oidc identity ID to be assigned")
	}

	stored, found, err := repository.FindByIssuerSubject(context.Background(), identity.Issuer, identity.Subject)
	if err != nil {
		t.Fatalf("find oidc identity: %v", err)
	}
	if !found {
		t.Fatal("expected oidc identity lookup to find stored record")
	}
	if stored.UserID != identity.UserID {
		t.Fatalf("expected oidc identity user_id %d, got %d", identity.UserID, stored.UserID)
	}
}
