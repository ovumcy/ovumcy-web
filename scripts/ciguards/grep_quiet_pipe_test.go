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
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

var (
	stepNameKey  = regexp.MustCompile(`(?m)^ {8}name:[ \t]*(.*)$`)
	stepShellKey = regexp.MustCompile(`(?m)^ {8}shell:[ \t]*(.*)$`)
	// `set -o pipefail`, `set -euo pipefail`, `set -e -o pipefail` — never
	// `set +o pipefail`, which turns it off.
	setPipefail = regexp.MustCompile(`(?m)^[ \t]*set\b[^#\n]*[ \t]-[A-Za-z]*o[ \t]+pipefail\b`)
	// A block scalar's header: `|`, `>-`, `|+`, `|2`, optionally commented.
	blockScalarHeader = regexp.MustCompile(`^[|>][-+0-9]*([ \t]+#.*)?$`)
	shellAssignment   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// heredocDelimiter is the delimiter of a heredoc the line opens (`<<EOF`,
// `<<-'EOF'`), or "". It reads the operator off the tokens, so a `<<` inside
// quotes or `$((…))` is not one; a bare `((…))` arithmetic command opens none.
func heredocDelimiter(line string) string {
	tokens := shellTokens(line)
	for k, tok := range tokens {
		if tok.op && tok.text == "((" {
			return ""
		}
		if tok.op && strings.HasPrefix(tok.text, "<<") && !strings.HasPrefix(tok.text, "<<<") &&
			k+1 < len(tokens) && !tokens[k+1].op {
			return unquote(tokens[k+1].text)
		}
	}
	return ""
}

// stepRunsUnderPipefail reports whether a step's script runs with pipefail on:
// `shell: bash` compiles to `bash -eo pipefail`, a custom template that names
// pipefail itself, or a script that runs `set -o pipefail`. A workflow step
// with no `shell:` runs under the Linux runner's `bash -e`
// (TestNoWorkflowMovesTheDefaultShell holds that).
func stepRunsUnderPipefail(step, body string) bool {
	if m := stepShellKey.FindStringSubmatch(step); m != nil {
		value, _, _ := strings.Cut(m[1], " #")
		value = strings.TrimSpace(value)
		if value == "bash" || strings.Contains(value, "pipefail") {
			return true
		}
	}
	return setPipefail.MatchString(body)
}

// stepScript is a step's `run:` script as logical lines: comment lines and
// heredoc payloads dropped, continuations (`\` at the end, or a line opening
// or closing on a pipe) joined.
func stepScript(step string) (string, bool) {
	cut, ok := runBodyIn(step)
	if !ok {
		return "", false
	}
	var logical []string
	joining := false
	heredoc := ""
	for i, line := range strings.Split(cut, "\n") {
		trimmed := strings.TrimSpace(line)
		if i == 0 {
			trimmed = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "run:"))
			if blockScalarHeader.MatchString(trimmed) {
				continue
			}
		}
		if heredoc != "" {
			if trimmed == heredoc {
				heredoc = ""
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		heredoc = heredocDelimiter(trimmed)
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

// shellToken is one word or operator of a shell line. Words keep their
// quotes; `$(…)` stays inside the word it sits in.
type shellToken struct {
	text string
	op   bool
}

// shellTokens splits one logical line into words and operators, honouring
// quotes and stopping at an unquoted comment.
func shellTokens(line string) []shellToken {
	var tokens []shellToken
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			tokens = append(tokens, shellToken{text: word.String()})
			word.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == ' ' || c == '\t':
			flush()
		case c == '#' && word.Len() == 0:
			flush()
			return tokens
		case c == '\\' && i+1 < len(line):
			word.WriteString(line[i : i+2])
			i++
		case c == '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				word.WriteString(line[i:])
				i = len(line)
				continue
			}
			word.WriteString(line[i : i+end+2])
			i += end + 1
		case c == '"':
			j := i + 1
			for j < len(line) && line[j] != '"' {
				switch {
				case line[j] == '\\':
					j++
				case strings.HasPrefix(line[j:], "$("):
					j += commandSubLen(line[j:]) - 1
				}
				j++
			}
			j = min(j, len(line)-1)
			word.WriteString(line[i : j+1])
			i = j
		case strings.HasPrefix(line[i:], "$("):
			n := commandSubLen(line[i:])
			word.WriteString(line[i : i+n])
			i += n - 1
		case strings.ContainsRune("|&;()<>", rune(c)):
			flush()
			j := i + 1
			if c == '<' || c == '>' {
				for j < len(line) && strings.ContainsRune("<>&|-", rune(line[j])) {
					j++
				}
			} else if j < len(line) && (line[j] == c || (c == '|' && line[j] == '&') || (c == '&' && line[j] == '>')) {
				j++
			}
			tokens = append(tokens, shellToken{text: line[i:j], op: true})
			i = j - 1
		default:
			word.WriteByte(c)
		}
	}
	flush()
	return tokens
}

