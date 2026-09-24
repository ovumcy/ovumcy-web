package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

type OIDCIdentityRepository struct {
	database *gorm.DB
}

func NewOIDCIdentityRepository(database *gorm.DB) *OIDCIdentityRepository {
	return &OIDCIdentityRepository{database: database}
}

// FindByIssuerSubject resolves the identity bound to (issuer, subject). A blank
// issuer or subject names no identity: it is answered as not-found without a
// query, so a missing operand can never match a row stored with an empty value.
func (repo *OIDCIdentityRepository) FindByIssuerSubject(ctx context.Context, issuer string, subject string) (models.OIDCIdentity, bool, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	if issuer == "" || subject == "" {
		return models.OIDCIdentity{}, false, nil
	}
	var identity models.OIDCIdentity
	if err := repo.database.WithContext(ctx).
		Where("issuer = ? AND subject = ?", issuer, subject).
		First(&identity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.OIDCIdentity{}, false, nil
		}
		return models.OIDCIdentity{}, false, err
	}
	return identity, true, nil
}

// Create inserts a new identity row. A zero identity.UserID is refused rather
// than written: an owner-scoped read or the account-erasure sweep can never
// address a row with no owner, so it would sit unreachable forever instead of
// failing loudly at write time.
func (repo *OIDCIdentityRepository) Create(ctx context.Context, identity *models.OIDCIdentity) error {
	if identity == nil || identity.UserID == 0 {
		return errOIDCIdentityOwnerRequired
	}
	return classifyOIDCIdentityCreateError(repo.database.WithContext(ctx).Create(identity).Error)
}

// CreateAndRevokeSessions binds a new identity to an EXISTING account and bumps
// that account's auth_session_version in the same transaction: a new sign-in
// identity changes how the account can be entered, so every session issued
// before the link is invalidated together with the write that made it.
//
// The bump is a compare-and-set from expectedSessionVersion, the version the
// caller verified its factors against, to the next one. An account found at
// any other version was revoked by another write in between (a password
// change, a TOTP re-enrollment); the link then rolls back with
// models.ErrAuthSessionVersionChanged, because the caller would otherwise
// mint a session at the version that revocation produced and survive it.
func (repo *OIDCIdentityRepository) CreateAndRevokeSessions(ctx context.Context, identity *models.OIDCIdentity, expectedSessionVersion int) error {
	if identity == nil || identity.UserID == 0 {
		return errOIDCIdentityOwnerRequired
	}
	return repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := bumpAuthSessionVersionFromTx(tx, identity.UserID, expectedSessionVersion); err != nil {
			return err
		}
		return classifyOIDCIdentityCreateError(tx.Create(identity).Error)
	})
}

// ListByUser returns the identities bound to userID, oldest first. A zero
// userID lists nothing.
func (repo *OIDCIdentityRepository) ListByUser(ctx context.Context, userID uint) ([]models.OIDCIdentity, error) {
	if userID == 0 {
		return nil, nil
	}
	var identities []models.OIDCIdentity
	if err := repo.database.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, err // codecov:ignore -- DB-layer error on the identities lookup; not reachable in unit tests
	}
	return identities, nil
}

// DeleteForUserAndRevokeSessions removes one identity, scoped to the owner in
// the query itself, and bumps that owner's auth_session_version in the same
// transaction. It reports false, and writes nothing, when no row with that id
// belongs to userID — another owner's identity id is indistinguishable from a
// missing one.
//
// It also owns the "a sign-in method remains" rule, inside the same
// transaction: when the delete would leave the account with no identity and no
// usable local password (local_auth_enabled with a stored hash, and
// localSignInOpen — the instance still accepting password sign-in), it rolls
// back and returns models.ErrOIDCUnlinkLastSignIn. A caller's earlier read is
// not enough: two concurrent unlinks of an account's two identities would each
// see the other one and remove both. The bump runs FIRST so the account row is
// write-locked before anything is counted — a row lock on Postgres, where the
// second unlink waits and its next statement sees the first one's commit, and
// the database write lock on SQLite, where BEGIN IMMEDIATE already serialises
// the whole transaction.
func (repo *OIDCIdentityRepository) DeleteForUserAndRevokeSessions(ctx context.Context, userID uint, identityID uint, localSignInOpen bool) (bool, error) {
	if userID == 0 || identityID == 0 {
		return false, nil
	}
	err := repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := bumpAuthSessionVersionTx(tx, userID); err != nil {
			if errors.Is(err, errOIDCIdentityOwnerRequired) {
				return errOIDCIdentityNotDeleted
			}
			return err // codecov:ignore -- DB-layer error from the owner bump other than a missing account; not reachable in unit tests
		}
		result := tx.Where("id = ? AND user_id = ?", identityID, userID).Delete(&models.OIDCIdentity{})
		if result.Error != nil {
			return result.Error // codecov:ignore -- DB-layer error on the identity DELETE; not reachable in unit tests
		}
		if result.RowsAffected == 0 {
			return errOIDCIdentityNotDeleted
		}
		return requireRemainingSignInTx(tx, userID, localSignInOpen)
	})
	if errors.Is(err, errOIDCIdentityNotDeleted) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// requireRemainingSignInTx refuses, with models.ErrOIDCUnlinkLastSignIn, an
