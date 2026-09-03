package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// AmbiguousEmailError is returned by every CLI-facing lookup below when a
// normalized address answers for more than one row — a legacy duplicate the
// boot-time email renormalizer left standing (two accounts on one mailbox,
// see RenormalizeUserEmail in internal/db) because it resolves the SAME
// conflict by keeping the older row's address and locking the newer one out
// under a form no address-taking command accepts. Silently acting on gorm's
// arbitrary first match would act on the WRONG account; the caller must
// re-address by id instead (`ovumcy users list` prints it).
//
// Error() names every matching id so a caller with no command-specific
// mapping (today: `users delete`, `webhook show|set`) still surfaces an
// actionable refusal without writing its own translation. A caller that DOES
// have an `--id` form of its own (reset-password: mapResetPasswordError)
// still maps this to its own richer, flag-specific wording.
type AmbiguousEmailError struct {
	Email string
	IDs   []uint
}

func (err *AmbiguousEmailError) Error() string {
	return fmt.Sprintf("email %s matches more than one account (ids %s)", err.Email, formatAmbiguousIDs(err.IDs))
}

// formatAmbiguousIDs renders the ambiguous match list the same style
// `users list` prints them in: ascending, comma-separated.
func formatAmbiguousIDs(ids []uint) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	return strings.Join(parts, ", ")
}

// normalizedEmailFinder is the narrow read surface resolveUniqueUserByEmail
// needs. Every repository/reader this package resolves an account by email
// through already provides it: AuthUserRepository, OperatorUserRepository,
// and WebhookOwnerReader are each satisfied structurally, with no shared base
// interface required.
type normalizedEmailFinder interface {
	FindAllByNormalizedEmail(ctx context.Context, email string) ([]models.User, error)
}

// resolveUniqueUserByEmail is the ambiguity-aware counterpart to a bare
// FindByNormalizedEmailOptional call, shared by every service that resolves
// an account by email on behalf of an operator: AuthService.ForceResetPasswordByEmail,
// OperatorUserService.GetUserByEmail, and WebhookSettingsCLIService.resolveOwner.
// It returns (user, false, nil) when nothing matches — the caller decides how
// to report "not found" in its own vocabulary — and *AmbiguousEmailError when
// more than one row matches, rather than silently returning whichever row the
// underlying query happened to return first.
func resolveUniqueUserByEmail(ctx context.Context, finder normalizedEmailFinder, normalizedEmail string) (models.User, bool, error) {
	matches, err := finder.FindAllByNormalizedEmail(ctx, normalizedEmail)
	if err != nil {
		return models.User{}, false, err
	}
	if len(matches) == 0 {
		return models.User{}, false, nil
	}
	if len(matches) > 1 {
		return models.User{}, false, &AmbiguousEmailError{Email: normalizedEmail, IDs: userIDsOf(matches)}
	}
	return matches[0], true, nil
}

// userIDsOf lists the ids of every matching row, ascending (FindAllByNormalizedEmail
// orders by id), so an ambiguous address is reported the same way `users list`
// orders accounts and the operator can go straight from the message to a
// `--id` retry.
func userIDsOf(users []models.User) []uint {
	ids := make([]uint, len(users))
	for i, user := range users {
		ids[i] = user.ID
	}
	return ids
}