// commandSubLen is the length of the `$(…)` that s opens on, through its
// matching paren (or to the end of s when unbalanced).
func commandSubLen(s string) int {
	depth := 0
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			if end := strings.IndexByte(s[i+1:], '\''); end >= 0 {
				i += end + 1
			}
		case '"':
			// A double-quoted string inside the substitution: its parens are
			// text, and a `$(…)` in it is skipped whole.
			for i++; i < len(s) && s[i] != '"'; i++ {
				switch {
				case s[i] == '\\':
					i++
				case strings.HasPrefix(s[i:], "$("):
					i += commandSubLen(s[i:]) - 1
				}
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

// commandSubs returns the inner text of every `$(…)` and backtick
// substitution in line outside single quotes, so a pipe inside one is judged
// like one outside it.
func commandSubs(line string) []string {
	var inner []string
	inDouble := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\':
			i++
		case c == '#' && !inDouble && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return inner
		case c == '"':
			inDouble = !inDouble
		case c == '\'' && !inDouble:
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				return inner
			}
			i += end + 1
		case c == '`':
			end := strings.IndexByte(line[i+1:], '`')
			if end < 0 {
				return inner
			}
			inner = append(inner, line[i+1:i+1+end])
			i += end + 1
		case strings.HasPrefix(line[i:], "$("):
			n := commandSubLen(line[i:])
			inner = append(inner, strings.TrimSuffix(line[i+2:i+n], ")"))
			i += n - 1
		}
	}
	return inner
}

// pipeWrappers run the next word as the command: the grep behind one still
// reads the pipe. Option words after a wrapper are skipped with it.
var pipeWrappers = []string{"command", "env", "!", "{", "sudo", "nice", "exec", "time", "stdbuf", "nohup"}

// commandEnd is an operator that ends a simple command.
func commandEnd(op string) bool {
	switch op {
	case "|", "||", "|&", "&&", "&", ";", ";;", "(", ")":
		return true
	}
	return false
}

func unquote(word string) string {
	return strings.NewReplacer(`'`, "", `"`, "").Replace(word)
}

// grepArgTakers are grep's short options whose argument is the rest of the
// cluster or the next word.
const grepArgTakers = "efmABCdD"

var grepLongArgTakers = []string{
	"--regexp", "--file", "--max-count", "--after-context", "--before-context",
	"--context", "--directories", "--devices", "--include", "--exclude",
	"--exclude-dir", "--exclude-from", "--label", "--binary-files",
}

// grepIsQuiet reports whether grep's argument words make it exit on its first
// match. GNU grep permutes, so an option after the pattern counts; `--` ends
// the options.
func grepIsQuiet(args []shellToken) bool {
	for i := 0; i < len(args); i++ {
		if args[i].op {
			// A redirection: its target word is not an argument.
			i++
			continue
		}
		word := unquote(args[i].text)
		switch {
		case word == "--":
			return false
		// GNU getopt takes any unambiguous prefix of a long option.
		case len(word) > len("--") && !strings.Contains(word, "=") &&
			(strings.HasPrefix("--quiet", word) || strings.HasPrefix("--silent", word)):
			return true
		case strings.HasPrefix(word, "--"):
			if !strings.Contains(word, "=") && slices.Contains(grepLongArgTakers, word) {
				i++
			}
		case strings.HasPrefix(word, "-") && len(word) > 1:
			for j := 1; j < len(word); j++ {
				if word[j] == 'q' {
					return true
				}
				if strings.IndexByte(grepArgTakers, word[j]) >= 0 {
					if j == len(word)-1 {
						i++
					}
					break
				}
			}
		}
	}
	return false
}

