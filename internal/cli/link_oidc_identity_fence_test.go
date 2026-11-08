package cli

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestRunLinkOIDCIdentityCommandRefusesWithoutAUsableFence is the operator
// half of the link's session revocation: linking bumps the account's
// AuthSessionVersion, so from a shell that cannot see the server's fence the
// command refuses with nothing written — no identity row and no session bump —
// rather than recording the revocation only in the database, where a restored
// backup would undo it.
func TestRunLinkOIDCIdentityCommandRefusesWithoutAUsableFence(t *testing.T) {
	t.Parallel()

	for name, fencePath := range map[string]string{
		"unset":    "",
		"relative": filepath.Join("fence", "calendar-feed.fence"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			databasePath := filepath.Join(t.TempDir(), "cli-link-oidc-no-fence.db")
			user := createCLILinkOIDCUser(t, databasePath, "cli-link-no-fence@example.com")
			before := loadCLIUsersRow(t, databasePath, user.ID)

			var output bytes.Buffer
			err := runLinkOIDCIdentityCommand(
				db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
				validLinkOIDCIdentityConfig(), fencePath,
				[]string{"--id", strconv.FormatUint(uint64(user.ID), 10), "--issuer", "https://idp.example.com", "--subject", "cli-link-no-fence"},
				&output,
			)
			if err == nil || !strings.Contains(err.Error(), "CALENDAR_FEED_FENCE_PATH") || !strings.Contains(err.Error(), "Nothing was changed") {
				t.Fatalf("expected the operator fence refusal, got %v", err)
			}
			if strings.Contains(output.String(), "Linked OIDC identity") {
				t.Fatalf("a refused link must not report success, got %q", output.String())
			}
			if _, found := loadCLILinkedOIDCIdentity(t, databasePath, user.ID); found {
				t.Fatal("a refused link must persist no identity")
			}
			after := loadCLIUsersRow(t, databasePath, user.ID)
			if after.AuthSessionVersion != before.AuthSessionVersion {
				t.Fatalf("a refused link must not revoke sessions: version %d became %d", before.AuthSessionVersion, after.AuthSessionVersion)
			}
		})
	}
}
