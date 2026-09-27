package db

import (
	"maps"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// authSessionVersionFromPredicate matches an account still at the session
// version its caller verified a factor against. Migration 041 (WEB-50/WEB-65)
// backfilled every row at or below 0 to 1 at boot, so this predicate no
// longer carries a legacy arm reading a stored 0 as version 1: plain equality
// is enough, and a row forced back to 0 or below by hand after that boot
// matches nothing (see the expectedSessionVersion guard below, which keeps
// the compared value itself out of that range).
const authSessionVersionFromPredicate = "auth_session_version = ?"

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
// outlive it. A missing account is ErrUserOwnerRequired.
//
// expectedSessionVersion below 1 is refused outright rather than clamped to 1
// and matched anyway: every caller normalizes before it reaches here
// (services.NormalizeAuthSessionVersion), so this only guards a caller that
// does not, not a path production traffic takes. Clamping cannot make an
// un-normalized 0 match a row forced to 0 by hand — the predicate is plain
// equality, so a clamped 1 would instead match a row already at 1 (the
// ordinary post-041 state), letting that caller's un-normalized expectation
// silently pass a compare-and-set it was never verified against. Refusing
// keeps expected and stored apart regardless of what a future caller passes
// in.
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
		return 0, models.ErrAuthSessionVersionChanged
	}
	next := expectedSessionVersion + 1
	values := make(map[string]any, len(columns)+1)
	maps.Copy(values, columns)
	values["auth_session_version"] = next
	result := query.Where(authSessionVersionFromPredicate, expectedSessionVersion).Updates(values)
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
