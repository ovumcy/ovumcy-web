package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// ErrAuthSessionEpochUnavailable is returned when a token is minted or checked
// on a service that holds no session epoch. It is a wiring or boot fault, never
// a state a request can reach, and it fails closed: nothing is minted, and
// nothing resolves.
var ErrAuthSessionEpochUnavailable = errors.New("auth session epoch unavailable")

// authSessionEpochStore is the narrow app_state surface the boot load needs.
type authSessionEpochStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
}

// LoadAuthSessionEpoch returns the session epoch this process serves under,
// creating it when the key is absent. It runs once at boot, AFTER the restore
// fence pass: a boot that saw a restore has already replaced the stored value
// inside the revocation's own transaction, so what this reads is the rotated
// one.
//
// An absent key is the upgrade path from a release that minted no epoch claim —
// every cookie already issued lacks it and is refused, which signs each owner
// out once. A stored value that is not a well-formed epoch is replaced the same
// way rather than trusted. created reports either, for the startup line.
func LoadAuthSessionEpoch(ctx context.Context, store authSessionEpochStore) (string, bool, error) {
	stored, found, err := store.Get(ctx, models.AppStateKeyAuthSessionEpoch)
	if err != nil {
		return "", false, fmt.Errorf("load auth session epoch: %w", err)
	}
	if found && security.ValidAuthSessionEpoch(stored) {
		return stored, false, nil
	}
	epoch, err := security.NewAuthSessionEpoch()
	if err != nil {
		return "", false, fmt.Errorf("generate auth session epoch: %w", err) // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	if err := store.Set(ctx, models.AppStateKeyAuthSessionEpoch, epoch); err != nil {
		return "", false, fmt.Errorf("store auth session epoch: %w", err)
	}
	return epoch, true, nil
}

// ConfigureSessionEpoch gives the service the epoch LoadAuthSessionEpoch
// returned. It is set once at boot, before the service is shared.
func (service *AuthService) ConfigureSessionEpoch(epoch string) {
	service.sessionEpoch = epoch
}

// SessionEpoch returns the epoch tokens are minted under and checked against.
// An empty epoch is an error, not a value: the pending-cookie writers and
// readers in the transport layer take it from here, and none of them may carry
// or compare an empty one.
func (service *AuthService) SessionEpoch() (string, error) {
	if service == nil || service.sessionEpoch == "" {
		return "", ErrAuthSessionEpochUnavailable
	}
	return service.sessionEpoch, nil
}

// SessionEpochCurrent reports whether a claimed epoch is the one in force. A
// missing claim, an empty server epoch and a mismatch all answer false.
func (service *AuthService) SessionEpochCurrent(claimed string) bool {
	if service == nil {
		return false
	}
	return security.AuthSessionEpochsEqual(claimed, service.sessionEpoch)
}
