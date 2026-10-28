package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func localSignInOn() (bool, error)  { return true, nil }
func localSignInOff() (bool, error) { return false, nil }

// runUsersCommand is the runner the users tests drive on an instance with local
// sign-in on; the oidc_only cases below call runUsersCommandIn directly.
func runUsersCommand(databaseConfig db.Config, args []string, fencePath string, input io.Reader, output io.Writer) error {
	return runUsersCommandIn(databaseConfig, localSignInOn, args, fencePath, input, output)
}

// Under OIDC_LOGIN_MODE=oidc_only nothing redeems a recovery code, so the CLI
// mints none and says so even on --show-recovery-code (WEB-342).
func TestRunUsersCommandCreateOnAnOIDCOnlyInstanceIssuesNoRecoveryCode(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"create", "owner@example.com", "--show-recovery-code"},
		{"create", "owner@example.com"},
	} {
		t.Run(strings.Join(args[2:], " "), func(t *testing.T) {
			t.Parallel()

			databasePath := createCLIUsersDatabase(t)
			var output bytes.Buffer
			err := runUsersCommandIn(
				db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
				localSignInOff,
				args,
				"",
				strings.NewReader("StrongPass1\n"),
				&output,
			)
			if err != nil {
				t.Fatalf("runUsersCommandIn(%v) returned error: %v", args, err)
			}

			rendered := output.String()
			if strings.Contains(rendered, "OVUM-") || strings.Contains(rendered, "Recovery code:") {
				t.Fatalf("did not expect a recovery code on an oidc_only instance: %q", rendered)
			}
			if !strings.Contains(rendered, "No recovery code was issued") {
				t.Fatalf("expected the output to say no code was issued, got %q", rendered)
			}
			if strings.Contains(rendered, "regenerate one from Settings") {
				t.Fatalf("did not expect the regenerate hint where regeneration is refused: %q", rendered)
			}

			database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			sqlDB, err := database.DB()
			if err != nil {
				t.Fatalf("open sql db: %v", err)
			}
			defer func() { _ = sqlDB.Close() }()
			var stored models.User
			if err := database.Where("email = ?", "owner@example.com").First(&stored).Error; err != nil {
				t.Fatalf("load created owner: %v", err)
			}
			if strings.TrimSpace(stored.RecoveryCodeHash) != "" {
				t.Fatal("expected an empty recovery_code_hash on an oidc_only instance")
			}
			if !stored.LocalAuthEnabled || strings.TrimSpace(stored.PasswordHash) == "" {
				t.Fatal("expected the owner to be created with a local password")
			}
		})
	}
}

// A mode that cannot be resolved refuses the create before the password is
// read or a row is written: guessing either way would be wrong.
func TestRunUsersCreateRefusesWhenTheModeCannotBeResolved(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	service := mustCLIUsersService(t, databasePath)
	modeFault := errors.New("OIDC_ISSUER_URL is required")

	err := runUsersCreate(service, func() (bool, error) { return false, modeFault }, []string{"owner@example.com"}, strings.NewReader("StrongPass1\n"), &bytes.Buffer{})
	if !errors.Is(err, modeFault) {
		t.Fatalf("expected the mode error, got %v", err)
	}
	if emails := listCLIUserEmails(t, databasePath); len(emails) != 0 {
		t.Fatalf("expected no account created, got %#v", emails)
	}
	if err := runUsersCreate(service, nil, []string{"owner@example.com"}, strings.NewReader("StrongPass1\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("expected a nil mode resolver to be refused")
	}
}
