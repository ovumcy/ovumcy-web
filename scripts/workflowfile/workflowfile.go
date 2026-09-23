// Package workflowfile reads a GitHub Actions workflow the way the guards that
// judge one need it read: find the module root, read the file with its line
// endings normalised, cut one job out of it by name, and read the shell a step
// declares for the harnesses that run that step's script.
//
// Three test packages assert something about a job declared under
// `.github/workflows` — publishgate holds ci.yml's `publish-image` gate to a
// condition that cannot read green over a skip, releasegate holds the release
// tag gate to the checks it claims to require, publishorder holds the publish
// job's steps to the order that keeps a public tag behind the signature — and
// each had carried its own copy of these three pieces.
//
// Three copies is three chances for one of them to start walking past a shape
// the other two still see, and every one of these guards fails in the same
// direction when its reader does: a job it cannot find is a job it does not
// judge, and the test reports green over exactly the defect it was written for.
// The copies are what makes that divergence possible, so there is one reader.
package workflowfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// jobHeader matches a job's own header line — two spaces of indentation, a
// name, a colon and nothing else. Cutting one job out of a workflow and
// counting all of them both rest on it, so they cannot disagree about what a
// job header looks like.
var jobHeader = regexp.MustCompile(`(?m)^  [A-Za-z0-9_.-]+:[ \t]*$`)

// stepShellKey matches a step's own `shell:` key. Eight spaces is a step key's
// depth in these workflows; a `shell:` any deeper is an action's `with:` input
// or a line of a script, neither of which is the shell the step runs under.
var stepShellKey = regexp.MustCompile(`(?m)^        shell:(.*)$`)

// stepItem matches the line that opens a step: a sequence item at a step's
// depth, whatever key it leads with. A step that opens on `- id:` or `- uses:`
// ends the one above it exactly as a `- name:` does; a reader that stopped only
// at `- name:` would hand the step above it that step's keys, its `shell:`
// among them.
var stepItem = regexp.MustCompile(`(?m)^      - `)

// stepDedent matches the first line past a step's own depth: fewer than eight
// spaces of indentation ahead of something other than whitespace. For the last
// step in a `steps:` list there is no next stepItem to stop at, so without
// this a job-level key written after `steps:` — a `defaults:` whose `shell:`
// then reads as the step's own — runs on into the block. Every job-level key
// starts shallower than a step's eight-space keys, whatever it holds beneath
// it, so this alone also answers for a job-level list following `steps:`. A
// comment line is not a key at any depth: one written shallower inside a step
// does not end it before the step's own `shell:`.
var stepDedent = regexp.MustCompile(`(?m)^ {0,7}[^\s#]`)

// bashStepFlags is what GitHub Actions compiles `shell: bash` to —
// `bash --noprofile --norc -eo pipefail {0}` — less the `{0}` the script file
// fills.
var bashStepFlags = []string{"--noprofile", "--norc", "-eo", "pipefail"}

// jobsKey is where the search for a job starts. Two-space indentation is not
// on its own the mark of a job: `on:` nests `push:` and `workflow_call:` at
// exactly that depth, so a reader that searched the whole document would hand
// back a trigger block for a job named `push` — a silently WRONG block rather
// than the silently empty one this package refuses. The block is non-empty, so
// nothing downstream notices.
const jobsKey = "\njobs:\n"

