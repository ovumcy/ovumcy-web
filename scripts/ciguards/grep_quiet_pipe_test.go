package ciguards

// WEB-486: no pipefail `run:` step pipes into a quiet grep.
//
// `grep -q` exits on its first match and closes the pipe; a writer still
// writing then dies of SIGPIPE, and pipefail reports the pipeline as failed —
// a check that matched fails the step. The image smoke's cache-header check
// failed this way. The accepted shape captures the output first and matches
// it with no pipe: `x="$(cmd)"; grep -q pat <<<"$x"`.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

var (
	stepNameKey  = regexp.MustCompile(`(?m)^ {8}name:[ \t]*(.*)$`)
	stepShellKey = regexp.MustCompile(`(?m)^ {8}shell:[ \t]*(\S+)`)
	stepRunKey   = regexp.MustCompile(`(?m)^ {8}run:`)
	setPipefail  = regexp.MustCompile(`(?m)^\s*set\b[^#\n]*\bpipefail\b`)
	// A single `|` (not `||`) followed by grep and the option words before its
	// pattern.
	pipeIntoGrep = regexp.MustCompile(`(?:^|[^|])\|[ \t]*grep((?:[ \t]+-\S+)*)`)
)

// stepRunsUnderPipefail reports whether a workflow step's script runs with
// pipefail on: `shell: bash` compiles to `bash -eo pipefail`, and any shell
// that runs `set -o pipefail` itself. A step with no `shell:` runs under the
// Linux runner's `bash -e` (TestNoWorkflowMovesTheDefaultShell holds that).
func stepRunsUnderPipefail(step, body string) bool {
	if m := stepShellKey.FindStringSubmatch(step); m != nil && m[1] == "bash" {
		return true
	}
	return setPipefail.MatchString(body)
}

