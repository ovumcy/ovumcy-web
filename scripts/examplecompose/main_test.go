// Package examplecompose guards docs/examples/**/docker-compose.yml against a
// regression class: the postgres:18+ image moved PGDATA and its declared
// VOLUME from /var/lib/postgresql/data to /var/lib/postgresql. A compose file
// that still mounts the named data volume at the old /var/lib/postgresql/data
// path silently writes into an anonymous volume instead (the named volume
// stays empty), so `docker compose down` (without -v) loses all data even
// though the healthcheck stays green. See CHANGELOG.md (fix(deploy) entry)
// for the incident this guards against.
package examplecompose

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// wantDataMount is the canonical postgres data directory for image majors
// 18+. It is intentionally NOT version-specific (no "postgres:18" match):
// the path has been stable across 18, 19, ... so the guard keeps working
// after a routine tag bump without an edit.
const wantDataMount = "/var/lib/postgresql"

var postgresDataVolumeMount = regexp.MustCompile(`(?m)^\s*-\s*postgres_data:(\S+)\s*$`)

// TestExamplePostgresComposeMountsDataVolumeAtCanonicalPath asserts that
// every example docker-compose.yml running a postgres image mounts the
// postgres_data named volume at wantDataMount. It fails closed: finding zero
// candidate files is itself a failure, so a renamed/moved example stack
// cannot silently stop being checked.
func TestExamplePostgresComposeMountsDataVolumeAtCanonicalPath(t *testing.T) {
	root := repoRoot(t)
	examplesDir := filepath.Join(root, "docs", "examples")

	var checked int
	err := filepath.WalkDir(examplesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "docker-compose.yml" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !regexp.MustCompile(`image:\s*postgres:`).Match(content) {
			return nil
		}
		checked++

		rel, _ := filepath.Rel(root, path)
		matches := postgresDataVolumeMount.FindAllSubmatch(content, -1)
		if len(matches) == 0 {
			t.Errorf("%s: postgres service present but no \"postgres_data:<path>\" volume mount found", rel)
			return nil
		}
		for _, m := range matches {
			got := string(m[1])
			if got != wantDataMount {
				t.Errorf("%s: postgres_data volume mounted at %q, want %q (postgres 18+ moved PGDATA off /var/lib/postgresql/data; the old path silently falls through to an anonymous volume)", rel, got, wantDataMount)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", examplesDir, err)
	}
	if checked == 0 {
		t.Fatalf("no postgres example docker-compose.yml files found under %s: the regression guard has nothing to check", examplesDir)
	}
}

// TestExtractPostgresDataMountDetectsRegression proves the regex used above
// actually distinguishes the fixed path from the pre-postgres:18 default it
// regresses to, using inline fixtures rather than the real example files.
func TestExtractPostgresDataMountDetectsRegression(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"canonical path (fixed)", "    volumes:\n      - postgres_data:/var/lib/postgresql\n", "/var/lib/postgresql"},
		{"pre-postgres:18 default (the bug)", "    volumes:\n      - postgres_data:/var/lib/postgresql/data\n", "/var/lib/postgresql/data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches := postgresDataVolumeMount.FindAllSubmatch([]byte(tc.line), -1)
			if len(matches) != 1 {
				t.Fatalf("expected exactly one match, got %d", len(matches))
			}
			got := string(matches[0][1])
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if (got == wantDataMount) != (tc.want == wantDataMount) {
				t.Fatalf("canonical-path check disagrees with fixture intent for %q", tc.name)
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
