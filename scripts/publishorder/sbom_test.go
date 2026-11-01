package publishorder

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// sbomDocument is the shape `imagetools inspect --format '{{ json .SBOM }}'`
// returns for one platform, cut down to the fields the step judges.
func sbomDocument(packages int) string {
	var entries []string
	for i := range packages {
		entries = append(entries, `{"name":"pkg`+strconv.Itoa(i)+`"}`)
	}
	return `{"SPDX":{"spdxVersion":"SPDX-2.3","packages":[` + strings.Join(entries, ",") + `]}}`
}

// TestTheSBOMStepRefusesADigestWithoutAnSPDXDocumentPerPlatform runs the SBOM
// step with `docker` shadowed. The order and content guards read names and
// substrings, and both stay green over a check emptied to `|| true` or a
// filter that accepts an empty package list; only running the script against
// a registry answer it must refuse can tell.
func TestTheSBOMStepRefusesADigestWithoutAnSPDXDocumentPerPlatform(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	python := requirePython(t)
	script := stepScript(t, job, sbomStep)

	both := `{"linux/amd64":` + sbomDocument(2) + `,"linux/arm64":` + sbomDocument(3) + `}`

	for _, testCase := range []struct {
		name      string
		platforms string
		answer    string
		// failures is how many reads fail before the registry answers.
		failures    int
		wantRefusal string
	}{
		{name: "every platform carries an SBOM", platforms: "linux/amd64,linux/arm64", answer: both},
		{
			name:        "one platform has no SBOM",
			platforms:   "linux/amd64,linux/arm64",
			answer:      `{"linux/amd64":` + sbomDocument(2) + `}`,
			wantRefusal: "no SPDX SBOM with packages for linux/arm64",
		},
		{
			name:        "an SBOM listing no packages",
			platforms:   "linux/amd64,linux/arm64",
			answer:      `{"linux/amd64":` + sbomDocument(2) + `,"linux/arm64":` + sbomDocument(0) + `}`,
			wantRefusal: "no SPDX SBOM with packages for linux/arm64",
		},
		{
			name:        "an index with no SBOM at all",
			platforms:   "linux/amd64,linux/arm64",
			answer:      `null`,
			wantRefusal: "no SPDX SBOM with packages for linux/amd64",
		},
		{
			name:        "a document that is not SPDX",
			platforms:   "linux/amd64",
			answer:      `{"linux/amd64":{"SPDX":{"spdxVersion":"CycloneDX","packages":[{}]}}}`,
			wantRefusal: "no SPDX SBOM with packages for linux/amd64",
		},
		{name: "a single-platform index answers the bare document", platforms: "linux/amd64", answer: sbomDocument(1)},
		{
			// The bare form names no platform, so it cannot stand for two.
			name:        "the bare document where two platforms are expected",
			platforms:   "linux/amd64,linux/arm64",
			answer:      sbomDocument(1),
			wantRefusal: "no SPDX SBOM with packages for linux/amd64",
		},
		{name: "a registry blip is asked again", platforms: "linux/amd64,linux/arm64", answer: both, failures: 2},
		{
			name:        "a registry that never answers",
			platforms:   "linux/amd64,linux/arm64",
			answer:      both,
			failures:    3,
			wantRefusal: "failed 3 times",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stubDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(stubDir, "answer.json"), []byte(testCase.answer), 0o644); err != nil {
				t.Fatalf("write the stub answer: %v", err)
			}
			stubLog := filepath.ToSlash(filepath.Join(stubDir, "docker.log"))

			preamble := strings.Join([]string{
				`python3() { ` + shellQuote(filepath.ToSlash(python)) + ` "$@"; }`,
				`sleep() { :; }`,
				`docker() {`,
				`  printf '%s\n' "$*" >> ` + shellQuote(stubLog),
				`  local calls; calls=$(wc -l < ` + shellQuote(stubLog) + `)`,
				`  if [ "${calls}" -le ` + strconv.Itoa(testCase.failures) + ` ]; then echo "registry unavailable" >&2; return 1; fi`,
				`  cat ` + shellQuote(filepath.ToSlash(filepath.Join(stubDir, "answer.json"))),
				`}`,
			}, "\n")

			command := runBashScript(t, bash, job, sbomStep, preamble+"\n"+script)
			command.Env = append(os.Environ(),
				"IMAGE_DIGEST="+imageName+"@"+digest,
				"IMAGE_PLATFORMS="+testCase.platforms,
				"RUNNER_TEMP="+filepath.ToSlash(t.TempDir()),
			)
			output, err := command.CombinedOutput()

			calls, readErr := os.ReadFile(filepath.FromSlash(stubLog))
			if readErr != nil {
				t.Fatalf("the step never read the registry: %v\n%s", readErr, output)
			}
			for _, call := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
				if !strings.Contains(call, "buildx imagetools inspect "+imageName+"@"+digest+" ") {
					t.Errorf("the step read %q, not the pushed digest %s", call, digest)
				}
			}

			if testCase.wantRefusal != "" {
				if err == nil {
					t.Fatalf("the step accepted an answer it must refuse:\n%s", output)
				}
				if !strings.Contains(string(output), "::error::") || !strings.Contains(string(output), testCase.wantRefusal) {
					t.Errorf("the step refused without saying %q in an ::error:: line:\n%s", testCase.wantRefusal, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("the step refused a digest carrying every SBOM: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), "SBOM attestation verified on every platform") {
				t.Errorf("the step passed without reaching its verdict line:\n%s", output)
			}
		})
	}
}
