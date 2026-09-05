// Package readmeversion guards README.md against release-tag drift: the
// current release tag is hardcoded in several places
// (intro blurb, Docker quick start image tag, cosign/attestation/SBOM
// verification examples, and the Releases section) with no single source of
// truth, so a release bump that updates one occurrence and misses another
// goes unnoticed. This test asserts every occurrence agrees.
//
// The same drift reaches the shipped example stacks: an `.env.example` under
// docs/examples/ that pins OVUMCY_IMAGE overrides its own compose file's
// default, and the runbook tells an operator to copy that template verbatim.
// So the release tag README.md asserts is also the tag every example env
// template must pin.
//
// It also reaches every compose file's own `${OVUMCY_IMAGE:-...}` fallback:
// an operator who never sets OVUMCY_IMAGE gets whatever tag is baked into
// that default, starting with the root docker-compose.yml the quick start
// tells a reader to run first.
package readmeversion

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var releaseTagPatterns = []*regexp.Regexp{
	regexp.MustCompile("latest tagged release is `v(\\d+\\.\\d+\\.\\d+)`"),
	regexp.MustCompile(`ghcr\.io/ovumcy/ovumcy-web:v(\d+\.\d+\.\d+)`),
	regexp.MustCompile("Latest tagged release: `v(\\d+\\.\\d+\\.\\d+)`"),
	// Keys on the sentence the mutable-tag caveat opens with, not on the tag's
	// digit shape: the digit shape is already covered by the ghcr.io pattern
	// above everywhere the tag appears next to an image reference, but this
	// sentence states the tag bare, in backticks, with no image reference
	// beside it, so that pattern never reaches it. Without this entry the tag
	// here can drift from the other eight occurrences and nothing notices.
	regexp.MustCompile("The `v(\\d+\\.\\d+\\.\\d+)` tag is mutable"),
}

