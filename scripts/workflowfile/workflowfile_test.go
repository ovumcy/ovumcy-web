package workflowfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a workflow shaped like the ones this package reads: a `on:` block
// whose children sit at the same two-space indentation as a job, and three jobs
// where one name is a prefix of another.
const fixture = `name: Fixture

on:
  push:
    tags:
      - "v*"
  workflow_call:

permissions:
  contents: read

jobs:
  first:
    runs-on: ubuntu-latest
    steps:
      - name: One
  second:
    runs-on: ubuntu-latest
    steps:
      - name: Two
  second-longer:
    runs-on: ubuntu-latest
    steps:
      - name: Three
`

func TestJobInReturnsOneJobAndStopsAtTheNext(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		job      string
		contains string
		absent   string
	}{
		{name: "a job followed by another", job: "first", contains: "- name: One", absent: "- name: Two"},
		// `second` is a prefix of `second-longer`, and the header match has to
		// end at the colon or it would return the wrong job — or, worse, the
		// right one running past its own end.
		{name: "a job whose name prefixes the next", job: "second", contains: "- name: Two", absent: "- name: Three"},
		{name: "the last job in the file", job: "second-longer", contains: "- name: Three", absent: "- name: Two"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			block, err := jobIn(fixture, testCase.job)
			if err != nil {
				t.Fatalf("jobIn(%q): %v", testCase.job, err)
			}
			if !strings.Contains(block, testCase.contains) {
				t.Errorf("the block for %q does not carry %q:\n%s", testCase.job, testCase.contains, block)
			}
			if strings.Contains(block, testCase.absent) {
				t.Errorf("the block for %q runs past its own end and carries %q:\n%s", testCase.job, testCase.absent, block)
			}
		})
	}
}

// TestJobInRefusesRatherThanAnsweringAboutSomethingElse is the whole reason this
// reader is not a plain substring search. Every caller treats what it gets back
// as a job: an empty block asserts nothing and reports green, and a block cut
// from `on:` asserts something true about the wrong text, which reports green
// too. Both are refusals here.
func TestJobInRefusesRatherThanAnsweringAboutSomethingElse(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		job     string
	}{
		// `push:` and `workflow_call:` sit under `on:` at a job's own
		// indentation. Scoping the search to `jobs:` is what keeps them out.
		{name: "a trigger sharing a job's indentation", content: fixture, job: "push"},
		{name: "another trigger", content: fixture, job: "workflow_call"},
		{name: "a job that is not declared", content: fixture, job: "publish"},
		{name: "a document with no jobs key", content: "name: Fixture\non:\n  push:\n", job: "first"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			block, err := jobIn(testCase.content, testCase.job)
			if err == nil {
				t.Fatalf("jobIn(%q) returned a block instead of refusing:\n%s", testCase.job, block)
			}
		})
	}
}

func TestJobHeadersCountsTheJobsAndNothingElse(t *testing.T) {
	jobsAt := strings.Index(fixture, "\njobs:\n")
	if jobsAt < 0 {
		t.Fatal("the fixture has no `jobs:` key")
	}

	if headers := JobHeaders(fixture[jobsAt:]); len(headers) != 3 {
		t.Errorf("JobHeaders found %d job headers, want 3: %q", len(headers), headers)
	}
	// Asked of the whole document it also sees `on:`'s children, which is why
	// the caller that counts jobs slices from `jobs:` first.
	if headers := JobHeaders(fixture); len(headers) <= 3 {
		t.Errorf("JobHeaders over the whole document found %d headers, and the two under `on:` should be among them: %q", len(headers), headers)
	}
}

func TestRepoRootFindsTheModuleRoot(t *testing.T) {
	root := RepoRoot(t)

	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("RepoRoot returned %s, which holds no go.mod: %v", root, err)
	}
}

// TestReadNormalisesLineEndings reads a real workflow, which is what every
// caller does. On a CRLF checkout this is the assertion that keeps the offsets
// the same as on the runner; on an LF one it costs nothing.
func TestReadNormalisesLineEndings(t *testing.T) {
	content := Read(t, ".github/workflows/docker-image.yml")

	if !strings.Contains(content, "\njobs:\n") {
		t.Fatal("the workflow read back carries no `jobs:` key")
	}
	if strings.Contains(content, "\r") {
		t.Error("the workflow read back still carries a carriage return, so a guard's offsets differ between a Windows checkout and the runner")
	}
}

func TestJobReadsAJobOutOfARealWorkflow(t *testing.T) {
	block := Job(t, ".github/workflows/docker-image.yml", "publish")

	if !strings.Contains(block, "      - name: Checkout\n") {
		t.Errorf("the publish job read back carries no steps:\n%s", block)
	}
}
