package models

import "errors"

// Sentinels that more than one layer has to name.
//
// internal/services deliberately does not import internal/db: the business
// layer would then carry the driver and the migration set in its build graph.
// A fact both layers speak about therefore has nowhere to live inside either of
// them, and declaring it in both produces two errors.New values with identical
// text — never errors.Is-equal, so a comparison across the boundary is false
// for an error whose message is character-for-character the one being tested
// for, while reading as a working check.
//
// internal/models is the one package both already import, and it costs nothing
// to reach: it is transport-free, and neither layer's dependency set grows by a
// single package. Each layer re-exports the value under its own name, so both
// spellings are the same error.
//
// A sentinel belongs here only when both layers need it. One that a single
// layer raises and only its own callers inspect stays in that layer.
// Regression: TestNoErrorSentinelTextIsDeclaredInMoreThanOneLayer.
var (
	// ErrResetTokenAlreadyConsumed reports that a password-reset redeem lost
	// the compare-and-swap: the token had already been redeemed, or the
	// password state changed after the token was issued. Raised by the CAS
	// update in the user repository and surfaced unchanged by the auth
	// service, it marks a replay or a concurrent redeem, not a DB failure.
	ErrResetTokenAlreadyConsumed = errors.New("reset token already consumed")

	// ErrOIDCLogoutStateUnattributed reports that a provider-logout state
	// read, write or delete arrived with no owner id. Every row is one
	// owner's, so a missing owner is invalid input rather than a licence to
	// match every owner. Raised by the logout-state repository, and ahead of
	// it by the service that fronts it: both layers refuse a zero owner on
	// their own, and this is the one value both refusals carry.
	ErrOIDCLogoutStateUnattributed = errors.New("oidc logout state requires an owner id")
)