// envImageAssignment matches any uncommented line in an example env template
// whose assignment target is OVUMCY_IMAGE, optionally exported, and captures
// everything to the right of the `=`. Keying on the KEY rather than on a
// well-formed value is the point: a pattern anchored to the value shape it
// expects SKIPS every other shape, and a skipped template is precisely the one
// that drifts unnoticed: a pattern running the value to end of line passes
// over `OVUMCY_IMAGE=...:v0.8.4 # keep in sync with README` and over an
// `export` prefix, and the walk below still reports success, because the
// remaining templates keep its pin count non-zero.
var envImageAssignment = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?OVUMCY_IMAGE[ \t]*=(.*)$`)

// ovumcyImageRef matches the published runtime image at an exact release tag.
var ovumcyImageRef = regexp.MustCompile(`^ghcr\.io/ovumcy/ovumcy-web:v(\d+\.\d+\.\d+)$`)

// composeFallbackImage matches the default inside a compose file's
// `${OVUMCY_IMAGE:-...}` interpolation. It keys on the variable name for the
// same reason envImageAssignment does: anchoring to the default's shape would
// skip a fallback written any other way, and the skipped file is precisely
// the one that drifts unnoticed — which is how the root docker-compose.yml
// went unchecked before this test existed.
var composeFallbackImage = regexp.MustCompile(`\$\{OVUMCY_IMAGE:-([^}]*)\}`)

// isComposeFile reports whether name has the docker-compose*.y*ml shape this
// repository's compose stacks use. The population TestComposeFallbackImagesMatchReleaseTag
// checks is DISCOVERED by walking the tree with this predicate rather than
// declared as a path list: a hardcoded list of the six compose files known
// today drifts from the tree the moment a new example stack is added, the
// same way the inventory this test package was missing had already drifted.
func isComposeFile(name string) bool {
	const prefix, suffix = "docker-compose", "ml"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	return strings.Contains(name[len(prefix):], ".y")
}

// isOutsideTrackedTree reports whether directory path belongs to a population
// this guard must not judge: a dot-directory (build, tooling or editor state),
// a node_modules vendor tree, or a directory that is itself the root of a
// different module or repository checkout nested under this one.
//
// Discovering the compose files by walking is what keeps the population from
// drifting, but a walk answers with everything on disk, and a developer
// machine carries whole extra checkouts of THIS repository underneath the
// root — each with its own docker-compose.yml pinned to whatever release it
// was cut at. Measured before this filter existed: the walk matched 67
// compose files where the repository has 6. They agree only while the tree
// has not been bumped, so the guard would go red on the developer's machine
// at the exact moment of a release bump, naming files from another checkout —
// a false refusal blocking the release, which is the failure this guard is
// meant to prevent rather than cause.
//
// The last case is judged by property, not by name: a directory carrying its
// own go.mod and/or its own git marker is a separate population, and a
// worktree's ".git" is a regular file (a "gitdir:" pointer), so what is
// checked is presence, not directory-ness.
func isOutsideTrackedTree(path string, entry os.DirEntry) bool {
	if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// composeReporter is the slice of *testing.T composeFallbacksMatchReleaseTag
// needs. A real *testing.T satisfies it unchanged; the fixtures below pass a
// recorder instead, so they can drive the REAL check over a deliberately
// inconsistent tree and read the verdict rather than be failed by it. Without
// that seam every test in this package only ever asserts that a consistent
// tree agrees with itself, and a checker narrowed until it reads nothing
// still passes them all.
type composeReporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// recordedReport collects what a check reported instead of failing the test.
// Fatalf records like Errorf and returns: on a real *testing.T it would not,
// but a fixture wants the whole verdict, and every Fatalf in the checker is
// its last act on that population anyway.
type recordedReport struct{ problems []string }

func (r *recordedReport) Helper() {}

func (r *recordedReport) Errorf(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func (r *recordedReport) Fatalf(format string, args ...any) { r.Errorf(format, args...) }

// writeFixtureFile creates relative (and any missing parents) under root.
func writeFixtureFile(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func composeFixture(tag string) string {
	return "services:\n  app:\n    image: ${OVUMCY_IMAGE:-ghcr.io/ovumcy/ovumcy-web:v" + tag + "}\n"
}

// TestComposeFallbackCheckRefusesAStaleFallback is this guard's negative
// fixture: the release bump the guard exists to police is exactly the moment
// one compose file gets left behind, and nothing else in this package ever
// shows the checker able to REFUSE. It drives the real
// composeFallbacksMatchReleaseTag over a synthetic tree, so narrowing the
// walk or inverting the comparison turns this test red instead of leaving
// every other test green about a consistent tree.
func TestComposeFallbackCheckRefusesAStaleFallback(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "docker-compose.yml", composeFixture("2.0.0"))
	writeFixtureFile(t, root, "docs/examples/postgres/docker-compose.yml", composeFixture("1.9.2"))

	var report recordedReport
	composeFallbacksMatchReleaseTag(&report, root, "2.0.0")

	if len(report.problems) != 1 {
		t.Fatalf("reported %d problems, want exactly 1 (the stale fallback):\n%s", len(report.problems), strings.Join(report.problems, "\n"))
	}
	if !strings.Contains(report.problems[0], "docs/examples/postgres/docker-compose.yml") {
		t.Errorf("the refusal does not name the stale file: %s", report.problems[0])
	}
	if !strings.Contains(report.problems[0], "1.9.2") {
		t.Errorf("the refusal does not name the tag it found: %s", report.problems[0])
	}
}

// TestComposeFallbackCheckIgnoresANestedCheckout pins the population rule: a
// developer machine carries whole extra checkouts of this repository under
// the root, each with its own compose files pinned to the release they were
// cut at. Judging those turns the guard red at the moment of a bump, naming
// files from another checkout — a false refusal that blocks the release this
// guard exists to protect.
func TestComposeFallbackCheckIgnoresANestedCheckout(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "docker-compose.yml", composeFixture("2.0.0"))
	writeFixtureFile(t, root, "nested-checkout/go.mod", "module nested\n\ngo 1.23\n")
	writeFixtureFile(t, root, "nested-checkout/docker-compose.yml", composeFixture("1.9.2"))
	writeFixtureFile(t, root, ".worktrees/copy/.git", "gitdir: ../../.git/worktrees/copy\n")
	writeFixtureFile(t, root, ".worktrees/copy/docker-compose.yml", composeFixture("0.8.4"))

	var report recordedReport
	composeFallbacksMatchReleaseTag(&report, root, "2.0.0")

	if len(report.problems) != 0 {
		t.Fatalf("judged compose files belonging to a nested checkout:\n%s", strings.Join(report.problems, "\n"))
	}
}

// TestReadmeReleaseTagPatternsCoverTheMutableTagSentence pins the site that
// was outside every pattern until this change: README's paragraph explaining
// that the release tag is mutable names the tag in prose, so a bump that
// missed it left the file disagreeing with itself unnoticed.
func TestReadmeReleaseTagPatternsCoverTheMutableTagSentence(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "README.md",
		"The `v7.7.7` tag is mutable and `pull_policy: always` re-pulls it on every restart.\n")

	got := readmeReleaseTags(t, root)
	if len(got) != 1 || got[0] != "7.7.7" {
		t.Fatalf("readmeReleaseTags = %v, want [7.7.7]: the mutable-tag sentence is a release site and must be in the population", got)
	}
}

// goDirective captures the version from go.mod's `go` line, which is the
// minimum toolchain that can build this module and the same value every
// workflow resolves through `go-version-file: go.mod`.
var goDirective = regexp.MustCompile(`(?m)^go[ \t]+(\d+\.\d+(?:\.\d+)?)[ \t]*$`)

// readmeRequirementsBlock captures the bullet list under the `Requirements:`
// line of the manual build instructions. The Go entry is found INSIDE this
// block rather than anywhere in the file, because "- Go " is not a
// collision-free key the way envImageAssignment's OVUMCY_IMAGE is: a bullet
// reading "- Go modules are vendored" is ordinary prose, and a scan over the
// whole README would read it as a version statement and go red on it.
var readmeRequirementsBlock = regexp.MustCompile(`(?ms)^Requirements:\n\n((?:[ \t]*-[^\n]*\n)+)`)

// readmeGoVersionSites are the places README.md tells a reader which Go a
// source build needs. Each keys on the LABEL and captures to a delimiter
// rather than matching a version shape, for the same reason envImageAssignment
// does: a pattern anchored to the shape it expects SKIPS a site written any
// other way, and a skipped site is precisely the one that drifts unnoticed.
// `within`, when set, narrows the search to submatch 1 of that pattern first —
// the label alone is then only as specific as it needs to be inside the block
// it belongs to.
var readmeGoVersionSites = []struct {
	what   string
	within *regexp.Regexp
	re     *regexp.Regexp
}{
	{"the Go version badge", nil, regexp.MustCompile(`img\.shields\.io/badge/Go-([^-]*)-`)},
	{"the Requirements list", readmeRequirementsBlock, regexp.MustCompile(`(?m)^[ \t]*-[ \t]+Go[ \t]+(\S+)`)},
}

// TestReadmeGoVersionMatchesGoMod asserts both README.md statements of the
// required Go version agree with go.mod's `go` directive.
//
// Nothing else catches this. Every workflow takes its toolchain from
// `go-version-file: go.mod`, so CI stays green no matter what the README
// claims, and the drift is only visible to someone building from source — who
// finds out by installing the wrong toolchain. It has already happened twice:
// the badge and the Requirements line sat at `Go 1.26+` against a stricter
// go.mod, were corrected by hand to `1.26.6+`, and then survived the 1.27.0 and
// 1.27.1 bumps unchanged. Two hand corrections of the same two lines is the
// argument for a guard rather than a third.
//
// It fails closed PER SITE: each one must yield at least one comparison, so a
// rewording that stops matching is reported by name rather than absorbed by
// the site that still matches. A single total across both sites would be the
// trap envImageAssignment's comment describes — the remaining match keeps the
// count non-zero and the guard reports success having checked half of what it
// names. A captured value that does not begin with a digit is reported rather
// than skipped, the same rule the image-pin walk applies, since a value
// nothing can parse is a value nobody is comparing.
func TestReadmeGoVersionMatchesGoMod(t *testing.T) {
	root := repoRoot(t)
	want := goModVersion(t, root)
	content := readmeContent(t, root)

	for _, site := range readmeGoVersionSites {
		scope := content
		if site.within != nil {
			block := site.within.FindSubmatch(content)
			if block == nil {
				t.Errorf("README.md no longer has the block %s lives in, so that site is no longer being checked at all", site.what)
				continue
			}
			scope = block[1]
		}

		// Counted PER SITE, not across all of them. A single total is the trap
		// envImageAssignment's comment describes: one site going dark stays
		// invisible while the other keeps the count non-zero, and the guard
		// reports success having checked half of what it names.
		compared := 0
		for _, m := range site.re.FindAllSubmatch(scope, -1) {
			raw := strings.TrimSpace(string(m[1]))
			got := strings.TrimSuffix(raw, "+")
			if got == "" || got[0] < '0' || got[0] > '9' {
				t.Errorf("README.md %s reads %q, which is not a Go version, so nothing can compare it against go.mod", site.what, raw)
				continue
			}
			compared++
			if got != want {
				t.Errorf(
					"README.md %s states Go %s while go.mod requires %s; a reader building from source installs a toolchain that cannot build this module, and no workflow catches it because every one of them resolves its toolchain with go-version-file: go.mod",
					site.what, got, want,
				)
			}
		}
		if compared == 0 {
			t.Errorf("README.md %s matched no Go version, so that site is no longer being compared against go.mod", site.what)
		}
	}
}

// TestReadmeGoVersionSitesKeyOnTheirLabel proves the patterns above find a site
// by its label rather than by the version shape it happens to carry today, on
// inline fixtures rather than the real README. A two-component version, a
// four-component one and a stale value all have to be FOUND — being found is
// what lets the comparison report them.
func TestReadmeGoVersionSitesKeyOnTheirLabel(t *testing.T) {
	badge := readmeGoVersionSites[0].re
	requirements := readmeGoVersionSites[1].re

	for _, tc := range []struct {
		name string
		re   *regexp.Regexp
		line string
		want string
	}{
		{"badge, patch version", badge, `<img src="https://img.shields.io/badge/Go-1.27.1+-00ADD8?logo=go" alt="Go Version">`, "1.27.1+"},
		{"badge, minor version", badge, `<img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go">`, "1.26+"},
		{"badge, stale value", badge, `<img src="https://img.shields.io/badge/Go-1.20.0+-00ADD8?logo=go">`, "1.20.0+"},
		{"badge, no plus", badge, `<img src="https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go">`, "1.27.1"},
		{"requirements, patch version", requirements, "- Go 1.27.1+", "1.27.1+"},
		{"requirements, indented", requirements, "  -   Go 1.27.1+", "1.27.1+"},
		{"requirements, no plus", requirements, "- Go 1.27.1", "1.27.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.re.FindStringSubmatch(tc.line)
			if m == nil {
				t.Fatalf("%q: site not found, so its version would never be compared", tc.line)
			}
			if got := strings.TrimSpace(m[1]); got != tc.want {
				t.Fatalf("%q: captured %q, want %q", tc.line, got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name string
		re   *regexp.Regexp
		line string
	}{
		{"a different badge", badge, `<img src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker">`},
		{"a requirements entry for something else", requirements, "- Node.js 22+"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if m := tc.re.FindStringSubmatch(tc.line); m != nil {
				t.Fatalf("%q: matched %q, but this line states no Go version", tc.line, m[1])
			}
		})
	}
}

