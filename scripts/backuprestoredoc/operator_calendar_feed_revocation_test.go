package backuprestoredoc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/ovumcy/ovumcy-web/internal/testdb"
)

// The claim these two guards make: an OPERATOR-driven calendar-feed revocation
// either survives the documented restore or does not happen at all. Its
// neighbour in this package covers the same restore against a revocation the
// OWNER made through the web path; this one covers the removals an operator
// performs from a shell — `ovumcy users delete` — where the process doing the
// removing is not the server and very often cannot see the server's fence at
// all.
//
// Three things make it an end-to-end guard rather than a unit test of the gate:
//
//   - the CLI is the REAL binary, executed as its own process with an
//     environment stated here in full. The defect this closes is precisely that
//     the command builds its fence out of ITS OWN environment, so a gate driven
//     in-process with a fence handed to it cannot observe it.
//   - the backup and the restore are the operator's own documented commands,
//     read out of the runbook by the rest of this package.
//   - the verdict is an unauthenticated HTTP GET of the old subscribe URL
//     through the shipped route table and the real handler, which is the
//     surface a calendar client actually polls. Every layer below it can be
//     right while the URL still answers 200.
//
// The fence file is created OUTSIDE the volume and outside the dump for the
// reason its neighbour states: the documented restore replaces the database,
// and a fence restored with it could never disagree with it.
//
// What each scenario asserts is BEHAVIOUR — refused, or removed and still
// removed after the restore — never which branch produced it, and one case is
// deliberately defended twice: an unset fence path is refused by the CLI's own
// path check, and would be refused a second time by the advance itself, whose
// anchor reports "not configured" for the empty path. Removing either branch
// alone therefore leaves that scenario green (measured), and removing the gate
// from the subcommand reddens it. A reader looking for the narrow unit
// coverage of each refusal shape finds it beside the gate, in internal/cli.

const (
	// runbookOperatorTargetEmail is the SECOND account each scenario keeps
	// beside the feed owner. The scenarios that must observe a REFUSAL address
	// it rather than the owner, so a scenario cannot pass by removing the very
	// row whose feed it then finds unreachable.
	runbookOperatorTargetEmail = "operator-target@example.com"

	// operatorRefusalTail is what every refusal that wrote nothing ends with.
	// An operator reading the tail of the message has to be able to tell that
	// the command it names did not half-run.
	operatorRefusalTail = "Nothing was changed"

	// operatorReconcileRemedy is the remedy of the refusals whose cause is a
	// fence only the SERVER can reconcile — the shape a restore leaves behind.
	// It is asserted where a scenario must prove WHICH refusal it got, because
	// a run whose fence path never reached the process would refuse too, for a
	// reason that proves nothing.
	operatorReconcileRemedy = "Start the server once"

	operatorCLIBuildBudget = 5 * time.Minute
	operatorCLIRunBudget   = 2 * time.Minute
)

// TestOperatorRevocationSurvivesTheDocumentedVolumeRestore is the SQLite half,
// over the runbook's named-volume archive and restore.
func TestOperatorRevocationSurvivesTheDocumentedVolumeRestore(t *testing.T) {
	commands := documentedVolumeCommands(t)
	requireDocker(t)
	binary := buildOperatorCLI(t)

	runOperatorRevocationScenarios(t, func(t *testing.T) runbookInstance {
		t.Helper()

		return &volumeRunbookInstance{
			commands: commands,
			binary:   binary,
			volume:   ephemeralVolume(t),
			workdir:  t.TempDir(),
			fence:    filepath.Join(t.TempDir(), "calendar-feed.fence"),
		}
	})
}

