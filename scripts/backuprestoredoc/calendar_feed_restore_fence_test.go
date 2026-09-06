package backuprestoredoc

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/ovumcy/ovumcy-web/internal/testdb"
)

// The finding these two guards exist for: restoring a backup taken BEFORE an
// owner revoked their calendar feed used to bring the subscribe URL back to
// life. The key-epoch sentinel cannot see it — its epoch lives in app_state and
// is restored together with the feed columns, so the two always agree — and
// SECRET_KEY is deliberately never touched here, because the moment it changes
// the sentinel handles the case and this one stops being the interesting one.
//
// Both halves run the operator's REAL documented procedure, the same commands
// the rest of this package reads out of the runbook, because the claim is about
// what a supported restore leaves behind, not about what a hand-written SQL
// rollback leaves behind. Each ends by proving that the fence, and only the
// fence, is what refuses the old URL: the key epoch is asserted to still match,
// which is the sentinel reporting nothing to do.

// runbookFeedSecretKey is the single application secret both halves use from
// first boot to final assertion. A restore under a CHANGED key is a different
// (already-covered) case.
const runbookFeedSecretKey = "runbook-calendar-feed-secret-key"

// armedRunbookFeed is what one boot handed the owner: the token that went into
// their subscribe URL and the selector half the server resolves it by.
type armedRunbookFeed struct {
	ownerID  uint
	token    string
	selector string
}

// TestDocumentedVolumeRestoreLeavesARevokedCalendarFeedRevoked is the SQLite
// half, over the runbook's own named-volume archive and restore.
//
// The fence file is deliberately created OUTSIDE the volume, because that is
// the whole mechanism: the documented restore deletes and recreates
// `ovumcy_data`, so anything inside it comes back at its backup-time value,
// and a fence that came back with the database could never disagree with it.
func TestDocumentedVolumeRestoreLeavesARevokedCalendarFeedRevoked(t *testing.T) {
	commands := documentedVolumeCommands(t)
	requireDocker(t)

	volume := ephemeralVolume(t)
	workdir := t.TempDir()
	fencePath := filepath.Join(t.TempDir(), "calendar-feed.fence")

	// Boot 1: an owner arms a feed, and the instance arms its fence beside it.
	var armed armedRunbookFeed
	dir := t.TempDir()
	database := openVolumeDatabase(t, dir)
	repos := fencedRepositories(db.NewRepositories(database), fencePath)
	bootCalendarFeedPasses(t, repos, fencePath)
	armed = armRunbookCalendarFeed(t, repos)
	assertRunbookFeedServes(t, repos, armed)
	writeVolume(t, volume, commands.image, dir)
	closeDatabase(t, database)

	// The operator's backup, exactly as the runbook writes it.
	if output, err := runVolumeScript(t, workdir, volume, commands.backup); err != nil {
		t.Fatalf("the documented archive command failed: %v\n%s", err, output)
	}

	// The owner revokes afterwards — the event the restore is about to undo.
	withVolumeCopy(t, volume, commands, func(dir string, repos *db.Repositories) {
		repos = fencedRepositories(repos, fencePath)
		if err := repos.Users.ClearCalendarFeedToken(context.Background(), armed.ownerID); err != nil {
			t.Fatalf("revoke the calendar feed: %v", err)
		}
		assertRunbookFeedIsDead(t, repos, armed)
		writeVolume(t, volume, commands.image, dir)
	})

	if output, err := runVolumeScript(t, workdir, volume, commands.restore); err != nil {
		t.Fatalf("the documented restore failed: %v\n%s", err, output)
	}

	// Boot 2, over the restored volume, with the fence file untouched.
	withVolumeCopy(t, volume, commands, func(_ string, repos *db.Repositories) {
		repos = fencedRepositories(repos, fencePath)
		assertRestoreResurrectedTheFeed(t, repos, armed)
		assertBootDisarmsTheResurrectedFeed(t, repos, fencePath, armed)
	})
}

