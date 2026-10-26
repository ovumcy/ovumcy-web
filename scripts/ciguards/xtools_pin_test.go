package ciguards

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// xtoolsPinKey is the env entry `.github/actions/go-install-on-xtools` builds
// against: staticcheck and golangci-lint in ci.yml, gosec in security.yml. Each
// workflow pins it in its own `env:` block, so a bump that lifts one copy
// leaves the other workflow's analyzers on an x/tools older than the
// toolchain's export data — and on the next Go release that workflow fails on
// every package while the other stays green.
const xtoolsPinKey = "ANALYSIS_XTOOLS_VERSION"

// xtoolsPinWorkflows are the workflows that must carry the pin whatever the
// sweep finds: the two whose analyzers are built on it today. A workflow that
// starts reading the key, or using the action built on it, later is swept in by
// TestTheAnalysisXToolsPinIsOneVersionAcrossWorkflows without an edit here.
var xtoolsPinWorkflows = []string{ciWorkflow, securityWorkflow}

var (
	// modulePin is the only value shape the pin may hold: a module version as
	// `go install …@<version>` and `go version -m` both spell it. An empty
	// value, an expression or a block scalar is not a pin and is refused
	// rather than compared.
	modulePin = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)
	// yamlTrailingComment is a `#` comment after a scalar: YAML starts one
	// only after whitespace.
	yamlTrailingComment = regexp.MustCompile(`\s+#.*$`)
)

// EnvPin returns the value of the one `env:` entry named key in a workflow's
// text. It fails closed on every shape that would let two workflows compare
// equal without pinning the same version: no entry, more than one (a job- or
// step-level entry shadows a workflow-level one for its own scope, so a second
// definition is a second pin), an entry outside an `env:` mapping, or a value
// that is not a literal module version.
func EnvPin(content, key string) (string, error) {
	lines := strings.Split(content, "\n")
	var found []int
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), key+":") {
			found = append(found, i)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no %s entry", key)
	case 1:
	default:
		at := make([]string, len(found))
		for n, i := range found {
			at[n] = fmt.Sprintf("line %d", i+1)
		}
		return "", fmt.Errorf("%s is defined %d times (%s) — one scope's entry shadows the other's, so there is no single pin to compare", key, len(found), strings.Join(at, ", "))
	}

	i := found[0]
	trimmed := strings.TrimLeft(lines[i], " ")
	indent := len(lines[i]) - len(trimmed)
	if parent := parentKey(lines, i, indent); parent != "env:" {
		return "", fmt.Errorf("line %d: %s sits under %q, not an `env:` mapping", i+1, key, parent)
	}

	value := strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
	value = scalar(value)
	if !modulePin.MatchString(value) {
		return "", fmt.Errorf("line %d: %s is %q, not a literal module version", i+1, key, value)
	}
	return value, nil
}

// scalar is a one-line YAML value without its trailing comment or quotes.
func scalar(value string) string {
	value = strings.TrimSpace(yamlTrailingComment.ReplaceAllString(value, ""))
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return value
}

// pinScope returns the job whose `env:` holds the one key entry EnvPin
// accepted, or "" when it sits in the workflow-level `env:`. A step-level
// entry covers one step only and is refused: a step reading the key anywhere
// else gets an empty value the pin sweep never sees.
func pinScope(content, key string) (string, error) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(trimmed, key+":") {
			continue
		}
		switch indent := len(line) - len(trimmed); indent {
		case 2:
			return "", nil
		case 6:
			return strings.TrimSuffix(parentKey(lines, i, 4), ":"), nil
		default:
			return "", fmt.Errorf("line %d: %s is indented %d, neither workflow- nor job-level, so it covers one step at most", i+1, key, indent)
		}
	}
	return "", fmt.Errorf("no %s entry", key)
}

// parentKey returns the nearest line above lines[i] indented less than indent
// — the mapping key the entry belongs to — trimmed, skipping blank and comment
// lines, or "" when the entry sits at the top of the document.
func parentKey(lines []string, i, indent int) string {
	for j := i - 1; j >= 0; j-- {
		trimmed := strings.TrimLeft(lines[j], " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(lines[j])-len(trimmed) < indent {
			return strings.TrimSpace(trimmed)
		}
	}
	return ""
}

// PinsDisagree refuses a set of per-workflow pins holding more than one value,
// naming every workflow and the value it pins.
func PinsDisagree(pins map[string]string) error {
	values := map[string]bool{}
	for _, v := range pins {
		values[v] = true
	}
	if len(values) <= 1 {
		return nil
	}
	files := make([]string, 0, len(pins))
	for f := range pins {
		files = append(files, f)
	}
	sort.Strings(files)
	each := make([]string, len(files))
	for n, f := range files {
		each[n] = path.Base(f) + "=" + pins[f]
	}
	return fmt.Errorf("%s differs between workflows (%s): the analyzers built in one workflow lag the other's x/tools", xtoolsPinKey, strings.Join(each, ", "))
}

