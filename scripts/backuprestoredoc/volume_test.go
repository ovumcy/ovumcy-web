package backuprestoredoc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/testenv"
	"gorm.io/gorm"
)

// The SQLite half of the runbook: the named-volume archive and restore an
// operator of the default compose deployment actually follows. Same shape as
// the Postgres half — the commands are read out of the document and executed —
// with two substitutions instead of one, and both of them fail closed:
//
//   - the volume name. `ovumcy_data` is the operator's REAL data volume and the
//     documented restore begins by deleting it, so a guard that ran the command
//     as written would destroy a self-hoster's health data the first time
//     someone ran `go test ./...` on a machine hosting the stack. Every command
//     is rewritten onto an ephemeral volume, and runScript refuses to execute a
//     script in which the literal survived (see refusedInAnExecutedCommand).
//   - `docker compose down` / `docker compose up -d`. These are lifecycle
//     bracketing around the mechanics, not the mechanics: this guard never
//     starts the application at all, it writes the volume and reads it back
//     itself. They are asserted PRESENT and then removed, so a runbook that
//     stops stopping the app, or that grows a third compose command between
//     them, fails extraction rather than being silently half-executed.
//
// Everything else — the `alpine` image and its tag, `tar czf`/`tar xzf`, the
// mounts, `$BACKUP_FILE`, `docker volume rm`/`create` — runs exactly as the
// document writes it.

const (
	volumeBackupSection  = "## Docker Named Volume Backup"
	volumeRestoreSection = "## Docker Named Volume Restore"

	// dataVolumeName is the volume the runbook names, and the literal this
	// guard substitutes away before running anything.
	dataVolumeName = "ovumcy_data"

	// The lifecycle commands: asserted present, never executed.
	composeDownCommand = "docker compose down"
	composeUpCommand   = "docker compose up -d"

	// composeStopCommand stops the app before the volume is archived. The
	// section's prose has to spell it out; the block beside it may NOT, because
	// runScript refuses to execute any script containing `docker compose` and
	// that refusal is what keeps a developer's `go test ./...` away from a real
	// deployment. So the block carries stopBeforeArchiveMarker instead, and the
	// two halves are checked together by
	// TestVolumeBackupBlockCarriesTheStopRequirement.
	composeStopCommand = "docker compose stop"

	// stopBeforeArchiveMarker is how the requirement travels with the block
	// itself: a comment, which every shell ignores and every operator who
	// copies the block still reads.
	stopBeforeArchiveMarker = "Stop the app first"

	// sqliteDatabaseFile is the database inside the data volume. The two
	// sidecars beside it are the NECESSARY half of what `docs/self-hosted.md`
	// asks of an archive — the claim assertArchiveCarriesTheWholeWALSet checks,
	// and all this package can check, since it never runs the application. The
	// sufficiency half is not here and is not implied: capturing all three from
	// a LIVE volume still loses a commit to a checkpoint landing mid-capture,
	// which is why the runbook requires a stopped app or an atomic snapshot.
	// internal/db's TestLiveWholeVolumeCaptureLosesACommitToACheckpointMidCapture
	// is where that half is proven.
	sqliteDatabaseFile = "ovumcy.db"

	dockerCommandTimeout = 3 * time.Minute
)

// walSetFiles is the trio the runbook names. In WAL mode the rows most recently
// written live in the `-wal` file until a checkpoint, which is exactly why an
// archive that carries only `ovumcy.db` restores an instance that boots clean
// and empty — the failure the runbook's Post-Restore Verification describes.
// Carrying all three is where that failure stops, not where the contract ends:
// see sqliteDatabaseFile above for the half proven elsewhere.
var walSetFiles = []string{sqliteDatabaseFile, sqliteDatabaseFile + "-wal", sqliteDatabaseFile + "-shm"}

var (
	backupFileAssignment = regexp.MustCompile(`(?m)^BACKUP_FILE="([^"]+)"$`)
	archiveHostDirMount  = regexp.MustCompile(`-v "\$PWD/([^:"]+):/backup`)
	runbookImageRef      = regexp.MustCompile(`(?m)^\s*(alpine:[^\s\\]+)`)
)

// volumeCommands is the named-volume procedure as the document spells it.
type volumeCommands struct {
	// backup is the whole fenced block of the backup section.
	backup string
	// restore is the restore block with the two compose lifecycle commands
	// removed, and nothing else changed.
	restore string
	// image is the container image both blocks run the archive tooling in,
	// read out of the document so a tag bump needs no edit here.
	image string
	// archiveDir and archiveFile are where the archive lands, relative to the
	// directory the operator runs in. Both blocks must agree on both.
	archiveDir  string
	archiveFile string
}

