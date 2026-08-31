// Package workflowfile reads a GitHub Actions workflow the way the guards that
// judge one need it read: find the module root, read the file with its line
// endings normalised, cut one job out of it by name.
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

// jobsKey is where the search for a job starts. Two-space indentation is not
// on its own the mark of a job: `on:` nests `push:` and `workflow_call:` at
// exactly that depth, so a reader that searched the whole document would hand
// back a trigger block for a job named `push` — a silently WRONG block rather
// than the silently empty one this package refuses. The block is non-empty, so
// nothing downstream notices.
const jobsKey = "\njobs:\n"

// RepoRoot walks up from the test's working directory to the module root.
func RepoRoot(t *testing.T) string {
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

	raw, err := os.ReadFile(filepath.Join(RepoRoot(t), filepath.FromSlash(workflow)))
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
func JobHeaders(t *testing.T, content string) []string {
	t.Helper()

	section, err := jobSection(content)
	if err != nil {
		t.Fatalf("%v, so there is nothing here to count", err)
	}
	return jobHeader.FindAllString(section, -1)
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
