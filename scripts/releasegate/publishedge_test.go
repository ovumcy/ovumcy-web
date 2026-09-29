package releasegate

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// publishJob is the job that builds, signs and promotes the image. Every
// script case in this package judges the gate; none of them would notice the
// gate stop being in this job's way.
const publishJob = "publish"

// The two runner conditions that put the gate on the release path, written as
// they are pinned: whitespace and the `${{ }}` wrapper are the only freedom a
// rewrite has. The gate must run on every tag, and `publish` may pass a
// `skipped` gate only where the gate is skipped by design, off the tag path.
const (
	gateCondition    = "github.ref_type == 'tag'"
	publishCondition = "!cancelled() && (needs.verify-release-tag.result == 'success' || (needs.verify-release-tag.result == 'skipped' && github.ref_type != 'tag'))"
)

var (
	// jobKeyLine is a job's own key: four spaces, then the key. Step keys sit
	// at eight and the lists under `steps:`, `needs:` and the like at six, so
	// nothing nested beneath a job can match it.
	jobKeyLine = regexp.MustCompile(`^    ([A-Za-z0-9_-]+):(.*)$`)
	jobID      = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

var errNoSuchKey = errors.New("no such key")

// yamlIndicators are the characters that cannot open a plain scalar.
const yamlIndicators = "!&*[]{},|>'\"%@`"

// TestPublishWaitsOnTheReleaseGate pins the edge itself. Without the need,
// `publish` starts alongside the gate rather than after it, and its `if:` reads
// the result of a job it does not wait on.
func TestPublishWaitsOnTheReleaseGate(t *testing.T) {
	block := workflowfile.Job(t, gateWorkflow, publishJob)
	needs, err := jobNeeds(block)
	if err != nil {
		t.Fatalf("%s, job %q: `needs:` cannot be read (%v), so nothing shows the job waits on %q:\n%s",
			gateWorkflow, publishJob, err, gateJob, block)
	}
	if !slices.Contains(needs, gateJob) {
		t.Fatalf("%s, job %q needs %q, not %q: a tag would be built, signed and promoted without waiting for the gate's verdict",
			gateWorkflow, publishJob, needs, gateJob)
	}
}

// TestPublishPassesATagOnlyOnTheGatesSuccess pins `publish`'s condition to the
// one form that lets a tag through on nothing but the gate's `success`.
// `always()` or a bare `!cancelled()` run it over a failed gate; dropping the
// ref-type conjunct runs it over a gate that skipped on a tag; and a form this
// reader cannot follow is refused as well, since it is a form nobody has
// checked.
func TestPublishPassesATagOnlyOnTheGatesSuccess(t *testing.T) {
	assertJobCondition(t, publishJob, publishCondition)
}

// TestTheReleaseGateRunsOnEveryTag pins the other half. A gate that skips on a
// tag is refused by `publish` — which is the only thing standing between that
// skip and an unchecked release — and a gate that also runs off the tag path
// asks for check runs the calling workflow is still producing.
func TestTheReleaseGateRunsOnEveryTag(t *testing.T) {
	assertJobCondition(t, gateJob, gateCondition)
}

// TestJobKeyReaderRefusesWhatItCannotRead pins the reader the two tests above
// stand on: every shape it does not recognise is an error, never an answer
// about some other part of the job.
func TestJobKeyReaderRefusesWhatItCannotRead(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		block     string
		wantNeeds []string
		wantIf    string
	}{
		{
			name:      "block list and wrapped condition",
			block:     "    needs:\n      # the gate\n      - verify-release-tag\n      - build\n    if: ${{ a == 'b c' }}\n    steps:\n      - name: x\n        if: always()\n",
			wantNeeds: []string{"verify-release-tag", "build"},
			wantIf:    "a=='b c'",
		},
		{
			name:      "flow list and bare condition",
			block:     "    needs: [ build, verify-release-tag ]\n    if: a  ==  'b'\n",
			wantNeeds: []string{"build", "verify-release-tag"},
			wantIf:    "a=='b'",
		},
		{
			name:      "scalar need",
			block:     "    needs: verify-release-tag\n    if: ${{a}}\n",
			wantNeeds: []string{"verify-release-tag"},
			wantIf:    "a",
		},
		{
			name:      "whitespace-only lines and a wrapped leading `!`",
			block:     "    needs:\n      - a\n      \n      - b\n        \n    if: ${{ !a }}\n          \n    steps:\n",
			wantNeeds: []string{"a", "b"},
			wantIf:    "!a",
		},
		{name: "unwrapped condition opening on a YAML indicator", block: "    needs: a\n    if: !cancelled() && a\n", wantNeeds: []string{"a"}},
		{name: "unwrapped condition opening on an alias", block: "    if: *a\n"},
		{name: "keys only inside a step", block: "    steps:\n      - name: x\n        needs: verify-release-tag\n        if: a\n"},
		{name: "duplicated keys", block: "    needs: a\n    needs: b\n    if: a\n    if: b\n"},
		{name: "plain scalar continued onto a deeper line", block: "    needs: a\n      b\n    if: a\n      || always()\n"},
		{name: "block scalars", block: "    needs: |\n      a\n    if: >-\n      a\n"},
		{name: "quoted values", block: "    needs: 'a'\n    if: \"a\"\n"},
		{name: "trailing comments", block: "    needs: a # b\n    if: a # b\n"},
		{name: "empty flow list and half a wrapper", block: "    needs: []\n    if: ${{ a\n"},
		{name: "flow list across lines and a wrapper never opened", block: "    needs: [a,\n      b]\n    if: a }}\n"},
		{name: "list item that is not a job id", block: "    needs:\n      - a\n      - { b: c }\n    if:\n"},
		{name: "list at the key's own depth", block: "    needs:\n    - a\n    if: ${{ }}\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			needs, err := jobNeeds(testCase.block)
			if testCase.wantNeeds == nil {
				if err == nil {
					t.Errorf("needs read as %q, want a refusal", needs)
				}
			} else if err != nil || !slices.Equal(needs, testCase.wantNeeds) {
				t.Errorf("needs = %q, %v; want %q", needs, err, testCase.wantNeeds)
			}

			condition, err := jobCondition(testCase.block)
			if testCase.wantIf == "" {
				if err == nil {
					t.Errorf("condition read as %q, want a refusal", condition)
				}
			} else if err != nil || condition != testCase.wantIf {
				t.Errorf("condition = %q, %v; want %q", condition, err, testCase.wantIf)
			}
		})
	}
}