// documentedVolumeCommands reads the named-volume backup and restore out of the
// runbook. Every shape assertion fails CLOSED, for the same reason the Postgres
// extraction does: an extraction that found nothing, or found something it does
// not recognise, is this guard's own failure rather than a licence to run less.
func documentedVolumeCommands(t *testing.T) volumeCommands {
	t.Helper()

	backup := singleShellBlock(t, volumeBackupSection)
	restoreBlock := singleShellBlock(t, volumeRestoreSection)

	for _, required := range []struct {
		command string
		name    string
		substr  string
	}{
		{backup, "the archive command", "tar czf"},
		{backup, "the archive command", dataVolumeName},
		{restoreBlock, "the restore", "tar xzf"},
		{restoreBlock, "the restore", "docker volume rm " + dataVolumeName},
		{restoreBlock, "the restore", "docker volume create " + dataVolumeName},
	} {
		if !strings.Contains(required.command, required.substr) {
			t.Fatalf("%s: %s no longer contains %q, so this guard cannot run the documented procedure:\n%s", runbookPath, required.name, required.substr, required.command)
		}
	}

	commands := volumeCommands{
		backup:      backup,
		restore:     withoutLifecycleCommands(t, restoreBlock),
		image:       agreedValue(t, "container image", runbookImageRef, backup, restoreBlock),
		archiveFile: agreedValue(t, "archive file name", backupFileAssignment, backup, restoreBlock),
		archiveDir:  agreedValue(t, "archive directory", archiveHostDirMount, backup, restoreBlock),
	}
	return commands
}

// withoutLifecycleCommands removes the two compose commands that bracket the
// documented restore, having first asserted that both are there and that they
// are the ONLY compose commands in the block.
func withoutLifecycleCommands(t *testing.T, block string) string {
	t.Helper()

	var (
		kept    []string
		removed = map[string]int{composeDownCommand: 0, composeUpCommand: 0}
		other   []string
	)
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if count, ok := removed[trimmed]; ok {
			removed[trimmed] = count + 1
			continue
		}
		if strings.Contains(trimmed, "docker compose") {
			other = append(other, trimmed)
		}
		kept = append(kept, line)
	}

	for command, count := range removed {
		if count != 1 {
			t.Fatalf("%s: the documented restore names %q %d times, want exactly 1 — this guard removes those two lines instead of executing them, so it refuses to guess at a block that changed shape:\n%s", runbookPath, command, count, block)
		}
	}
	if len(other) > 0 {
		t.Fatalf("%s: the documented restore grew compose command(s) this guard does not know how to skip: %v", runbookPath, other)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// agreedValue extracts one value with pattern from every block and requires
// them all to name the same thing. A backup that writes one archive and a
// restore that replays another documents a restore that cannot work, and no
// assertion about either block alone would see it.
func agreedValue(t *testing.T, name string, pattern *regexp.Regexp, blocks ...string) string {
	t.Helper()

	agreed := ""
	for _, block := range blocks {
		match := pattern.FindStringSubmatch(block)
		if match == nil {
			t.Fatalf("%s: no %s found in:\n%s", runbookPath, name, block)
		}
		if agreed == "" {
			agreed = match[1]
			continue
		}
		if match[1] != agreed {
			t.Fatalf("%s: the two named-volume blocks disagree on the %s: %q against %q — following the runbook as written would back up one thing and restore another", runbookPath, name, agreed, match[1])
		}
	}
	return agreed
}

// TestVolumeBackupBlockCarriesTheStopRequirement keeps the one requirement that
// decides whether the archive is complete inside the artefact an operator
// actually takes away. A fenced block in a runbook is copied — into a cron job,
// into a wiki, into a shell — and the prose around it is not; an operator who
// copies this block and never re-reads the section archives a live volume, and
// a checkpoint landing between tar's read of `ovumcy.db` and its read of
// `ovumcy.db-wal` drops a commit that was already in the database, with nothing
// in the run reporting a problem.
//
// The requirement stays a COMMENT, and one that does not name the compose
// command: this package executes the block verbatim, and runScript refuses any
// script containing `docker compose` — the refusal that keeps a developer's
// `go test ./...` from addressing a real deployment. Weakening it to let a
// comment through would trade a live-deployment guard for a doc convenience.
// So the block carries the marker and the section's prose carries the command,
// and both ends are checked here: a block whose comment was dropped stops
// travelling with the requirement, and prose that lost the command leaves the
// comment pointing at nothing.
func TestVolumeBackupBlockCarriesTheStopRequirement(t *testing.T) {
	block := singleShellBlock(t, volumeBackupSection)

	marked := false
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, stopBeforeArchiveMarker) {
			marked = true
			break
		}
	}
	if !marked {
		t.Errorf("%s: the %s block carries no comment saying %q, so an operator who copies the block without the prose around it archives a live volume and can lose a commit that was already in the database:\n%s",
			runbookPath, volumeBackupSection, stopBeforeArchiveMarker, block)
	}

	if section := runbookSectionText(t, volumeBackupSection); !strings.Contains(section, composeStopCommand) {
		t.Errorf("%s: section %q no longer names %q, so the block's comment sends the operator to a paragraph that no longer tells them how to stop the app", runbookPath, volumeBackupSection, composeStopCommand)
	}
}

