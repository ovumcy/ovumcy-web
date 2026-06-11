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