// pipesIntoQuietGrep reports whether one logical line, or any command
// substitution in it, pipes into a quiet grep.
func pipesIntoQuietGrep(line string) bool {
	if slices.ContainsFunc(commandSubs(line), pipesIntoQuietGrep) {
		return true
	}
	tokens := shellTokens(line)
	for i := range tokens {
		if !tokens[i].op || (tokens[i].text != "|" && tokens[i].text != "|&") {
			continue
		}
		j := i + 1
	scan:
		for j < len(tokens) {
			if tokens[j].op {
				if tokens[j].text != "(" {
					break scan
				}
				j++
				continue
			}
			word := unquote(tokens[j].text)
			switch {
			case shellAssignment.MatchString(word):
				j++
			case slices.Contains(pipeWrappers, word):
				for j++; j < len(tokens) && !tokens[j].op && strings.HasPrefix(tokens[j].text, "-"); j++ {
				}
			case word == "timeout":
				for j++; j < len(tokens) && !tokens[j].op && strings.HasPrefix(tokens[j].text, "-"); j++ {
					if opt := unquote(tokens[j].text); opt == "-k" || opt == "-s" || opt == "--kill-after" || opt == "--signal" {
						j++
					}
				}
				j++ // the duration
			default:
				break scan
			}
		}
		if j >= len(tokens) || tokens[j].op {
			continue
		}
		switch filepath.Base(unquote(tokens[j].text)) {
		case "grep", "egrep", "fgrep":
		default:
			continue
		}
		end := j + 1
		for end < len(tokens) && (!tokens[end].op || !commandEnd(tokens[end].text)) {
			end++
		}
		if grepIsQuiet(tokens[j+1 : end]) {
			return true
		}
	}
	return false
}

// PipeIntoQuietGrep refuses a pipefail step whose script pipes into a quiet
// grep, naming the step and the shape that replaces it. step is a workflow
// step as workflowfile.Steps returns it. judged is false for a step this rule
// has nothing to say about: no `run:`, or no pipefail.
func PipeIntoQuietGrep(step string) (judged bool, err error) {
	body, ok := stepScript(step)
	if !ok || !stepRunsUnderPipefail(step, body) {
		return false, nil
	}
	name := "(unnamed)"
	if m := stepNameKey.FindStringSubmatch(step); m != nil {
		name = strings.TrimSpace(m[1])
	}
	var offending []string
	for line := range strings.SplitSeq(body, "\n") {
		if pipesIntoQuietGrep(line) {
			offending = append(offending, line)
		}
	}
	if len(offending) > 0 {
		return true, fmt.Errorf("step %q runs under pipefail and pipes into a quiet grep, which closes the pipe on its first match so a writer still writing fails a check that passed (WEB-486); capture, then match with no pipe — x=\"$(cmd)\"; grep -q pat <<<\"$x\": %q", name, offending)
	}
	return true, nil
}

// compositeSteps returns a composite action's steps re-indented to a workflow
// step's depth (keys at eight spaces), so one reader judges both.
func compositeSteps(content string) []string {
	_, rest, ok := strings.Cut("\n"+content, "\n  steps:\n")
	if !ok {
		return nil
	}
	var steps []string
	for line := range strings.SplitSeq(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case trimmed != "" && !strings.HasPrefix(trimmed, "#") && indent < 4:
			return steps
		case strings.HasPrefix(line, "    - "):
			steps = append(steps, "        "+line[len("    - "):]+"\n")
		case len(steps) > 0:
			steps[len(steps)-1] += "  " + line + "\n"
		}
	}
	return steps
}

func compositeActionFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "actions", "*", "action.y*ml"))
	if err != nil {
		t.Fatalf("glob composite actions: %v", err)
	}
	var files []string
	for _, path := range paths {
		files = append(files, ".github/actions/"+filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path))
	}
	sort.Strings(files)
	return files
}