// TestOperatorRevocationSurvivesTheDocumentedPostgresRestore is the same five
// claims over the runbook's Postgres dump and replay. The engine is the only
// thing that differs, which is why both halves run the SAME scenario bodies:
// the fence lives outside the database either way, and the operator runs the
// same command against both.
func TestOperatorRevocationSurvivesTheDocumentedPostgresRestore(t *testing.T) {
	commands := documentedPostgresCommands(t)
	binary := buildOperatorCLI(t)

	runOperatorRevocationScenarios(t, func(t *testing.T) runbookInstance {
		t.Helper()

		dsn, container := testdb.StartPostgres(t, "ovumcy_runbook_operator_feed")
		return &postgresRunbookInstance{
			commands:  commands,
			binary:    binary,
			container: container,
			config:    db.Config{Driver: db.DriverPostgres, PostgresURL: dsn},
			dsn:       dsn,
			backupDir: t.TempDir(),
			fence:     filepath.Join(t.TempDir(), "calendar-feed.fence"),
		}
	})
}

// runOperatorRevocationScenarios is the whole claim, written once and run
// against each engine. Every scenario gets a brand-new instance: a fresh
// database, a fresh fence file, and a backup of its own.
func runOperatorRevocationScenarios(t *testing.T, newInstance func(t *testing.T) runbookInstance) {
	t.Helper()

	// An operator's shell with no fence configured is the ordinary case — the
	// server has one, the shell that runs the CLI does not. The removal must
	// not happen at all there: recorded only inside the database, it is undone
	// by restoring a backup taken before it, and nothing in the run would say
	// so.
	t.Run("an unset fence path refuses the removal", func(t *testing.T) {
		instance := newInstance(t)
		armed, targetID := bootAndArmRunbookInstance(t, instance)
		instance.documentedBackup(t)

		result := instance.operatorCLI(t, operatorFenceUnset, "users", "delete", "--id", formatRunbookID(targetID), "--yes")
		assertOperatorCLIRefusedTheRemoval(t, result)

		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRunbookAccountPresent(t, repos, runbookOperatorTargetEmail)
			assertRunbookFeedServesOverHTTP(t, repos, armed)
		})

		// The restore and the restart change neither answer: nothing was
		// revoked, so nothing may be disarmed either. This is the positive
		// anchor for the two scenarios below, where the same restart is what
		// takes a resurrected feed down.
		instance.documentedRestore(t)
		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRunbookBootIsANoOp(t, repos, instance.serverFencePath())
			assertRunbookAccountPresent(t, repos, runbookOperatorTargetEmail)
			assertRunbookFeedServesOverHTTP(t, repos, armed)
		})
	})

	// The same refusal for a path that is set and absolute but names a
	// directory this process cannot see — what copying the container's fence
	// path into a host shell gives, and what an unmounted volume looks like.
	t.Run("a fence path with no directory behind it refuses the removal", func(t *testing.T) {
		instance := newInstance(t)
		armed, targetID := bootAndArmRunbookInstance(t, instance)
		instance.documentedBackup(t)

		result := instance.operatorCLI(t, operatorFenceMissingDirectory, "users", "delete", "--id", formatRunbookID(targetID), "--yes")
		assertOperatorCLIRefusedTheRemoval(t, result, result.fencePath)

		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRunbookAccountPresent(t, repos, runbookOperatorTargetEmail)
			assertRunbookFeedServesOverHTTP(t, repos, armed)
		})

		instance.documentedRestore(t)
		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRunbookBootIsANoOp(t, repos, instance.serverFencePath())
			assertRunbookAccountPresent(t, repos, runbookOperatorTargetEmail)
			assertRunbookFeedServesOverHTTP(t, repos, armed)
		})
	})

	// The confirmed removal: the CLI can see the server's own fence, so the
	// erasure is recorded outside the database and goes through. The restore
	// then brings the account and its subscribe URL back — that is what a
	// restore does — and the restart the runbook tells the operator to perform
	// takes the URL down again.
	t.Run("a confirmed removal outlives the documented restore", func(t *testing.T) {
		instance := newInstance(t)
		armed, _ := bootAndArmRunbookInstance(t, instance)
		instance.documentedBackup(t)

		result := instance.operatorCLI(t, operatorFenceServerFile, "users", "delete", "--id", formatRunbookID(armed.ownerID), "--yes")
		assertOperatorCLIRemovedTheAccount(t, result)

		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
		})

		instance.documentedRestore(t)
		instance.withDatabase(t, func(repos *db.Repositories) {
			// The finding first, at the layer the owner sees: the restore put
			// the whole row back and the old URL answers again.
			assertRestoreResurrectedTheFeed(t, repos, armed)
			assertRunbookFeedServesOverHTTP(t, repos, armed)

			assertRunbookBootDisarmed(t, repos, instance.serverFencePath())
			assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
		})
	})

	// The same, with the server started once between the removal and the
	// restore. That start reconciles both halves onto the token the removal
	// minted, which must not cost the fence its memory of the removal: the
	// restore still lands on a database older than the file.
	t.Run("a confirmed removal outlives a restore across an intermediate boot", func(t *testing.T) {
		instance := newInstance(t)
		armed, _ := bootAndArmRunbookInstance(t, instance)
		instance.documentedBackup(t)

		result := instance.operatorCLI(t, operatorFenceServerFile, "users", "delete", "--id", formatRunbookID(armed.ownerID), "--yes")
		assertOperatorCLIRemovedTheAccount(t, result)

		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRunbookBootIsANoOp(t, repos, instance.serverFencePath())
			assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
		})

		instance.documentedRestore(t)
		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRestoreResurrectedTheFeed(t, repos, armed)
			assertRunbookFeedServesOverHTTP(t, repos, armed)

			assertRunbookBootDisarmed(t, repos, instance.serverFencePath())
			assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
		})
	})

	// The window the gate exists for: a restore has already happened and the
	// server has NOT been started since, so the database half is behind the
	// file half and the next boot is what would answer for it. An operator
	// command that recorded its own removal here would move both halves onto
	// one fresh token, the halves would agree, and the boot would find nothing
	// to answer for — the restore masked by the very mechanism meant to catch
	// it, and a feed the owner revoked serving again.
	t.Run("a restore not yet answered for is not masked by the operator CLI", func(t *testing.T) {
		instance := newInstance(t)
		armed, targetID := bootAndArmRunbookInstance(t, instance)
		instance.documentedBackup(t)

		// The owner revokes through the web path, exactly as its neighbour
		// guard does.
		instance.withDatabase(t, func(repos *db.Repositories) {
			if err := repos.Users.ClearCalendarFeedToken(context.Background(), armed.ownerID); err != nil {
				t.Fatalf("revoke the calendar feed: %v", err)
			}
			assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
		})

		instance.documentedRestore(t)

		result := instance.operatorCLI(t, operatorFenceServerFile, "users", "delete", "--id", formatRunbookID(targetID), "--yes")
		assertOperatorCLIRefusedTheRemoval(t, result, instance.serverFencePath(), operatorReconcileRemedy)

		instance.withDatabase(t, func(repos *db.Repositories) {
			assertRunbookAccountPresent(t, repos, runbookOperatorTargetEmail)
			// The resurrected feed is still live at this point: no boot has
			// run. Without this the 404 below could come from a feed that was
			// never restored in the first place.
			assertRunbookFeedServesOverHTTP(t, repos, armed)

			assertRunbookBootDisarmed(t, repos, instance.serverFencePath())
			assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
		})
	})
}