// readsKey reports whether any uncommented line of a workflow names key — a
// definition, or a read through `env.` or `$`.
func readsKey(content, key string) bool {
	word := regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `\b`)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if word.MatchString(line) {
			return true
		}
	}
	return false
}

// xtoolsAction is the composite action that builds an analyzer against the
// pinned x/tools. What the pin protects is the `xtools-version` input of every
// step that uses it, so the guard keys on those steps, not on the env name.
const xtoolsAction = "go-install-on-xtools"

// xtoolsInputExpression is the only value a step may pass as `xtools-version`,
// compared without spaces: the workflow's own pin, read through `env`.
const xtoolsInputExpression = "${{env." + xtoolsPinKey + "}}"

var (
	// xtoolsActionUse is a step key `uses:` naming the action, in whatever
	// spelling of the path; a comment line mentioning it does not match.
	xtoolsActionUse = regexp.MustCompile(`(?m)^[ \t]*uses:.*` + regexp.QuoteMeta(xtoolsAction))
	// xtoolsVersionLine is the input at a `with:` entry's depth, as Steps
	// re-indents a step: keys at eight spaces, `with:` entries at ten.
	xtoolsVersionLine = regexp.MustCompile(`(?m)^ {10}xtools-version:(.*)$`)
	stepNameLine      = regexp.MustCompile(`(?m)^ {8}name:[ \t]*(.*?)[ \t]*$`)
)

// xtoolsVersionInput judges one step, as workflowfile.Steps returns it, that
// uses the action: it must pass `xtools-version` exactly once, as the
// expression reading the workflow's own pin. A literal, another env name or no
// input at all builds the analyzer on an x/tools the pin sweep never compares.
func xtoolsVersionInput(step string) error {
	inputs := xtoolsVersionLine.FindAllStringSubmatch(step, -1)
	switch len(inputs) {
	case 0:
		return fmt.Errorf("passes no xtools-version input, so the analyzer is built on an x/tools nothing pins")
	case 1:
	default:
		return fmt.Errorf("passes xtools-version %d times", len(inputs))
	}
	value := scalar(inputs[0][1])
	if workflowfile.WithoutSpaces(value) != xtoolsInputExpression {
		return fmt.Errorf("passes xtools-version %q; it must be ${{ env.%s }} so the pin the sweep compares is the one the analyzer is built on", value, xtoolsPinKey)
	}
	return nil
}

// xtoolsStep is one step using the action and the job it runs in.
type xtoolsStep struct{ job, text string }

// xtoolsSteps is every step of every workflow that uses the action, keyed by
// workflow and then by "job / step name" — the key the failure message and the
// anti-vacuity check both name — read from the source with no allowlist. Two
// steps sharing a key fail here: one would hide the other from the guard.
func xtoolsSteps(t *testing.T) map[string]map[string]xtoolsStep {
	t.Helper()

	found := map[string]map[string]xtoolsStep{}
	for _, workflow := range allWorkflowFiles(t) {
		content := workflowfile.Read(t, workflow)
		for _, header := range workflowfile.JobHeaders(t, workflow, content) {
			job := strings.TrimSuffix(strings.TrimSpace(header), ":")
			for _, step := range workflowfile.Steps(workflowfile.Job(t, workflow, job)) {
				if !xtoolsActionUse.MatchString(step) {
					continue
				}
				name := "(unnamed)"
				if m := stepNameLine.FindStringSubmatch(step); m != nil {
					name = scalar(m[1])
				}
				if found[workflow] == nil {
					found[workflow] = map[string]xtoolsStep{}
				}
				key := job + " / " + name
				if _, dup := found[workflow][key]; dup {
					t.Fatalf("%s: two steps using %s are both %q — name them apart, or the guard judges only one", workflow, xtoolsAction, key)
				}
				found[workflow][key] = xtoolsStep{job: job, text: step}
			}
		}
	}
	return found
}

// The steps that build staticcheck, golangci-lint and gosec on the pin today.
// Named, not counted: a sweep that stopped finding one of them fails here
// rather than judging fewer steps in green.
var knownXToolsSteps = map[string][]string{
	ciWorkflow:       {"test-go-analysis / Install staticcheck", "test-go-analysis / Install golangci-lint"},
	securityWorkflow: {"gosec / Install gosec"},
}