// TestDocumentedPostgresRestoreLeavesARevokedCalendarFeedRevoked is the same
// claim over the runbook's Postgres dump and replay. The engine is the only
// thing that differs: the fence lives outside the database either way, and the
// dump carries app_state exactly as the SQLite archive carries it.
func TestDocumentedPostgresRestoreLeavesARevokedCalendarFeedRevoked(t *testing.T) {
	commands := documentedPostgresCommands(t)
	dsn, container := testdb.StartPostgres(t, "ovumcy_runbook_feed_fence")
	config := db.Config{Driver: db.DriverPostgres, PostgresURL: dsn}
	fencePath := filepath.Join(t.TempDir(), "calendar-feed.fence")

	var armed armedRunbookFeed
	withRepositories(t, config, func(repos *db.Repositories) {
		repos = fencedRepositories(repos, fencePath)
		bootCalendarFeedPasses(t, repos, fencePath)
		armed = armRunbookCalendarFeed(t, repos)
		assertRunbookFeedServes(t, repos, armed)
	})

	backupDir := t.TempDir()
	runDocumentedBackup(t, container, backupDir, commands)

	withRepositories(t, config, func(repos *db.Repositories) {
		repos = fencedRepositories(repos, fencePath)
		if err := repos.Users.ClearCalendarFeedToken(context.Background(), armed.ownerID); err != nil {
			t.Fatalf("revoke the calendar feed: %v", err)
		}
		assertRunbookFeedIsDead(t, repos, armed)
	})

	runDocumentedRestore(t, container, backupDir, commands)

	withRepositories(t, config, func(repos *db.Repositories) {
		repos = fencedRepositories(repos, fencePath)
		assertRestoreResurrectedTheFeed(t, repos, armed)
		assertBootDisarmsTheResurrectedFeed(t, repos, fencePath, armed)
	})
}

// TestVolumeRestoreOfAnUnanchoredBackupDisarmsOnTheFirstFencedBoot is the
// SQLite half of the second finding, one step earlier in an instance's life
// than the two guards above: the backup was taken while the server had NO
// usable fence at all, and the fence arrives only afterwards.
//
// Nothing in that backup can disagree with a fence file, because the unfenced
// server never wrote one — so the boot that finally has a fence sees both
// halves empty, which used to be the signature of a brand-new installation and
// was answered by adopting the rows in front of it. The revocation the owner
// made after the backup was therefore undone by the restore and nothing was
// left to notice.
func TestVolumeRestoreOfAnUnanchoredBackupDisarmsOnTheFirstFencedBoot(t *testing.T) {
	commands := documentedVolumeCommands(t)
	requireDocker(t)

	// binary is deliberately left empty: this scenario never runs the operator
	// CLI. Its subject is a server that had no fence, not a shell that cannot
	// see one, and building the binary for it would cost minutes per run.
	runUnanchoredHistoryScenario(t, &volumeRunbookInstance{
		commands: commands,
		volume:   ephemeralVolume(t),
		workdir:  t.TempDir(),
		fence:    filepath.Join(t.TempDir(), "calendar-feed.fence"),
	})
}

// TestPostgresRestoreOfAnUnanchoredBackupDisarmsOnTheFirstFencedBoot is the
// same claim over the runbook's dump and replay. The engine is the only thing
// that differs: an unfenced server records nothing outside either database.
func TestPostgresRestoreOfAnUnanchoredBackupDisarmsOnTheFirstFencedBoot(t *testing.T) {
	commands := documentedPostgresCommands(t)
	dsn, container := testdb.StartPostgres(t, "ovumcy_runbook_unanchored_feed")

	runUnanchoredHistoryScenario(t, &postgresRunbookInstance{
		commands:  commands,
		container: container,
		config:    db.Config{Driver: db.DriverPostgres, PostgresURL: dsn},
		dsn:       dsn,
		backupDir: t.TempDir(),
		fence:     filepath.Join(t.TempDir(), "calendar-feed.fence"),
	})
}