// runbookInstance is one deployment under test. Each engine implements it over
// its own documented procedure; the scenarios above address only this
// interface, so the two halves cannot drift into asserting different things.
type runbookInstance interface {
	// serverFencePath is the anchor the SERVER was configured with. It always
	// lives outside whatever the documented backup captures.
	serverFencePath() string
	// documentedBackup and documentedRestore run the runbook's own commands.
	documentedBackup(t *testing.T)
	documentedRestore(t *testing.T)
	// withDatabase opens this instance's database through the repositories
	// production wires — the restore fence attached — and persists whatever
	// the callback changed.
	withDatabase(t *testing.T, fn func(repos *db.Repositories))
	// withUnfencedDatabase is the same, for the period BEFORE the operator
	// mounted a fence: the fence object is attached exactly as production
	// attaches it, over an anchor with no path, so every write goes through the
	// code an unfenced server really runs rather than through a shortcut that
	// skips it. Its user is the guard for a backup taken during that period.
	withUnfencedDatabase(t *testing.T, fn func(repos *db.Repositories))
	// operatorCLI runs the real binary against this instance's database, with
	// the fence the scenario chose and nothing else of this machine's
	// environment that the application reads.
	operatorCLI(t *testing.T, fence operatorFenceSetting, args ...string) operatorCLIResult
}