func TestEveryStepUsingTheXToolsActionPassesTheWorkflowsPin(t *testing.T) {
	found := xtoolsSteps(t)
	for workflow, names := range knownXToolsSteps {
		for _, name := range names {
			if _, ok := found[workflow][name]; !ok {
				t.Fatalf("%s: step %q was not found among the steps using %s — it was renamed or the sweep lost it, and this guard would judge nothing", workflow, name, xtoolsAction)
			}
		}
	}
	for workflow, steps := range found {
		scope, err := pinScope(workflowfile.Read(t, workflow), xtoolsPinKey)
		if err != nil {
			t.Errorf("%s: %v", workflow, err)
		}
		for name, step := range steps {
			if err := xtoolsVersionInput(step.text); err != nil {
				t.Errorf("%s: step %q %v", workflow, name, err)
			}
			if scope != "" && step.job != scope {
				t.Errorf("%s: step %q reads %s, but the pin is job %q's own, so here it is empty", workflow, name, xtoolsPinKey, scope)
			}
		}
	}
}

func TestXToolsVersionInputRefusesEveryInputThatIsNotTheWorkflowsPin(t *testing.T) {
	step := func(with string) string {
		return "        name: Install tool\n        uses: ./.github/actions/go-install-on-xtools\n        with:\n          package: example.com/cmd/tool\n          version: ${{ env.TOOL_VERSION }}\n" + with
	}
	accepted := map[string]string{
		"the pin":           step("          xtools-version: ${{ env.ANALYSIS_XTOOLS_VERSION }}\n"),
		"the pin, no space": step("          xtools-version: ${{env.ANALYSIS_XTOOLS_VERSION}} # x/tools\n"),
		"the pin, quoted":   step("          xtools-version: \"${{ env.ANALYSIS_XTOOLS_VERSION }}\"\n"),
	}
	for name, src := range accepted {
		if err := xtoolsVersionInput(src); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
	refused := map[string]string{
		"a literal version": step("          xtools-version: v0.50.0\n"),
		"a quoted literal":  step("          xtools-version: 'v0.50.0'\n"),
		"another env name":  step("          xtools-version: ${{ env.OTHER_XTOOLS_VERSION }}\n"),
		"a longer env name": step("          xtools-version: ${{ env.ANALYSIS_XTOOLS_VERSION_OLD }}\n"),
		"a vars context":    step("          xtools-version: ${{ vars.ANALYSIS_XTOOLS_VERSION }}\n"),
		"an empty value":    step("          xtools-version:\n"),
		"no input":          step(""),
		"the input twice":   step("          xtools-version: ${{ env.ANALYSIS_XTOOLS_VERSION }}\n          xtools-version: v0.50.0\n"),
	}
	for name, src := range refused {
		if err := xtoolsVersionInput(src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestXToolsActionUseIgnoresCommentsAndOtherActions(t *testing.T) {
	for _, src := range []string{
		"        uses: ./.github/actions/go-install-on-xtools\n",
		"        uses: \"./.github/actions/go-install-on-xtools\"\n",
		"        uses: ./.github/actions/go-install-on-xtools # pinned\n",
	} {
		if !xtoolsActionUse.MatchString(src) {
			t.Errorf("a use of the action was missed: %q", src)
		}
	}
	for _, src := range []string{
		"        # uses: ./.github/actions/go-install-on-xtools\n",
		"        uses: ./.github/actions/setup-go\n",
		"        run: echo go-install-on-xtools\n",
	} {
		if xtoolsActionUse.MatchString(src) {
			t.Errorf("not a use of the action: %q", src)
		}
	}
}

// WEB-299: ci.yml and security.yml pin ANALYSIS_XTOOLS_VERSION separately.
// Every workflow that names the key, or has a step using the action built on
// it, must define it exactly once, as a literal, and every definition must
// agree; ci.yml and security.yml are required by name, so a sweep that stopped
// finding them fails rather than comparing nothing.
func TestTheAnalysisXToolsPinIsOneVersionAcrossWorkflows(t *testing.T) {
	pins := map[string]string{}
	users := xtoolsSteps(t)
	for _, workflow := range allWorkflowFiles(t) {
		content := workflowfile.Read(t, workflow)
		if !readsKey(content, xtoolsPinKey) && len(users[workflow]) == 0 {
			continue
		}
		pin, err := EnvPin(content, xtoolsPinKey)
		if err != nil {
			t.Fatalf("%s: %v", workflow, err)
		}
		pins[workflow] = pin
	}
	for _, workflow := range xtoolsPinWorkflows {
		if _, ok := pins[workflow]; !ok {
			t.Fatalf("%s: no %s pin was read — it builds its analyzers through go-install-on-xtools, so the sweep must find and parse it", workflow, xtoolsPinKey)
		}
	}
	if err := PinsDisagree(pins); err != nil {
		t.Fatal(err)
	}
}

func TestEnvPinRefusesEveryShapeThatIsNotOnePin(t *testing.T) {
	accepted := []struct{ name, src, want string }{
		{"workflow-level", "env:\n  # x/tools v0.1.0\n  ANALYSIS_XTOOLS_VERSION: v0.51.0\n", "v0.51.0"},
		{"job-level", "jobs:\n  a:\n    env:\n      X: y\n      ANALYSIS_XTOOLS_VERSION: v0.51.0 # pinned\n", "v0.51.0"},
		{"quoted", "env:\n  ANALYSIS_XTOOLS_VERSION: \"v0.52.1-0.20260101000000-abcdef123456\"\n", "v0.52.1-0.20260101000000-abcdef123456"},
	}
	for _, c := range accepted {
		got, err := EnvPin(c.src, xtoolsPinKey)
		if err != nil || got != c.want {
			t.Errorf("%s: EnvPin = %q, %v; want %q", c.name, got, err, c.want)
		}
	}

	refused := []struct{ name, src string }{
		{"missing", "env:\n  GOSEC_VERSION: v2.29.0\n"},
		{"commented out", "env:\n  # ANALYSIS_XTOOLS_VERSION: v0.51.0\n"},
		{"shadowed by a job", "env:\n  ANALYSIS_XTOOLS_VERSION: v0.51.0\njobs:\n  a:\n    env:\n      ANALYSIS_XTOOLS_VERSION: v0.49.0\n"},
		{"not under env", "jobs:\n  a:\n    with:\n      ANALYSIS_XTOOLS_VERSION: v0.51.0\n"},
		{"empty", "env:\n  ANALYSIS_XTOOLS_VERSION:\n"},
		{"empty quoted", "env:\n  ANALYSIS_XTOOLS_VERSION: ''\n"},
		{"expression", "env:\n  ANALYSIS_XTOOLS_VERSION: ${{ vars.XTOOLS }}\n"},
		{"block scalar", "env:\n  ANALYSIS_XTOOLS_VERSION: >-\n    v0.51.0\n"},
	}
	for _, c := range refused {
		if got, err := EnvPin(c.src, xtoolsPinKey); err == nil {
			t.Errorf("%s: EnvPin accepted %q", c.name, got)
		}
	}
}

func TestPinScopeNamesTheJobOrRefusesAStepLevelPin(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"workflow-level", "env:\n  ANALYSIS_XTOOLS_VERSION: v0.51.0\njobs:\n  a:\n", ""},
		{"job-level", "jobs:\n  a:\n    runs-on: x\n    env:\n      X: y\n      ANALYSIS_XTOOLS_VERSION: v0.51.0\n", "a"},
	}
	for _, c := range cases {
		if got, err := pinScope(c.src, xtoolsPinKey); err != nil || got != c.want {
			t.Errorf("%s: pinScope = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	stepLevel := "jobs:\n  a:\n    steps:\n      - name: s\n        env:\n          ANALYSIS_XTOOLS_VERSION: v0.51.0\n"
	if got, err := pinScope(stepLevel, xtoolsPinKey); err == nil {
		t.Errorf("step-level: pinScope accepted %q", got)
	}
}

func TestPinsDisagreeNamesEveryWorkflowOnADifference(t *testing.T) {
	if err := PinsDisagree(map[string]string{ciWorkflow: "v0.51.0", securityWorkflow: "v0.51.0"}); err != nil {
		t.Fatalf("equal pins refused: %v", err)
	}
	err := PinsDisagree(map[string]string{ciWorkflow: "v0.52.0", securityWorkflow: "v0.51.0"})
	if err == nil {
		t.Fatal("differing pins accepted")
	}
	for _, want := range []string{"ci.yml=v0.52.0", "security.yml=v0.51.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

func TestReadsKeyIgnoresCommentsOnly(t *testing.T) {
	if readsKey("env:\n  # ANALYSIS_XTOOLS_VERSION is pinned elsewhere\n", xtoolsPinKey) {
		t.Error("a comment counted as a read")
	}
	if !readsKey("      with:\n        v: ${{ env.ANALYSIS_XTOOLS_VERSION }}\n", xtoolsPinKey) {
		t.Error("an env. read was missed")
	}
	if readsKey("env:\n  ANALYSIS_XTOOLS_VERSION_OLD: v1\n", xtoolsPinKey) {
		t.Error("a longer key counted as the pin")
	}
}
