package publishorder

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The commits the freshness fixtures reason about, oldest first. They are
// forty hex digits because the step refuses a revision label that is not one.
var (
	oldCommit = strings.Repeat("1", 40)
	midCommit = strings.Repeat("2", 40)
	newCommit = strings.Repeat("3", 40)

	// elsewhereCommit is on no line of the history the stub knows, which is how
	// the stub says "diverged".
	elsewhereCommit = strings.Repeat("4", 40)
)

const (
	platformDigest    = "sha256:" + "b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0"
	attestationDigest = "sha256:" + "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	configDigest      = "sha256:" + "c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2"
	revisionLabel     = "org.opencontainers.image.revision"
)

// liveRegistry is what GHCR answers for the live `main` tag. Bodies are the
// real shapes: an index whose FIRST entry is an attestation manifest with no
// platform (so a step that took the first entry would follow the wrong one), a
// platform manifest naming its config, and the config carrying the label.
type liveRegistry struct {
	tokenStatus    string
	mainStatus     string
	platformStatus string
	configStatus   string
	// mainBody is the body served for the `main` tag.
	mainBody string
	// configBody is the image config the platform manifest names.
	configBody string
}

func indexBody() string {
	return `{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[` +
		`{"digest":"` + attestationDigest + `","platform":{"os":"unknown","architecture":"unknown"}},` +
		`{"digest":"` + platformDigest + `","platform":{"os":"linux","architecture":"amd64"}}]}`
}

func platformBody() string {
	return `{"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"digest":"` + configDigest + `"}}`
}

func configWithLabel(value string) string {
	return `{"architecture":"amd64","config":{"Labels":{"` + revisionLabel + `":"` + value + `"}}}`
}

func liveAt(commit string) liveRegistry {
	return liveRegistry{
		tokenStatus:    "200",
		mainStatus:     "200",
		platformStatus: "200",
		configStatus:   "200",
		mainBody:       indexBody(),
		configBody:     configWithLabel(commit),
	}
}

// TestThePublishJobSerialisesItsAliasWrites pins the lock the freshness step
// relies on: it reads what is live and the promotion writes, and only a group
// that admits one publish at a time lets the first still be true at the second.
// A release tag must NOT share `main`'s group, because a group keeps one
// pending job and replaces the waiting one, and nothing replaces a release.
func TestThePublishJobSerialisesItsAliasWrites(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)

	match := regexp.MustCompile(`(?m)^    concurrency:\n      group: (.+)\n      cancel-in-progress: (.+)\n`).FindStringSubmatch(job)
	if match == nil {
		t.Fatalf("%s, job %q declares no `concurrency:` of `group:` then `cancel-in-progress:`: two commits' publishes run side by side and the later FINISH, not the newer commit, decides what `latest` serves", publishWorkflow, publishJob)
	}

	const wantGroup = "publish-${{ github.ref_type == 'tag' && github.ref || 'main' }}"
	if got := strings.TrimSpace(match[1]); got != wantGroup {
		t.Errorf("%s, job %q: concurrency group is %q, want %q. Every publish that moves an alias must share one group, and a release tag must have its own so a later `main` publish cannot cancel it while it waits", publishWorkflow, publishJob, got, wantGroup)
	}
	if got := strings.TrimSpace(match[2]); got != "false" {
		t.Errorf("%s, job %q: cancel-in-progress is %q, want false: a publish already underway would be cut off half-promoted", publishWorkflow, publishJob, got)
	}
}

