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
package readmeversion

import (
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
	content, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}

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