// repoRoot walks up from the test's working directory to the module root. It
// is not exported: every caller reaches a workflow through `Read`, which joins
// the path itself, and a guard handed the root would join its own — the
// duplication this package exists to end.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// Read returns a workflow's text with CRLF normalised away, so that a checkout
// on Windows and one on the runner hand every caller the same offsets. The
// path is written with forward slashes, relative to the module root.
func Read(t *testing.T, workflow string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(workflow)))
	if err != nil {
		t.Fatalf("read %s: %v", workflow, err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// Job returns the text of one job, from its header to the next job header at
// the same indentation. It fails closed: a renamed or removed job is a failure
// here, never a silently empty search, because a guard handed an empty block
// asserts nothing about a workflow and says so in green.
func Job(t *testing.T, workflow, job string) string {
	t.Helper()

	block, err := jobIn(Read(t, workflow), job)
	if err != nil {
		t.Fatalf("%s: %v", workflow, err)
	}
	return block
}

// JobHeaders returns every job header a workflow declares. It takes the whole
// document and scopes itself, so a caller counting jobs does not re-derive
// either what a header looks like or where the job section starts — both of
// which this package already had to decide in order to cut one job out.
func JobHeaders(t *testing.T, workflow, content string) []string {
	t.Helper()

	section, err := jobSection(content)
	if err != nil {
		// The name is carried separately because this takes content rather than
		// a path: a caller reading more than one workflow — releasegate reads
		// two — gets a refusal it can attribute, which is the whole use of a
		// guard that exists to say which file drifted.
		t.Fatalf("%s: %v, so there is nothing here to count", workflow, err)
	}
	return jobHeader.FindAllString(section, -1)
}

// Step returns the text of one step of a job, block being the job as Job
// returned it: from below the step's `- name:` line to the next step. It fails
// closed like Job, for the same reason.
func Step(t *testing.T, workflow, job, block, step string) string {
	t.Helper()

	text, err := stepIn(block, step)
	if err != nil {
		t.Fatalf("%s, job %q: %v", workflow, job, err)
	}
	return text
}

// stepIn is Step's whole answer, kept out of the `*testing.T` wrapper so that
// where it stops can be tested rather than only triggered.
func stepIn(block, step string) (string, error) {
	header := "      - name: " + step + "\n"
	start := strings.Index(block, header)
	if start < 0 {
		return "", fmt.Errorf("no step named %q — it was renamed or removed, and this guard would judge nothing", step)
	}
	rest := block[start+len(header):]
	return rest[:stepEnd(rest)], nil
}

// stepEnd is where the step whose keys rest opens on ends: at the next step,
// or at the first line shallower than a step's keys. Step and Steps both end a
// step here, so they cannot disagree about where one does.
func stepEnd(rest string) int {
	end := len(rest)
	if next := stepItem.FindStringIndex(rest); next != nil {
		end = next[0]
	}
	if dedent := stepDedent.FindStringIndex(rest); dedent != nil && dedent[0] < end {
		end = dedent[0]
	}
	return end
}

// stepsKey opens a job's `steps:` list.
const stepsKey = "\n    steps:\n"

// Steps returns every step of a job, block being the job as Job returned it,
// each ending where Step ends a named one — so the last step never reads a
// job-level key written after `steps:`. The key an item opens on is
// re-indented to a step key's depth: a step opening on `- if:` carries its
// `if:` where one opening on `- name:` would. A job with no `steps:` list has
// none.
func Steps(block string) []string {
	// Anchored on a line, as jobSection anchors `jobs:`: a job whose first
	// key is `steps:` has no newline in front of it.
	prefixed := "\n" + block
	start := strings.Index(prefixed, stepsKey)
	if start < 0 {
		return nil
	}
	rest := prefixed[start+len(stepsKey):]

	var steps []string
	for {
		item := stepItem.FindStringIndex(rest)
		if item == nil {
			return steps
		}
		if dedent := stepDedent.FindStringIndex(rest); dedent != nil && dedent[0] < item[0] {
			return steps
		}
		body := rest[item[1]:]
		// The opening line sits shallower than a step's keys, so the end is
		// looked for past it.
		first := len(body)
		if newline := strings.IndexByte(body, '\n'); newline >= 0 {
			first = newline + 1
		}
		end := first + stepEnd(body[first:])
		steps = append(steps, "        "+body[:end])
		rest = body[end:]
	}
}

// BashStepFlags returns the flags bash runs a step's script file under, read
// off the step's own `shell:` key in block (the step as Step returned it).
// Only `shell: bash` has flags a harness can reproduce and name: a step
// that declares no shell runs as `bash -e {0}` on a Linux runner — errexit
// without pipefail — and any other value is another interpreter or another
// template. Either is a failure here, never a run under flags assumed for it.
func BashStepFlags(t *testing.T, workflow, step, block string) []string {
	t.Helper()

	flags, err := bashStepFlagsIn(block)
	if err != nil {
		t.Fatalf("%s, step %q: %v", workflow, step, err)
	}
	return flags
}

// bashStepFlagsIn is BashStepFlags' whole answer, kept out of the
// `*testing.T` wrapper so its refusals can be tested rather than only
// triggered.
func bashStepFlagsIn(block string) ([]string, error) {
	matches := stepShellKey.FindAllStringSubmatch("\n"+block, -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("declares `shell:` %d times, not once — with none the step runs as `bash -e {0}`, which has no pipefail, and a harness that ran it under `shell: bash`'s flags would pass a pipeline the step itself lets through", len(matches))
	}

	value, _, _ := strings.Cut(matches[0][1], " #")
	if value = strings.TrimSpace(value); value != "bash" {
		return nil, fmt.Errorf("declares `shell: %s`, and `shell: bash` is the only shell whose invocation this harness reproduces", value)
	}
	return append([]string(nil), bashStepFlags...), nil
}

// jobSection returns the document from its `jobs:` key onward, which is the
// only region a job header may be looked for in.
func jobSection(content string) (string, error) {
	// The key is anchored on a LINE, not on a preceding newline: a workflow
	// whose very first line is `jobs:` has no newline in front of it, and a
	// reader that demanded one would report a document that has the key as a
	// document that has none — true about the search, false about the file.
	prefixed := "\n" + content

	at := strings.Index(prefixed, jobsKey)
	if at < 0 {
		return "", fmt.Errorf("no `jobs:` key")
	}
	// The trailing newline of `jobs:` is kept, because it is the one a job
	// header is matched against.
	return prefixed[at+len(jobsKey)-1:], nil
}

// jobIn is Job's whole answer, kept out of the `*testing.T` wrapper so its
// refusals can be tested rather than only triggered.
func jobIn(content, job string) (string, error) {
	section, err := jobSection(content)
	if err != nil {
		return "", fmt.Errorf("%w, so there is nothing here a job named %q could be declared in", err, job)
	}

	header := "\n  " + job + ":\n"
	start := strings.Index(section, header)
	if start < 0 {
		return "", fmt.Errorf("no job named %q — it was renamed or removed, and this guard would judge nothing", job)
	}
	rest := section[start+len(header):]

	if next := jobHeader.FindStringIndex(rest); next != nil {
		return rest[:next[0]], nil
	}
	return rest, nil
}