func TestNoPipefailStepPipesIntoAQuietGrep(t *testing.T) {
	judged := 0
	judge := func(where, step string) {
		ok, err := PipeIntoQuietGrep(step)
		if ok {
			judged++
		}
		if err != nil {
			t.Errorf("%s: %v", where, err)
		}
	}
	for _, wf := range allWorkflowFiles(t) {
		for _, header := range workflowfile.JobHeaders(t, wf, workflowfile.Read(t, wf)) {
			job := strings.TrimSuffix(strings.TrimSpace(header), ":")
			for _, step := range workflowfile.Steps(workflowfile.Job(t, wf, job)) {
				judge(fmt.Sprintf("%s, job %q", wf, job), step)
			}
		}
	}
	actions := compositeActionFiles(t)
	for _, action := range actions {
		steps := compositeSteps(workflowfile.Read(t, action))
		if len(steps) == 0 {
			t.Errorf("%s: no composite steps read — the action was reshaped, and this guard would judge nothing there", action)
		}
		for _, step := range steps {
			judge(action, step)
		}
	}
	// Dozens of `shell: bash` steps exist; zero means the reader broke.
	if judged == 0 {
		t.Fatal("judged zero pipefail steps across every workflow and composite action — the sweep itself is broken")
	}
}

type quietGrepCase struct {
	name   string
	step   []string
	refuse bool
}

func bashStep(lines ...string) []string {
	return append([]string{"        name: Smoke", "        shell: bash", "        run: |"}, lines...)
}