// TestThePublicAliasesNeverMoveBackwards runs the freshness step for real,
// against a stubbed registry and a stubbed `gh`, over every answer the live
// image and GitHub can give. The property is one direction: the run proceeds
// only when its commit is the live commit or descends from it. Every refusal is
// held to its own message, and every case to "this step writes nothing".
func TestThePublicAliasesNeverMoveBackwards(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	python := requirePython(t)
	script := stepScript(t, job, freshnessStep)

	history := strings.Join([]string{oldCommit, midCommit, newCommit}, " ")

	for _, testCase := range []struct {
		name string
		// run is the commit this publish is for; refType is the caller's.
		run     string
		refType string
		live    liveRegistry
		// ghFails makes the comparison itself unanswerable.
		ghFails bool
		// wantError is the step's own message for the branch this case is
		// about; empty means the run must proceed.
		wantError string
		// wantCompare is the exact comparison the step must ask, `<live>...<run>`:
		// base first, so `behind` means this run is the OLDER one. Empty means
		// no comparison may be asked.
		wantCompare string
		// wantNoCalls means the step must not touch the network at all.
		wantNoCalls bool
		wantOutput  string
	}{
		{
			name:        "this commit is newer than the live one",
			run:         newCommit,
			refType:     "branch",
			live:        liveAt(oldCommit),
			wantCompare: oldCommit + "..." + newCommit,
			wantOutput:  "'ahead' relative to " + oldCommit,
		},
		{
			name:        "the live image is this very commit",
			run:         midCommit,
			refType:     "branch",
			live:        liveAt(midCommit),
			wantCompare: midCommit + "..." + midCommit,
			wantOutput:  "'identical' relative to " + midCommit,
		},
		{
			// The measured defect: run 36782607690 for the ancestor promoted after
			// run 36783421026 for its descendant.
			name:        "this commit is an ancestor of the live one",
			run:         oldCommit,
			refType:     "branch",
			live:        liveAt(newCommit),
			wantCompare: newCommit + "..." + oldCommit,
			wantError:   "an ancestor of " + newCommit,
		},
		{
			name:        "this commit is neither ahead of nor behind the live one",
			run:         elsewhereCommit,
			refType:     "branch",
			live:        liveAt(newCommit),
			wantCompare: newCommit + "..." + elsewhereCommit,
			wantError:   "as 'diverged'",
		},
		{
			name:        "GitHub cannot compare the two commits",
			run:         newCommit,
			refType:     "branch",
			live:        liveAt(oldCommit),
			ghFails:     true,
			wantCompare: oldCommit + "..." + newCommit,
			wantError:   "could not compare",
		},
		{
			name:    "there is no main tag yet",
			run:     oldCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(newCommit)
				r.mainStatus = "404"
				return r
			}(),
			wantOutput: "no `main` tag",
		},
		{
			name:    "the main manifest cannot be read",
			run:     newCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(oldCommit)
				r.mainStatus = "500"
				return r
			}(),
			wantError: "live `main` manifest returned HTTP 500",
		},
		{
			name:    "GHCR refuses the pull token",
			run:     newCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(oldCommit)
				r.tokenStatus = "403"
				return r
			}(),
			wantError: "HTTP 403 for a pull token",
		},
		{
			name:    "the platform manifest cannot be read",
			run:     newCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(oldCommit)
				r.platformStatus = "404"
				return r
			}(),
			wantError: "live platform manifest",
		},
		{
			name:    "the image config cannot be read",
			run:     newCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(oldCommit)
				r.configStatus = "500"
				return r
			}(),
			wantError: "live image config",
		},
		{
			name:    "the live image carries no revision label",
			run:     newCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(oldCommit)
				r.configBody = `{"config":{"Labels":{}}}`
				return r
			}(),
			wantError: "no usable org.opencontainers.image.revision label",
		},
		{
			name:    "the revision label is not a commit",
			run:     newCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(oldCommit)
				r.configBody = configWithLabel("main")
				return r
			}(),
			wantError: "no usable org.opencontainers.image.revision label",
		},
		{
			// A single image manifest has no `manifests` list; the config is read
			// from it directly.
			name:    "the main tag is a single image manifest, not an index",
			run:     newCommit,
			refType: "branch",
			live: func() liveRegistry {
				r := liveAt(oldCommit)
				r.mainBody = platformBody()
				return r
			}(),
			wantCompare: oldCommit + "..." + newCommit,
			wantOutput:  "'ahead' relative to " + oldCommit,
		},
		{
			// A release tag names one commit and moves no alias; the step must not
			// even ask, so an older release is published, which is the point of
			// the separate group.
			name:        "a release tag older than the live image",
			run:         oldCommit,
			refType:     "tag",
			live:        liveAt(newCommit),
			wantNoCalls: true,
			wantOutput:  "moves no alias",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := filepath.ToSlash(t.TempDir())
			for file, body := range map[string]string{
				"main.json":     testCase.live.mainBody,
				"platform.json": platformBody(),
				"config.json":   testCase.live.configBody,
			} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
					t.Fatalf("write the %s fixture: %v", file, err)
				}
			}

			ghFails := "0"
			if testCase.ghFails {
				ghFails = "1"
			}
			command := runBashScript(t, bash, job, freshnessStep, freshnessStub(python)+"\n"+script)
			command.Env = append(os.Environ(),
				"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
				"GITHUB_SHA="+testCase.run,
				"GITHUB_REF_TYPE="+testCase.refType,
				"GH_TOKEN=stub",
				"IMAGE_PATH="+imagePath,
				"REGISTRY_USER=github-actions",
				"REGISTRY_PASSWORD=stub",
				"STUB_DIR="+dir,
				"STUB_HISTORY="+history,
				"STUB_PLATFORM_DIGEST="+platformDigest,
				"STUB_CONFIG_DIGEST="+configDigest,
				"STUB_GH_FAILS="+ghFails,
				"STUB_TOKEN_STATUS="+testCase.live.tokenStatus,
				"STUB_MAIN_STATUS="+testCase.live.mainStatus,
				"STUB_PLATFORM_STATUS="+testCase.live.platformStatus,
				"STUB_CONFIG_STATUS="+testCase.live.configStatus,
			)
			// The job's own constants (the Accept header) reach the step as the
			// workflow supplies them.
			for key, value := range jobEnv(job) {
				command.Env = append(command.Env, key+"="+value)
			}

			outputBytes, runErr := command.CombinedOutput()
			output := string(outputBytes)

			calls, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
			log := string(calls)

			// Whatever the verdict, this step reads and never writes: a promotion
			// hiding in a freshness check would defeat the ordering above.
			if strings.Contains(log, "WRITE ") {
				t.Fatalf("the freshness step wrote to a registry:\n%s", log)
			}
			// Every registry read after the token carried the token it was issued.
			for _, line := range strings.Split(log, "\n") {
				if strings.HasPrefix(line, "GET ") && !strings.Contains(line, "/token?") && !strings.HasSuffix(line, " auth=Bearer stub-token") {
					t.Fatalf("a registry read went out without the pull token: %q", line)
				}
			}

			if testCase.wantNoCalls && strings.TrimSpace(log) != "" {
				t.Fatalf("the step touched the network when it owed nothing:\n%s", log)
			}
			compares := 0
			for _, line := range strings.Split(log, "\n") {
				if strings.HasPrefix(line, "GH ") {
					compares++
					if want := "GH api repos/ovumcy/ovumcy-web/compare/" + testCase.wantCompare + " --jq .status"; testCase.wantCompare == "" || line != want {
						t.Fatalf("the step asked GitHub %q, want %q (base is the LIVE commit, head is this run's)", line, want)
					}
				}
			}
			if testCase.wantCompare != "" && compares != 1 {
				t.Fatalf("the step asked GitHub %d times for the comparison %s, want exactly once:\n%s", compares, testCase.wantCompare, log)
			}

			if testCase.wantError != "" {
				if runErr == nil {
					t.Fatalf("the step let this run proceed and owed a refusal.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantError)
				return
			}
			if runErr != nil {
				t.Fatalf("the step refused a run it owes a pass: %v\n%s", runErr, output)
			}
			if testCase.wantOutput != "" && !strings.Contains(output, testCase.wantOutput) {
				t.Fatalf("the step passed without saying %q:\n%s", testCase.wantOutput, output)
			}
		})
	}
}

