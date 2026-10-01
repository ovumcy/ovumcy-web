package workflowfile

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The readers below take a job as Job returned it and read the job's OWN
// `if:` and `needs:` keys. They are line-based and fail closed: every shape
// they do not recognise is an error, never an answer about some other part of
// the job — a block scalar, a value continued onto a deeper line, a key written
// twice, a quoted or commented value. A guard that read a respelled key as
// "absent" would judge nothing and report green over the defect it exists for,
// so each caller fails its test on an error.
//
// A block-scalar `if:` (`|` or `>`) is refused by JobIf and JobCondition. The
// one caller whose workflow folds its condition across lines opts into
// JobFoldedCondition by name, so the exception is visible at the call site
// instead of being a second reader.

// ErrNoSuchKey is wrapped by the error a reader returns when the job does not
// declare the key at all. A caller for which an absent key is an answer (a job
// with no `needs:`) tests for it with errors.Is; every other error is a
// refusal to read.
var ErrNoSuchKey = errors.New("no such key")

var (
	// jobKeyLine is a job's own key: four spaces, then the key. Step keys sit
	// at eight and the lists under `steps:`, `needs:` and the like at six, so
	// nothing nested beneath a job can match it.
	jobKeyLine = regexp.MustCompile(`^    ([A-Za-z0-9_-]+):(.*)$`)
	jobID      = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	needsEntry = regexp.MustCompile(`^      - ([A-Za-z0-9_.-]+)$`)
	// blockScalarHeader is the indicator that opens a literal or folded block
	// scalar, with its optional chomping mark.
	blockScalarHeader = regexp.MustCompile(`^[>|][-+]?$`)
	whitespaceRun     = regexp.MustCompile(`[ \t\n\r]+`)
)

// yamlIndicators are the characters that cannot open a plain scalar.
const yamlIndicators = "!&*[]{},|>'\"%@`"

// JobIf returns a job's own `if:` value as written, trimmed, `${{ }}` wrapper
// included. A block scalar, a value continued onto a deeper line, an empty
// value, a key written twice and an absent key are errors.
func JobIf(block string) (string, error) {
	value, nested, err := jobKey(block, "if")
	if err != nil {
		return "", err
	}
	expression := strings.TrimSpace(value)
	if blockScalarHeader.MatchString(expression) {
		return "", fmt.Errorf("`if: %s` is a block scalar, which this reader refuses", expression)
	}
	if len(nested) > 0 {
		return "", fmt.Errorf("`if:` continues onto %q", nested)
	}
	if expression == "" {
		return "", errors.New("`if:` is empty")
	}
	return expression, nil
}

// JobCondition returns a job's own `if:` expression with the `${{ }}` wrapper
// removed and the whitespace around it trimmed; the spacing inside is kept, so
// a caller matching on `a == 'b'` still can. Beyond JobIf's refusals it refuses
// a wrapper with only one half, an unwrapped value opening on a YAML indicator
// (YAML reads `!cancelled()` as a tag and `*x` as an alias, so neither is the
// expression written), and a value carrying a comment or a double quote.
func JobCondition(block string) (string, error) {
	value, err := JobIf(block)
	if err != nil {
		return "", err
	}
	return conditionOf(value)
}

// JobFoldedCondition is JobCondition for a job whose `if:` may also be a
// block scalar (`>-`, `>`, `|`): the lines beneath the marker are joined with
// whitespace collapsed to single spaces. Everything else is refused as
// JobCondition refuses it.
func JobFoldedCondition(block string) (string, error) {
	value, nested, err := jobKey(block, "if")
	if err != nil {
		return "", err
	}
	if !blockScalarHeader.MatchString(strings.TrimSpace(value)) {
		return JobCondition(block)
	}
	if len(nested) == 0 {
		return "", fmt.Errorf("`if: %s` has no lines beneath it", strings.TrimSpace(value))
	}
	return conditionOf(whitespaceRun.ReplaceAllString(strings.TrimSpace(strings.Join(nested, " ")), " "))
}

func conditionOf(value string) (string, error) {
	expression := strings.TrimSpace(value)
	if strings.HasPrefix(expression, "${{") != strings.HasSuffix(expression, "}}") {
		return "", fmt.Errorf("`if: %s` opens or closes a `${{ }}` wrapper without the other half", expression)
	}
	if strings.HasPrefix(expression, "${{") {
		expression = expression[len("${{") : len(expression)-len("}}")]
	} else if expression != "" && strings.ContainsRune(yamlIndicators, rune(expression[0])) {
		return "", fmt.Errorf("`if: %s` starts with a YAML indicator and needs the `${{ }}` wrapper", expression)
	}
	if bare := WithoutSpaces(expression); bare == "" || strings.ContainsAny(bare, "#\"") {
		return "", fmt.Errorf("`if: %s` is not an expression this reader follows", strings.TrimSpace(value))
	}
	return strings.TrimSpace(expression), nil
}

// JobNeeds returns a job's `needs:` in any of the three forms a workflow
// writes it: a scalar, a flow list on one line, or a block list one level
// deeper than the key. An absent key wraps ErrNoSuchKey; an empty list, an
// entry that is not a job id, a quoted or commented entry, a list at the key's
// own depth and a key written twice are refused.
func JobNeeds(block string) ([]string, error) {
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
		return "", nil, fmt.Errorf("`%s:`: %w", key, ErrNoSuchKey)
	}
	if value != "" && !strings.HasPrefix(value, " ") {
		return "", nil, fmt.Errorf("`%s:%s` is not a key and its value", key, value)
	}
	return value, nested, nil
}

// WithoutSpaces drops every whitespace character outside a single-quoted
// string literal; an expression's doubled-quote escape closes and reopens a
// literal, so it needs no case of its own. A caller comparing an expression to
// a pinned one normalises both through it.
func WithoutSpaces(expression string) string {
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
