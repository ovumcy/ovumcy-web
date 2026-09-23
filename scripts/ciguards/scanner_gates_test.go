package ciguards

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

const (
	securityWorkflow = ".github/workflows/security.yml"
	codeqlWorkflow   = ".github/workflows/codeql.yml"
)

// ---------------------------------------------------------------------------
// A `changes`-gated scanner job must not fail open when `changes` itself is
// cancelled or fails.
// ---------------------------------------------------------------------------

// changesGatedJobs names every job whose `if:` (or, for a matrix job gated at
// step level instead, its per-leg run flag) is computed from a `changes`
// job's detection output. TestEveryChangesGatedJobInSecurityAndCodeQLIsPinned
// enumerates the set from the workflows, so a job added later with no row
// here fails rather than going unguarded.
var changesGatedJobs = []struct {
	workflow string
	job      string
}{
	{securityWorkflow, "gosec"},
	{securityWorkflow, "govulncheck"},
	{securityWorkflow, "trivy-fs"},
	{securityWorkflow, "trivy-image"},
	{codeqlWorkflow, "analyze"},
}

// JobIfSurvivesACancelledOrFailedChanges refuses a job `if:` that reads a
// `needs.changes` output without also carrying `!cancelled()`. Without it,
// GitHub Actions attaches an implicit success()-of-needs predicate to any
// `if:` with no status-check function of its own: a FAILED or cancelled
// `changes` job then SKIPS this job rather than running it, and a skipped
// job is a SATISFIED required check.
func JobIfSurvivesACancelledOrFailedChanges(condition string) error {
	if !strings.Contains(condition, "!cancelled()") {
		return fmt.Errorf("`if:` (%s) has no `!cancelled()` — a failed or cancelled `changes` job skips this job into a satisfied required check instead of running it", condition)
	}
	return nil
}

// changesOutputComparison matches a comparison against one of `changes`'
// outputs wherever it appears in a job — its own `if:`, or a matrix job's
// step-level `env:` expression — so one rule reads both.
var changesOutputComparison = regexp.MustCompile(`needs\.changes\.outputs\.[A-Za-z_]+\s*(==|!=)\s*'(true|false)'`)

// ChangesOutputComparisonsAreFailSafe refuses any comparison against a
// `changes` output that is not `!= 'false'` — in particular `== 'true'`,
// which reads an ABSENT or empty output (a crashed `changes` job, an output
// renamed on one side of an edit) as "do not run".
func ChangesOutputComparisonsAreFailSafe(content string) error {
	matches := changesOutputComparison.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return fmt.Errorf("found zero comparisons against a `changes` output — the scan itself is broken, since this job is only in this set because it reads one")
	}
	var offending []string
	for _, m := range matches {
		if m[1] != "!=" || m[2] != "false" {
			offending = append(offending, m[0])
		}
	}
	if len(offending) > 0 {
		return fmt.Errorf("%d comparison(s) against a `changes` output are not `!= 'false'`: %v", len(offending), offending)
	}
	return nil
}

// negatedFalseTerm matches one matrix leg's term in codeql.yml's
// RUN_THIS_LANGUAGE: a NEGATED equals-false, which keeps a matrix leg outside
// the named languages defaulting to "run".
var negatedFalseTerm = regexp.MustCompile(`!\(matrix\.language == '[a-z-]+' && needs\.changes\.outputs\.[A-Za-z_]+ == 'false'\)`)

// RunThisLanguageNeverSkipsAnUnlistedLanguage refuses codeql.yml's
// RUN_THIS_LANGUAGE unless it is built from exactly one negated-false term
// per matrix leg (three today) and never compares a `changes` output against
// 'true' — an OR of per-language positives is fail-CLOSED for a leg added
// later, since none of the named terms would match it.
func RunThisLanguageNeverSkipsAnUnlistedLanguage(content string) error {
	// Scoped to needs.changes.outputs.* comparisons: the steps' own
	// env.RUN_THIS_LANGUAGE == 'true' guards compare the already-resolved
	// string and are the correct shape for a step-level gate.
	for _, m := range changesOutputComparison.FindAllStringSubmatch(content, -1) {
		if m[2] == "true" {
			return fmt.Errorf("RUN_THIS_LANGUAGE compares a `changes` output against 'true' (%q) — that reads an absent/empty output as skip", m[0])
		}
	}
	terms := negatedFalseTerm.FindAllString(content, -1)
	if len(terms) != 3 {
		return fmt.Errorf("found %d negated-false term(s) in RUN_THIS_LANGUAGE, want 3 (one per matrix leg — go, javascript-typescript, actions): %v", len(terms), terms)
	}
	return nil
}

