package testdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	postgresTestUser     = "ovumcy"
	postgresTestPassword = "ovumcy"
	postgresTestImage    = "postgres:17-alpine"

	dockerCommandTimeout          = 30 * time.Second
	dockerImagePullTimeout        = 3 * time.Minute
	dockerRunTimeout              = 3 * time.Minute
	postgresContainerReadyTimeout = 90 * time.Second
	postgresHostReachableTimeout  = 90 * time.Second
	postgresPingTimeout           = 5 * time.Second
)

// StartPostgresDSN launches an isolated Postgres container for tests and
// returns a DSN suitable for gorm.io/driver/postgres.
func StartPostgresDSN(t *testing.T, databaseName string) string {
	t.Helper()

	dsn, _ := StartPostgres(t, databaseName)
	return dsn
}

// StartPostgres is StartPostgresDSN plus the container id, for a test that must
// also run a command INSIDE the container — the operator runbook reaches
// pg_dump and psql through `docker compose exec -T postgres`, and a guard that
// executes those commands needs the container the DSN points at, not only a
// host connection to it. The container is started with POSTGRES_USER and
// POSTGRES_DB set, so `$POSTGRES_USER`/`$POSTGRES_DB` resolve inside it exactly
// as they do in the bundled compose stack.
func StartPostgres(t *testing.T, databaseName string) (dsn string, containerID string) {
	t.Helper()

	requireDockerBinary(t)

	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		t.Fatal("postgres test database name is required")
	}

	ensurePostgresImageAvailable(t)

	containerID = runDockerCommand(t, "run", "-d", "--rm", "-P",
		"-e", "POSTGRES_USER="+postgresTestUser,
		"-e", "POSTGRES_PASSWORD="+postgresTestPassword,
		"-e", "POSTGRES_DB="+databaseName,
		postgresTestImage,
	)

	t.Cleanup(func() {
		_ = runDockerCommandAllowFailure("rm", "-f", containerID)
	})

	waitForPostgresReadiness(t, containerID, databaseName)
	port := loadPostgresMappedPort(t, containerID)
	dsn = fmt.Sprintf(
		"host=127.0.0.1 port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		port,
		postgresTestUser,
		postgresTestPassword,
		databaseName,
	)
	waitForHostSQLReadiness(t, dsn)

	return dsn, containerID
}

func waitForPostgresReadiness(t *testing.T, containerID string, databaseName string) {
	t.Helper()

	deadline := time.Now().Add(postgresContainerReadyTimeout)
	for time.Now().Before(deadline) {
		if _, err := runDockerCommandWithError("exec", containerID, "pg_isready", "-U", postgresTestUser, "-d", databaseName); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	logs, _ := runDockerCommandWithError("logs", containerID)
	t.Fatalf("postgres test container %s did not become ready in time; logs: %s", containerID, logs)
}

func loadPostgresMappedPort(t *testing.T, containerID string) string {
	t.Helper()

	output := runDockerCommand(t, "port", containerID, "5432/tcp")
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("docker port returned no mapping for postgres container %s", containerID)
	}

	mapping := strings.TrimSpace(lines[0])
	lastColon := strings.LastIndex(mapping, ":")
	if lastColon < 0 || lastColon == len(mapping)-1 {
		t.Fatalf("unexpected docker port mapping %q", mapping)
	}
	return mapping[lastColon+1:]
}

func waitForHostSQLReadiness(t *testing.T, dsn string) {
	t.Helper()

	deadline := time.Now().Add(postgresHostReachableTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		pingErr := pingPostgresDSN(dsn)
		if pingErr == nil {
			return
		}
		lastErr = pingErr
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("postgres test database did not become reachable from host in time: %v", lastErr)
}

func pingPostgresDSN(dsn string) error {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), postgresPingTimeout)
	defer cancel()
	return database.PingContext(ctx)
}

func ensurePostgresImageAvailable(t *testing.T) {
	t.Helper()

	if _, err := runDockerCommandWithError("image", "inspect", postgresTestImage); err == nil {
		return
	}

	runDockerCommand(t, "pull", postgresTestImage)
}

// requirePostgresEnv arms fail-closed mode. Unset — the default on a developer
// machine — a host that cannot run the Postgres suite skips it, exactly as
// before. Set, a skip becomes a failure, because on the CI job that owns this
// suite a skip is a lane reporting success for tests that never ran.
const requirePostgresEnv = "OVUMCY_REQUIRE_POSTGRES"

// postgresIsRequired reads the gate fail-closed: anything other than an absent,
// empty or explicitly false value arms it. A gate that disarmed on an
// unexpected value would restore the silence it exists to remove, and would do
// it invisibly — the lane would simply go green again.
func postgresIsRequired() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(requirePostgresEnv))) {
	case "", "0", "false":
		return false
	default:
		return true
	}
}

// testingT is the slice of *testing.T the docker helpers below actually use.
// It exists so the failure-mode guard can observe WHICH verdict a docker error
// reaches — a skip or a failure — without a docker daemon and without failing
// the test that does the observing. *testing.T satisfies it unchanged.
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
}

// skipUnlessPostgresRequired is the ONLY place in this package that may skip,
// and requireDockerBinary is its only caller.
func skipUnlessPostgresRequired(t testingT, format string, args ...any) {
	t.Helper()

	reason := fmt.Sprintf(format, args...)
	if postgresIsRequired() {
		t.Fatalf("%s is set, so a skipped postgres suite is a failure: %s", requirePostgresEnv, reason)
		return
	}
	t.Skipf("%s", reason)
}

// requireDockerBinary is the one probe allowed to skip: a host with no docker
// binary at all cannot run these tests, and on a developer machine that is not
// an operational failure. Every docker error AFTER this point is one.
func requireDockerBinary(t testingT) {
	t.Helper()

	if _, err := dockerLookPath("docker"); err != nil {
		skipUnlessPostgresRequired(t, "docker is required for postgres tests: %v", err)
	}
}

// runDockerCommand runs a docker command that has already cleared the
// docker-absent preflight, so its failure is operational — a broken image, an
// exhausted port range, a pull that timed out — and fails the test. It used to
// skip, and the image pull, the container start and the port lookup all come
// through here: a broken runner therefore reported the identical "docker is
// unavailable" skip as a host with no docker at all, and the package went green
// having tested nothing. The two readiness waits already failed loudly and are
// unchanged.
func runDockerCommand(t testingT, args ...string) string {
	t.Helper()

	output, err := runDockerCommandWithError(args...)
	if err != nil {
		t.Fatalf("the docker-absent preflight passed, so this is an operational failure and not a reason to skip: %v", err)
		return ""
	}
	return output
}

// dockerLookPath and runDockerCommandWithError are variables, not plain
// functions, so the failure-mode guard can inject "docker is absent" and "a
// docker command failed" separately. Production code reads them as ordinary
// calls.
var dockerLookPath = exec.LookPath

var runDockerCommandWithError = func(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeoutFor(args...))
	defer cancel()

	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("docker %s timed out", strings.Join(args, " "))
	}
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func runDockerCommandAllowFailure(args ...string) error {
	_, err := runDockerCommandWithError(args...)
	return err
}

func dockerTimeoutFor(args ...string) time.Duration {
	if len(args) == 0 {
		return dockerCommandTimeout
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "pull":
		return dockerImagePullTimeout
	case "run":
		return dockerRunTimeout
	default:
		return dockerCommandTimeout
	}
}
