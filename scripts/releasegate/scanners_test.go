package releasegate

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The step that reads the scanner workflows' verdicts on the tagged commit.
// ci.yml's own lanes are the other step's; Security, CodeQL and Gitleaks hold
// their own required checks and used to be read by nothing on the tag path.
const (
	scannerStep = "Assert the scanners passed on the tagged commit"

	securityFile = ".github/workflows/security.yml"
	codeqlFile   = ".github/workflows/codeql.yml"
	gitleaksFile = ".github/workflows/gitleaks.yml"
)

// scannerRun is one row of the run listing the step reads: id, event, head
// branch, workflow file and conclusion.
type scannerRun struct {
	id, event, branch, file, conclusion string
}

type scannerWorld struct {
	runs           []scannerRun
	jobs           []checkRun
	reruns         []checkRun
	jobsUnreadable bool
	// tree, when set, makes the step run inside a real two-commit repository
	// — the ancestor, then the tagged commit changing one path — so the
	// trivy-fs walk lists real history and runs security.yml's real detect
	// step over a real diff. The ancestor carries one push run of
	// security.yml whose trivy-fs job concluded ancestorTrivyFS.
	tree *scannerTree
}

type scannerTree struct {
	changed         string
	ancestorTrivyFS string
}

// withTrivyFS sets trivy-fs's conclusion in the Security runs of one event
// only, so a case can say what the queue run and the push run each did.
func (w scannerWorld) withTrivyFS(event, conclusion string) scannerWorld {
	ids := map[string]bool{}
	for _, run := range w.runs {
		if run.file == securityFile && run.event == event {
			ids[run.id] = true
		}
	}
	jobs := slices.Clone(w.jobs)
	for index := range jobs {
		if jobs[index].name == "trivy-fs" && ids[jobs[index].suite] {
			jobs[index].conclusion = conclusion
		}
	}
	w.jobs = jobs
	return w
}

// buildScannerTree commits the ancestor and the tagged commit into dir and
// returns their SHAs. security.yml is copied in, because the walk cuts its
// detect step out of the checked-out file exactly as the job does.
func buildScannerTree(t *testing.T, dir string, tree scannerTree) (ancestor, tagged string) {
	t.Helper()

	git := func(args ...string) string {
		command := exec.Command("git", append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		command.Dir = dir
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "GIT_") {
				command.Env = append(command.Env, entry)
			}
		}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	write := func(rel, content string) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	security, err := os.ReadFile(filepath.Join(repoRootDir(t), filepath.FromSlash(securityFile)))
	if err != nil {
		t.Fatalf("read %s: %v", securityFile, err)
	}
	git("init", "-q")
	write(securityFile, strings.ReplaceAll(string(security), "\r\n", "\n"))
	write(tree.changed, "before\n")
	git("add", "-A")
	git("commit", "-q", "-m", "ancestor")
	ancestor = git("rev-parse", "HEAD")
	write(tree.changed, "after\n")
	git("add", "-A")
	git("commit", "-q", "-m", "tagged")
	return ancestor, git("rev-parse", "HEAD")
}

func repoRootDir(t *testing.T) string {
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
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

// scannerJobNames reads the three job lists out of the step's own env.
func scannerJobNames(t *testing.T) (security, codeql, gitleaks []string) {
	t.Helper()

	block := workflowfile.Step(t, gateWorkflow, gateJob, workflowfile.Job(t, gateWorkflow, gateJob), scannerStep)
	marker := "        env:\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("%s, step %q: no `env:` mapping", gateWorkflow, scannerStep)
	}
	env := map[string]string{}
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		entry := strings.TrimPrefix(line, "          ")
		if strings.HasPrefix(entry, "#") || strings.HasPrefix(entry, " ") {
			continue
		}
		if match := envEntry.FindStringSubmatch(entry); match != nil && !strings.Contains(match[2], "${{") {
			env[match[1]] = strings.TrimSpace(match[2])
		}
	}
	for _, key := range []string{"SECURITY_JOBS", "CODEQL_JOBS", "GITLEAKS_JOBS"} {
		if env[key] == "" {
			t.Fatalf("%s, step %q declares no %s, so a scanner workflow is read for nothing", gateWorkflow, scannerStep, key)
		}
	}
	return strings.Split(env["SECURITY_JOBS"], "|"), strings.Split(env["CODEQL_JOBS"], "|"), strings.Split(env["GITLEAKS_JOBS"], "|")
}