// TestRequirementsSiteIgnoresProseOutsideItsBlock is the regression for the
// label collision. "- Go " is not a collision-free key the way OVUMCY_IMAGE
// is: a bullet reading "- Go modules are vendored" is ordinary prose, and an
// unscoped scan reads it as a version statement and goes red on it. Confining
// the site to the Requirements block leaves only the entry that belongs to it.
func TestRequirementsSiteIgnoresProseOutsideItsBlock(t *testing.T) {
	site := readmeGoVersionSites[1]
	readme := []byte("## Notes\n\n- Go modules are vendored\n- Go generate is not used\n\n" +
		"Requirements:\n\n- Go 1.27.1+\n- Node.js 22+\n\n```bash\nmake build\n```\n")

	block := site.within.FindSubmatch(readme)
	if block == nil {
		t.Fatal("Requirements block not found in the fixture, so the site would go unchecked")
	}
	matches := site.re.FindAllSubmatch(block[1], -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly the Requirements entry, got %d matches", len(matches))
	}
	if got := string(matches[0][1]); got != "1.27.1+" {
		t.Fatalf("captured %q, want %q", got, "1.27.1+")
	}
}

// goModVersion reads the `go` directive. It fails closed for its callers: a
// go.mod the pattern cannot read aborts rather than leaving the comparison
// running against an empty string, which every README value would fail.
func goModVersion(t *testing.T, root string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	m := goDirective.FindSubmatch(content)
	if m == nil {
		t.Fatal("no `go` directive found in go.mod: nothing to compare README.md against")
	}
	return string(m[1])
}

