// Package openapinullable guards docs/openapi.yaml against the OpenAPI 3.1
// `nullable` keyword coming back.
//
// OpenAPI 3.1 adopted JSON Schema 2020-12, which has no `nullable` keyword at
// all — a schema author wanting a nullable string writes `type: [string,
// "null"]` instead. Nine `nullable: true` occurrences in this spec were
// converted to that form (finding API-6), but nothing stopped the keyword
// from being reintroduced by a later edit: no test and no CI gate read the
// spec for it. This closes that gap the same way the conversion was verified
// by hand, permanently: scan every line of the committed spec and refuse the
// keyword wherever it appears, naming the file and line so the refusal is
// actionable.
//
// It is deliberately dependency-free, matching
// TestOpenAPIContractMatchesRegisteredRoutes (internal/api): the spec's text
// is scanned line-by-line rather than pulling in a YAML library.
package openapinullable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// nullableKeyword matches a YAML `nullable` key wherever it starts — at the
// top of a line, after a `- ` list marker, or inside flow-mapping syntax like
// `{type: string, nullable: true}` — but not the word "nullable" appearing in
// ordinary prose (a description sentence has no colon immediately after it).
// The leading \b keeps a name merely ENDING in "nullable" (there are none
// today, but the keyword is short enough that one is plausible) from
// matching.
var nullableKeyword = regexp.MustCompile(`\bnullable\s*:`)

// findNullableOccurrences scans content line by line and returns one
// "file:line: text" report per line naming the nullable keyword. content is
// scanned as-is rather than parsed as YAML, so it reports a keyword hiding
// inside a block scalar (a `description: |` body) too — OpenAPI 3.1 forbids
// the keyword everywhere in the document, not only where a schema author
// meant it as a schema property.
func findNullableOccurrences(path string, content []byte) []string {
	var violations []string
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		if nullableKeyword.MatchString(line) {
			violations = append(violations, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
		}
	}
	return violations
}

// TestOpenAPISpecForbidsTheNullableKeyword is the CI gate: the real spec must
// contain zero occurrences of the `nullable` keyword. OpenAPI 3.1 (this
// spec's declared version) has no such keyword; a schema meaning "this field
// may be null" is expressed with `type: [X, "null"]` instead.
func TestOpenAPISpecForbidsTheNullableKeyword(t *testing.T) {
	root := repoRoot(t)
	relPath := filepath.Join("docs", "openapi.yaml")
	content, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	if len(content) == 0 {
		t.Fatalf("%s is empty: the guard has nothing to check", relPath)
	}

	if violations := findNullableOccurrences(filepath.ToSlash(relPath), content); len(violations) > 0 {
		t.Errorf("docs/openapi.yaml uses the OpenAPI 3.1 `nullable` keyword, which JSON Schema 2020-12 dropped; replace each with `type: [X, \"null\"]`:\n  %s", strings.Join(violations, "\n  "))
	}
}

// TestNullableKeywordCheckCatchesEveryYAMLShape proves the matcher used
// above, over fixtures this repository does not contain — not a
// reimplementation of its rule, the function itself. It must refuse the
// keyword in block style, inside a flow mapping, and after a list marker, and
// it must NOT refuse the word "nullable" appearing in prose with no colon
// after it, or the already-converted 3.1 form.
func TestNullableKeywordCheckCatchesEveryYAMLShape(t *testing.T) {
	violating := []struct {
		name string
		line string
	}{
		{"block style", "        nullable: true"},
		{"block style, false", "        nullable: false"},
		{"list item", "      - nullable: true"},
		{"flow mapping", `        schema: {type: string, nullable: true}`},
		{"no space before colon", "        nullable:true"},
	}
	for _, tc := range violating {
		t.Run(tc.name, func(t *testing.T) {
			got := findNullableOccurrences("docs/openapi.yaml", []byte(tc.line))
			if len(got) != 1 {
				t.Fatalf("line %q: expected exactly one violation, got %d: %v", tc.line, len(got), got)
			}
		})
	}

	clean := []struct {
		name string
		line string
	}{
		{"converted 3.1 form", `        type: [string, "null"]`},
		{"prose mentioning the word, no colon", "        description: This field is nullable for legacy accounts."},
		{"unrelated key with the word as a suffix", "        notnullable: true"},
	}
	for _, tc := range clean {
		t.Run(tc.name, func(t *testing.T) {
			if got := findNullableOccurrences("docs/openapi.yaml", []byte(tc.line)); len(got) != 0 {
				t.Fatalf("line %q: expected no violations, got %v", tc.line, got)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
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
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
