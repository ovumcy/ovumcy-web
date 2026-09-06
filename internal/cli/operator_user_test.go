package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestNormalizeOperatorEmailArgumentMatchesTheLookupItFeeds pins the property
// the pre-check exists for: an address this function accepts is an address
// OperatorUserService can then resolve, and one it rejects is one the service
// would have rejected too.
//
// The decorated form is the case that mattered. net/mail.ParseAddress — the
// check this replaced — accepts `Owner <owner@example.com>`, while
// NormalizeAuthEmail refuses it, so the argument passed the command's own
// check and was refused several frames later by a sentinel the command did not
// map: the operator got the raw "operator user email is invalid" text, and for
// `reset-password` that happened after the password prompt.
func TestNormalizeOperatorEmailArgumentMatchesTheLookupItFeeds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "a bare address is normalized to the stored form", raw: "  Owner@Example.COM  ", want: "owner@example.com"},
		{name: "a display name is refused", raw: "Owner <owner@example.com>", wantErr: true},
		{name: "angle brackets alone are refused", raw: "<owner@example.com>", wantErr: true},
		{name: "a value that is not an address at all is refused", raw: "not-an-email", wantErr: true},
		{name: "blank is refused", raw: "   ", wantErr: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeOperatorEmailArgument(testCase.raw)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected %q to be refused, got %q", testCase.raw, got)
				}
				if !strings.Contains(err.Error(), "invalid email address") {
					t.Fatalf("expected the operator-facing wording, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeOperatorEmailArgument(%q): %v", testCase.raw, err)
			}
			if got != testCase.want {
				t.Fatalf("normalizeOperatorEmailArgument(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
			// The service is the authority: whatever this returns must survive
			// the normalization the lookup itself performs, unchanged.
			if services.NormalizeAuthEmail(got) != got {
				t.Fatalf("%q does not survive the lookup's own normalization, so the pre-check accepts what the lookup would refuse", got)
			}
		})
	}
}

// TestMapOperatorUserLookupErrorBranches unit-tests the mapper `reset-password`,
// `users delete`, `users set-email` and `link-oidc-identity` share, directly
// with constructed sentinels — cheaper and more precise than engineering a live
// database fixture per outcome, and the only layer some of them are reachable
// at: the ambiguous-address shape cannot be produced through a fully migrated
// database at all, because idx_users_email_normalized forbids two rows on one
// mailbox. It is reachable in the wild only on a database migrated from a
// version that allowed it, which is exactly the state
// TestOperatorUserServiceGetUserByEmailRefusesAnAmbiguousAddress covers on the
// service side.
func TestMapOperatorUserLookupErrorBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		err             error
		userID          uint
		normalizedEmail string
		want            string
	}{
		{
			name:            "an ambiguous address names every match and points at --id",
			err:             &services.AmbiguousEmailError{Email: "owner@example.com", IDs: []uint{5, 18}},
			normalizedEmail: "owner@example.com",
			want:            "email owner@example.com matches 2 accounts (ids 5, 18); retry with --id (see ovumcy users list)",
		},
		{
			name:   "an unknown id points at users list",
			err:    services.ErrOperatorUserNotFound,
			userID: 42,
			want:   "no account carries id 42 (see ovumcy users list)",
		},
		{
			name:            "an unknown address is named back",
			err:             services.ErrOperatorUserNotFound,
			normalizedEmail: "owner@example.com",
			want:            "user owner@example.com not found",
		},
		{
			name: "a missing id",
			err:  services.ErrOperatorUserIDRequired,
			want: "an account id is required (see ovumcy users list)",
		},
		{
			name: "a missing address",
			err:  services.ErrOperatorUserEmailRequired,
			want: "email is required",
		},
		{
			name: "a decorated address",
			err:  services.ErrOperatorUserEmailInvalid,
			want: "invalid email address: pass the bare address, with no display name or angle brackets",
		},
		{
			name: "a storage failure keeps its cause",
			err:  errors.New("db exploded"),
			want: "look up account: db exploded",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := mapOperatorUserLookupError(testCase.err, testCase.userID, testCase.normalizedEmail)
			if got == nil || got.Error() != testCase.want {
				t.Fatalf("mapOperatorUserLookupError() = %v, want %q", got, testCase.want)
			}
		})
	}
}
