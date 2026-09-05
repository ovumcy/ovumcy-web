package backuprestoredoc

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/testenv"
)

// runbookCommandTimeout bounds one documented command. Every step here is a
// dump, an archive or a replay of a fixture-sized database against a container
// on the same host: a step that has not finished inside this budget is hung,
// not slow, and an unbounded hang would run to the job timeout, which GitHub
// reports as a cancelled job with no failed step to read.
const runbookCommandTimeout = 2 * time.Minute

// refusedInAnExecutedCommand is the safety net under every command this package
// runs. Each entry is a literal that a documented command legitimately contains
// and that MUST have been substituted away before execution, so the check is
// not a style rule but the thing that keeps `go test ./...` on a developer's
// own machine from reaching a real deployment:
//
//   - `docker compose` would address the operator's compose project — the
//     running application, its database, its volumes;
//   - `ovumcy_data` is the operator's real data volume, and the restore the
//     runbook documents begins by DELETING it. A run that got this far with the
//     literal intact would destroy a self-hoster's health data on a machine
//     where the guard was only supposed to read a document.
//
// Each caller substitutes for its own reasons — an ephemeral container, an
// ephemeral volume — and this refuses to run whatever a substitution missed.
var refusedInAnExecutedCommand = []struct {
	literal string
	why     string
}{
	{"docker compose", "addresses a compose project this guard has not started"},
	{dataVolumeName, "still names the operator's own data volume, which the documented restore deletes"},
}

// runScript runs commands taken verbatim from the runbook, in dir, after every
// substitution the caller owes has already been made.
//
// `set -euo pipefail` is the harness, not part of the procedure: an operator
// runs these commands one at a time and reads each result before starting the
// next, and this is what makes a failed first step stop the run rather than
// leave the verdict to whatever the last command reported.
func runScript(t *testing.T, dir string, script string) (string, error) {
	t.Helper()

	for _, refused := range refusedInAnExecutedCommand {
		if strings.Contains(script, refused.literal) {
			t.Fatalf("refusing to run a command that %s (%q survived substitution):\n%s", refused.why, refused.literal, script)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), runbookCommandTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, bashPath(t), "-c", "set -euo pipefail\n"+script)
	command.Dir = dir
	// MSYS_NO_PATHCONV/MSYS2_ARG_CONV_EXCL are a Windows-dev-host shim and
	// nothing else: Git Bash rewrites arguments that look like POSIX paths
	// before handing them to a native binary, which turns the runbook's
	// `-v "$PWD/backups:/backup"` into a Windows path pair and mounts the
	// wrong thing (measured: `/backup` arrived as `C:/Program Files/Git/backup`).
	// On the Linux runner neither variable exists or means anything, so this
	// changes what CI executes not at all.
	command.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1", "MSYS2_ARG_CONV_EXCL=*")

	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("a documented command did not finish inside %s:\n%s\n%s", runbookCommandTimeout, script, output)
	}
	return string(output), err
}

// bashPath locates the shell the runbook's commands are written for. The
// redirections, the pipe and the line continuations are load-bearing parts of
// the documented procedure — the `-T` of `docker compose exec` exists so the
// dump can arrive on stdin — so the commands are run by a shell rather than
// reassembled as argv. Absence is a skip unless OVUMCY_REQUIRE_BASH declares
// this lane owns the runbook check, in which case it is a failure — this
// package is the only thing that runs the self-hosting runbook's commands at
// all, so a lane that promised the check ran cannot report green having found
// no shell. A bash that resolves but does not behave like one is a separate,
// always-fatal case, checked once it is found rather than assumed.
func bashPath(t *testing.T) string {
	t.Helper()

	path := testenv.RequireLookPath(t, "bash", "bash")
	testenv.ProbeShell(t, path, "printf ok", "ok")
	return path
}
