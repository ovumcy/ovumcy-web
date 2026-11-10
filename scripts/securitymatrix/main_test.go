// Package securitymatrix holds the Test Enforcement Matrix in SECURITY.md to
// the one property that makes it a matrix and not prose: every row that names
// a test names one that exists, in the file the row cites it with.
//
// Nothing else read the mapping. Three rows once named the wrong file and
// stayed that way until a manual walk, and a renamed test would have left its
// row pointing at nothing while the claim read as enforced.
//
// A row fails when a cited file is missing, when a cited `TestXxx` is not a
// top-level function of the Go file it is cited with, or when it cites no test
// at all and does not carry the label the document uses for claims no test can
// enforce. Function names are found with go/parser, so a name that survives
// only in a comment does not count.
//
// Cell grammar the checker reads: markdown link targets and backticked repo
// paths are file cites; a bare `name_test.go` resolves against the directory
// the cell names as `dir/`; a backticked `TestXxx` binds to the first Go file
// cite that follows it in the cell, and `TestXxx*` is satisfied by any declared
// name with that prefix.
package securitymatrix

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	matrixHeading = "## Test Enforcement Matrix"
	tableHeader   = "| Claim | Enforced by |"
)

var (
	citeToken   = regexp.MustCompile("\\[[^\\]]*\\]\\(([^)\\s]+)\\)|`([^`]+)`")
	testName    = regexp.MustCompile(`^(Test[A-Za-z0-9_]*)(\*?)$`)
	pathLike    = regexp.MustCompile(`^[A-Za-z0-9_.\-]+(/[A-Za-z0-9_.\-]+)*$`)
	fileExtList = regexp.MustCompile(`\.(go|md|ya?ml|ts|js|mjs|json|sh|toml|sql|html|css|txt)$`)
	exemptLabel = regexp.MustCompile(`(?i)not test-enforceable`)
)

type matrixRow struct {
	section string
	claim   string
	cell    string
}

type cite struct {
	kind  string // "file", "dir" or "test"
	value string
	pos   int
	end   int
}

// listJoiner is what may sit between two file cites for them to read as one
// list ("[a](a) / [b](b)", "`a`, `b`") rather than as separate claims.
var listJoiner = regexp.MustCompile(`^\s*(,|/|and|or)?\s*$`)

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

// splitCells splits a table row on unescaped pipes outside backticks.
func splitCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	var cells []string
	var cur strings.Builder
	inCode := false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\' && i+1 < len(runes) && runes[i+1] == '|':
			cur.WriteRune('|')
			i++
		case r == '`':
			inCode = !inCode
			cur.WriteRune(r)
		case r == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		cells = append(cells, rest)
	}
	return cells
}

// matrixRows returns the data rows of every claim table in the matrix
// section, the subsections that held one, and the structural failures.
func matrixRows(md string) (rows []matrixRow, sections []string, failures []string) {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == matrixHeading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, nil, []string{"no `" + matrixHeading + "` heading"}
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	section := ""
	seen := map[string]bool{}
	for i := start; i < end; i++ {
		l := lines[i]
		if strings.HasPrefix(l, "### ") {
			section = strings.TrimSpace(strings.TrimPrefix(l, "### "))
			continue
		}
		if strings.TrimSpace(l) != tableHeader {
			continue
		}
		if !seen[section] {
			seen[section] = true
			sections = append(sections, section)
		}
		i += 2 // header, separator
		for ; i < end && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
			cells := splitCells(lines[i])
			if len(cells) != 2 {
				failures = append(failures, section+": row does not split into 2 cells ("+strconv.Itoa(len(cells))+"): "+clip(lines[i]))
				continue
			}
			rows = append(rows, matrixRow{section: section, claim: cells[0], cell: cells[1]})
		}
		i--
	}
	return rows, sections, failures
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 90 {
		return s[:90] + "..."
	}
	return s
}

func parseCites(cell string) []cite {
	var out []cite
	for _, m := range citeToken.FindAllStringSubmatchIndex(cell, -1) {
		if m[2] >= 0 { // link target
			target := cell[m[2]:m[3]]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				continue
			}
			if i := strings.Index(target, "#"); i >= 0 {
				target = target[:i]
			}
			if target == "" {
				continue
			}
			out = append(out, cite{kind: "file", value: target, pos: m[0], end: m[1]})
			continue
		}
		code := cell[m[4]:m[5]]
		switch {
		case testName.MatchString(code):
			out = append(out, cite{kind: "test", value: code, pos: m[0], end: m[1]})
		case strings.HasSuffix(code, "/") && pathLike.MatchString(strings.TrimSuffix(code, "/")):
			out = append(out, cite{kind: "dir", value: code, pos: m[0], end: m[1]})
		case pathLike.MatchString(code) && fileExtList.MatchString(code):
			out = append(out, cite{kind: "file", value: code, pos: m[0], end: m[1]})
		}
	}
	return out
}

func isGoFile(p string) bool { return strings.HasSuffix(p, ".go") }

