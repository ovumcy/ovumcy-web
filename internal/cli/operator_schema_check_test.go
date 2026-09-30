package cli

import (
	"bytes"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestOperatorCommandsRefuseADatabaseWithoutTheNormalizedEmailIndex drives
// every account subcommand against a database whose idx_users_email_normalized
// was dropped out of band. Each must answer with the server's own boot refusal
// before it asks for a password or touches an account: without the index, `users
// create` or `users set-email` can put a second account on an address.
func TestOperatorCommandsRefuseADatabaseWithoutTheNormalizedEmailIndex(t *testing.T) {
	t.Parallel()

	commands := []struct {
		name string
		run  func(config db.Config, owner models.User, output *bytes.Buffer) error
	}{
		{"users list", func(config db.Config, _ models.User, output *bytes.Buffer) error {
			return runUsersCommand(config, []string{"list"}, "", strings.NewReader(""), output)
		}},
		{"users create", func(config db.Config, owner models.User, output *bytes.Buffer) error {
			return runUsersCommand(config, []string{"create", " " + strings.ToUpper(owner.Email)}, "", strings.NewReader("StrongPass1\n"), output)
		}},
		{"users set-email", func(config db.Config, owner models.User, output *bytes.Buffer) error {
			return runUsersCommand(config, []string{"set-email", "--id", strconv.FormatUint(uint64(owner.ID), 10), "second@example.com"}, "", strings.NewReader(""), output)
		}},
		{"users delete", func(config db.Config, owner models.User, output *bytes.Buffer) error {
			return runUsersCommand(config, []string{"delete", owner.Email, "--yes"}, "/fence/ovumcy.fence", strings.NewReader(""), output)
		}},
		{"reset-password", func(config db.Config, owner models.User, output *bytes.Buffer) error {
			prompt := func() ([]byte, error) {
				t.Errorf("reset-password prompted for a password against a database it must refuse")
				return []byte("StrongPass2"), nil
			}
			return runResetPasswordCommand(config, []string{owner.Email}, "/fence/ovumcy.fence", prompt, output)
		}},
		{"link-oidc-identity", func(config db.Config, owner models.User, output *bytes.Buffer) error {
			return runLinkOIDCIdentityCommand(config, validLinkOIDCIdentityConfig(), []string{owner.Email, "--issuer", "https://idp.example.com", "--subject", "schema-check-subject"}, output)
		}},
		{"webhook show", func(config db.Config, owner models.User, output *bytes.Buffer) error {
			return runWebhookCommand(config, testWebhookSecretKey, []string{"show", owner.Email}, strings.NewReader(""), output)
		}},
		{"webhook set", func(config db.Config, owner models.User, output *bytes.Buffer) error {
			return runWebhookCommand(config, testWebhookSecretKey, []string{"set", owner.Email, "--enabled=true", "--url-stdin"}, strings.NewReader(testWebhookURLWithToken+"\n"), output)
		}},
		{"notify", func(config db.Config, _ models.User, output *bytes.Buffer) error {
			return runNotifyOperatorCommand(config, testWebhookSecretKey, "en", time.UTC, false, nil, output)
		}},
	}

	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			t.Parallel()

			databasePath := createCLIUsersDatabase(t)
			owner := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
			config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}
			dropCLINormalizedEmailIndex(t, config)
			before := snapshotCLIAccountTables(t, config)

			var output bytes.Buffer
			err := command.run(config, owner, &output)
			if err == nil || !strings.HasPrefix(err.Error(), "schema check failed: ") || !strings.Contains(err.Error(), db.NormalizedEmailIndexName) {
				t.Fatalf("want the schema refusal naming %s, got %v", db.NormalizedEmailIndexName, err)
			}
			if output.Len() != 0 {
				t.Fatalf("a refused command printed %q", output.String())
			}
			if after := snapshotCLIAccountTables(t, config); !reflect.DeepEqual(before, after) {
				t.Fatalf("a refused command changed the account tables:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// TestOperatorRepositoriesOpenAMigratedDatabase is the control: the check the
// refusal above depends on passes on the database the migrations built.
func TestOperatorRepositoriesOpenAMigratedDatabase(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	repositories, fence, closeDatabase, err := openOperatorRepositories(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}, "")
	if err != nil {
		t.Fatalf("openOperatorRepositories on a migrated database: %v", err)
	}
	defer closeDatabase()
	if repositories == nil || fence == nil {
		t.Fatalf("openOperatorRepositories returned repositories %v and fence %v", repositories, fence)
	}
}

// TestOperatorNotifyWritesItsReportToTheInjectedOutput is the control for the
// notify row above: that row's "printed nothing" claim only means something
// while the injected writer is the one a completed pass prints to.
func TestOperatorNotifyWritesItsReportToTheInjectedOutput(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}

	var output bytes.Buffer
	if err := runNotifyOperatorCommand(config, testWebhookSecretKey, "en", time.UTC, false, []string{"--dry-run"}, &output); err != nil {
		t.Fatalf("notify on a migrated database: %v", err)
	}
	if !strings.Contains(output.String(), "Webhook notify pass complete") {
		t.Fatalf("the report did not reach the injected writer, got %q", output.String())
	}
}

func dropCLINormalizedEmailIndex(t *testing.T, config db.Config) {
	t.Helper()

	database, err := db.OpenDatabase(config)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := database.Exec("DROP INDEX " + db.NormalizedEmailIndexName).Error; err != nil {
		t.Fatalf("drop index: %v", err)
	}
}

func snapshotCLIAccountTables(t *testing.T, config db.Config) map[string][]map[string]any {
	t.Helper()

	database, err := db.OpenDatabase(config)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	snapshot := map[string][]map[string]any{}
	for _, table := range []string{"users", "oidc_identities", "app_state"} {
		var rows []map[string]any
		if err := database.Table(table).Order("rowid").Find(&rows).Error; err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		snapshot[table] = rows
	}
	return snapshot
}