// operatorFenceSetting is how a scenario configures the operator's shell.
type operatorFenceSetting int

const (
	// operatorFenceUnset leaves CALENDAR_FEED_FENCE_PATH out of the
	// environment entirely.
	operatorFenceUnset operatorFenceSetting = iota
	// operatorFenceMissingDirectory sets an absolute path whose directory does
	// not exist. The anchor reports that as "no token", never as an error, so
	// it is a different refusal from an unreadable file and gets its own
	// scenario.
	operatorFenceMissingDirectory
	// operatorFenceServerFile points the CLI at the server's own fence file —
	// the only configuration under which an operator removal may proceed.
	operatorFenceServerFile
)

// resolve turns the setting into the value the CLI's environment carries.
func (setting operatorFenceSetting) resolve(t *testing.T, serverFencePath string) string {
	t.Helper()

	switch setting {
	case operatorFenceUnset:
		return ""
	case operatorFenceMissingDirectory:
		return filepath.Join(t.TempDir(), "no-such-directory", "calendar-feed.fence")
	case operatorFenceServerFile:
		return serverFencePath
	default:
		t.Fatalf("unknown operator fence setting %d", setting)
		return ""
	}
}

// volumeRunbookInstance is the SQLite deployment: the application's database
// inside a named docker volume, archived and restored by the runbook's own
// commands.
type volumeRunbookInstance struct {
	commands volumeCommands
	binary   string
	volume   string
	workdir  string
	fence    string
}

func (instance *volumeRunbookInstance) serverFencePath() string { return instance.fence }

func (instance *volumeRunbookInstance) documentedBackup(t *testing.T) {
	t.Helper()

	if output, err := runVolumeScript(t, instance.workdir, instance.volume, instance.commands.backup); err != nil {
		t.Fatalf("the documented archive command failed: %v\n%s", err, output)
	}
}

func (instance *volumeRunbookInstance) documentedRestore(t *testing.T) {
	t.Helper()

	if output, err := runVolumeScript(t, instance.workdir, instance.volume, instance.commands.restore); err != nil {
		t.Fatalf("the documented restore failed: %v\n%s", err, output)
	}
}

func (instance *volumeRunbookInstance) withDatabase(t *testing.T, fn func(repos *db.Repositories)) {
	t.Helper()

	instance.withDatabaseFencedAt(t, instance.fence, fn)
}

func (instance *volumeRunbookInstance) withUnfencedDatabase(t *testing.T, fn func(repos *db.Repositories)) {
	t.Helper()

	instance.withDatabaseFencedAt(t, "", fn)
}

func (instance *volumeRunbookInstance) withDatabaseFencedAt(t *testing.T, fencePath string, fn func(repos *db.Repositories)) {
	t.Helper()

	withVolumeCopy(t, instance.volume, instance.commands, func(dir string, repos *db.Repositories) {
		fn(fencedRepositories(repos, fencePath))
		writeVolume(t, instance.volume, instance.commands.image, dir)
	})
}

// operatorCLI hands the volume's own copy of the database to the binary and
// puts back whatever the command left behind — including a command that
// changed nothing, so a scenario asserting "nothing changed" is reading the
// same bytes the CLI saw.
func (instance *volumeRunbookInstance) operatorCLI(t *testing.T, fence operatorFenceSetting, args ...string) operatorCLIResult {
	t.Helper()

	dir := t.TempDir()
	readVolume(t, instance.volume, instance.commands.image, dir)
	result := runOperatorCLI(t, instance.binary, map[string]string{
		"DB_DRIVER": string(db.DriverSQLite),
		"DB_PATH":   filepath.Join(dir, sqliteDatabaseFile),
	}, fence.resolve(t, instance.fence), args...)
	writeVolume(t, instance.volume, instance.commands.image, dir)

	return result
}