// greenScanners is a normally merged commit: each scanner workflow ran in the
// merge queue and on the push to main, every lane green.
func greenScanners(t *testing.T) scannerWorld {
	t.Helper()

	security, codeql, gitleaks := scannerJobNames(t)
	world := scannerWorld{}
	for index, workflow := range []struct {
		file string
		jobs []string
	}{{securityFile, security}, {codeqlFile, codeql}, {gitleaksFile, gitleaks}} {
		for offset, event := range []string{"merge_group", "push"} {
			id := string(rune('a'+index)) + string(rune('0'+offset))
			branch := "main"
			if event == "merge_group" {
				branch = queueRunBranch
			}
			world.runs = append(world.runs, scannerRun{id, event, branch, workflow.file, "success"})
			for _, name := range workflow.jobs {
				world.jobs = append(world.jobs, checkRun{id, name, "success"})
			}
		}
	}
	return world
}

func (w scannerWorld) withJob(name, conclusion string) scannerWorld {
	jobs := make([]checkRun, 0, len(w.jobs))
	for _, row := range w.jobs {
		if row.name == name {
			row.conclusion = conclusion
		}
		jobs = append(jobs, row)
	}
	w.jobs = jobs
	return w
}

func (w scannerWorld) withoutJob(name string) scannerWorld {
	w.jobs = slices.DeleteFunc(slices.Clone(w.jobs), func(row checkRun) bool { return row.name == name })
	return w
}

func (w scannerWorld) withoutRunsOf(file string) scannerWorld {
	w.runs = slices.DeleteFunc(slices.Clone(w.runs), func(run scannerRun) bool { return run.file == file })
	return w
}

func (w scannerWorld) mapRuns(change func(*scannerRun)) scannerWorld {
	runs := slices.Clone(w.runs)
	for index := range runs {
		change(&runs[index])
	}
	w.runs = runs
	return w
}

func runScannerStep(t *testing.T, world scannerWorld) (string, error) {
	t.Helper()

	block := workflowfile.Step(t, gateWorkflow, gateJob, workflowfile.Job(t, gateWorkflow, gateJob), scannerStep)
	script := runScript(t, scannerStep, block)
	security, codeql, gitleaks := scannerJobNames(t)

	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return shellQuote(filepath.ToSlash(path))
	}

	taggedSHA, ancestorSHA := "5049126faa3152cced900c304c3640e4ec724ba5", "no-ancestor"
	var ancestorRows strings.Builder
	if world.tree != nil {
		ancestorSHA, taggedSHA = buildScannerTree(t, dir, *world.tree)
		ancestorRows.WriteString("push\tanc1\tmain\t" + securityFile + "\n")
		world.jobs = append(slices.Clone(world.jobs), checkRun{"anc1", "trivy-fs", world.tree.ancestorTrivyFS})
	}

	var runRows, jobRows strings.Builder
	for _, run := range world.runs {
		runRows.WriteString(run.event + "\t" + run.id + "\t" + run.branch + "\t" + run.file + "\t" + run.conclusion + "\n")
	}
	for _, row := range world.jobs {
		jobRows.WriteString(row.suite + "\t" + row.name + "\t" + row.conclusion + "\t1\n")
	}
	for _, row := range world.reruns {
		jobRows.WriteString(row.suite + "\t" + row.name + "\t" + row.conclusion + "\t2\n")
	}

	jobsReply := `awk -F'\t' -v id="$id" '$1 == id' ` + write("jobs.tsv", jobRows.String())
	if world.jobsUnreadable {
		jobsReply = `echo "the harness made this read fail: $url" >&2; return 1`
	}
	preamble := strings.Join([]string{
		`gh() {`,
		`  url=""`,
		`  for arg in "$@"; do case "$arg" in repos/*) url="$arg";; esac; done`,
		`  case "$url" in`,
		`    */actions/runs/*/jobs*) id="${url#*/actions/runs/}"; id="${id%%/*}"; ` + jobsReply + ` ;;`,
		`    *"head_sha=` + ancestorSHA + `"*) cat ` + write("ancestor-runs.tsv", ancestorRows.String()) + ` ;;`,
		`    */actions/runs*) cat ` + write("runs.tsv", runRows.String()) + ` ;;`,
		`    */check-runs*) echo "` + stubChecksAPIRead + `: $url" >&2; return 1 ;;`,
		`    *) echo "` + stubUnservedEndpoint + `: $*" >&2; return 1 ;;`,
		`  esac`,
		`}`,
		`git() {`,
		`  case "${1:-}" in`,
		`    rev-parse) printf '%s\n' "$GITHUB_SHA" ;;`,
		// The trivy-fs walk's history reads go to the real git, over the
		// fixture repository when the case built one and over nothing
		// otherwise, where they fail as an unreadable history does.
		`    rev-list|diff) command git "$@" ;;`,
		`    *) echo "` + stubUnexpectedGit + `: $*" >&2; return 1 ;;`,
		`  esac`,
		`}`,
		"",
	}, "\n")

	bash := bashPath(t)
	requireWorkingBash(t, bash)
	environ := append(os.Environ(),
		"GITHUB_SHA="+taggedSHA,
		"ANCESTOR_WALK_BOUND=20",
		"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
		"GITHUB_REF_NAME=v2.0.0",
		"GH_TOKEN=stub",
		"SECURITY_JOBS="+strings.Join(security, "|"),
		"CODEQL_JOBS="+strings.Join(codeql, "|"),
		"GITLEAKS_JOBS="+strings.Join(gitleaks, "|"),
	)
	output, err := runStepScript(t, bash, scannerStep, block, preamble+script, dir, environ)
	for _, stubAbort := range []string{stubUnservedEndpoint, stubUnexpectedGit, stubChecksAPIRead} {
		if strings.Contains(output, stubAbort) {
			t.Fatalf("the step reached a harness stub (%q) instead of judging: it may read only the run listing and one run's jobs.\n%s", stubAbort, output)
		}
	}
	return output, err
}

