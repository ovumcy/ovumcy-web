package db

import (
	"errors"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// ErrResetTokenAlreadyConsumed is returned by
// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS when the CAS predicate
// matches 0 rows — i.e. the reset token has already been redeemed or the
// password state changed since the token was issued. It indicates a replay
// or concurrent redeem, not a DB error.
//
// It is the shared value from internal/models, not a second one with the same
// text: internal/services names the same fact and cannot import this package,
// so callers there compare against services.ErrResetTokenAlreadyConsumed and
// must match the error this layer raises.
var ErrResetTokenAlreadyConsumed = models.ErrResetTokenAlreadyConsumed

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
// It is the shared value from internal/models, not a second one with the same
// text: internal/services refuses the same input ahead of this layer and
// cannot import this package, so callers there compare against
// services.ErrOIDCLogoutStateUnattributed and must match the error this layer
// raises.
var ErrOIDCLogoutStateUnattributed = models.ErrOIDCLogoutStateUnattributed

// UniqueConstraintError reports that a write was refused by a unique index.
//
// Constraint is **not** read out of the database's refusal: it is the name the
// calling repository declared for the one unique constraint that write can
// violate (see classifyUniqueConstraintError). Neither driver hands this layer
// a usable constraint name — the sqlite translator replaces the driver error
// with a bare sentinel, and postgres words its own message differently — so a
// caller must treat this as documentation of the write, not as a fact read back
// from the schema. Branching on it is branching on the repository's own
// annotation; today's consumers only test for the type's presence
// (`internal/services`: registration, operator-user, symptom).
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

// UniqueConstraint returns the caller-declared constraint name, with the
// caveat on the Constraint field: it is the repository's annotation of the
// write, not a name extracted from the database's refusal.
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

// classifyUniqueConstraintError turns a duplicate-key refusal into a
// UniqueConstraintError carrying constraint, the name the calling repository
// declares for the one unique index that write can violate.
//
// The single signal it reads is gorm.ErrDuplicatedKey, which both drivers
// produce because newGORMConfig sets TranslateError (gorm_config.go, pinned by
// TestNewGORMConfigEnablesTranslateError). errors.Is, not ==: the postgres
// translator wraps the sentinel around the pgconn error.
//
// It deliberately does not sniff the driver's message text. That could never be
// right for both dialects at once — "UNIQUE constraint failed:" is SQLite's
// wording and postgres does not emit it — and it is not even reachable on
// SQLite, whose translator replaces the driver error with the bare sentinel and
// throws the wording away. Absence of message sniffing is pinned by
// TestClassifyUniqueConstraintErrorIgnoresDriverMessageText.
func classifyUniqueConstraintError(err error, constraint string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return &UniqueConstraintError{
			Constraint: constraint,
			Err:        err,
		}
	}

	return err
}