// postgresRunbookInstance is the Postgres deployment: pg_dump and psql, run
// inside the container exactly as the runbook writes them.
type postgresRunbookInstance struct {
	commands  runbookCommands
	binary    string
	container string
	config    db.Config
	dsn       string
	backupDir string
	fence     string
}

func (instance *postgresRunbookInstance) serverFencePath() string { return instance.fence }

func (instance *postgresRunbookInstance) documentedBackup(t *testing.T) {
	t.Helper()

	runDocumentedBackup(t, instance.container, instance.backupDir, instance.commands)
}

func (instance *postgresRunbookInstance) documentedRestore(t *testing.T) {
	t.Helper()

	runDocumentedRestore(t, instance.container, instance.backupDir, instance.commands)
}

func (instance *postgresRunbookInstance) withDatabase(t *testing.T, fn func(repos *db.Repositories)) {
	t.Helper()

	instance.withDatabaseFencedAt(t, instance.fence, fn)
}

func (instance *postgresRunbookInstance) withUnfencedDatabase(t *testing.T, fn func(repos *db.Repositories)) {
	t.Helper()

	instance.withDatabaseFencedAt(t, "", fn)
}

func (instance *postgresRunbookInstance) withDatabaseFencedAt(t *testing.T, fencePath string, fn func(repos *db.Repositories)) {
	t.Helper()

	withRepositories(t, instance.config, func(repos *db.Repositories) {
		fn(fencedRepositories(repos, fencePath))
	})
}

func (instance *postgresRunbookInstance) operatorCLI(t *testing.T, fence operatorFenceSetting, args ...string) operatorCLIResult {
	t.Helper()

	return runOperatorCLI(t, instance.binary, map[string]string{
		"DB_DRIVER":    string(db.DriverPostgres),
		"DATABASE_URL": instance.dsn,
	}, fence.resolve(t, instance.fence), args...)
}

// operatorCLIResult is one run of the real binary.
type operatorCLIResult struct {
	args      []string
	fencePath string
	stdout    string
	stderr    string
	err       error
}

// output is what an operator reads: the command's own stdout and the line the
// binary exits on, which reaches stderr.
func (result operatorCLIResult) output() string {
	return result.stdout + result.stderr
}

func (result operatorCLIResult) command() string {
	return "ovumcy " + strings.Join(result.args, " ")
}

// operatorInheritedEnv is the only part of this machine's environment the CLI
// process inherits: the operating system's own plumbing, never a variable the
// application reads. CALENDAR_FEED_FENCE_PATH is deliberately not here — the
// scenarios that leave the fence unset must reach a process that genuinely has
// none, and a shell that happened to export one would hand the command the
// very anchor it is being asked to do without.
var operatorInheritedEnv = []string{"PATH", "SystemRoot", "TEMP", "TMP", "HOME"}