// TestReleaseTagGateReadsTheScannerWorkflows runs the new step's real script
// over states the scanner workflows can leave on a tagged commit.
func TestReleaseTagGateReadsTheScannerWorkflows(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		world       func(scannerWorld) scannerWorld
		wantRefusal bool
		wantOutput  string
	}{
		{name: "every scanner green in the queue and on main", world: func(w scannerWorld) scannerWorld { return w }},
		{
			// The positive control for `skipped`: a path-gated lane is cleared
			// for the diff. A step that refused it could never tag a docs-only
			// commit, and one that accepted it silently would hide the gap.
			name:       "a lane cleared for the diff by its path gate",
			world:      func(w scannerWorld) scannerWorld { return w.withJob("gosec", "skipped") },
			wantOutput: "`gosec` was skipped in every",
		},
		{
			// The merge-queue run skipped trivy-fs (no manifest changed
			// against the queue base); the push run to `main` scans every
			// tree, and it is what answers for this one.
			name:       "trivy-fs skipped in the queue and scanned by the push run",
			world:      func(w scannerWorld) scannerWorld { return w.withTrivyFS("merge_group", "skipped") },
			wantOutput: "`trivy-fs` concluded success in a run on",
		},
		{
			// The v2.0.0 shape: every judged run skipped trivy-fs and nothing
			// else scanned the tree. A skip is not a scan.
			name:        "trivy-fs skipped in every run on the tag with no scan on record",
			world:       func(w scannerWorld) scannerWorld { return w.withJob("trivy-fs", "skipped") },
			wantRefusal: true,
			wantOutput:  "`trivy-fs` was skipped in every judged",
		},
		{
			name: "trivy-fs skipped in the queue and failed in the push run",
			world: func(w scannerWorld) scannerWorld {
				return w.withTrivyFS("merge_group", "skipped").withTrivyFS("push", "failure")
			},
			wantRefusal: true,
			wantOutput:  "did not conclude success",
		},
		{
			// The positive control for the walk: the ancestor's manifests
			// are the tag's, and its scan of them passed.
			name: "trivy-fs scanned on an ancestor with the same manifests",
			world: func(w scannerWorld) scannerWorld {
				w = w.withJob("trivy-fs", "skipped")
				w.tree = &scannerTree{changed: "docs/notes.md", ancestorTrivyFS: "success"}
				return w
			},
			wantOutput: "`trivy-fs` concluded success on",
		},
		{
			name: "trivy-fs failed on an ancestor with the same manifests",
			world: func(w scannerWorld) scannerWorld {
				w = w.withJob("trivy-fs", "skipped")
				w.tree = &scannerTree{changed: "docs/notes.md", ancestorTrivyFS: "failure"}
				return w
			},
			wantRefusal: true,
			wantOutput:  "whose manifests are",
		},
		{
			// A lockfile changed between the ancestor and the tag, so the
			// ancestor's scan read other manifests and says nothing here.
			name: "a lockfile changed since the ancestor that scanned",
			world: func(w scannerWorld) scannerWorld {
				w = w.withJob("trivy-fs", "skipped")
				w.tree = &scannerTree{changed: "package-lock.json", ancestorTrivyFS: "success"}
				return w
			},
			wantRefusal: true,
			wantOutput:  "runs trivy-fs for what changed",
		},
		{name: "trivy-image failed", world: func(w scannerWorld) scannerWorld { return w.withJob("trivy-image", "failure") }, wantRefusal: true},
		{name: "trivy-image-arm64 failed", world: func(w scannerWorld) scannerWorld { return w.withJob("trivy-image-arm64", "failure") }, wantRefusal: true},
		{name: "govulncheck cancelled", world: func(w scannerWorld) scannerWorld { return w.withJob("govulncheck", "cancelled") }, wantRefusal: true},
		{name: "a CodeQL leg failed", world: func(w scannerWorld) scannerWorld { return w.withJob("Analyze (go)", "failure") }, wantRefusal: true},
		{name: "a CodeQL leg is still running", world: func(w scannerWorld) scannerWorld { return w.withJob("Analyze (actions)", "pending") }, wantRefusal: true},
		{name: "gitleaks failed", world: func(w scannerWorld) scannerWorld { return w.withJob("gitleaks", "failure") }, wantRefusal: true},
		{name: "a renamed gitleaks lane reads as no verdict", world: func(w scannerWorld) scannerWorld { return w.withoutJob("gitleaks") }, wantRefusal: true},
		{name: "no CodeQL run exists", world: func(w scannerWorld) scannerWorld { return w.withoutRunsOf(codeqlFile) }, wantRefusal: true},
		{name: "no Security run exists", world: func(w scannerWorld) scannerWorld { return w.withoutRunsOf(securityFile) }, wantRefusal: true},
		{
			name: "a Gitleaks queue run that did not conclude success",
			world: func(w scannerWorld) scannerWorld {
				return w.setConclusion(gitleaksFile, "merge_group", "cancelled")
			},
			wantRefusal: true,
		},
		{
			// The queue run is the test of the commit that lands; a push run a
			// newer push cancelled is a duplicate and must not block the tag.
			name: "queue runs green and the push runs cancelled",
			world: func(w scannerWorld) scannerWorld {
				return w.setConclusion(securityFile, "push", "cancelled").
					setConclusion(codeqlFile, "push", "cancelled").
					setConclusion(gitleaksFile, "push", "cancelled")
			},
		},
		{
			name: "queue run failed and the push run green",
			world: func(w scannerWorld) scannerWorld {
				return w.setConclusion(securityFile, "merge_group", "failure")
			},
			wantRefusal: true,
		},
		{
			name: "no queue run and the push run green",
			world: func(w scannerWorld) scannerWorld {
				return w.withoutEvent("merge_group")
			},
		},
		{
			name: "no queue run and the push run cancelled",
			world: func(w scannerWorld) scannerWorld {
				return w.withoutEvent("merge_group").setConclusion(securityFile, "push", "cancelled")
			},
			wantRefusal: true,
		},
		{
			// A pull_request run executes the copy of the workflow on the PR's
			// own branch, so nothing it reports is proof.
			name: "only a pull_request run of CodeQL exists",
			world: func(w scannerWorld) scannerWorld {
				return w.mapRuns(func(run *scannerRun) {
					if run.file == codeqlFile {
						run.event = "pull_request"
					}
				})
			},
			wantRefusal: true,
		},
		{
			name: "a push run on a branch other than main",
			world: func(w scannerWorld) scannerWorld {
				return w.withoutRunsOf(gitleaksFile).withRun(scannerRun{"z9", "push", "side", gitleaksFile, "success"})
			},
			wantRefusal: true,
		},
		{
			// The workflow is the file: a lookalike file declaring the same job
			// names proves nothing about the scanner.
			name: "the lanes exist only in another workflow file",
			world: func(w scannerWorld) scannerWorld {
				return w.mapRuns(func(run *scannerRun) {
					if run.file == securityFile {
						run.file = ".github/workflows/other.yml"
					}
				})
			},
			wantRefusal: true,
		},
		{
			name:  "a failed first attempt cleared by a re-run",
			world: func(w scannerWorld) scannerWorld { return w.withJob("gosec", "failure").withRerun("gosec", "success") },
		},
		{
			name:        "a green first attempt, then a failed re-run",
			world:       func(w scannerWorld) scannerWorld { return w.withRerun("gosec", "failure") },
			wantRefusal: true,
		},
		{name: "the jobs of a run cannot be read", world: func(w scannerWorld) scannerWorld { w.jobsUnreadable = true; return w }, wantRefusal: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runScannerStep(t, testCase.world(greenScanners(t)))

			if testCase.wantRefusal && err == nil {
				t.Fatalf("the gate published this tag and owed a refusal.\n%s", output)
			}
			if !testCase.wantRefusal && err != nil {
				t.Fatalf("the gate refused a tag it owes: %v\n%s", err, output)
			}
			if testCase.wantOutput != "" && !strings.Contains(output, testCase.wantOutput) {
				t.Fatalf("the output does not say %q.\n%s", testCase.wantOutput, output)
			}
		})
	}
}

