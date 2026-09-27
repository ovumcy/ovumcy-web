package db

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file guards the invariant WEB-50/WEB-65 established: migration 041
// backfills every users.auth_session_version at or below 0 to 1 at boot, so
// no predicate or reader anywhere in this module may still special-case a
// legacy 0 (or below) as a match for a current version. Two arms used to
// carry that special case — authSessionVersionFromPredicate's `(? = 1 AND
// auth_session_version <= 0)` OR clause and
// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS's identical arm — and both
// are gone. This sweeps the module's own source for either spelling
// reappearing, in a Go string literal or (were one ever added outside
// migrations/) a .sql file, so a regression is caught at the text it would be
// written in, not only in the two call sites known today.
var authSessionVersionLegacyArmPattern = regexp.MustCompile(
	`auth_session_version\s*(<=\s*0|<\s*1)`,
)

// authSessionVersionLegacyArmModuleRoot walks up from the package directory to
// the module root, the same way password_hash_writer_reachability_test.go's
// own copy does — duplicated rather than shared, per this repository's
// barrier-file convention of staying self-contained.
func authSessionVersionLegacyArmModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving the working directory: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s; the sweep would measure nothing", dir)
		}
		dir = parent
	}
}

// authSessionVersionLegacyArmFinding is one line the sweep flagged, kept as a
// value rather than a bare string so a failure message can name the file and
// line without re-parsing it.
type authSessionVersionLegacyArmFinding struct {
	relPath string
	line    int
	text    string
}

// scanForAuthSessionVersionLegacyArm walks root/internal, skipping
// root/migrations entirely (schema history stays as written;
// migration-immutability governs it, not this guard) and every _test.go file
// (a test may legitimately name the retired arm in a comment or as a literal
// it asserts against, the way this package's own CAS tests do), and reports
// every remaining .go or .sql file whose text still matches
// authSessionVersionLegacyArmPattern. visited collects every file inspected,
// by relative path, so the caller can prove the sweep actually reached the
// files that used to hold the arm — not merely that it found nothing.
func scanForAuthSessionVersionLegacyArm(root string) (findings []authSessionVersionLegacyArmFinding, visited []string, err error) {
	internalDir := filepath.Join(root, "internal")
	walkErr := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		ext := filepath.Ext(name)
		if ext != ".go" && ext != ".sql" {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		visited = append(visited, rel)

		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer func() {
			_ = file.Close()
		}()

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			if authSessionVersionLegacyArmPattern.MatchString(line) {
				findings = append(findings, authSessionVersionLegacyArmFinding{
					relPath: rel,
					line:    lineNumber,
					text:    strings.TrimSpace(line),
				})
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			return scanErr
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	return findings, visited, nil
}

// TestNoAuthSessionVersionLegacyZeroMatchArmSurvives is the regression guard
// for WEB-50/WEB-65: neither authSessionVersionFromPredicate nor
// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS (nor anything added beside
// them) may special-case a stored auth_session_version at or below 0 as a
// match for a current version, now that migration 041 makes that state
// unreachable in a freshly migrated database.
func TestNoAuthSessionVersionLegacyZeroMatchArmSurvives(t *testing.T) {
	root, err := authSessionVersionLegacyArmModuleRoot()
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}
	findings, visited, err := scanForAuthSessionVersionLegacyArm(root)
	if err != nil {
		t.Fatalf("scan for the legacy arm: %v", err)
	}

	// Anti-vacuity: prove the walk actually reached the two files that used to
	// carry the arm, by name, rather than trusting an empty findings list from
	// a walk that silently saw nothing.
	mustVisit := []string{
		"internal/db/auth_session_version_cas.go",
		"internal/db/user_repository.go",
	}
	for _, want := range mustVisit {
		found := false
		for _, got := range visited {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("sweep never visited %s (visited %d files) -- it is proving nothing", want, len(visited))
		}
	}

	if len(findings) > 0 {
		lines := make([]string, 0, len(findings))
		for _, f := range findings {
			lines = append(lines, fmt.Sprintf("%s:%d: %s", f.relPath, f.line, f.text))
		}
		t.Fatalf("a legacy auth_session_version<=0/<1 match arm reappeared outside migrations/:\n%s", strings.Join(lines, "\n"))
	}
}