// runOperatorCLI executes the built binary as its own process. The environment
// is composed here rather than inherited because the defect this guard covers
// is exactly that the command resolves its fence from ITS OWN environment: a
// gate handed a fence by its caller cannot observe it, and neither can one
// whose process quietly inherited the server's.
func runOperatorCLI(t *testing.T, binary string, databaseEnv map[string]string, fencePath string, args ...string) operatorCLIResult {
	t.Helper()

	environment := make([]string, 0, len(databaseEnv)+len(operatorInheritedEnv)+2)
	for name, value := range databaseEnv {
		environment = append(environment, name+"="+value)
	}
	environment = append(environment, "SECRET_KEY="+runbookFeedSecretKey)
	if fencePath != "" {
		environment = append(environment, security.CalendarFeedFencePathEnv+"="+fencePath)
	}
	for _, name := range operatorInheritedEnv {
		if value, found := os.LookupEnv(name); found {
			environment = append(environment, name+"="+value)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), operatorCLIRunBudget)
	defer cancel()

	// The operator's working directory is not the server's, which is what
	// makes a relative fence path unusable from here and why the scenarios
	// only ever configure absolute ones.
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = environment
	command.Dir = t.TempDir()

	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("ovumcy %s did not finish inside %s", strings.Join(args, " "), operatorCLIRunBudget)
	}

	return operatorCLIResult{args: args, fencePath: fencePath, stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// buildOperatorCLI builds the binary an operator runs, once per guard.
func buildOperatorCLI(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "ovumcy")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	tool, err := exec.LookPath("go")
	if err != nil {
		// Not a skip: this guard's whole subject is the binary, and a run that
		// could not build it has measured nothing about it.
		t.Fatalf("no go toolchain on PATH to build the operator CLI with: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), operatorCLIBuildBudget)
	defer cancel()

	command := exec.CommandContext(ctx, tool, "build", "-o", binary, "./cmd/ovumcy")
	command.Dir = repoRoot(t)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build the operator CLI: %v\n%s", err, output)
	}
	return binary
}

// bootAndArmRunbookInstance is every scenario's opening state: the instance
// boots once, one owner arms a real calendar feed, and a second account exists
// beside them for the operator to address.
func bootAndArmRunbookInstance(t *testing.T, instance runbookInstance) (armedRunbookFeed, uint) {
	t.Helper()

	var (
		armed    armedRunbookFeed
		targetID uint
	)
	instance.withDatabase(t, func(repos *db.Repositories) {
		bootCalendarFeedPasses(t, repos, instance.serverFencePath())
		armed = armRunbookCalendarFeed(t, repos)
		targetID = seedRunbookAccount(t, repos, runbookOperatorTargetEmail)
		assertRunbookFeedServesOverHTTP(t, repos, armed)
	})
	return armed, targetID
}

// seedRunbookAccount creates the account the operator addresses. It arms no
// feed: the scenarios are about what a removal records, not about how many
// feeds are armed.
func seedRunbookAccount(t *testing.T, repos *db.Repositories, email string) uint {
	t.Helper()

	user := &models.User{
		DisplayName:      "operator target",
		Email:            email,
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("seed the account the operator addresses: %v", err)
	}
	return user.ID
}

func formatRunbookID(userID uint) string {
	return strconv.FormatUint(uint64(userID), 10)
}

// assertOperatorCLIRefusedTheRemoval is the gate's own claim: a non-zero exit,
// a message that names the variable the operator has to fix and says plainly
// that nothing ran, and — where a scenario passes them — the details that say
// WHICH refusal this was, so a run whose fence path never reached the process
// cannot pass for a run that reached it and found a restore.
func assertOperatorCLIRefusedTheRemoval(t *testing.T, result operatorCLIResult, alsoMentions ...string) {
	t.Helper()

	if result.err == nil {
		t.Errorf("`%s` exited 0: a removal this process cannot record outside the database has to be refused, because restoring a backup taken before it undoes the removal and nothing in the run says so\nstdout: %s\nstderr: %s",
			result.command(), result.stdout, result.stderr)
	}
	for _, required := range append([]string{security.CalendarFeedFencePathEnv, operatorRefusalTail}, alsoMentions...) {
		if !strings.Contains(result.output(), required) {
			t.Errorf("the refusal of `%s` never mentions %q, so the operator cannot tell what stopped it or whether it half-ran: %s",
				result.command(), required, result.output())
		}
	}
}

// assertOperatorCLIRemovedTheAccount is the other side of the gate: with the
// server's own fence in reach the removal proceeds. It fails rather than
// reports, because everything the scenario asserts afterwards is about a
// removal that happened.
func assertOperatorCLIRemovedTheAccount(t *testing.T, result operatorCLIResult) {
	t.Helper()

	if result.err != nil {
		t.Fatalf("`%s` failed against the server's own fence: %v\nstdout: %s\nstderr: %s",
			result.command(), result.err, result.stdout, result.stderr)
	}
}

func assertRunbookAccountPresent(t *testing.T, repos *db.Repositories, email string) {
	t.Helper()

	if _, found, err := repos.Users.FindByNormalizedEmailOptional(context.Background(), email); err != nil || !found {
		t.Errorf("the account %s must still be there after a refused removal (found=%v, err=%v)", email, found, err)
	}
}

// assertRunbookBootIsANoOp is the restart of an instance with nothing to
// answer for: the two halves agree, so no feed may be disarmed. It is what
// keeps the disarm assertions below from passing on a fence that simply
// disarms on every start.
func assertRunbookBootIsANoOp(t *testing.T, repos *db.Repositories, fencePath string) {
	t.Helper()

	if outcome := bootCalendarFeedPasses(t, repos, fencePath); outcome != (services.CalendarFeedRestoreFenceOutcome{}) {
		t.Fatalf("this boot must find nothing to answer for, got %+v", outcome)
	}
}

// assertRunbookBootDisarmed is the restart after a restore: the file outlived
// the database generation it was written against, so every armed feed goes.
func assertRunbookBootDisarmed(t *testing.T, repos *db.Repositories, fencePath string) {
	t.Helper()

	outcome := bootCalendarFeedPasses(t, repos, fencePath)
	if !outcome.ContinuityBroken {
		t.Fatalf("the boot after the documented restore must report it: the fence file outlived the database generation it was written against, got %+v", outcome)
	}
	if outcome.DisarmedFeeds < 1 {
		t.Fatalf("the restored feed must be disarmed by that boot, got %d rows", outcome.DisarmedFeeds)
	}
}

// runbookFeedPoll is one unauthenticated GET of a subscribe URL.
type runbookFeedPoll struct {
	status      int
	contentType string
	body        string
}

// pollRunbookFeed polls the owner's subscribe URL the way a calendar client
// does: no cookie, no session, the token in the path, through the shipped
// route table and the real handler over THESE repositories. Every layer under
// it can agree that a feed is revoked while this one still answers 200, which
// is why the verdict is taken here.
func pollRunbookFeed(t *testing.T, repos *db.Repositories, armed armedRunbookFeed) runbookFeedPoll {
	t.Helper()

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler, err := api.NewHandler(
		runbookFeedSecretKey, time.UTC, i18nManager, false,
		bootstrap.BuildDependencies(repos, []byte(runbookFeedSecretKey), i18nManager, bootstrap.Options{}),
	)
	if err != nil {
		t.Fatalf("build the http handler: %v", err)
	}

	app := fiber.New()
	api.RegisterRoutes(app, handler)

	request := httptest.NewRequest(http.MethodGet, api.CalendarFeedRateLimitPrefix+"/"+armed.token+".ics", nil)
	// No deadline: the 404 path deliberately spends the same cost the verify
	// path spends, which is well past Fiber's one-second test default.
	response, err := app.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("poll the subscribe URL: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read the feed response: %v", err)
	}
	return runbookFeedPoll{
		status:      response.StatusCode,
		contentType: response.Header.Get(fiber.HeaderContentType),
		body:        string(body),
	}
}

