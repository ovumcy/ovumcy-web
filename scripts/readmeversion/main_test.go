// Package readmeversion guards README.md against release-tag drift: the
// current release tag (currently v1.8.0) is hardcoded in several places
// (intro blurb, Docker quick start image tag, cosign/attestation/SBOM
// verification examples, and the Releases section) with no single source of
// truth, so a release bump that updates one occurrence and misses another
// goes unnoticed. This test asserts every occurrence agrees.
package readmeversion

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var releaseTagPatterns = []*regexp.Regexp{
	regexp.MustCompile("latest tagged release is `v(\\d+\\.\\d+\\.\\d+)`"),
	regexp.MustCompile(`ghcr\.io/ovumcy/ovumcy-web:v(\d+\.\d+\.\d+)`),
	regexp.MustCompile("Latest tagged release: `v(\\d+\\.\\d+\\.\\d+)`"),
}

// TestReadmeReleaseTagsAgree asserts every occurrence of the current release
// tag in README.md is the same version. It fails closed: finding zero
// occurrences is itself a failure, so a rewording that stops matching the
// patterns above cannot silently stop being checked.
func TestReadmeReleaseTagsAgree(t *testing.T) {
	root := repoRoot(t)
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

	want := found[0]
	for _, got := range found[1:] {
		if got != want {
			t.Errorf("README.md release tags disagree: found both v%s and v%s; update every occurrence to the same released version", want, got)
		}
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
