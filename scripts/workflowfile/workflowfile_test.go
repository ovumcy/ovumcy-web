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

// TestJobInReadsAWorkflowThatOpensOnJobs is the line-anchoring case. The key is
// there, so a refusal would be the reader describing its own search rather than
// the document it was handed.
func TestJobInReadsAWorkflowThatOpensOnJobs(t *testing.T) {
	block, err := jobIn("jobs:\n  only:\n    runs-on: ubuntu-latest\n", "only")
	if err != nil {
		t.Fatalf("jobIn refused a workflow whose first line is `jobs:`: %v", err)
	}
	if !strings.Contains(block, "runs-on: ubuntu-latest") {
		t.Errorf("the block returned is not the job's:\n%s", block)
	}
}

// TestJobHeadersCountsOnlyTheJobs is the counting half of the question jobIn
// answers: `on:` nests two keys at a job's own indentation, and a count that
// included them would tell a caller comparing jobs against something else that
// it is short of entries it never had.
func TestJobHeadersCountsOnlyTheJobs(t *testing.T) {
	if headers := JobHeaders(t, "fixture.yml", fixture); len(headers) != 3 {
		t.Errorf("JobHeaders found %d headers, want the 3 jobs the fixture declares — `on:`'s `push:` and `workflow_call:` sit at the same indentation and are not jobs: %q", len(headers), headers)
	}
}

func TestRepoRootFindsTheModuleRoot(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repoRoot returned %s, which holds no go.mod: %v", root, err)
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

	// `runs-on:` and not a step name: what this package answers for is that a
	// job's body comes back from a real file, and the name of the publish job's
	// first step is a fact about the release pipeline. Asserted here, renaming
	// it would redden a package that reads YAML and knows nothing about
	// publishing — where `publishorder`'s pinned step list already reports it.
	if !strings.Contains(block, "\n    runs-on: ") {
		t.Errorf("the publish job read back carries no `runs-on:`, so this is not a job body:\n%s", block)
	}
}

// TestStepInStopsAtEveryStepWhateverItOpensOn feeds stepIn a job whose steps
// open on `- id:` and `- uses:` as well as `- name:`, each declaring a `shell:`
// the named step above it does not.
func TestStepInStopsAtEveryStepWhateverItOpensOn(t *testing.T) {
	const job = "    steps:\n" +
		"      - name: First\n        run: |\n          true\n" +
		"      - id: probe\n        shell: bash\n        run: \"true\"\n" +
		"      - name: Second\n        run: |\n          true\n" +
		"      - uses: actions/checkout@v4\n        shell: bash\n" +
		"      - name: Last\n        run: |\n          true\n"
	const lone = "        run: |\n          true\n"

	for _, step := range []string{"First", "Second", "Last"} {
		got, err := stepIn(job, step)
		if err != nil {
			t.Fatalf("stepIn(%q): %v", step, err)
		}
		if got != lone {
			t.Errorf("stepIn(%q) = %q, want %q", step, got, lone)
		}
	}

	if block, err := stepIn(job, "Missing"); err == nil {
		t.Errorf("stepIn answered %q for a step the job does not declare instead of refusing", block)
	}
}

// TestStepInStopsBeforeJobLevelKeysAfterTheLastStep is the last-step case:
// there is no next `      - ` item for stepIn to stop at, so without the
// dedent check a job-level key written after `steps:` reads as the last
// step's own.
func TestStepInStopsBeforeJobLevelKeysAfterTheLastStep(t *testing.T) {
	const lone = "        run: |\n          true\n"

	for _, testCase := range []struct {
		name string
		job  string
	}{
		{
			name: "a job-level scalar key, its own key bleeding in",
			job: "    steps:\n" +
				"      - name: Only\n        run: |\n          true\n" +
				"    defaults:\n      run:\n        shell: bash\n",
		},
		{
			name: "a job-level list key following steps",
			job: "    steps:\n" +
				"      - name: Only\n        run: |\n          true\n" +
				"    needs:\n      - build\n      - test\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := stepIn(testCase.job, "Only")
			if err != nil {
				t.Fatalf("stepIn(%q): %v", "Only", err)
			}
			if got != lone {
				t.Errorf("stepIn(%q) = %q, want %q — it ran on past the last step into the job-level key after `steps:`", "Only", got, lone)
			}
		})
	}
}

// TestStepInKeepsKeysPastAShallowComment pins the other side of the dedent
// check: a comment written shallower than a step's keys is not a job-level
// key, so the step's own `shell:` after it still belongs to the step.
func TestStepInKeepsKeysPastAShallowComment(t *testing.T) {
	const step = "        run: |\n          true\n      # why sh\n        shell: sh {0}\n"
	job := "    steps:\n      - name: Only\n" + step

	got, err := stepIn(job, "Only")
	if err != nil {
		t.Fatalf("stepIn(%q): %v", "Only", err)
	}
	if got != step {
		t.Errorf("stepIn(%q) = %q, want %q — the shallow comment cut the step before its own `shell:`", "Only", got, step)
	}
}

// TestStepReadsAStepOutOfARealWorkflow anchors Step on a workflow the
// repository ships, so the wrapper is exercised and not only its answer.
func TestStepReadsAStepOutOfARealWorkflow(t *testing.T) {
	const workflow = ".github/workflows/docker-image.yml"
	block := Step(t, workflow, "publish", Job(t, workflow, "publish"), "Checkout")
	if !strings.Contains(block, "uses: actions/checkout") {
		t.Errorf("Step read the publish job's Checkout step as %q", block)
	}
}

// TestBashStepFlagsIsWhatShellBashCompilesTo pins the answer for the one shell
// a harness may run a step under, including a trailing YAML comment on the key.
func TestBashStepFlagsIsWhatShellBashCompilesTo(t *testing.T) {
	want := "--noprofile --norc -eo pipefail"

	for _, block := range []string{
		"        shell: bash\n        run: |\n          true\n",
		"        id: image\n        shell: bash # the step's own\n        run: |\n          true\n",
	} {
		flags := BashStepFlags(t, "fixture.yml", "Fixture", block)
		if got := strings.Join(flags, " "); got != want {
			t.Errorf("BashStepFlags read %q off\n%s\nwant %q", got, block, want)
		}
	}
}

// TestBashStepFlagsRefusesAShellItWasNotHanded is every block a harness must
// not run under `shell: bash`'s flags. The first is the shape of `Scan the image
// before publishing it`: no `shell:`, so `bash -e {0}` on the runner.
func TestBashStepFlagsRefusesAShellItWasNotHanded(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		block string
	}{
		{name: "no shell declared", block: "        run: |\n          true\n"},
		{name: "another interpreter", block: "        shell: sh\n        run: |\n          true\n"},
		{name: "a custom bash template", block: "        shell: bash -e {0}\n        run: |\n          true\n"},
		{name: "a shell key only inside the script", block: "        run: |\n          shell: bash\n"},
		{name: "a shell key only among an action's inputs", block: "        with:\n          shell: bash\n"},
		{name: "two shell keys", block: "        shell: bash\n        shell: bash\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if flags, err := bashStepFlagsIn(testCase.block); err == nil {
				t.Fatalf("bashStepFlagsIn answered %q instead of refusing:\n%s", flags, testCase.block)
			}
		})
	}
}