// account state that has no identity left and no usable local password. It
// reads under the caller's transaction, after the account row was locked.
func requireRemainingSignInTx(tx *gorm.DB, userID uint, localSignInOpen bool) error {
	var remaining int64
	if err := tx.Model(&models.OIDCIdentity{}).Where("user_id = ?", userID).Count(&remaining).Error; err != nil {
		return err // codecov:ignore -- DB-layer error counting remaining identities; not reachable in unit tests
	}
	if remaining > 0 {
		return nil
	}
	var owner models.User
	if err := tx.Select("id", "local_auth_enabled", "password_hash").Where("id = ?", userID).Take(&owner).Error; err != nil {
		return err // codecov:ignore -- DB-layer error loading the owner row bumpAuthSessionVersionTx already confirmed exists; not reachable in unit tests
	}
	if localSignInOpen && owner.LocalAuthEnabled && strings.TrimSpace(owner.PasswordHash) != "" {
		return nil
	}
	return models.ErrOIDCUnlinkLastSignIn
}

// errOIDCIdentityNotDeleted rolls back an unlink that matched no owned row; it
// never leaves this file — the caller sees false, nil.
var errOIDCIdentityNotDeleted = errors.New("oidc identity not deleted")

// TouchLastUsed stamps last_used_at on the identity that is both identityID
// AND owned by userID. Like DeleteForUserAndRevokeSessions it is scoped by
// the caller-supplied owner in the query, not by identityID alone: an id from
// a session or claim response is combined with the session's own user_id
// before it reaches storage, never trusted alone, so a stale or foreign
// identityID can never touch another owner's row.
func (repo *OIDCIdentityRepository) TouchLastUsed(ctx context.Context, identityID uint, userID uint, usedAt time.Time) error {
	if identityID == 0 || userID == 0 {
		return nil
	}
	if usedAt.IsZero() {
		usedAt = time.Now().UTC()
	}
	return repo.database.WithContext(ctx).Model(&models.OIDCIdentity{}).
		Where("id = ? AND user_id = ?", identityID, userID).
		Update("last_used_at", usedAt).Error
}

var errOIDCIdentityOwnerRequired = errors.New("oidc identity owner is required")

// bumpAuthSessionVersionTx increments the account's session version inside tx
// and refuses a zero-row outcome, so a link or unlink naming an account that
// does not exist rolls back instead of committing a write no session tracks.
func bumpAuthSessionVersionTx(tx *gorm.DB, userID uint) error {
	result := tx.Model(&models.User{}).
		Where("id = ?", userID).
		UpdateColumn("auth_session_version", gorm.Expr("auth_session_version + 1"))
	if result.Error != nil {
		return result.Error // codecov:ignore -- DB-layer error on the session-version UPDATE; not reachable in unit tests
	}
	if result.RowsAffected == 0 {
		return errOIDCIdentityOwnerRequired
	}
	return nil
}

// bumpAuthSessionVersionFromTx moves the account from expectedSessionVersion to
// the next version inside tx, or refuses: errOIDCIdentityOwnerRequired when the
// account does not exist, models.ErrAuthSessionVersionChanged when it holds any
// other version. A legacy row still holding 0 reads as version 1, so it matches
// an expected 1 and is written as 2 — a literal, not an increment, or the
// bumped row would read as the version it was revoking.
func bumpAuthSessionVersionFromTx(tx *gorm.DB, userID uint, expectedSessionVersion int) error {
	if expectedSessionVersion < 1 {
		expectedSessionVersion = 1
	}
	result := tx.Model(&models.User{}).
		Where("id = ?", userID).
		Where("(auth_session_version = ? OR (? = 1 AND auth_session_version <= 0))", expectedSessionVersion, expectedSessionVersion).
		UpdateColumn("auth_session_version", expectedSessionVersion+1)
	if result.Error != nil {
		return result.Error // codecov:ignore -- DB-layer error on the session-version UPDATE; not reachable in unit tests
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var owners int64
	if err := tx.Model(&models.User{}).Where("id = ?", userID).Count(&owners).Error; err != nil {
		return err // codecov:ignore -- DB-layer error counting the account after a refused bump; not reachable in unit tests
	}
	if owners == 0 {
		return errOIDCIdentityOwnerRequired
	}
	return models.ErrAuthSessionVersionChanged
}