func TestChangesGatedScannerJobsSurviveACancelledOrFailedChanges(t *testing.T) {
	for _, j := range changesGatedJobs {
		block := workflowfile.Job(t, j.workflow, j.job)
		if err := JobIfSurvivesACancelledOrFailedChanges(jobCondition(t, block)); err != nil {
			t.Fatalf("%s %s: %v", j.workflow, j.job, err)
		}
		if j.job == "analyze" {
			if err := RunThisLanguageNeverSkipsAnUnlistedLanguage(block); err != nil {
				t.Fatalf("%s %s: %v", j.workflow, j.job, err)
			}
			continue
		}
		if err := ChangesOutputComparisonsAreFailSafe(block); err != nil {
			t.Fatalf("%s %s: %v", j.workflow, j.job, err)
		}
	}
}

func TestJobIfSurvivesACancelledOrFailedChangesRefusesThePreFixCondition(t *testing.T) {
	if err := JobIfSurvivesACancelledOrFailedChanges("needs.changes.outputs.run_go != 'false'"); err == nil {
		t.Fatal("a condition with no !cancelled() was accepted")
	}
}

func TestChangesOutputComparisonsAreFailSafeRefusesAnEqualsTrueComparison(t *testing.T) {
	if err := ChangesOutputComparisonsAreFailSafe("if: needs.changes.outputs.run_go == 'true'"); err == nil {
		t.Fatal("an == 'true' comparison was accepted")
	}
}

func TestRunThisLanguageNeverSkipsAnUnlistedLanguageRefusesTheOldORShape(t *testing.T) {
	old := "(matrix.language == 'go' && needs.changes.outputs.run_go != 'false') || " +
		"(matrix.language == 'javascript-typescript' && needs.changes.outputs.run_js != 'false') || " +
		"(matrix.language == 'actions' && needs.changes.outputs.run_actions != 'false')"
	if err := RunThisLanguageNeverSkipsAnUnlistedLanguage(old); err == nil {
		t.Fatal("the OR-of-positives shape (fail-closed for an unlisted matrix leg) was accepted")
	}
}

var jobNeedsLine = regexp.MustCompile(`(?m)^    needs:\s*(.+)$`)

func jobDeclaresNeedsChanges(t *testing.T, workflow, job string) bool {
	t.Helper()
	m := jobNeedsLine.FindStringSubmatch(workflowfile.Job(t, workflow, job))
	if m == nil {
		return false
	}
	needs := strings.TrimSpace(m[1])
	return needs == "changes" || needs == "[changes]" || needs == "['changes']"
}

