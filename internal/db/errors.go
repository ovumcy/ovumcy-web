package db

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// ErrResetTokenAlreadyConsumed is returned by
// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS when the CAS predicate
// matches 0 rows — i.e. the reset token has already been redeemed or the
// password state changed since the token was issued. It indicates a replay
// or concurrent redeem, not a DB error.
var ErrResetTokenAlreadyConsumed = errors.New("reset token already consumed")

// ErrUnsupportedUserRole is returned by the two account-creating repository
// methods when the row they were handed carries a role this product does not
// have. Ovumcy is owner-role-only — every account is the sole owner of its own
// data, and there is no viewer or partner role at all
// (`docs/SECURITY_INVARIANTS.md`) — so a row with any other role would be
// stored and then read by code written on the assumption that owner is the only
// role there is.
var ErrUnsupportedUserRole = errors.New("unsupported user role")

// ErrOIDCLogoutStateUnattributed is returned by the OIDC logout-state
// repository when a read, a delete or a write arrives with no owner id. Every
// row is one owner's, so a missing owner is invalid input rather than a
// licence to match every owner: a query that dropped the user_id term would
// hand one owner another owner's end-session material, and a row written
// without one is unreachable by account erasure
// (`docs/SECURITY_INVARIANTS.md`).
//
// It is a SEPARATE value from services.ErrOIDCLogoutStateUnattributed, whose
// text is identical: `errors.Is` between the two is false. Neither can wrap the
// other without `internal/services` importing `internal/db` — no production
// file in `internal/services` does, so the whole business-logic package would
// gain `internal/db`, gorm and the SQLite driver in its build graph for one
// error value. What keeps the split harmless is that every owner-scoped entry
// point on OIDCLogoutStateService refuses a zero owner BEFORE delegating, so
// this value never escapes to a caller testing for the services one. Do not
// drop either pre-check: `TestOIDCLogoutStateServiceRefusesAnAbsentOwner`
// pins that the store is not reached at all, which is what the split rests on.
// (`ErrResetTokenAlreadyConsumed` above is the same shape and is not covered
// that way; the pair is one decision, not two.)
var ErrOIDCLogoutStateUnattributed = errors.New("oidc logout state requires an owner id")

type UniqueConstraintError struct {
	Constraint string
	Err        error
}

func (err *UniqueConstraintError) Error() string {
	if strings.TrimSpace(err.Constraint) == "" {
		return "unique constraint violation"
	}
	return "unique constraint violation: " + err.Constraint
}

func (err *UniqueConstraintError) Unwrap() error {
	return err.Err
}

func (err *UniqueConstraintError) UniqueConstraint() string {
	return err.Constraint
}

type SymptomSeedError struct {
	Err error
}

func (err *SymptomSeedError) Error() string {
	return "symptom seed write failed"
}

func (err *SymptomSeedError) Unwrap() error {
	return err.Err
}

func (err *SymptomSeedError) SymptomSeedFailure() bool {
	return true
}

func classifyUserCreateError(err error) error {
	return classifyUniqueConstraintError(err, "users.email")
}

func classifyOIDCIdentityCreateError(err error) error {
	return classifyUniqueConstraintError(err, "oidc_identities.issuer_subject")
}

func classifyUniqueConstraintError(err error, defaultConstraint string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return &UniqueConstraintError{
			Constraint: defaultConstraint,
			Err:        err,
		}
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "unique constraint failed") {
		const marker = "unique constraint failed:"
		constraint := defaultConstraint
		index := strings.Index(message, marker)
		if index >= 0 {
			extracted := strings.TrimSpace(message[index+len(marker):])
			if extracted != "" {
				constraint = extracted
			}
		}
		return &UniqueConstraintError{
			Constraint: constraint,
			Err:        err,
		}
	}

	return err
}
