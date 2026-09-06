package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// This file holds the addressing vocabulary the account subcommands share:
// how an operator's `<email>|--id <id>` pair is turned into a row, and how a
// refusal to turn it into one is reported. `reset-password`, `users delete`
// and `link-oidc-identity` take the same pair and must answer it identically —
// an operator who cannot reach an account should not have to work out which
// subcommand's private wording they are reading.

// normalizeOperatorEmailArgument applies the SAME rule the account lookup
// resolves by — services.NormalizeAuthEmail, which OperatorUserService's own
// normalizeOperatorUserEmail calls — so the address a subcommand accepts is
// exactly the address it can then resolve.
//
// net/mail.ParseAddress is the check this replaced, and it is broader in
// precisely the way that matters here: it accepts `Owner <a@b.com>`, which
// NormalizeAuthEmail refuses, so the argument passed the CLI's own check and
// was refused several steps later by a sentinel the command did not map.
func normalizeOperatorEmailArgument(raw string) (string, error) {
	normalized := services.NormalizeAuthEmail(raw)
	if normalized == "" {
		return "", errors.New("invalid email address: pass the bare address, with no display name or angle brackets")
	}
	return normalized, nil
}

// resolveOperatorUser turns one address handle into a row. The id form exists
// because the address form cannot reach every row: a legacy stored value the
// strict NormalizeAuthEmail rule refuses is rejected before any lookup runs,
// and its bare address resolves the OTHER account on that mailbox — the one
// the boot repair let keep the address.
func resolveOperatorUser(service *services.OperatorUserService, userID uint, email string) (models.OperatorUserSummary, error) {
	if userID != 0 {
		return service.GetUserByID(context.Background(), userID)
	}
	return service.GetUserByEmail(context.Background(), email)
}

// mapOperatorUserLookupError is the one operator-facing wording for every way
// resolveOperatorUser can refuse. Its callers pass the handles they were given
// so the message can name the one the operator actually typed.
//
// The ambiguous-address case is where this command family differs from the
// service's own error text: services.AmbiguousEmailError (shared with
// WebhookSettingsCLIService.resolveOwner — `webhook show|set`, which has no
// --id form at all) is never resolved silently to one match, and the mapping
// here adds the explicit "retry with --id" pointer that error's bare id list
// leaves implicit.
func mapOperatorUserLookupError(err error, userID uint, normalizedEmail string) error {
	var ambiguous *services.AmbiguousEmailError
	if errors.As(err, &ambiguous) {
		return fmt.Errorf(
			"email %s matches %d accounts (ids %s); retry with --id (see ovumcy users list)",
			ambiguous.Email, len(ambiguous.IDs), formatUserIDs(ambiguous.IDs),
		)
	}

	switch {
	case errors.Is(err, services.ErrOperatorUserNotFound):
		return operatorUserNotFoundError(userID, normalizedEmail)
	case errors.Is(err, services.ErrOperatorUserIDRequired):
		return errors.New("an account id is required (see ovumcy users list)")
	case errors.Is(err, services.ErrOperatorUserEmailRequired):
		return errors.New("email is required")
	case errors.Is(err, services.ErrOperatorUserEmailInvalid):
		// Reachable even though every caller normalizes its argument first:
		// the service is the authority on its own preconditions, and a mapper
		// that answers only what today's callers let through turns a later
		// change in one of them into an unreadable raw-sentinel line.
		return errors.New("invalid email address: pass the bare address, with no display name or angle brackets")
	default:
		return fmt.Errorf("look up account: %w", err)
	}
}

// operatorUserNotFoundError says different things for the two handles because
// they were addressed differently: an unknown id points the operator at `users
// list`, while an unknown address might just be a typo of one that was never
// an account.
func operatorUserNotFoundError(userID uint, normalizedEmail string) error {
	if userID != 0 {
		return fmt.Errorf("no account carries id %d (see ovumcy users list)", userID)
	}
	return fmt.Errorf("user %s not found", normalizedEmail)
}

// formatUserIDs renders the ambiguous match list the same style `users list`
// prints them in: ascending, comma-separated.
func formatUserIDs(ids []uint) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	return strings.Join(parts, ", ")
}
