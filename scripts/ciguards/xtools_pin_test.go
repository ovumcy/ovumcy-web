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
// starts reading the key later is swept in by
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
	value = strings.TrimSpace(yamlTrailingComment.ReplaceAllString(value, ""))
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	if !modulePin.MatchString(value) {
		return "", fmt.Errorf("line %d: %s is %q, not a literal module version", i+1, key, value)
	}
	return value, nil
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

// WEB-299: ci.yml and security.yml pin ANALYSIS_XTOOLS_VERSION separately.
// Every workflow that names the key must define it exactly once, as a literal,
// and every definition must agree; ci.yml and security.yml are required by
// name, so a sweep that stopped finding them fails rather than comparing
// nothing.
func TestTheAnalysisXToolsPinIsOneVersionAcrossWorkflows(t *testing.T) {
	pins := map[string]string{}
	for _, workflow := range allWorkflowFiles(t) {
		content := workflowfile.Read(t, workflow)
		if !readsKey(content, xtoolsPinKey) {
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