// stepRunBody is the step's `run:` script: the lines below `run:` indented
// deeper than it, comments dropped, continuations (`\` at the end, or a line
// opening or closing on a pipe) joined into one logical line.
func stepRunBody(step string) (string, bool) {
	loc := stepRunKey.FindStringIndex(step)
	if loc == nil {
		return "", false
	}
	lines := strings.Split(step[loc[0]:], "\n")
	var logical []string
	joining := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "run:"))
			if trimmed == "|" || trimmed == ">" || strings.HasPrefix(trimmed, "|-") || strings.HasPrefix(trimmed, ">-") {
				continue
			}
		} else if indent := len(line) - len(strings.TrimLeft(line, " ")); trimmed != "" && indent <= 8 {
			break
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		opensOnPipe := strings.HasPrefix(trimmed, "|") && !strings.HasPrefix(trimmed, "||")
		if (joining || opensOnPipe) && len(logical) > 0 {
			logical[len(logical)-1] += " " + trimmed
		} else {
			logical = append(logical, trimmed)
		}
		last := logical[len(logical)-1]
		joining = strings.HasSuffix(last, `\`) ||
			(strings.HasSuffix(last, "|") && !strings.HasSuffix(last, "||"))
		logical[len(logical)-1] = strings.TrimSuffix(last, `\`)
	}
	return strings.Join(logical, "\n"), true
}

// quietGrepOption is a grep option word that makes it exit on the first match.
func quietGrepOption(word string) bool {
	switch word {
	case "--quiet", "--silent":
		return true
	}
	return !strings.HasPrefix(word, "--") && strings.HasPrefix(word, "-") && strings.ContainsRune(word, 'q')
}

// PipeIntoQuietGrep refuses a pipefail step whose script pipes into a quiet
// grep, naming the step and the shape that replaces it. step is a step as
// workflowfile.Steps returns it.
func PipeIntoQuietGrep(step string) error {
	body, ok := stepRunBody(step)
	if !ok || !stepRunsUnderPipefail(step, body) {
		return nil
	}
	name := "(unnamed)"
	if m := stepNameKey.FindStringSubmatch(step); m != nil {
		name = strings.TrimSpace(m[1])
	}
	var offending []string
	for line := range strings.SplitSeq(body, "\n") {
		for _, m := range pipeIntoGrep.FindAllStringSubmatch(line, -1) {
			if slices.ContainsFunc(strings.Fields(m[1]), quietGrepOption) {
				offending = append(offending, line)
			}
		}
	}
	if len(offending) > 0 {
		return fmt.Errorf("step %q runs under pipefail and pipes into a quiet grep, which closes the pipe on its first match so a writer still writing fails a check that passed (WEB-486); capture, then match with no pipe — x=\"$(cmd)\"; grep -q pat <<<\"$x\": %q", name, offending)
	}
	return nil
}

func TestNoPipefailStepPipesIntoAQuietGrep(t *testing.T) {
	steps := 0
	for _, wf := range allWorkflowFiles(t) {
		for _, header := range workflowfile.JobHeaders(t, wf, workflowfile.Read(t, wf)) {
			job := strings.TrimSuffix(strings.TrimSpace(header), ":")
			for _, step := range workflowfile.Steps(workflowfile.Job(t, wf, job)) {
				steps++
				if err := PipeIntoQuietGrep(step); err != nil {
					t.Errorf("%s, job %q: %v", wf, job, err)
				}
			}
		}
	}
	if steps == 0 {
		t.Fatal("found zero steps across every workflow — the sweep itself is broken")
	}
}

type quietGrepCase struct {
	name   string
	step   []string
	refuse bool
}

var quietGrepCases = []quietGrepCase{
	{"pipe into grep -q under shell: bash", []string{
		"        name: Smoke",
		"        shell: bash",
		"        run: |",
		`          curl -fsS http://x/healthz | grep -q '"status":"ok"'`,
	}, true},
	// The WEB-486 shape: a continuation line opening on the pipe.
	{"continued pipe into grep -iq", []string{
		"        name: Smoke",
		"        shell: bash",
		"        run: |",
		`          curl -fsS -D - -o /dev/null "http://x$asset" \`,
		`            | tr -d '\r' | grep -iq '^cache-control: public'`,
	}, true},
	{"set -euo pipefail with no shell:", []string{
		"        name: Smoke",
		"        run: |",
		"          set -euo pipefail",
		`          if printf '%s\n' "${tags[@]}" | grep -qx latest; then echo y; fi`,
	}, true},
	{"pipe into grep --quiet", []string{
		"        name: Smoke",
		"        shell: bash",
		`        run: printf '%s' "$x" | grep -E --quiet '^a'`,
	}, true},
	{"capture then herestring", []string{
		"        name: Smoke",
		"        shell: bash",
		"        run: |",
		`          headers="$(curl -fsS -D - -o /dev/null "http://x$asset" | tr -d '\r')"`,
		`          grep -iq '^cache-control: public' <<<"$headers"`,
	}, false},
	{"pipe into a grep that reads to the end", []string{
		"        name: Smoke",
		"        shell: bash",
		"        run: |",
		`          asset="$(printf '%s' "$login" | grep -oE '/static/[^"]+')"`,
	}, false},
	{"or-list, not a pipe", []string{
		"        name: Smoke",
		"        shell: bash",
		"        run: |",
		"          test -f x || grep -q y z",
	}, false},
	// `bash -e` alone reports only the last command's status.
	{"no pipefail", []string{
		"        name: Smoke",
		"        run: |",
		`          curl -fsS http://x/healthz | grep -q ok`,
	}, false},
	{"comment naming the shape", []string{
		"        name: Smoke",
		"        shell: bash",
		"        run: |",
		"          # never `cmd | grep -q pat` here",
		"          true",
	}, false},
}

func TestPipeIntoQuietGrepDecidesEachShape(t *testing.T) {
	for _, c := range quietGrepCases {
		t.Run(c.name, func(t *testing.T) {
			err := PipeIntoQuietGrep(strings.Join(c.step, "\n") + "\n")
			if c.refuse && err == nil {
				t.Fatal("accepted a pipe into a quiet grep under pipefail")
			}
			if !c.refuse && err != nil {
				t.Fatalf("refused an accepted shape: %v", err)
			}
			if c.refuse && (!strings.Contains(err.Error(), `step "Smoke"`) || !strings.Contains(err.Error(), `<<<`)) {
				t.Fatalf("refusal names neither the step nor the capture-then-match shape: %v", err)
			}
		})
	}
}
