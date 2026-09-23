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

// TestStepsSplitsEveryStepAndStopsAtTheJob feeds Steps a job whose steps open
// on three different keys, with a shallow comment inside one and a job-level
// key after the last: each step is returned whole and alone, and the last one
// stops before the job's own key.
func TestStepsSplitsEveryStepAndStopsAtTheJob(t *testing.T) {
	const job = "    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: First\n        run: |\n          true\n      # why sh\n        shell: sh {0}\n" +
		"      - id: probe\n        shell: bash\n" +
		"      - if: always()\n        uses: actions/checkout@v4\n" +
		"    defaults:\n      run:\n        shell: bash\n"
	want := []string{
		"        name: First\n        run: |\n          true\n      # why sh\n        shell: sh {0}\n",
		"        id: probe\n        shell: bash\n",
		"        if: always()\n        uses: actions/checkout@v4\n",
	}

	got := Steps(job)
	if len(got) != len(want) {
		t.Fatalf("Steps returned %d steps, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %q, want %q", i, got[i], want[i])
		}
	}

	if steps := Steps("    runs-on: ubuntu-latest\n    uses: ./.github/workflows/x.yml\n"); steps != nil {
		t.Errorf("a job with no `steps:` gave %q", steps)
	}
	if steps := Steps("    steps:\n      - id: only\n"); len(steps) != 1 {
		t.Errorf("a job opening on `steps:` gave %q, want its one step", steps)
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

// TestBashStepFlagsIsHowTheRunnerStartsTheStep pins the answer for each
// invocation a harness may run a step under: `shell: bash` at a workflow
// step's depth and at a composite action step's, a trailing YAML comment on
// the key included, and a workflow step with no `shell:` — the shape of
// `Scan the image before publishing it` — which runs as `bash -e {0}`, without
// pipefail. A `shell:` deeper than the step's keys is not the step's own.
func TestBashStepFlagsIsHowTheRunnerStartsTheStep(t *testing.T) {
	const bash, bare = "--noprofile --norc -eo pipefail", "-e"

	for _, testCase := range []struct {
		name, block, want string
	}{
		{"shell: bash", "        shell: bash\n        run: |\n          true\n", bash},
		{"a commented shell: bash", "        id: image\n        shell: bash # the step's own\n        run: |\n          true\n", bash},
		{"a composite step's shell: bash", "      id: diff\n      shell: bash\n      run: |\n        true\n", bash},
		{"a composite step opening on its item", "    - id: diff\n      shell: bash\n      run: |\n        true\n", bash},
		{"a comment ahead of the first key", "        # why\n        shell: bash\n", bash},
		{"no shell declared", "        run: |\n          true\n", bare},
		{"a shell key only inside the script", "        run: |\n          shell: bash\n", bare},
		{"a shell key only among an action's inputs", "        with:\n          shell: bash\n", bare},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			flags := BashStepFlags(t, "fixture.yml", "Fixture", testCase.block)
			if got := strings.Join(flags, " "); got != testCase.want {
				t.Errorf("BashStepFlags read %q off\n%s\nwant %q", got, testCase.block, testCase.want)
			}
		})
	}
}

// TestBashStepFlagsRefusesAShellItWasNotHanded is every block a harness must
// not run under flags it assumed: another interpreter or template, a composite
// step the runner would refuse, and a block whose own keys it cannot place.
func TestBashStepFlagsRefusesAShellItWasNotHanded(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		block string
	}{
		{name: "another interpreter", block: "        shell: sh\n        run: |\n          true\n"},
		{name: "a custom bash template", block: "        shell: bash -e {0}\n        run: |\n          true\n"},
		{name: "two shell keys", block: "        shell: bash\n        shell: bash\n"},
		{name: "a composite step with no shell", block: "      id: diff\n      run: |\n        true\n"},
		{name: "a composite step's shell only among its inputs", block: "      with:\n        shell: bash\n"},
		{name: "a composite step's other interpreter", block: "      shell: pwsh\n"},
		{name: "keys at neither step depth", block: "          shell: bash\n"},
		{name: "no key at all", block: "\n        # only a comment\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if flags, err := bashStepFlagsIn(testCase.block); err == nil {
				t.Fatalf("bashStepFlagsIn answered %q instead of refusing:\n%s", flags, testCase.block)
			}
		})
	}
}

// defaultShellMovers returns every line of a workflow that changes what a step
// with no `shell:` runs under: a `defaults:` block setting a `shell:`, or a
// `runs-on:` that is not an `ubuntu-` runner (Windows defaults to pwsh). A
// `runs-on:` whose value is a list or an expression is counted too, since
// which runner it names cannot be read off the line.
func defaultShellMovers(content string) []string {
	var movers []string
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if value, ok := strings.CutPrefix(trimmed, "runs-on:"); ok {
			if !strings.HasPrefix(strings.TrimSpace(value), "ubuntu-") {
				movers = append(movers, line)
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "defaults:") {
			continue
		}
		indent := len(line) - len(trimmed)
		for _, below := range lines[i+1:] {
			rest := strings.TrimLeft(below, " ")
			if rest == "" || strings.HasPrefix(rest, "#") {
				continue
			}
			if len(below)-len(rest) <= indent {
				break
			}
			if strings.HasPrefix(rest, "shell:") {
				movers = append(movers, line+" › "+rest)
			}
		}
	}
	return movers
}

// TestNoWorkflowMovesTheDefaultShell holds the premise of BashStepFlags'
// answer for a workflow step with no `shell:` — the Linux runner's
// `bash -e {0}`. A step block does not carry its workflow's or its job's
// `defaults.run.shell`, nor its job's runner, so a workflow setting either
// fails here rather than letting a harness run such a step under flags the
// runner does not use.
func TestNoWorkflowMovesTheDefaultShell(t *testing.T) {
	for _, content := range []string{
		"defaults:\n  run:\n    shell: sh\njobs:\n",
		"jobs:\n  build:\n    defaults:\n      run:\n        # why\n        shell: bash\n",
		"jobs:\n  build:\n    runs-on: windows-latest\n",
		"jobs:\n  build:\n    runs-on: ${{ matrix.os }}\n",
		"jobs:\n  build:\n    runs-on:\n      - self-hosted\n",
	} {
		if movers := defaultShellMovers(content); len(movers) == 0 {
			t.Errorf("defaultShellMovers found nothing in\n%s", content)
		}
	}
	control := "jobs:\n  build:\n    defaults:\n      run:\n        working-directory: web\n    runs-on: ubuntu-24.04\n    steps:\n      - shell: bash\n"
	if movers := defaultShellMovers(control); len(movers) != 0 {
		t.Errorf("defaultShellMovers flagged %q in a workflow that moves nothing", movers)
	}

	workflows, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "workflows", "*.yml"))
	if err != nil || len(workflows) == 0 {
		t.Fatalf("no workflows found to judge (%v)", err)
	}
	for _, path := range workflows {
		workflow := ".github/workflows/" + filepath.Base(path)
		if movers := defaultShellMovers(Read(t, workflow)); len(movers) != 0 {
			t.Errorf("%s: %q changes what a step with no `shell:` runs under, and BashStepFlags answers such a step with the Linux runner's `bash -e`", workflow, movers)
		}
	}
}