func assertJobCondition(t *testing.T, job, want string) {
	t.Helper()

	block := workflowfile.Job(t, gateWorkflow, job)
	got, err := jobCondition(block)
	if err != nil {
		t.Fatalf("%s, job %q: the job-level `if:` cannot be read (%v); want exactly `${{ %s }}`:\n%s",
			gateWorkflow, job, err, want, block)
	}
	if got != withoutSpaces(want) {
		t.Fatalf("%s, job %q: the job-level `if:` reads %q; want exactly `${{ %s }}`",
			gateWorkflow, job, got, want)
	}
}

// jobCondition returns a job's own `if:` with the `${{ }}` wrapper and every
// space outside a string literal removed.
func jobCondition(block string) (string, error) {
	value, nested, err := jobKey(block, "if")
	if err != nil {
		return "", err
	}
	if len(nested) > 0 {
		return "", fmt.Errorf("`if:` continues onto %q", nested)
	}
	expression := strings.TrimSpace(value)
	if strings.HasPrefix(expression, "${{") != strings.HasSuffix(expression, "}}") {
		return "", fmt.Errorf("`if: %s` opens or closes a `${{ }}` wrapper without the other half", expression)
	}
	if strings.HasPrefix(expression, "${{") {
		expression = expression[len("${{") : len(expression)-len("}}")]
	} else if expression != "" && strings.ContainsRune(yamlIndicators, rune(expression[0])) {
		// Unwrapped, YAML reads the value before the runner does: `!cancelled()`
		// is a tag, `*x` an alias, `[a]` a sequence. None is the expression
		// written, so none may normalise to the pinned one.
		return "", fmt.Errorf("`if: %s` starts with a YAML indicator and needs the `${{ }}` wrapper", expression)
	}
	if expression = withoutSpaces(expression); expression == "" || strings.ContainsAny(expression, "#\"") {
		return "", fmt.Errorf("`if: %s` is not an expression this reader follows", strings.TrimSpace(value))
	}
	return expression, nil
}

// jobNeeds returns a job's `needs:` in any of the three forms a workflow
// writes it: a scalar, a flow list, or a block list one level deeper.
func jobNeeds(block string) ([]string, error) {
	value, nested, err := jobKey(block, "needs")
	if err != nil {
		return nil, err
	}
	value = strings.TrimSpace(value)

	var needs []string
	switch {
	case value == "":
		for _, line := range nested {
			match := needsEntry.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("`needs:` item %q is not a job id", line)
			}
			needs = append(needs, match[1])
		}
	case len(nested) > 0:
		return nil, fmt.Errorf("`needs: %s` continues onto %q", value, nested)
	case strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"):
		for _, item := range strings.Split(value[1:len(value)-1], ",") {
			needs = append(needs, strings.TrimSpace(item))
		}
	default:
		needs = []string{value}
	}

	if len(needs) == 0 {
		return nil, errors.New("`needs:` lists nothing")
	}
	for _, need := range needs {
		if !jobID.MatchString(need) {
			return nil, fmt.Errorf("`needs:` entry %q is not a job id", need)
		}
	}
	return needs, nil
}

// jobKey returns what follows `key:` on its line and the non-comment lines
// nested deeper beneath it. A key written twice is refused rather than read
// either way: which copy wins is a parser's choice, not this file's.
func jobKey(block, key string) (string, []string, error) {
	lines := strings.Split(block, "\n")
	value, nested, found := "", []string(nil), false
	for i := 0; i < len(lines); i++ {
		match := jobKeyLine.FindStringSubmatch(lines[i])
		if match == nil || match[1] != key {
			continue
		}
		if found {
			return "", nil, fmt.Errorf("`%s:` is written twice", key)
		}
		found, value = true, match[2]
		for ; i+1 < len(lines); i++ {
			next := lines[i+1]
			trimmed := strings.TrimSpace(next)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if !strings.HasPrefix(next, "     ") {
				break
			}
			nested = append(nested, next)
		}
	}
	if !found {
		return "", nil, fmt.Errorf("`%s:`: %w", key, errNoSuchKey)
	}
	if value != "" && !strings.HasPrefix(value, " ") {
		return "", nil, fmt.Errorf("`%s:%s` is not a key and its value", key, value)
	}
	return value, nested, nil
}

// withoutSpaces drops every whitespace character outside a single-quoted
// string literal; an expression's doubled-quote escape closes and reopens a literal, so
// it needs no case of its own.
func withoutSpaces(expression string) string {
	var out strings.Builder
	quoted := false
	for _, r := range expression {
		if r == '\'' {
			quoted = !quoted
		}
		if !quoted && (r == ' ' || r == '\t' || r == '\n') {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