func isTestFile(p string) bool {
	return strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, ".spec.ts") || strings.HasSuffix(p, ".test.mjs")
}

func declaredFuncs(path string) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
			names[fn.Name.Name] = true
		}
	}
	return names, nil
}

// checkRow returns the failures of one row against the tree under root.
func checkRow(root string, row matrixRow) []string {
	var failures []string
	fail := func(msg string) {
		failures = append(failures, row.section+": "+msg+" — row: "+clip(row.claim))
	}
	cites := parseCites(row.cell)

	dir := ""
	for _, c := range cites {
		if c.kind == "dir" {
			dir = c.value
		}
	}
	type fileCite struct {
		rel string
		pos int
		end int
	}
	var files []fileCite
	hasTest := false
	for _, c := range cites {
		switch c.kind {
		case "dir":
			if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(c.value))); err != nil || !st.IsDir() {
				fail("cited directory does not exist: " + c.value)
			}
		case "file":
			rel := c.value
			if !strings.Contains(rel, "/") && dir != "" {
				rel = dir + rel
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				fail("cited file does not exist: " + rel)
				continue
			}
			files = append(files, fileCite{rel: rel, pos: c.pos, end: c.end})
			if isTestFile(rel) {
				hasTest = true
			}
		case "test":
			hasTest = true
		}
	}

	parsed := map[string]map[string]bool{}
	for _, f := range files {
		if !isGoFile(f.rel) {
			continue
		}
		if _, done := parsed[f.rel]; done {
			continue
		}
		names, err := declaredFuncs(filepath.Join(root, filepath.FromSlash(f.rel)))
		if err != nil {
			fail("cited Go file does not parse: " + f.rel)
			continue
		}
		parsed[f.rel] = names
	}

	for _, c := range cites {
		if c.kind != "test" {
			continue
		}
		// The name binds to the first Go file cited after it, plus the files
		// chained to that one as a single list.
		var bound []string
		last := -1
		for _, f := range files {
			if !isGoFile(f.rel) {
				continue
			}
			if len(bound) == 0 {
				if f.pos > c.pos {
					bound = append(bound, f.rel)
					last = f.end
				}
				continue
			}
			if f.pos >= last && listJoiner.MatchString(row.cell[last:f.pos]) {
				bound = append(bound, f.rel)
				last = f.end
				continue
			}
			break
		}
		if len(bound) == 0 {
			fail(c.value + " is cited with no Go file after it")
			continue
		}
		m := testName.FindStringSubmatch(c.value)
		found, parsedAny := false, false
		for _, b := range bound {
			names, ok := parsed[b]
			if !ok {
				continue // already reported as unparsable
			}
			parsedAny = true
			for n := range names {
				if n == m[1] || (m[2] == "*" && strings.HasPrefix(n, m[1])) {
					found = true
				}
			}
		}
		switch {
		case found || !parsedAny:
		case m[2] == "*":
			fail("no function with prefix " + c.value + " is declared in " + strings.Join(bound, " or "))
		default:
			fail(c.value + " is not declared as a top-level function in " + strings.Join(bound, " or "))
		}
	}

	if !hasTest && !exemptLabel.MatchString(row.cell) {
		fail("row cites no test and is not labelled not test-enforceable")
	}
	return failures
}

func checkMatrix(root, md string) (failures []string, rows []matrixRow, sections []string) {
	rows, sections, failures = matrixRows(md)
	for _, r := range rows {
		failures = append(failures, checkRow(root, r)...)
	}
	sort.Strings(failures)
	return failures, rows, sections
}

func TestSecurityMatrixRowsCiteTestsThatExist(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "SECURITY.md"))
	if err != nil {
		t.Fatalf("read SECURITY.md: %v", err)
	}
	failures, rows, sections := checkMatrix(root, string(raw))
	for _, f := range failures {
		t.Error(f)
	}

	// Load-bearing sites, by name: a parser that matched no table, or only
	// part of the section, would pass with no failures.
	if len(sections) == 0 || sections[0] != "Release Authenticity" {
		t.Errorf("first visited subsection = %v, want Release Authenticity first", sections)
	}
	if len(sections) == 0 || sections[len(sections)-1] != "Medical Safety Disclaimer" {
		t.Errorf("last visited subsection = %v, want Medical Safety Disclaimer last", sections)
	}
	const anchorTest = "TestCSRFExemptionListIsExactlyOneRoute"
	const anchorFile = "cmd/ovumcy/csrf_exemption_guard_test.go"
	anchored := false
	for _, r := range rows {
		for _, c := range parseCites(r.cell) {
			if c.kind == "test" && c.value == anchorTest && strings.Contains(r.cell, "("+anchorFile+")") {
				anchored = len(checkRow(root, r)) == 0
			}
		}
	}
	if !anchored {
		t.Errorf("no row cites %s in %s and resolves", anchorTest, anchorFile)
	}
}

func writeFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("pkg/a_test.go", "package pkg\n\nimport \"testing\"\n\n// TestOnlyInComment is mentioned here.\nfunc TestReal(t *testing.T) {}\n\nfunc TestPrefixOne(t *testing.T) {}\n\ntype S struct{}\n\nfunc (S) TestMethod(t *testing.T) {}\n")
	write("pkg/b_test.go", "package pkg\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n")
	write("docs/x.md", "x\n")
	return root
}

func fixtureDoc(rows ...string) string {
	return "## Test Enforcement Matrix\n\n### Sub\n\n" + tableHeader + "\n| --- | --- |\n" + strings.Join(rows, "\n") + "\n\n## Next\n"
}

func TestSecurityMatrixCheckerFiresOnEachFailureClass(t *testing.T) {
	root := writeFixtureTree(t)
	cases := []struct {
		name string
		row  string
		want string // substring of the single expected failure, "" for none
	}{
		{"missing file", "| c | `TestReal` in [pkg/gone_test.go](pkg/gone_test.go) |", "cited file does not exist: pkg/gone_test.go"},
		{"missing anchor-stripped file", "| c | [docs](docs/nope.md#frag) |", "cited file does not exist: docs/nope.md"},
		{"name absent from file", "| c | `TestMissing` in [pkg/a_test.go](pkg/a_test.go) |", "TestMissing is not declared as a top-level function in pkg/a_test.go"},
		{"name only in a comment", "| c | `TestOnlyInComment` in [pkg/a_test.go](pkg/a_test.go) |", "TestOnlyInComment is not declared"},
		{"name is a method", "| c | `TestMethod` in [pkg/a_test.go](pkg/a_test.go) |", "TestMethod is not declared"},
		{"name in the other file of the cell", "| c | `TestOther` in [pkg/a_test.go](pkg/a_test.go); `TestReal` in [pkg/b_test.go](pkg/b_test.go) |", "TestOther is not declared as a top-level function in pkg/a_test.go"},
		{"wildcard with no match", "| c | `TestNope*` in [pkg/a_test.go](pkg/a_test.go) |", "no function with prefix TestNope*"},
		{"name with no file after it", "| c | [pkg/a_test.go](pkg/a_test.go) holds `TestReal` |", "cited with no Go file after it"},
		{"no test and no label", "| c | [docs/x.md](docs/x.md) |", "cites no test"},
		{"two cells only", "| a | b | c |", "does not split into 2 cells (3)"},
		{"one cell only", "| a |", "does not split into 2 cells (1)"},
		{"bare names resolve against the named directory", "| c | `a_test.go`, `gone_test.go` in `pkg/` |", "cited file does not exist: pkg/gone_test.go"},
		{"directory missing", "| c | `a_test.go` in `nodir/` |", "cited directory does not exist: nodir/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failures, _, _ := checkMatrix(root, fixtureDoc(tc.row))
			joined := strings.Join(failures, "\n")
			if len(failures) == 0 || !strings.Contains(joined, tc.want) {
				t.Fatalf("failures = %q, want one containing %q", failures, tc.want)
			}
		})
	}
}

func TestSecurityMatrixCheckerAcceptsEachDocumentedShape(t *testing.T) {
	root := writeFixtureTree(t)
	rows := []string{
		"| c | `TestReal`, `TestPrefixOne` in [pkg/a_test.go](pkg/a_test.go) |",
		"| c | `TestPrefix*` in [pkg/a_test.go](pkg/a_test.go); `TestOther` in [pkg/b_test.go](pkg/b_test.go) |",
		"| c | `a_test.go`, `b_test.go` in `pkg/` |",
		"| c | [pkg/b_test.go](pkg/b_test.go) |",
		"| c | `TestOther` in [pkg/a_test.go](pkg/a_test.go) / [pkg/b_test.go](pkg/b_test.go) |",
		"| c | Not test-enforceable: a reviewed policy, see [docs/x.md](docs/x.md#top) |",
		"| c | Policy-level — not test-enforceable |",
		"| c | `a \\| b` in [pkg/a_test.go](pkg/a_test.go) and [site](https://example.invalid/x_test.go) |",
	}
	failures, got, sections := checkMatrix(root, fixtureDoc(rows...))
	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %q", failures)
	}
	if len(got) != len(rows) || len(sections) != 1 || sections[0] != "Sub" {
		t.Fatalf("parsed rows=%d sections=%v, want every row under Sub", len(got), sections)
	}
}

func TestSecurityMatrixSectionEndsAtTheNextTopLevelHeading(t *testing.T) {
	root := writeFixtureTree(t)
	doc := fixtureDoc("| c | `TestReal` in [pkg/a_test.go](pkg/a_test.go) |") +
		"\n" + tableHeader + "\n| --- | --- |\n| c | `TestMissing` in [pkg/a_test.go](pkg/a_test.go) |\n"
	failures, rows, _ := checkMatrix(root, doc)
	if len(failures) != 0 || len(rows) != 1 {
		t.Fatalf("a table past the next ## heading was read: rows=%d failures=%q", len(rows), failures)
	}
}