var quietGrepCases = []quietGrepCase{
	{"pipe into grep -q", bashStep(`          curl -fsS http://x/healthz | grep -q '"status":"ok"'`), true},
	// The WEB-486 shape: a continuation line opening on the pipe.
	{"continued pipe into grep -iq", bashStep(
		`          curl -fsS -D - -o /dev/null "http://x$asset" \`,
		`            | tr -d '\r' | grep -iq '^cache-control: public'`), true},
	{"quiet flag after the pattern", bashStep(`          curl -fsS http://x | grep '"ok"' -q`), true},
	{"env-prefixed grep", bashStep(`          git diff --name-only | LC_ALL=C grep -qE '^web/'`), true},
	{"egrep", bashStep(`          printf '%s' "$x" | egrep -q 'a|b'`), true},
	{"pipe with stderr", bashStep(`          make 2>&1 |& grep -q done`), true},
	{"grep --quiet", bashStep(`          printf '%s' "$x" | grep -E --quiet '^a'`), true},
	{"inside a command substitution", bashStep(`          if [ "$(curl -fsS x | grep -q ok && echo y)" = y ]; then :; fi`), true},
	{"after a quoted paren in a substitution", bashStep(`          x="$(printf ")"; curl y | grep -q z && echo y)"`), true},
	{"inside quoted backticks", bashStep("          out=\"`curl x | grep -q ok && echo y`\""), true},
	{"brace group", bashStep(`          curl x | { grep -q ok; }`), true},
	{"subshell", bashStep(`          curl x | ( grep -q ok )`), true},
	{"behind timeout", bashStep(`          curl x | timeout -k 1 5 grep -q ok`), true},
	{"behind sudo", bashStep(`          curl x | sudo grep -q ok`), true},
	{"abbreviated --quiet", bashStep(`          curl x | grep --qui ok`), true},
	{"after an arithmetic shift", bashStep(
		`          mask=$(( 1 << bits ))`,
		`          (( y = 1 << bits ))`,
		`          curl x | grep -q ok`), true},
	{"after a quoted <<", bashStep(
		`          echo "a <<EOF b"`,
		`          curl x | grep -q ok`), true},
	{"set -euo pipefail with no shell:", []string{
		"        name: Smoke",
		"        run: |",
		"          set -euo pipefail",
		`          if printf '%s\n' "${tags[@]}" | grep -qx latest; then echo y; fi`,
	}, true},
	{"single-line run", []string{
		"        name: Smoke",
		"        shell: bash",
		`        run: printf '%s' "$x" | grep -q a`,
	}, true},
	{"capture then herestring", bashStep(
		`          headers="$(curl -fsS -D - -o /dev/null "http://x$asset" | tr -d '\r')"`,
		`          grep -iq '^cache-control: public' <<<"$headers"`), false},
	{"grep that reads to the end", bashStep(`          asset="$(printf '%s' "$login" | grep -oE '/static/[^"]+')"`), false},
	{"-e argument holding a q", bashStep(`          printf '%s' "$x" | grep -oequeue`), false},
	{"-e then a pattern spelled -q", bashStep(`          printf '%s' "$x" | grep -o -e -q`), false},
	{"quoted pattern holding -q", bashStep(`          printf '%s' "$x" | grep -o 'a -q b'`), false},
	{"after --", bashStep(`          printf '%s' "$x" | grep -o -- -q`), false},
	{"or-list, not a pipe", bashStep("          test -f x || grep -q y z"), false},
	{"trailing comment", bashStep("          echo hi # never `cmd | grep -q pat`"), false},
	{"heredoc payload", bashStep(
		"          cat > check.sh <<'EOF'",
		"          curl -fsS x | grep -q ok",
		"          EOF",
		"          sh check.sh"), false},
	{"block scalar keep indicator", []string{
		"        name: Smoke",
		"        shell: bash",
		"        run: |+",
		`          grep -q ok <<<"$x"`,
	}, false},
	// `bash -e` alone reports only the last command's status.
	{"no pipefail", []string{
		"        name: Smoke",
		"        run: |",
		"          curl -fsS http://x/healthz | grep -q ok",
	}, false},
	{"set +o pipefail", []string{
		"        name: Smoke",
		"        run: |",
		"          set +o pipefail",
		"          curl -fsS http://x/healthz | grep -q ok",
	}, false},
	{"custom bash template without pipefail", []string{
		"        name: Smoke",
		"        shell: bash {0}",
		"        run: |",
		"          curl -fsS http://x/healthz | grep -q ok",
	}, false},
	{"comment line naming the shape", bashStep("          # never `cmd | grep -q pat` here", "          true"), false},
}

func TestPipeIntoQuietGrepDecidesEachShape(t *testing.T) {
	for _, c := range quietGrepCases {
		t.Run(c.name, func(t *testing.T) {
			_, err := PipeIntoQuietGrep(strings.Join(c.step, "\n") + "\n")
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

// TestRunBodyInReadsOnlyTheStepsOwnRunKey: an action input named `run` is not
// the step's script.
func TestRunBodyInReadsOnlyTheStepsOwnRunKey(t *testing.T) {
	step := strings.Join([]string{
		"        name: Uses",
		"        uses: some/action@deadbeef",
		"        with:",
		"          run: curl x | grep -q ok",
	}, "\n")
	if body, ok := runBodyIn(step); ok {
		t.Fatalf("read a `with:` input as the step's run: %q", body)
	}
	item := "      - run: |\n          echo hi\n        shell: bash\n"
	if body, ok := runBodyIn(item); !ok || body != "      - run: |\n          echo hi" {
		t.Fatalf("a step opening on `- run:` read as %q (ok=%v)", body, ok)
	}
}

func TestCompositeStepsReadsEveryStepAtAWorkflowStepsDepth(t *testing.T) {
	content := strings.Join([]string{
		"name: x",
		"runs:",
		"  using: composite",
		"  steps:",
		"    - name: One",
		"      shell: bash",
		"      run: |",
		"        curl x | grep -q ok",
		"    - name: Two",
		"      shell: sh",
		"      run: echo two",
		"outputs:",
		"  y: z",
	}, "\n")
	steps := compositeSteps(content)
	if len(steps) != 2 {
		t.Fatalf("read %d step(s), want 2: %q", len(steps), steps)
	}
	if _, err := PipeIntoQuietGrep(steps[0]); err == nil {
		t.Fatalf("the composite `shell: bash` step piping into grep -q was accepted: %q", steps[0])
	}
	if judged, _ := PipeIntoQuietGrep(steps[1]); judged {
		t.Fatalf("the `shell: sh` step was judged as pipefail: %q", steps[1])
	}
	if strings.Contains(steps[1], "outputs:") {
		t.Fatalf("the last step ran past the end of `steps:`: %q", steps[1])
	}
}