// runUnanchoredHistoryScenario is the whole claim, written once and run against
// each engine. It is the operator sequence the fence's own documentation asks
// for — "mount a directory and set CALENDAR_FEED_FENCE_PATH" — arriving late,
// after the instance has already served and been backed up without one.
//
// The verdict is taken over HTTP, through the shipped route table and the real
// handler, because that is the surface a calendar client polls: every layer
// under it can agree that a feed is revoked while the URL still answers 200.
func runUnanchoredHistoryScenario(t *testing.T, instance runbookInstance) {
	t.Helper()

	var armed armedRunbookFeed

	// Boot 1, with no CALENDAR_FEED_FENCE_PATH: an owner arms a feed on an
	// instance that cannot record anything outside its own database.
	instance.withUnfencedDatabase(t, func(repos *db.Repositories) {
		assertUnanchoredBoot(t, repos)
		armed = armRunbookCalendarFeed(t, repos)
		assertRunbookFeedServesOverHTTP(t, repos, armed)
	})

	instance.documentedBackup(t)

	// The owner revokes afterwards, through the same web path the guards above
	// use — and still with no fence, so the revocation exists in exactly one
	// place: the database the restore below is about to replace.
	instance.withUnfencedDatabase(t, func(repos *db.Repositories) {
		if err := repos.Users.ClearCalendarFeedToken(context.Background(), armed.ownerID); err != nil {
			t.Fatalf("revoke the calendar feed: %v", err)
		}
		assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
	})

	instance.documentedRestore(t)

	// Only now does the operator mount a fence volume and set the variable —
	// the remedy every unanchored start has been logging. The first boot with
	// it is where the revocation is either honoured or lost for good.
	instance.withDatabase(t, func(repos *db.Repositories) {
		// The finding first, at the layer the owner sees: the restore put the
		// revoked feed back and the old subscribe URL answers again. Without
		// this the 404 below could come from a feed that was never resurrected.
		assertRunbookFeedServesOverHTTP(t, repos, armed)

		// The verdict is the poll, taken immediately after the boot and before
		// anything is said about WHICH branch produced it: a pass that reported
		// the right outcome and left the URL serving would still be the defect.
		// The outcome is then checked for shape, so a run that disarmed by
		// disarming on every start cannot pass for this one.
		outcome := bootCalendarFeedPasses(t, repos, instance.serverFencePath())
		assertRunbookFeedIsGoneOverHTTP(t, repos, armed)
		if !outcome.ContinuityBroken || !outcome.UnanchoredHistory {
			t.Errorf("the first fenced boot over a database that ran unanchored must report exactly that, got %+v", outcome)
		}
		if outcome.DisarmedFeeds != 1 {
			t.Errorf("the resurrected feed must be the one row that boot disarmed, got %d", outcome.DisarmedFeeds)
		}

		// And the start after it is an ordinary restart. This is the other half
		// of the claim: the evidence the unfenced runs left behind is consumed
		// by the boot that answers for it, so an operator who mounted a fence
		// does not lose every subscribe URL on every start from now on.
		assertRunbookBootIsANoOp(t, repos, instance.serverFencePath())
	})
}

// assertUnanchoredBoot is a start with no fence configured at all. It runs the
// same two passes main runs, in main's order, and asserts the fence reports
// itself unavailable rather than quietly arming: a run in which this boot found
// a usable fence would be measuring the wrong instance entirely.
func assertUnanchoredBoot(t *testing.T, repos *db.Repositories) {
	t.Helper()

	fence := services.NewCalendarFeedRestoreFence(repos.AppState, repos.Users, security.NewCalendarFeedFenceFile(""))
	outcome, err := fence.Enforce(context.Background())
	if err != nil {
		t.Fatalf("a server without a fence must still boot: %v", err)
	}
	if !outcome.Unanchored {
		t.Fatalf("this boot has no CALENDAR_FEED_FENCE_PATH and must report itself unanchored, got %+v", outcome)
	}

	sentinel := services.NewCalendarFeedRotationSentinel(repos.AppState, repos.Users, []byte(runbookFeedSecretKey))
	rotation, err := sentinel.Enforce(context.Background())
	if err != nil {
		t.Fatalf("the key-rotation sentinel failed: %v", err)
	}
	if rotation.RotationDetected {
		t.Fatal("SECRET_KEY never changes in this guard; a detected rotation means the wrong mechanism is being measured")
	}
}

// fencedRepositories attaches the restore fence, which is what production
// wiring does through bootstrap.BuildRepositories. Without it the revoke below
// would change the rows and record nothing outside the database, which is
// exactly the state before this fix — so a guard that forgot this line would go
// green by measuring the defect instead of the repair.
//
// An EMPTY fencePath is a deliberate configuration, not a missing one: it is
// the instance whose operator never set CALENDAR_FEED_FENCE_PATH, wired through
// the same object production wires so its writes take the same code path.
func fencedRepositories(repos *db.Repositories, fencePath string) *db.Repositories {
	return repos.WithCalendarFeedFence(services.NewCalendarFeedRestoreFence(
		repos.AppState, repos.Users, security.NewCalendarFeedFenceFile(fencePath),
	))
}