// freshnessStub shadows `curl`, `gh` and `python3` (the interpreter the other
// registry fixtures resolve) with shell functions. The registry stub answers
// the ENDPOINT, as stubRegistry does, and logs what was asked; `gh` computes
// the comparison from a history order the fixture supplies, so the verdict
// follows the direction the step asked in rather than a status the case
// asserted — a step that swapped base and head would be answered the opposite
// verdict by the same history.
func freshnessStub(python string) string {
	lines := []string{
		`python3() { ` + shellQuote(filepath.ToSlash(python)) + ` "$@"; }`,
		`curl() {`,
		`  local out="" url="" method=GET auth=""`,
		`  while [ $# -gt 0 ]; do`,
		`    case "$1" in`,
		`      -o) out="$2"; shift 2 ;;`,
		`      -X) method="$2"; shift 2 ;;`,
		`      -H) case "$2" in Authorization:*) auth="${2#Authorization: }" ;; esac; shift 2 ;;`,
		`      -w|-K|--connect-timeout|--max-time|--retry|--retry-delay) shift 2 ;;`,
		`      --data-binary) shift 2 ;;`,
		`      -*) shift ;;`,
		`      *) url="$1"; shift ;;`,
		`    esac`,
		`  done`,
		`  if [ "$method" != GET ]; then printf 'WRITE %s %s\n' "$method" "$url" >> "$STUB_DIR/calls.log"; fi`,
		`  printf 'GET %s auth=%s\n' "$url" "$auth" >> "$STUB_DIR/calls.log"`,
		`  case "$url" in`,
		`    *"/token?"*)`,
		`      printf '%s' '{"token": "stub-token"}' > "$out"`,
		`      printf '%s' "$STUB_TOKEN_STATUS"; return 0 ;;`,
		`    *"/manifests/main")`,
		`      cp "$STUB_DIR/main.json" "$out"; printf '%s' "$STUB_MAIN_STATUS"; return 0 ;;`,
		// Only the digests the fixture's own bodies name are served, so a step
		// that followed the index's attestation entry, or any digest it was not
		// pointed at, reaches an endpoint nothing answers.
		`    *"/manifests/${STUB_PLATFORM_DIGEST}")`,
		`      cp "$STUB_DIR/platform.json" "$out"; printf '%s' "$STUB_PLATFORM_STATUS"; return 0 ;;`,
		`    *"/blobs/${STUB_CONFIG_DIGEST}")`,
		`      cp "$STUB_DIR/config.json" "$out"; printf '%s' "$STUB_CONFIG_STATUS"; return 0 ;;`,
		`  esac`,
		`  printf 'the step called an endpoint this fixture does not serve: %s\n' "$url" >&2`,
		`  return 1`,
		`}`,
		`gh() {`,
		`  printf 'GH %s\n' "$*" >> "$STUB_DIR/calls.log"`,
		`  if [ "$STUB_GH_FAILS" = 1 ]; then echo "gh: HTTP 404" >&2; return 1; fi`,
		`  local range="${2##*/compare/}" base head i=0 ib=-1 ih=-1 c`,
		`  base="${range%%...*}"; head="${range##*...}"`,
		`  for c in $STUB_HISTORY; do`,
		`    if [ "$c" = "$base" ]; then ib=$i; fi`,
		`    if [ "$c" = "$head" ]; then ih=$i; fi`,
		`    i=$(( i + 1 ))`,
		`  done`,
		`  if [ "$base" = "$head" ]; then echo identical`,
		`  elif [ "$ib" -lt 0 ] || [ "$ih" -lt 0 ]; then echo diverged`,
		`  elif [ "$ib" -lt "$ih" ]; then echo ahead`,
		`  else echo behind; fi`,
		`}`,
	}
	return strings.Join(lines, "\n")
}
