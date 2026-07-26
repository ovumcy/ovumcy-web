package services

import (
	"context"
	"net/mail"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// authEmailRenormalizerAppState is the narrow app_state surface the one-shot
// pass needs: read the done-marker, write it once.
type authEmailRenormalizerAppState interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
}

// authEmailRenormalizerUserStore lists candidate rows and performs the two
// narrow operations the repair needs.
type authEmailRenormalizerUserStore interface {
	ListOperatorUserSummaries(ctx context.Context) ([]models.OperatorUserSummary, error)
	ExistsByNormalizedEmailExcludingUser(ctx context.Context, email string, excludeUserID uint) (bool, error)
	RenormalizeUserEmail(ctx context.Context, userID uint, fromEmail string, toEmail string) (bool, error)
}

// AuthEmailRenormalizeOutcome reports what one Run pass did, for the
// operator-facing startup log line. Counts, never addresses — emails must not
// reach logs.
type AuthEmailRenormalizeOutcome struct {
	// AlreadyDone: the marker was present, nothing was scanned.
	AlreadyDone bool
	// Renormalized counts rows rewritten to their bare parsed address.
	Renormalized int64
	// SkippedConflicts counts rows whose bare address is already answered by a
	// DIFFERENT account (two accounts on one mailbox — the duplicate the old
	// normalizer allowed). They are left untouched for operator review and can
	// no longer sign in under the strict rule.
	SkippedConflicts int64
	// SkippedUnrenormalizable counts rows whose stored value cannot be reduced
	// to a strict addr-spec at all (for example a quoted local part whose
	// decoded form no longer parses). Left untouched, same consequence.
	SkippedUnrenormalizable int64
}

// AuthEmailRenormalizer is the one-shot boot pass paired with the strict
// NormalizeAuthEmail rule. Rows written by the pre-strict normalizer store the
// WHOLE decorated input ("john doe <a@b.com>"); under the strict rule no login
// input can ever match them again (every input normalizes to the bare
// address), so the pass rewrites each such row to its bare parsed address
// before the server starts serving. This cannot be a SQL migration: reducing
// an RFC 5322 form to its addr-spec requires mail.ParseAddress.
//
// Collision policy: the listing is ordered oldest-first (created_at, then id),
// so when two accounts share one mailbox the oldest gets the bare address and
// every later one is counted in SkippedConflicts and left as stored — signing
// it in again is an operator decision, not something the pass can resolve.
//
// The done-marker is written only after a complete pass, so a crash mid-pass
// re-runs it on the next boot; every individual rewrite is idempotent (a row
// already rewritten no longer differs from its canonical form).
type AuthEmailRenormalizer struct {
	appState authEmailRenormalizerAppState
	users    authEmailRenormalizerUserStore
}

func NewAuthEmailRenormalizer(appState authEmailRenormalizerAppState, users authEmailRenormalizerUserStore) *AuthEmailRenormalizer {
	return &AuthEmailRenormalizer{appState: appState, users: users}
}

// Run executes the pass. It returns the outcome for the startup log; any
// storage error aborts the pass with the marker unwritten so the next boot
// retries.
func (renormalizer *AuthEmailRenormalizer) Run(ctx context.Context) (AuthEmailRenormalizeOutcome, error) {
	_, done, err := renormalizer.appState.Get(ctx, models.AppStateKeyAuthEmailRenormalizeV1)
	if err != nil {
		return AuthEmailRenormalizeOutcome{}, err
	}
	if done {
		return AuthEmailRenormalizeOutcome{AlreadyDone: true}, nil
	}

	users, err := renormalizer.users.ListOperatorUserSummaries(ctx)
	if err != nil {
		return AuthEmailRenormalizeOutcome{}, err
	}

	var outcome AuthEmailRenormalizeOutcome
	for _, user := range users {
		stored := user.Email
		lowered := strings.ToLower(strings.TrimSpace(stored))
		if stored == lowered && NormalizeAuthEmail(lowered) == lowered {
			continue // already canonical byte for byte
		}
		address, err := mail.ParseAddress(lowered)
		if err != nil {
			outcome.SkippedUnrenormalizable++
			continue
		}
		canonical := strings.ToLower(address.Address)
		if NormalizeAuthEmail(canonical) != canonical {
			outcome.SkippedUnrenormalizable++
			continue
		}
		taken, err := renormalizer.users.ExistsByNormalizedEmailExcludingUser(ctx, canonical, user.ID)
		if err != nil {
			return outcome, err
		}
		if taken {
			outcome.SkippedConflicts++
			continue
		}
		changed, err := renormalizer.users.RenormalizeUserEmail(ctx, user.ID, stored, canonical)
		if err != nil {
			return outcome, err
		}
		if changed {
			outcome.Renormalized++
		}
	}

	if err := renormalizer.appState.Set(ctx, models.AppStateKeyAuthEmailRenormalizeV1, "done"); err != nil {
		return outcome, err
	}
	return outcome, nil
}