// armRunbookCalendarFeed creates one owner and arms a real calendar feed for
// them, returning what a subscribe URL would have carried.
func armRunbookCalendarFeed(t *testing.T, repos *db.Repositories) armedRunbookFeed {
	t.Helper()

	user := &models.User{
		DisplayName:      "feed owner",
		Email:            "feed-owner@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("seed the feed owner: %v", err)
	}

	token, columns, err := services.GenerateCalendarFeedToken([]byte(runbookFeedSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	if err := repos.Users.SaveCalendarFeedToken(context.Background(), user.ID, columns); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}
	return armedRunbookFeed{ownerID: user.ID, token: token, selector: columns.Selector}
}

// bootCalendarFeedPasses runs the two boot passes main runs, in main's order,
// against this database. Both are here on purpose: the sentinel's presence is
// what makes the final assertion — that it reports nothing while the fence
// disarms — mean something.
func bootCalendarFeedPasses(t *testing.T, repos *db.Repositories, fencePath string) services.CalendarFeedRestoreFenceOutcome {
	t.Helper()

	fence := services.NewCalendarFeedRestoreFence(repos.AppState, repos.Users, security.NewCalendarFeedFenceFile(fencePath))
	fenceOutcome, err := fence.Enforce(context.Background())
	if err != nil {
		t.Fatalf("the restore fence failed: %v", err)
	}
	if fenceOutcome.Unanchored {
		t.Fatalf("the fence at %s must be usable in this guard, got %v", fencePath, fenceOutcome.UnanchoredCause)
	}

	sentinel := services.NewCalendarFeedRotationSentinel(repos.AppState, repos.Users, []byte(runbookFeedSecretKey))
	rotation, err := sentinel.Enforce(context.Background())
	if err != nil {
		t.Fatalf("the key-rotation sentinel failed: %v", err)
	}
	if rotation.RotationDetected {
		t.Fatal("SECRET_KEY never changes in this guard; a detected rotation means the wrong mechanism is being measured")
	}
	return fenceOutcome
}

// assertRunbookFeedServes proves the token is live at this moment, through the
// same verification the feed route performs.
func assertRunbookFeedServes(t *testing.T, repos *db.Repositories, armed armedRunbookFeed) {
	t.Helper()

	user, found, err := repos.Users.FindByCalendarFeedSelector(context.Background(), armed.selector)
	if err != nil || !found {
		t.Fatalf("the armed selector must resolve its owner (found=%v, err=%v)", found, err)
	}
	if !services.VerifyCalendarFeedToken([]byte(runbookFeedSecretKey), armed.token, models.CalendarFeedTokenColumns{
		Selector:     user.CalendarFeedSelector,
		VerifierHash: user.CalendarFeedVerifierHash,
		VerifierMAC:  user.CalendarFeedVerifierMAC,
	}) {
		t.Fatal("the armed token must verify; without this the guard proves nothing about revoking it")
	}
}

// assertRunbookFeedIsDead is the state a revocation leaves: the selector
// resolves no row at all, which is what the route answers 404 to.
func assertRunbookFeedIsDead(t *testing.T, repos *db.Repositories, armed armedRunbookFeed) {
	t.Helper()

	if _, found, err := repos.Users.FindByCalendarFeedSelector(context.Background(), armed.selector); err != nil || found {
		t.Fatalf("the revoked selector must resolve no row (found=%v, err=%v)", found, err)
	}
}

// assertRestoreResurrectedTheFeed is the finding itself, asserted BEFORE the
// fix runs: the restore put the revoked feed back, byte for byte, and the token
// verifies again under the unchanged key. Without this the two guards could go
// green over a restore that never resurrected anything.
func assertRestoreResurrectedTheFeed(t *testing.T, repos *db.Repositories, armed armedRunbookFeed) {
	t.Helper()

	assertRunbookFeedServes(t, repos, armed)

	// And the sentinel's own input is unchanged, which is why it cannot help:
	// the epoch it would compare came back inside the same dump.
	epoch, err := security.CalendarFeedKeyEpoch([]byte(runbookFeedSecretKey))
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch: %v", err)
	}
	stored, found, err := repos.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedKeyEpoch)
	if err != nil || !found {
		t.Fatalf("the restored database must carry the key epoch (found=%v, err=%v)", found, err)
	}
	if stored != epoch {
		t.Fatal("the restored key epoch differs from the current one: this run is measuring a rotation, not a restore")
	}
}

// assertBootDisarmsTheResurrectedFeed is the fix: the boot the operator's
// restart runs takes the resurrected feed down before any listener exists.
func assertBootDisarmsTheResurrectedFeed(t *testing.T, repos *db.Repositories, fencePath string, armed armedRunbookFeed) {
	t.Helper()

	outcome := bootCalendarFeedPasses(t, repos, fencePath)
	if !outcome.ContinuityBroken {
		t.Fatal("the fence must report the restore; the file outlived the database generation it was written against")
	}
	if outcome.DisarmedFeeds != 1 {
		t.Fatalf("the restored feed must be the one row disarmed, got %d", outcome.DisarmedFeeds)
	}
	assertRunbookFeedIsDead(t, repos, armed)

	// A second boot is a plain restart now: the halves agree again, and an
	// operator does not get their feeds cleared every start.
	if again := bootCalendarFeedPasses(t, repos, fencePath); again != (services.CalendarFeedRestoreFenceOutcome{}) {
		t.Fatalf("the boot after the disarm must be a no-op, got %+v", again)
	}
}
