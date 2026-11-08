package examplecompose

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The example stacks under docs/examples are what an operator runs, and the
// Postgres major they pin is the one the migrations and the runtime have to
// be proven on. ci.yml's e2e-postgres-smoke job is the browser run against
// Postgres; it starts the image E2E_POSTGRES_IMAGE names. A tag bump on one
// side alone leaves the shipped major unproven while every lane stays green.

const (
	ciWorkflow       = ".github/workflows/ci.yml"
	postgresSmokeJob = "e2e-postgres-smoke"
)

var (
	composePostgresImage = regexp.MustCompile(`(?m)^\s*image:\s*["']?((?:[^\s"'/]+/)*postgres:[^\s"']+)["']?\s*$`)
	smokePostgresImage   = regexp.MustCompile(`(?m)^\s*E2E_POSTGRES_IMAGE:\s*["']?([^\s"'#]+)["']?\s*(?:#.*)?$`)
	postgresMajor        = regexp.MustCompile(`postgres:(\d+)(?:[.\-]|$)`)
)

// imageMajor reads the major version out of a postgres image reference, or
// returns "" when the tag carries none (`postgres:latest`, `postgres:alpine`):
// such a reference names no major and cannot be at parity with anything.
func imageMajor(ref string) string {
	match := postgresMajor.FindStringSubmatch(ref)
	if match == nil {
		return ""
	}
	return match[1]
}

// examplePostgresImages returns every postgres image the example compose
// files pin, keyed by file. It fails closed on zero: a renamed or moved
// example stack must not stop being compared.
func examplePostgresImages(t *testing.T) map[string]string {
	t.Helper()

	root := repoRoot(t)
	images := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, "docs", "examples"), func(path string, d os.DirEntry, err error) error {
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
		rel, _ := filepath.Rel(root, path)
		for _, match := range composePostgresImage.FindAllSubmatch(content, -1) {
			images[filepath.ToSlash(rel)] = string(match[1])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/examples: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("no docs/examples/**/docker-compose.yml pins a postgres image, so the major the stacks ship cannot be compared with anything")
	}
	return images
}

// smokeImage returns the E2E_POSTGRES_IMAGE the e2e-postgres-smoke job sets.
// Exactly one is expected inside that job: none means the job falls back to
// the runner script's default, which is no pin at all.
func smokeImage(t *testing.T, workflow string) string {
	t.Helper()

	block, err := jobBlock(workflow, postgresSmokeJob)
	if err != nil {
		t.Fatalf("%s: %v", ciWorkflow, err)
	}
	matches := smokePostgresImage.FindAllStringSubmatch(block, -1)
	if len(matches) != 1 {
		t.Fatalf("%s, job %q: want exactly one E2E_POSTGRES_IMAGE, found %d — without it the job runs the runner script's default image, which nothing ties to the example stacks", ciWorkflow, postgresSmokeJob, len(matches))
	}
	return matches[0][1]
}

// jobBlock cuts one job out of a workflow: from its two-space header to the
// next header at that indentation.
func jobBlock(workflow, job string) (string, error) {
	header := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(job) + `:\s*$`)
	loc := header.FindStringIndex(workflow)
	if loc == nil {
		return "", errMissingJob(job)
	}
	rest := workflow[loc[1]:]
	if next := regexp.MustCompile(`(?m)^  [A-Za-z0-9_-]+:\s*$`).FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	return rest, nil
}

type errMissingJob string

func (e errMissingJob) Error() string { return "no job named " + string(e) }

// parityFindings judges one smoke image against the example images and
// returns one line per stack whose major differs, or a line for an image
// whose major cannot be read at all.
func parityFindings(smoke string, examples map[string]string) []string {
	var findings []string
	smokeMajor := imageMajor(smoke)
	if smokeMajor == "" {
		return []string{"E2E_POSTGRES_IMAGE " + smoke + " names no Postgres major"}
	}
	for file, image := range examples {
		major := imageMajor(image)
		if major == "" {
			findings = append(findings, file+" pins "+image+", which names no Postgres major")
			continue
		}
		if major != smokeMajor {
			findings = append(findings, file+" pins "+image+" (major "+major+") and e2e-postgres-smoke runs "+smoke+" (major "+smokeMajor+")")
		}
	}
	return findings
}

// TestTheExampleStacksAndThePostgresSmokeRunTheSameMajor fails when the
// Postgres major the example stacks pin differs from the one the
// e2e-postgres-smoke job runs. Bump both in the same change.
func TestTheExampleStacksAndThePostgresSmokeRunTheSameMajor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(ciWorkflow)))
	if err != nil {
		t.Fatalf("read %s: %v", ciWorkflow, err)
	}
	smoke := smokeImage(t, strings.ReplaceAll(string(raw), "\r\n", "\n"))

	for _, finding := range parityFindings(smoke, examplePostgresImages(t)) {
		t.Errorf("%s. The browser run against Postgres must prove the major an operator runs: change E2E_POSTGRES_IMAGE in %s and the example stacks together", finding, ciWorkflow)
	}
}

// TestTheParityCheckTellsAMismatchedMajorFromAMatchingOne holds the
// comparison itself to both verdicts on inputs this test owns, so the guard
// above cannot go green by comparing nothing or by comparing whole tags.
func TestTheParityCheckTellsAMismatchedMajorFromAMatchingOne(t *testing.T) {
	examples := map[string]string{"docs/examples/a/docker-compose.yml": "postgres:18.4-alpine3.22"}

	if got := parityFindings("postgres:18-alpine", examples); len(got) != 0 {
		t.Errorf("the same major under another tag was refused: %v", got)
	}
	if got := parityFindings("postgres:17-alpine", examples); len(got) != 1 {
		t.Errorf("major 17 against an example stack on 18 was accepted: %v", got)
	}
	if got := parityFindings("postgres:latest", examples); len(got) != 1 {
		t.Errorf("an image naming no major was accepted: %v", got)
	}
	if got := parityFindings("docker.io/library/postgres:18.4", map[string]string{"x": "postgres:1"}); len(got) != 1 {
		t.Errorf("major 1 was read as a prefix of 18: %v", got)
	}
}
