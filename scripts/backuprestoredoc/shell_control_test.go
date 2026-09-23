package backuprestoredoc

import (
	"strings"
	"testing"
)

// TestRunScriptStopsAtTheFirstFailure is the positive control for the
// fail-fast convention runScript applies to the runbook's commands: a failing
// command ends the run, and so does a pipeline whose first stage fails. Without
// it a failed dump would leave the verdict to whatever the last command
// reported. The first case is the anchor: a script that fails nowhere has to
// reach its end, or the other two would pass on a helper that runs nothing.
func TestRunScriptStopsAtTheFirstFailure(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		script      string
		wantReached bool
	}{
		{name: "nothing fails", script: "true | true\necho reached\n", wantReached: true},
		{name: "a failing command (errexit)", script: "false\necho reached\n"},
		{name: "a pipeline whose first stage fails (pipefail)", script: "false | true\necho reached\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runScript(t, t.TempDir(), testCase.script)
			reached := strings.Contains(output, "reached")

			if testCase.wantReached && (!reached || err != nil) {
				t.Fatalf("a script that fails nowhere did not run to its end: %v\n%s", err, output)
			}
			if !testCase.wantReached && (reached || err == nil) {
				t.Fatalf("the run went on past a failed command (exit: %v):\n%s", err, output)
			}
		})
	}
}