// TestReadmeReleaseTagsAgree asserts every occurrence of the current release
// tag in README.md is the same version. It fails closed: finding zero
// occurrences is itself a failure, so a rewording that stops matching the
// patterns above cannot silently stop being checked.
func TestReadmeReleaseTagsAgree(t *testing.T) {
	found := readmeReleaseTags(t, repoRoot(t))

	want := found[0]
	for _, got := range found[1:] {
		if got != want {
			t.Errorf("README.md release tags disagree: found both v%s and v%s; update every occurrence to the same released version", want, got)
		}
	}
}

// TestExampleEnvImagePinsMatchReleaseTag asserts that every `.env.example`
// under docs/examples/ which pins OVUMCY_IMAGE names the same release tag
// README.md asserts. An example env template is copied to `.env` as the first
// setup step, and the assignment wins over the `${OVUMCY_IMAGE:-...}` default
// in the compose file beside it, so a template left behind at an older tag
// silently downgrades the stack it ships with.
//
// It fails closed twice over: no `.env.example` files at all, or no
// OVUMCY_IMAGE pin in any of them, is a failure rather than a pass, so neither
// a moved example stack nor a dropped pin can leave the guard checking
// nothing. A value that is not the published image at an exact release tag is
// reported too — an unparsable pin is a pin nobody is comparing.
func TestExampleEnvImagePinsMatchReleaseTag(t *testing.T) {
	root := repoRoot(t)
	want := readmeReleaseTags(t, root)[0]
	examplesDir := filepath.Join(root, "docs", "examples")

	var templates, pins int
	err := filepath.WalkDir(examplesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != ".env.example" {
			return nil
		}
		templates++
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, m := range envImageAssignment.FindAllSubmatch(content, -1) {
			pins++
			value := envImageValue(string(m[1]))
			ref := ovumcyImageRef.FindStringSubmatch(value)
			if ref == nil {
				t.Errorf("%s: OVUMCY_IMAGE=%q is not ghcr.io/ovumcy/ovumcy-web at an exact release tag, so nothing can tell which image an operator copying this template would run", rel, value)
				continue
			}
			if ref[1] != want {
				t.Errorf("%s: OVUMCY_IMAGE pins v%s while the release tag asserted in README.md is v%s; copying this template overrides the compose default and deploys the older image", rel, ref[1], want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", examplesDir, err)
	}
	if templates == 0 {
		t.Fatalf("no .env.example files found under %s: the drift guard has nothing to check", examplesDir)
	}
	if pins == 0 {
		t.Fatalf("no OVUMCY_IMAGE assignment found in any .env.example under %s: the drift guard has nothing to check", examplesDir)
	}
}

// TestComposeFallbackImagesMatchReleaseTag asserts that every compose file's
// `${OVUMCY_IMAGE:-...}` fallback names the same release tag README.md
// asserts. That fallback is what an operator gets when OVUMCY_IMAGE is unset
// — which is the default path through every quick start in this repository,
// starting with the root docker-compose.yml.
//
// The population is discovered by walking the repository root for
// docker-compose*.y*ml files (isComposeFile), not by a list of paths: a list
// is exactly how the root docker-compose.yml went unchecked while five
// example-stack copies were. It fails closed twice over, per the same rule
// TestExampleEnvImagePinsMatchReleaseTag applies: zero compose files found at
// all is a failure, and a compose file that IS found but yields no
// `${OVUMCY_IMAGE:-...}` fallback is reported by name rather than silently
// skipped — a single total across all files would let the remaining files
// keep the count non-zero while this one drifts unseen, the trap
// envImageAssignment's comment describes. A fallback value that is not the
// published image at an exact release tag is reported too.
func TestComposeFallbackImagesMatchReleaseTag(t *testing.T) {
	root := repoRoot(t)
	composeFallbacksMatchReleaseTag(t, root, readmeReleaseTags(t, root)[0])
}

// composeFallbacksMatchReleaseTag walks root for docker-compose*.y*ml files
// (isComposeFile) and asserts every `${OVUMCY_IMAGE:-...}` fallback names
// want. It is a helper, not inlined into the test above, so a fixture built
// on a synthetic root can drive the exact same check the real repository
// gets — the same reason readmeReleaseTags takes root as a parameter.
func composeFallbacksMatchReleaseTag(t composeReporter, root, want string) {
	t.Helper()
	var files, fallbacks int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && isOutsideTrackedTree(path, d) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isComposeFile(d.Name()) {
			return nil
		}
		files++

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		matches := composeFallbackImage.FindAllSubmatch(content, -1)
		if len(matches) == 0 {
			t.Errorf("%s: no ${OVUMCY_IMAGE:-...} fallback found, so this compose file's default image is no longer being checked", rel)
			return nil
		}
		for _, m := range matches {
			fallbacks++
			value := string(m[1])
			ref := ovumcyImageRef.FindStringSubmatch(value)
			if ref == nil {
				t.Errorf("%s: OVUMCY_IMAGE fallback %q is not ghcr.io/ovumcy/ovumcy-web at an exact release tag, so nothing can tell which image an operator who never sets OVUMCY_IMAGE would run", rel, value)
				continue
			}
			if ref[1] != want {
				t.Errorf("%s: OVUMCY_IMAGE fallback pins v%s while the release tag asserted in README.md is v%s; an operator who never sets OVUMCY_IMAGE runs the older image", rel, ref[1], want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if files == 0 {
		t.Fatalf("no docker-compose*.y*ml files found under %s: the drift guard has nothing to check", root)
	}
	if fallbacks == 0 {
		t.Fatalf("no OVUMCY_IMAGE fallback found in any compose file under %s: the drift guard has nothing to check", root)
	}
}

// TestEnvImageAssignmentMatchesEveryPinShape proves the matcher keys on the
// assignment target rather than on one value shape, using inline fixtures
// rather than the real templates. The trailing-comment and `export` cases are
// the regression: a value-anchored pattern skipped both, so a template written
// either way contributed no pin at all — it could carry a stale tag while the
// walk above passed, since the other templates kept the pin count non-zero.
func TestEnvImageAssignmentMatchesEveryPinShape(t *testing.T) {
	const image = "ghcr.io/ovumcy/ovumcy-web:v1.9.2"

	pinned := []struct {
		name string
		line string
		want string
	}{
		{"bare assignment", "OVUMCY_IMAGE=" + image, image},
		{"trailing comment", "OVUMCY_IMAGE=" + image + " # keep in sync with README", image},
		{"export prefix", "export OVUMCY_IMAGE=" + image, image},
		{"double quoted", `OVUMCY_IMAGE="` + image + `"`, image},
		{"single quoted with comment", "OVUMCY_IMAGE='" + image + "'  # note", image},
		{"indented and padded", "  OVUMCY_IMAGE =  " + image + "  ", image},
		{"empty value", "OVUMCY_IMAGE=", ""},
		{"stale tag with comment", "OVUMCY_IMAGE=ghcr.io/ovumcy/ovumcy-web:v0.8.4 # pinned", "ghcr.io/ovumcy/ovumcy-web:v0.8.4"},
	}
	for _, tc := range pinned {
		t.Run(tc.name, func(t *testing.T) {
			matches := envImageAssignment.FindAllStringSubmatch(tc.line+"\nTZ=UTC\n", -1)
			if len(matches) != 1 {
				t.Fatalf("%q: expected exactly one OVUMCY_IMAGE assignment, got %d", tc.line, len(matches))
			}
			if got := envImageValue(matches[0][1]); got != tc.want {
				t.Fatalf("%q: got value %q, want %q", tc.line, got, tc.want)
			}
		})
	}

	ignored := []struct {
		name string
		line string
	}{
		{"commented out", "# OVUMCY_IMAGE=" + image},
		{"different variable", "OVUMCY_IMAGE_TAG=v1.9.2"},
		{"key as a suffix", "EXTRA_OVUMCY_IMAGE=" + image},
	}
	for _, tc := range ignored {
		t.Run(tc.name, func(t *testing.T) {
			if matches := envImageAssignment.FindAllStringSubmatch(tc.line+"\nTZ=UTC\n", -1); len(matches) != 0 {
				t.Fatalf("%q: expected no OVUMCY_IMAGE assignment, got %d", tc.line, len(matches))
			}
		})
	}
}

// envImageValue reduces the captured right-hand side of an OVUMCY_IMAGE line
// to the image reference it sets: surrounding whitespace, one pair of matching
// quotes and a trailing `#` comment are removed. Whatever remains is compared
// against the release tag and, when it does not parse as an image reference,
// reported verbatim — never dropped.
func envImageValue(raw string) string {
	value := strings.TrimSpace(raw)
	if i := strings.Index(value, "#"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return value
}

// readmeReleaseTags returns every release-tag occurrence in README.md, in the
// order releaseTagPatterns finds them. It fails closed for its callers: zero
// occurrences aborts the test, so a rewording that stops matching the patterns
// cannot silently turn a guard built on this tag into a no-op.
func readmeReleaseTags(t *testing.T, root string) []string {
	t.Helper()
	content := readmeContent(t, root)

	var found []string
	for _, re := range releaseTagPatterns {
		for _, m := range re.FindAllSubmatch(content, -1) {
			found = append(found, string(m[1]))
		}
	}
	if len(found) == 0 {
		t.Fatal("no release-tag occurrences found in README.md: the drift guard has nothing to check")
	}
	return found
}

// readmeContent reads README.md or aborts. Both drift guards above compare
// against it, and a read error must stop them rather than let either run over
// empty bytes and pass by finding nothing.
func readmeContent(t *testing.T, root string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	return content
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