// assertRunbookFeedServesOverHTTP is the positive anchor every scenario
// carries: the URL answers with a calendar. Without it the 404 assertions
// would pass just as well against a route that never served anything.
func assertRunbookFeedServesOverHTTP(t *testing.T, repos *db.Repositories, armed armedRunbookFeed) {
	t.Helper()

	poll := pollRunbookFeed(t, repos, armed)
	if poll.status != http.StatusOK {
		t.Fatalf("the armed subscribe URL must serve, got %d: %s", poll.status, poll.body)
	}
	if !strings.Contains(poll.contentType, "text/calendar") {
		t.Errorf("the served feed must be a calendar, got content type %q", poll.contentType)
	}
	if !strings.Contains(poll.body, "BEGIN:VCALENDAR") {
		t.Errorf("the served feed carries no calendar body: %q", poll.body)
	}
}

// assertRunbookFeedIsGoneOverHTTP is the revocation as a calendar client sees
// it: the same bare 404 an unknown token gets.
func assertRunbookFeedIsGoneOverHTTP(t *testing.T, repos *db.Repositories, armed armedRunbookFeed) {
	t.Helper()

	poll := pollRunbookFeed(t, repos, armed)
	if poll.status != http.StatusNotFound {
		t.Fatalf("the revoked subscribe URL must answer 404, got %d (%s): %s", poll.status, poll.contentType, poll.body)
	}
}