func TestEveryChangesGatedJobInSecurityAndCodeQLIsPinned(t *testing.T) {
	pinned := map[string]bool{}
	for _, j := range changesGatedJobs {
		pinned[j.workflow+"::"+j.job] = true
	}

	seen := map[string]bool{}
	var missing []string
	for _, wf := range []string{securityWorkflow, codeqlWorkflow} {
		content := workflowfile.Read(t, wf)
		for _, header := range workflowfile.JobHeaders(t, wf, content) {
			name := strings.TrimSuffix(strings.TrimSpace(header), ":")
			if name == "changes" || !jobDeclaresNeedsChanges(t, wf, name) {
				continue
			}
			key := wf + "::" + name
			seen[key] = true
			if !pinned[key] {
				missing = append(missing, key)
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("job(s) gated on `changes` are not in changesGatedJobs: %v", missing)
	}
	for _, j := range changesGatedJobs {
		if key := j.workflow + "::" + j.job; !seen[key] {
			t.Fatalf("pinned job %s no longer declares `needs: changes` — changesGatedJobs is stale", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Every caller of the diff action: gated to the events with a base, and
// executed by the harness.
// ---------------------------------------------------------------------------

// detectCasesByWorkflow is the behaviour table each caller of the diff action
// is executed against.
var detectCasesByWorkflow = map[string][]detectCase{
	ciWorkflow: detectCases,
	securityWorkflow: {
		{"docs only", "pull_request", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_trivyfs": "false", "run_trivyimage": "false"}},
		{"non-ASCII Go path", "pull_request", []string{"internal/api/évil.go"},
			map[string]string{"run_go": "true", "run_trivyfs": "false", "run_trivyimage": "true"}},
		{"nested lockfile", "pull_request", []string{"web/workspace/nested/package-lock.json"},
			map[string]string{"run_go": "false", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"web/embed.go", "pull_request", []string{"web/embed.go"},
			map[string]string{"run_go": "true", "run_trivyimage": "true"}},
		{"web/src only", "pull_request", []string{"web/src/js/a.js"},
			map[string]string{"run_go": "false", "run_trivyfs": "false", "run_trivyimage": "false"}},
		{"Go test file only", "pull_request", []string{"internal/x/a_test.go"},
			map[string]string{"run_go": "true", "run_trivyimage": "false"}},
		{"this workflow", "pull_request", []string{securityWorkflow},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"the pull-retry action", "pull_request", []string{".github/actions/docker-pull-retry/action.yml"},
			map[string]string{"run_go": "false", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"merge_group, docs only", "merge_group", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_trivyfs": "false", "run_trivyimage": "false"}},
		{"push", "push", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"schedule", "schedule", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"workflow_dispatch", "workflow_dispatch", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
	},
	codeqlWorkflow: {
		{"docs only", "pull_request", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_js": "false", "run_actions": "false"}},
		{"non-ASCII Go path", "pull_request", []string{"internal/api/évil.go"},
			map[string]string{"run_go": "true", "run_js": "false", "run_actions": "false"}},
		{"template html", "pull_request", []string{"internal/templates/x.html"},
			map[string]string{"run_go": "false", "run_js": "true", "run_actions": "false"}},
		{"nested package.json", "pull_request", []string{"web/tools/package.json"},
			map[string]string{"run_js": "true"}},
		{"nested package-lock.json", "pull_request", []string{"e2e/fixtures/package-lock.json"},
			map[string]string{"run_js": "true"}},
		{"the diff action", "pull_request", []string{diffAction + "/action.yml"},
			map[string]string{"run_go": "false", "run_js": "false", "run_actions": "true"}},
		{"this workflow", "pull_request", []string{codeqlWorkflow},
			map[string]string{"run_go": "true", "run_js": "true", "run_actions": "true"}},
		{"merge_group, docs only", "merge_group", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_js": "false", "run_actions": "false"}},
		{"push", "push", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_js": "true", "run_actions": "true"}},
		{"schedule", "schedule", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_js": "true", "run_actions": "true"}},
	},
}

// TestEveryCallerOfTheDiffActionIsGatedAndExecuted enumerates the action's
// callers from the workflows themselves: each must call it from its `changes`
// job, put diffEvents on that job's checkout and on the list step — the deep
// fetch is paid only where a base exists — and have a behaviour table above.
func TestEveryCallerOfTheDiffActionIsGatedAndExecuted(t *testing.T) {
	uses := "uses: ./" + diffAction + "\n"
	gate := "        if: " + diffEvents + "\n"

	callers := map[string]bool{}
	for _, wf := range allWorkflowFiles(t) {
		content := workflowfile.Read(t, wf)
		for _, header := range workflowfile.JobHeaders(t, wf, content) {
			job := strings.TrimSuffix(strings.TrimSpace(header), ":")
			block := workflowfile.Job(t, wf, job)
			if !strings.Contains(block, uses) {
				continue
			}
			if job != "changes" {
				t.Errorf("%s: job %q calls %s; only a `changes` job's list step is executed by this package", wf, job, diffAction)
				continue
			}
			callers[wf] = true
			steps := stepHeader.Split(block, -1)[1:]
			for _, needle := range []string{uses, "uses: actions/checkout@"} {
				found := false
				for _, step := range steps {
					if !strings.Contains(step, needle) {
						continue
					}
					found = true
					if !strings.Contains(step, gate) {
						t.Errorf("%s changes: the step with %q does not carry `if: %s`", wf, strings.TrimSpace(needle), diffEvents)
					}
				}
				if !found {
					t.Errorf("%s changes: no step with %q", wf, strings.TrimSpace(needle))
				}
			}
		}
	}
	if len(callers) == 0 {
		t.Fatalf("no workflow calls %s — this sweep would judge nothing", diffAction)
	}
	for wf := range callers {
		if len(detectCasesByWorkflow[wf]) == 0 {
			t.Errorf("%s calls %s but has no behaviour table in detectCasesByWorkflow", wf, diffAction)
		}
	}
	for wf := range detectCasesByWorkflow {
		if !callers[wf] {
			t.Errorf("detectCasesByWorkflow has a table for %s, which no longer calls %s", wf, diffAction)
		}
	}
}

func TestScannerDetectStepsDecideEachScannerFromTheDiff(t *testing.T) {
	for _, wf := range []string{securityWorkflow, codeqlWorkflow} {
		for _, c := range detectCasesByWorkflow[wf] {
			t.Run(path.Base(wf)+"/"+c.name, func(t *testing.T) {
				got := detectRun{workflow: wf, event: c.event, files: c.files, queueBase: queueBaseReal}.run(t)
				for k, v := range c.want {
					if got[k] != v {
						t.Errorf("%s = %q, want %q (all outputs: %v)", k, got[k], v, got)
					}
				}
			})
		}
	}
}

// TestTrivyFSRunsForAnAddedBinary drives security.yml's content fallback: a
// changed blob git classifies as binary forces trivy-fs whatever its path.
func TestTrivyFSRunsForAnAddedBinary(t *testing.T) {
	got := detectRun{workflow: securityWorkflow, event: "pull_request", binaries: []string{"some/dir/asset.bin"}, queueBase: queueBaseReal}.run(t)
	if got["run_trivyfs"] != "true" {
		t.Fatalf("run_trivyfs = %q for a binary-only diff, want \"true\" (all outputs: %v)", got["run_trivyfs"], got)
	}
}

// repoGit runs git in the repository under test, in the hermetic environment.
func repoGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot(t)
	cmd.Env = hermeticGitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// trackedFiles lists the repository's tracked paths matching pathspecs.
func trackedFiles(t *testing.T, pathspecs ...string) []string {
	t.Helper()
	var files []string
	for _, f := range strings.Split(repoGit(t, append([]string{"ls-files", "-z", "--"}, pathspecs...)...), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// TestCodeQLJavaScriptGateFiresForEveryTrackedNpmManifest runs each npm
// manifest and lockfile the repository tracks, at any depth, through
// codeql.yml's detect step alone: every one shapes what the JavaScript
// analysis extracts.
func TestCodeQLJavaScriptGateFiresForEveryTrackedNpmManifest(t *testing.T) {
	manifests := trackedFiles(t, "*package.json", "*package-lock.json", "*npm-shrinkwrap.json")
	if !contains(manifests, "package.json") {
		t.Fatalf("the root package.json is not among the tracked manifests %v — the listing is broken", manifests)
	}
	for _, m := range manifests {
		got := detectRun{workflow: codeqlWorkflow, event: "pull_request", files: []string{m}, queueBase: queueBaseReal}.run(t)
		if got["run_js"] != "true" {
			t.Errorf("a diff touching only %s leaves run_js = %q", m, got["run_js"])
		}
	}
}

// ---------------------------------------------------------------------------
// trivy-image's gate covers every file the image is built from, as the
// Dockerfile and .dockerignore define that set.
// ---------------------------------------------------------------------------

var dockerCopyInstruction = regexp.MustCompile(`(?i)^(COPY|ADD)\s+(.*)$`)

// DockerContextSources returns every build-context path a COPY or ADD in
// dockerfile reads. An instruction carrying --from reads another stage, not
// the context, and is skipped. Forms this parser does not model — the JSON
// array form, heredocs, wildcards, URLs — are refused rather than guessed at.
func DockerContextSources(dockerfile string) ([]string, error) {
	joined := strings.ReplaceAll(strings.ReplaceAll(dockerfile, "\r\n", "\n"), "\\\n", " ")
	var sources []string
	for _, raw := range strings.Split(joined, "\n") {
		m := dockerCopyInstruction.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			continue
		}
		if strings.Contains(m[2], "<<") {
			return nil, fmt.Errorf("%q: heredoc COPY/ADD is not modelled", raw)
		}
		var args []string
		fromStage := false
		for _, field := range strings.Fields(m[2]) {
			if strings.HasPrefix(field, "--") {
				fromStage = fromStage || strings.HasPrefix(field, "--from=")
				continue
			}
			args = append(args, field)
		}
		if fromStage {
			continue
		}
		if len(args) > 0 && strings.HasPrefix(args[0], "[") {
			return nil, fmt.Errorf("%q: the JSON form of COPY/ADD is not modelled", raw)
		}
		if len(args) < 2 {
			return nil, fmt.Errorf("%q: no source and destination", raw)
		}
		for _, src := range args[:len(args)-1] {
			if strings.ContainsAny(src, "*?[") || strings.Contains(src, "://") {
				return nil, fmt.Errorf("%q: source %q is a wildcard or URL, which is not modelled", raw, src)
			}
			sources = append(sources, path.Clean(strings.TrimPrefix(src, "/")))
		}
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no COPY or ADD reads the build context")
	}
	return sources, nil
}

// dockerignoreRule is one .dockerignore line: a pattern matched against a
// context path or any of its parent directories, excluding it — or, for a
// `!` line, including it again. The last matching line wins.
type dockerignoreRule struct {
	pattern *regexp.Regexp
	exclude bool
}

// DockerignoreRules parses .dockerignore with Docker's matching rules: a
// pattern is anchored at the context root, `*` and `?` stay within one path
// segment, `**` spans any number of them. Character classes and escapes are
// refused rather than guessed at.
func DockerignoreRules(content string) ([]dockerignoreRule, error) {
	var rules []dockerignoreRule
	for _, raw := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := dockerignoreRule{exclude: true}
		if strings.HasPrefix(line, "!") {
			rule.exclude = false
			line = strings.TrimSpace(line[1:])
		}
		line = path.Clean(strings.TrimPrefix(line, "/"))
		var re strings.Builder
		re.WriteString("^")
		for i := 0; i < len(line); i++ {
			switch c := line[i]; {
			case c == '*' && i+1 < len(line) && line[i+1] == '*':
				i++
				if i+1 < len(line) && line[i+1] == '/' {
					i++
					re.WriteString("(.*/)?")
				} else {
					re.WriteString(".*")
				}
			case c == '*':
				re.WriteString("[^/]*")
			case c == '?':
				re.WriteString("[^/]")
			case c == '[' || c == '\\':
				return nil, fmt.Errorf(".dockerignore line %q: character classes and escapes are not modelled", raw)
			default:
				re.WriteString(regexp.QuoteMeta(line[i : i+1]))
			}
		}
		re.WriteString("$")
		compiled, err := regexp.Compile(re.String())
		if err != nil {
			return nil, fmt.Errorf(".dockerignore line %q: %w", raw, err)
		}
		rule.pattern = compiled
		rules = append(rules, rule)
	}
	return rules, nil
}

// DockerignoreExcludes reports whether rules keep file out of the context.
func DockerignoreExcludes(rules []dockerignoreRule, file string) bool {
	excluded := false
	for _, rule := range rules {
		for p := file; ; p = path.Dir(p) {
			if rule.pattern.MatchString(p) {
				excluded = rule.exclude
				break
			}
			if !strings.Contains(p, "/") {
				break
			}
		}
	}
	return excluded
}

// ImageAffectingFiles is every tracked file a COPY source covers and
// .dockerignore leaves in the context.
func ImageAffectingFiles(tracked, sources []string, rules []dockerignoreRule) []string {
	var files []string
	for _, f := range tracked {
		covered := false
		for _, src := range sources {
			if src == "." || f == src || strings.HasPrefix(f, src+"/") {
				covered = true
				break
			}
		}
		if covered && !DockerignoreExcludes(rules, f) {
			files = append(files, f)
		}
	}
	return files
}

func TestDockerignoreRulesMatchAsDockerDoes(t *testing.T) {
	rules, err := DockerignoreRules("# comment\n*.pem\n**/*_test.go\nweb/src/\nsecrets/\n!secrets/keep.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]bool{
		"a.pem":                  true,
		"certs/a.pem":            false, // `*` stays in the root segment
		"a_test.go":              true,  // `**/` spans zero segments too
		"internal/x/a_test.go":   true,
		"web/src/js/a.js":        true, // a directory pattern excludes its contents
		"web/srcx/a.js":          false,
		"web/embed.go":           false,
		"secrets/key":            true,
		"secrets/keep.txt":       false, // the later `!` line wins
		"internal/x/a_test.go.x": false,
	} {
		if got := DockerignoreExcludes(rules, file); got != want {
			t.Errorf("%s: excluded = %v, want %v", file, got, want)
		}
	}
	if _, err := DockerignoreRules("web/[a-z]*\n"); err == nil {
		t.Error("a character class was accepted instead of refused")
	}
}

func TestDockerContextSourcesSkipsStageCopiesAndRefusesUnmodelledForms(t *testing.T) {
	got, err := DockerContextSources("FROM x AS b\nCOPY go.mod go.sum ./\ncopy --chown=1:1 web \\\n  ./web\nCOPY --from=b /out/app /app\n# COPY docs ./docs\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"go.mod", "go.sum", "web"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sources = %v, want %v", got, want)
	}
	for _, refused := range []string{`COPY ["a", "b"]`, "COPY web/*.go ./", "ADD https://example.invalid/x /x", "COPY <<EOF /x"} {
		if _, err := DockerContextSources(refused + "\n"); err == nil {
			t.Errorf("%q was accepted instead of refused", refused)
		}
	}
}

// imageAffectingFiles derives the set from the repository's own Dockerfile,
// .dockerignore and git index.
func imageAffectingFiles(t *testing.T) []string {
	t.Helper()
	sources, err := DockerContextSources(workflowfile.Read(t, "Dockerfile"))
	if err != nil {
		t.Fatalf("Dockerfile: %v", err)
	}
	rules, err := DockerignoreRules(workflowfile.Read(t, ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	return ImageAffectingFiles(trackedFiles(t), sources, rules)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var trivyImageGateLine = regexp.MustCompile(`(?m)^([ \t]*)trivyimage_changes=.*$`)

// TestTrivyImageGateFiresForEveryFileTheImageIsBuiltFrom runs security.yml's
// list and detect steps once over a diff holding every image-affecting file,
// with the detect step instrumented to record what its trivy-image gate
// selected. The gate filters line by line, so a file it selects here is one
// that fires it alone, and one it drops never would.
func TestTrivyImageGateFiresForEveryFileTheImageIsBuiltFrom(t *testing.T) {
	files := imageAffectingFiles(t)

	if !contains(files, "web/embed.go") {
		t.Fatalf("web/embed.go — compiled into the image from web/ — is not in the set derived from the Dockerfile and .dockerignore (%d files)", len(files))
	}
	webSrc := trackedFiles(t, "web/src/")
	if len(webSrc) == 0 {
		t.Fatal("no tracked file under web/src/ — the exclusion check below would judge nothing")
	}
	if contains(files, webSrc[0]) {
		t.Fatalf("%s is in the derived set, but .dockerignore keeps web/src/ out of the build context", webSrc[0])
	}

	script := detectStep(t, securityWorkflow).script
	loc := trivyImageGateLine.FindStringSubmatchIndex(script)
	if loc == nil {
		t.Fatal("no `trivyimage_changes=` assignment in security.yml's detect step — it was renamed or reshaped, and this guard would judge nothing")
	}
	probe := filepath.ToSlash(filepath.Join(t.TempDir(), "gate"))
	indent := script[loc[2]:loc[3]]
	instrumented := script[:loc[1]] + "\n" + indent + `printf '%s\n' "$trivyimage_changes" > '` + probe + `'` + script[loc[1]:]

	detectRun{workflow: securityWorkflow, detect: instrumented, event: "pull_request", files: files, queueBase: queueBaseReal}.run(t)

	recorded, err := os.ReadFile(filepath.FromSlash(probe))
	if err != nil {
		t.Fatalf("the instrumented gate recorded nothing: %v", err)
	}
	selected := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(string(recorded), "\r\n", "\n"), "\n") {
		selected[line] = true
	}
	var missed []string
	for _, f := range files {
		if !selected[f] {
			missed = append(missed, f)
		}
	}
	sort.Strings(missed)
	if len(missed) > 0 {
		t.Fatalf("trivy-image's gate does not fire for %d file(s) the Dockerfile copies into the image: %v", len(missed), missed)
	}
}
