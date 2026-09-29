package main

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestMustVerifySchemaInvariantsLetsAMigratedDatabaseBoot drives the real boot
// wrapper against a freshly migrated database: it must return rather than stop
// the process. The refusals are proven in internal/db and internal/bootstrap;
// the guard in boot_pass_budget_guard_test.go pins this call in main() by name.
func TestMustVerifySchemaInvariantsLetsAMigratedDatabaseBoot(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "schema-invariants-boot.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories, _ := bootstrap.BuildRepositories(database, "")

	mustVerifySchemaInvariants(repositories)
}

// TestServerRefusesToStartWithoutTheNormalizedEmailIndex runs the built binary
// against a database whose index was dropped out of band. It is the one test
// that reaches the refusal through main() and mustVerifySchemaInvariants'
// log.Fatalf: the server must exit non-zero with the refusal in its output,
// where a wrapper that only logged the error would go on to listen until the
// deadline. Skipped under `go test -short`, like the CLI smoke.
func TestServerRefusesToStartWithoutTheNormalizedEmailIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess boot test skipped under -short")
	}

	dbPath := filepath.Join(t.TempDir(), "missing-index.db")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	if err := database.Exec("DROP INDEX " + db.NormalizedEmailIndexName).Error; err != nil {
		t.Fatalf("drop index: %v", err)
	}
	closeDatabase(database)

	binary := buildOvumcyBinary(t)
	env := map[string]string{
		"DB_DRIVER":                "sqlite",
		"DB_PATH":                  dbPath,
		"SECRET_KEY":               strings.Repeat("a", 32),
		"PORT":                     strconv.Itoa(freeTCPPort(t)),
		"CALENDAR_FEED_FENCE_PATH": "",
	}
	const deadline = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = filteredOSEnv(env)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("the server was still running after %v on a database without %s; it must refuse to start\n%s", deadline, db.NormalizedEmailIndexName, output.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the server must exit non-zero on a database without %s, got err=%v\n%s", db.NormalizedEmailIndexName, err, output.String())
	}
	for _, want := range []string{"schema check failed", db.NormalizedEmailIndexName + " on users (lower(trim(email))) is missing"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("the refusal must contain %q, got:\n%s", want, output.String())
		}
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}
