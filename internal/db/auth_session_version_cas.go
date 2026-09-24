package db

import (
	"maps"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// authSessionVersionFromPredicate matches an account still at the session
// version its caller verified a factor against. A legacy row still holding 0
// reads as version 1, so it matches an expected 1 and nothing else.
const authSessionVersionFromPredicate = "(auth_session_version = ? OR (? = 1 AND auth_session_version <= 0))"

// updateFromAuthSessionVersionTx writes columns to the account and moves its
// auth_session_version from expectedSessionVersion to the next version, in
// one UPDATE inside tx, and returns the version it wrote. It is the one
// compare-and-set every revoking write that precedes a session mint goes
// through.
//
// The account found at any other version was revoked by another write after
// the caller verified its factors (a password change, a TOTP re-enrollment,
// a sign-out everywhere from another device); nothing is written and the
// result is models.ErrAuthSessionVersionChanged, because the caller would
// otherwise mint a session at the version that revocation produced and
// outlive it. A missing account is ErrUserOwnerRequired. The written version
// is a literal, not an increment: a legacy 0 row is written as 2, or the
// bumped row would read as the version it was revoking.
//
// The guarantee rests on the UPDATE's predicate being re-evaluated after a
// lock wait: Postgres re-checks it against the committed row a concurrent
// writer left, and SQLite serialises the whole write transaction.
func updateFromAuthSessionVersionTx(tx *gorm.DB, userID uint, expectedSessionVersion int, columns map[string]any) (int, error) {
	query, err := scopedUserUpdateTx(tx, userID)
	if err != nil {
		return 0, err
	}
	if expectedSessionVersion < 1 {
		expectedSessionVersion = 1
	}
	next := expectedSessionVersion + 1
	values := make(map[string]any, len(columns)+1)
	maps.Copy(values, columns)
	values["auth_session_version"] = next
	result := query.Where(authSessionVersionFromPredicate, expectedSessionVersion, expectedSessionVersion).Updates(values)
	if result.Error != nil {
		return 0, result.Error // codecov:ignore -- DB-layer error on the session-version UPDATE; not reachable in unit tests
	}
	if result.RowsAffected == 1 {
		return next, nil
	}
	owner, err := scopedUserUpdateTx(tx, userID)
	if err != nil {
		// codecov:ignore:start -- the same id was accepted by scopedUserUpdateTx above
		return 0, err
	}
	// codecov:ignore:end
	var owners int64
	if err := owner.Count(&owners).Error; err != nil {
		// codecov:ignore:start -- DB-layer error counting the account after a refused compare-and-set; not reachable in unit tests
		return 0, err
	}
	// codecov:ignore:end
	if owners == 0 {
		return 0, ErrUserOwnerRequired
	}
	return 0, models.ErrAuthSessionVersionChanged
}