// singleShellBlock returns the one fenced shell block of a runbook section.
func singleShellBlock(t *testing.T, heading string) string {
	t.Helper()

	blocks := bashBlock.FindAllStringSubmatch(runbookSectionText(t, heading), -1)
	if len(blocks) != 1 {
		t.Fatalf("%s: expected exactly 1 shell block under %q, found %d — the extraction no longer matches the document", runbookPath, heading, len(blocks))
	}
	return strings.TrimSpace(blocks[0][1])
}

// TestDocumentedVolumeRestoreReplacesDriftedDataWithTheArchivedGeneration runs
// the runbook's named-volume archive and restore, as the runbook writes them,
// against an ephemeral volume holding the application's own SQLite database.
//
// It mirrors the Postgres half step for step — seed, archive, drift, restore,
// read back through the repositories — and adds the claim that is specific to
// this half: the database runs in WAL mode, so the archive has to carry
// `ovumcy.db-wal` and `ovumcy.db-shm` beside `ovumcy.db`. The fixture is
// written with the connection held OPEN, which is what puts those rows in the
// WAL rather than in the database file, so an archive that missed the sidecars
// cannot pass by accident.
func TestDocumentedVolumeRestoreReplacesDriftedDataWithTheArchivedGeneration(t *testing.T) {
	commands := documentedVolumeCommands(t)
	requireDocker(t)

	volume := ephemeralVolume(t)
	workdir := t.TempDir()

	seeded := seedVolumeGenerationOne(t, volume, commands)

	if output, err := runVolumeScript(t, workdir, volume, commands.backup); err != nil {
		t.Fatalf("the documented archive command failed: %v\n%s", err, output)
	}
	archivePath := filepath.Join(workdir, filepath.FromSlash(commands.archiveDir), commands.archiveFile)
	assertArchiveCarriesTheWholeWALSet(t, archivePath)

	driftVolumeToGenerationTwo(t, volume, commands, seeded)
	assertVolumeHoldsGenerationTwo(t, volume, commands)

	if output, err := runVolumeScript(t, workdir, volume, commands.restore); err != nil {
		t.Fatalf("the documented restore failed: %v\n%s", err, output)
	}

	withVolumeCopy(t, volume, commands, func(_ string, repos *db.Repositories) {
		assertGenerationOneReadsBackFrom(t, repos, seeded)
	})
}

// TestDocumentedVolumeRunbookKeepsTheLifecycleBracket pins the two commands
// this guard deliberately does not execute. They are the operator's whole
// protection against archiving a database that is being written to, so their
// disappearance from the runbook would be a real regression — and one this
// package would otherwise be the last to notice, since it skips them.
func TestDocumentedVolumeRunbookKeepsTheLifecycleBracket(t *testing.T) {
	restore := singleShellBlock(t, volumeRestoreSection)

	for _, command := range []string{composeDownCommand, composeUpCommand} {
		if !strings.Contains(restore, command) {
			t.Errorf("%s: the documented restore no longer stops and starts the app with %q", runbookPath, command)
		}
	}
	if index := strings.Index(restore, composeDownCommand); index >= 0 {
		if strings.Index(restore, composeUpCommand) < index {
			t.Errorf("%s: the documented restore starts the app before it stops it", runbookPath)
		}
	}
}

// TestRunbookStillClaimsTheWholeWALSetIsCaptured ties the archive assertion to
// the sentence it enforces. assertArchiveCarriesTheWholeWALSet requires all
// three files because docs/self-hosted.md promises all three; if that promise
// were quietly dropped from the runbook, the guard would go on enforcing a
// claim the operator is no longer given — and, worse, the sharp edge it exists
// for would have left the document with nothing failing.
func TestRunbookStillClaimsTheWholeWALSetIsCaptured(t *testing.T) {
	contract := runbookSectionText(t, postgresSection)

	for _, name := range walSetFiles {
		if !strings.Contains(contract, name) {
			t.Errorf("%s: section %q no longer names %s, which this guard asserts every archive carries", runbookPath, postgresSection, name)
		}
	}
	if !strings.Contains(contract, "WAL mode") {
		t.Errorf("%s: section %q no longer explains that the database runs in WAL mode, which is why the sidecars matter", runbookPath, postgresSection)
	}
}