func (w scannerWorld) setConclusion(file, event, conclusion string) scannerWorld {
	return w.mapRuns(func(run *scannerRun) {
		if run.file == file && run.event == event {
			run.conclusion = conclusion
		}
	})
}

func (w scannerWorld) withoutEvent(event string) scannerWorld {
	w.runs = slices.DeleteFunc(slices.Clone(w.runs), func(run scannerRun) bool { return run.event == event })
	return w
}

func (w scannerWorld) withRun(run scannerRun) scannerWorld {
	w.runs = append(slices.Clone(w.runs), run)
	return w
}

// withRerun adds a second attempt of every job of that name, the way a re-run
// of the run lists it.
func (w scannerWorld) withRerun(name, conclusion string) scannerWorld {
	reruns := slices.Clone(w.reruns)
	for _, row := range w.jobs {
		if row.name == name {
			reruns = append(reruns, checkRun{row.suite, name, conclusion})
		}
	}
	w.reruns = reruns
	return w
}

// TestTheScannerStepNamesEveryJobOfTheScannerWorkflows derives the expected job
// set from the scanner workflows themselves, so a lane added to one of them and
// not to the step is a red here rather than a scan nobody reads on the tag path.
func TestTheScannerStepNamesEveryJobOfTheScannerWorkflows(t *testing.T) {
	security, codeql, gitleaks := scannerJobNames(t)

	headers := func(file string) []string {
		var names []string
		for _, header := range workflowfile.JobHeaders(t, file, workflowfile.Read(t, file)) {
			names = append(names, strings.TrimSuffix(strings.TrimSpace(header), ":"))
		}
		return names
	}
	// `changes` only decides which lanes run; it is not a scan.
	without := func(names []string, drop string) []string {
		return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return name == drop })
	}

	wantSecurity := without(headers(securityFile), "changes")
	wantGitleaks := without(headers(gitleaksFile), "changes")

	var wantCodeQL []string
	for _, match := range regexp.MustCompile(`(?m)^\s+- language: (\S+)$`).FindAllStringSubmatch(workflowfile.Read(t, codeqlFile), -1) {
		wantCodeQL = append(wantCodeQL, "Analyze ("+match[1]+")")
	}
	if len(wantCodeQL) == 0 || len(wantSecurity) == 0 || len(wantGitleaks) == 0 {
		t.Fatalf("a scanner workflow yielded no jobs (security %v, codeql %v, gitleaks %v): the reader drifted", wantSecurity, wantCodeQL, wantGitleaks)
	}

	for _, set := range []struct {
		file      string
		got, want []string
	}{
		{securityFile, security, wantSecurity},
		{codeqlFile, codeql, wantCodeQL},
		{gitleaksFile, gitleaks, wantGitleaks},
	} {
		got, want := slices.Clone(set.got), slices.Clone(set.want)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s, step %q reads %v, but %s declares %v", gateWorkflow, scannerStep, got, set.file, want)
		}
	}
}

// TestTheScannerStepCannotFailOpen covers what running the script cannot see:
// the runner honours `if:` and `continue-on-error:`, and either on the step lets
// a refusal pass.
func TestTheScannerStepCannotFailOpen(t *testing.T) {
	job := workflowfile.Job(t, gateWorkflow, gateJob)
	block := workflowfile.Step(t, gateWorkflow, gateJob, job, scannerStep)
	if found := workflowfile.StepFailOpenKeys(block); len(found) > 0 {
		t.Errorf("%s, job %q, step %q carries %q: its refusal would no longer block `publish`", gateWorkflow, gateJob, scannerStep, found)
	}
}