// seedVolumeGenerationOne fills a fresh database with generation 1 and puts it
// into the volume WITH THE CONNECTION STILL OPEN — the rows are in the WAL at
// that moment, which is the state the runbook's claim is about.
func seedVolumeGenerationOne(t *testing.T, volume string, commands volumeCommands) seededGeneration {
	t.Helper()

	dir := t.TempDir()
	database := openVolumeDatabase(t, dir)
	seeded := seedGenerationInto(t, db.NewRepositories(database))

	assertWALSetOnDisk(t, dir)
	writeVolume(t, volume, commands.image, dir)
	closeDatabase(t, database)

	return seeded
}

// driftVolumeToGenerationTwo takes the volume's own copy of the database, reads
// generation 1 back out of it — which already proves the sidecars survived the
// round trip — drifts it, and puts it back.
func driftVolumeToGenerationTwo(t *testing.T, volume string, commands volumeCommands, seeded seededGeneration) {
	t.Helper()

	withVolumeCopy(t, volume, commands, func(dir string, repos *db.Repositories) {
		assertGenerationOneReadsBackFrom(t, repos, seeded)
		driftToGenerationTwoInto(t, repos, seeded)
		assertWALSetOnDisk(t, dir)
		writeVolume(t, volume, commands.image, dir)
	})
}

// assertVolumeHoldsGenerationTwo is the drift's own proof: without it, a
// snapshot that silently failed to land would leave generation 1 in the volume,
// and the restore would be asserted against a database it never had to change.
func assertVolumeHoldsGenerationTwo(t *testing.T, volume string, commands volumeCommands) {
	t.Helper()

	withVolumeCopy(t, volume, commands, func(_ string, repos *db.Repositories) {
		accounts, err := repos.Users.CountUsers(context.Background())
		if err != nil {
			t.Fatalf("count the accounts in the drifted volume: %v", err)
		}
		if accounts != 2 {
			t.Fatalf("the drifted volume holds %d accounts, want 2: the drift never reached the volume, so a restore that did nothing would pass", accounts)
		}
	})
}

// assertArchiveCarriesTheWholeWALSet checks the runbook's claim that the
// whole-volume archive captures `ovumcy.db`, `ovumcy.db-wal` and
// `ovumcy.db-shm` together. Nothing checked it before this guard.
func assertArchiveCarriesTheWholeWALSet(t *testing.T, archivePath string) {
	t.Helper()

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open the archive the documented command wrote: %v", err)
	}
	defer func() { _ = file.Close() }()

	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("the documented archive is not gzip-compressed, which `tar czf` makes it: %v", err)
	}
	defer func() { _ = compressed.Close() }()

	carried := map[string]int64{}
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read the documented archive: %v", err)
		}
		if header.Typeflag == tar.TypeReg {
			carried[path.Base(header.Name)] = header.Size
		}
	}

	var missing []string
	for _, name := range walSetFiles {
		if _, ok := carried[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the documented archive is missing %v — %s promises it captures all three together, and an archive without the WAL restores an instance that boots clean and empty. It carried: %v", missing, runbookPath, sortedNames(carried))
	}
	if carried[sqliteDatabaseFile] == 0 {
		t.Fatalf("the documented archive carries an empty %s", sqliteDatabaseFile)
	}
}

// assertWALSetOnDisk fails when SQLite is not in the state this half's claim is
// about. A fixture that produced no `-wal` would make the archive assertion
// vacuous, so it is checked rather than assumed.
func assertWALSetOnDisk(t *testing.T, dir string) {
	t.Helper()

	for _, name := range walSetFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s is not on disk beside the open database: the fixture is not in the WAL state this guard is about (%v)", name, err)
		}
	}
}

// withVolumeCopy extracts the volume into a temporary directory, opens the
// application's own database layer over it, and closes the pool afterwards.
func withVolumeCopy(t *testing.T, volume string, commands volumeCommands, fn func(dir string, repos *db.Repositories)) {
	t.Helper()

	dir := t.TempDir()
	readVolume(t, volume, commands.image, dir)
	database := openVolumeDatabase(t, dir)
	defer closeDatabase(t, database)

	fn(dir, db.NewRepositories(database))
}

func openVolumeDatabase(t *testing.T, dir string) *gorm.DB {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(dir, sqliteDatabaseFile)})
	if err != nil {
		t.Fatalf("open the sqlite database in %s: %v", dir, err)
	}
	return database
}

// runVolumeScript substitutes the ephemeral volume for the one the runbook
// names and runs the rest verbatim. runScript refuses the script if the
// substitution missed anything.
func runVolumeScript(t *testing.T, dir string, volume string, script string) (string, error) {
	t.Helper()

	return runScript(t, dir, strings.ReplaceAll(script, dataVolumeName, volume))
}

// ephemeralVolume creates the volume this run works on and removes it
// afterwards. Its name deliberately cannot contain the documented one: the
// substitution's safety check looks for that literal, and a name built around
// it would defeat the check it depends on.
func ephemeralVolume(t *testing.T) string {
	t.Helper()

	name := fmt.Sprintf("ovumcyguard%dtestdata", time.Now().UnixNano())
	if strings.Contains(name, dataVolumeName) {
		t.Fatalf("the ephemeral volume name %q contains %q, which would defeat the check that keeps this guard away from a real deployment", name, dataVolumeName)
	}

	runDocker(t, nil, "volume", "create", name)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), dockerCommandTimeout)
		defer cancel()
		// The documented restore removes and recreates this volume itself, so
		// its absence here is not an error worth failing a passing test over.
		_ = exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name).Run()
	})
	return name
}

// writeVolume replaces the volume's contents with the files in dir. This is the
// guard's own plumbing, standing in for the running application that would
// normally own the volume — never a documented command.
func writeVolume(t *testing.T, volume string, image string, dir string) {
	t.Helper()

	runDocker(t, tarDirectory(t, dir),
		"run", "--rm", "-i", "-v", volume+":/target", image,
		"sh", "-c", "cd /target && rm -rf ./* ./.[!.]* 2>/dev/null; tar xf -",
	)
}

// readVolume copies the volume's contents into dir.
func readVolume(t *testing.T, volume string, image string, dir string) {
	t.Helper()

	archive := runDocker(t, nil,
		"run", "--rm", "-i", "-v", volume+":/source:ro", image,
		"sh", "-c", "cd /source && tar cf - .",
	)
	untarInto(t, []byte(archive), dir)
}

// requireDocker skips when there is no docker to talk to, the same idiom the
// repository's other container-backed tests use — but only on a machine where
// the owning lane has not declared docker mandatory. CI sets
// OVUMCY_REQUIRE_DOCKER on the lane that runs this package, which turns that
// same absence into a failure: the ubuntu runner always has docker, so
// finding none there is a broken runner, not a developer-host allowance, and
// this is the one guard that would otherwise go quiet about it. Every docker
// command after this point (runDocker below) already fails rather than skips
// on any error, so this preflight is the only place absence is even a
// candidate for a skip.
func requireDocker(t *testing.T) {
	t.Helper()

	testenv.RequireLookPath(t, "docker", "docker")
}

// runDocker runs one docker command with stdin, returning its stdout.
func runDocker(t *testing.T, stdin []byte, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), dockerCommandTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "docker", args...)
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("docker %s did not finish inside %s", strings.Join(args, " "), dockerCommandTimeout)
		}
		t.Fatalf("docker %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String()
}

// tarDirectory packs the regular files of dir — the database and its sidecars —
// into an uncompressed tar stream.
func tarDirectory(t *testing.T, dir string) []byte {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var buffer bytes.Buffer
	archive := tar.NewWriter(&buffer)
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		header := &tar.Header{Name: "./" + entry.Name(), Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatalf("write tar header for %s: %v", entry.Name(), err)
		}
		if _, err := archive.Write(content); err != nil {
			t.Fatalf("write tar entry for %s: %v", entry.Name(), err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close the tar stream: %v", err)
	}
	return buffer.Bytes()
}

// untarInto unpacks a flat tar stream into dir. Only regular files are taken,
// and only by base name: the archive comes from a volume this test created, and
// a flat extraction cannot be walked out of the directory.
func untarInto(t *testing.T, stream []byte, dir string) {
	t.Helper()

	archive := tar.NewReader(bytes.NewReader(stream))
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("read the volume archive: %v", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			t.Fatalf("read %s out of the volume archive: %v", header.Name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(header.Name)), content, 0o600); err != nil {
			t.Fatalf("write %s: %v", header.Name, err)
		}
	}
}

func sortedNames(carried map[string]int64) []string {
	names := make([]string, 0, len(carried))
	for name := range carried {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
