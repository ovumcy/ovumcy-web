package releasegate

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The Docker Hub mirror is the last leg of the release this package gates: a
// tag that passed the gate and was signed on GHCR is copied there by `cosign
// copy`. Twice on 2026-09-29 (runs 36512010639, 36512597460) that copy was
// refused with Docker Hub's 429 during a merge burst, once on the
// cross-registry copy and once on an alias. The step now asks again on that
// one verdict; these cases hold it to asking on nothing else and to failing
// the run when the throttle outlasts the budget, which is what keeps a retry
// from turning an incomplete mirror into a green release.
const (
	mirrorJob      = "publish"
	mirrorStepName = "Mirror the signed digest to Docker Hub"

	mirrorImage    = "ghcr.io/example/app"
	mirrorName     = "docker.io/example/app"
	mirrorDigest   = "sha256:7c4fe266e670ecb5e2ae77786014ad88feff5f838fba754aaf88cba6a85dd54c"
	mirrorIdentity = `^https://github\.com/example/app/\.github/workflows/docker-image\.yml@`

	// The fixture's own budget; the shipped one waits minutes on purpose and
	// is TestTheShippedMirrorCopyBudgetActuallyWaits's subject.
	mirrorCopyAttempts = 3

	// The two shapes the throttle took in the logs: a GET, whose body names
	// it, and a HEAD, which has no body and leaves only the status line.
	throttledGet  = "Error: GET https://index.docker.io/v2/example/app/manifests/sha256:8690088dccfecc39469336046c3e63c5813302a169e3b1d1dcbaf1544446fa5b: TOOMANYREQUESTS: You have reached your unauthenticated pull rate limit. https://www.docker.com/increase-rate-limit"
	throttledHead = "Error: HEAD https://index.docker.io/v2/example/app/manifests/sha256-028c9d860666078391c016835015b35a72a8db41420e68a4f0e3e3096db92c69: unexpected status code 429 Too Many Requests (HEAD responses have no body, use GET for details)"
	// An answer that is not the throttle, over a digest that happens to hold
	// `429`: matched on the digits alone, it would be retried.
	unknownManifest = "Error: GET https://index.docker.io/v2/example/app/manifests/sha256:4290aa: MANIFEST_UNKNOWN: manifest unknown"
)

func TestTheMirrorCopyRetriesOnlyDockerHubsRateLimit(t *testing.T) {
	bash := bashPath(t)
	requireWorkingBash(t, bash)
	block := mirrorStepBlock(t)
	script := runScript(t, mirrorStepName, block)

	crossRegistry := "COSIGN copy --force " + mirrorImage + "@" + mirrorDigest + " " + mirrorName + "@" + mirrorDigest
	latestAlias := "COSIGN copy --force " + mirrorName + "@" + mirrorDigest + " " + mirrorName + ":latest"
	signed := "COSIGN sign --yes " + mirrorName + "@" + mirrorDigest

	for _, testCase := range []struct {
		name string
		// failDest is the copy destination the registry refuses, failures
		// how many times, and message what cosign prints when it does.
		failDest string
		failures int
		message  string
		// wantCalls is how many times the refused copy may run: every
		// attempt of the budget for the throttle, one for anything else.
		wantCalls int
		// wantSleeps is every pause the step takes, in order: one between
		// attempts, growing with the attempt, none after the last.
		wantSleeps  []string
		wantRefusal string
		wantOrdered []string
	}{
		{
			name:        "the cross-registry copy is throttled, then accepted",
			failDest:    mirrorName + "@" + mirrorDigest,
			failures:    mirrorCopyAttempts - 1,
			message:     throttledGet,
			wantCalls:   mirrorCopyAttempts,
			wantSleeps:  []string{"SLEEP 1", "SLEEP 2"},
			wantOrdered: []string{crossRegistry, "SLEEP 1", crossRegistry, "SLEEP 2", crossRegistry, signed, latestAlias},
		},
		{
			name:        "an alias copy is throttled on a HEAD, then accepted",
			failDest:    mirrorName + ":latest",
			failures:    1,
			message:     throttledHead,
			wantCalls:   2,
			wantSleeps:  []string{"SLEEP 1"},
			wantOrdered: []string{crossRegistry, signed, latestAlias, "SLEEP 1", latestAlias},
		},
		{
			name:        "the throttle outlasts the budget before anything is signed",
			failDest:    mirrorName + "@" + mirrorDigest,
			failures:    mirrorCopyAttempts * 10,
			message:     throttledGet,
			wantCalls:   mirrorCopyAttempts,
			wantSleeps:  []string{"SLEEP 1", "SLEEP 2"},
			wantRefusal: "rate limit on all",
		},
		{
			name:        "the throttle outlasts the budget on an alias",
			failDest:    mirrorName + ":latest",
			failures:    mirrorCopyAttempts * 10,
			message:     throttledHead,
			wantCalls:   mirrorCopyAttempts,
			wantSleeps:  []string{"SLEEP 1", "SLEEP 2"},
			wantRefusal: "rate limit on all",
		},
		{
			name:        "a refusal that is not the throttle is asked once",
			failDest:    mirrorName + "@" + mirrorDigest,
			failures:    mirrorCopyAttempts * 10,
			message:     unknownManifest,
			wantCalls:   1,
			wantRefusal: "not on Docker Hub's rate limit",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// The sleep shim records each pause instead of taking it, so the
			// fixture runs the shipped arithmetic at a one-second unit without
			// waiting on it.
			preamble := `copies=0` + "\n" +
				`sleep() { printf 'SLEEP %s\n' "$*" >&2; }` + "\n" +
				`cosign() { printf 'COSIGN %s\n' "$*" >&2; ` +
				`if [ "$1" = copy ] && [ "$4" = ` + shellQuote(testCase.failDest) + ` ]; then copies=$(( copies + 1 )); ` +
				`if [ "$copies" -le ` + strconv.Itoa(testCase.failures) + ` ]; then printf '%s\n' ` + shellQuote(testCase.message) + ` >&2; return 1; fi; fi; ` +
				`return 0; }`

			output, err := runStepScript(t, bash, mirrorStepName, block, preamble+"\n"+script, "", append(os.Environ(),
				"DIGEST="+mirrorDigest,
				"TAG_REFS="+mirrorImage+":latest",
				"IMAGE_NAME="+mirrorImage,
				"MIRROR_NAME="+mirrorName,
				"IDENTITY_REGEXP="+mirrorIdentity,
				"VERIFY_ATTEMPTS=1",
				"VERIFY_DELAY_SECONDS=0",
				"COPY_ATTEMPTS="+strconv.Itoa(mirrorCopyAttempts),
				"COPY_DELAY_SECONDS=1",
			))

			var sleeps []string
			for _, line := range strings.Split(output, "\n") {
				if strings.HasPrefix(line, "SLEEP ") {
					sleeps = append(sleeps, line)
				}
			}
			if !slices.Equal(sleeps, testCase.wantSleeps) {
				t.Errorf("the step paused %q, want %q:\n%s", sleeps, testCase.wantSleeps, output)
			}

			// The cross-registry copy reads GHCR; every alias reads the mirror.
			source := mirrorName + "@" + mirrorDigest
			if testCase.failDest == mirrorName+"@"+mirrorDigest {
				source = mirrorImage + "@" + mirrorDigest
			}
			refused := "COSIGN copy --force " + source + " " + testCase.failDest
			if got := strings.Count(output, refused+"\n"); got != testCase.wantCalls {
				t.Errorf("the step ran %q %d time(s), want %d:\n%s", refused, got, testCase.wantCalls, output)
			}

			if testCase.wantRefusal != "" {
				if err == nil {
					t.Fatalf("the step passed a mirror whose copy never landed:\n%s", output)
				}
				if !strings.Contains(output, "::error::") || !strings.Contains(output, testCase.wantRefusal) {
					t.Errorf("the step failed without saying %q:\n%s", testCase.wantRefusal, output)
				}
				if testCase.failDest == mirrorName+"@"+mirrorDigest && strings.Contains(output, signed) {
					t.Errorf("the step signed a mirror whose bytes never crossed:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("the step failed: %v\n%s", err, output)
			}
			rest := output
			for _, want := range testCase.wantOrdered {
				at := strings.Index(rest, want+"\n")
				if at < 0 {
					t.Fatalf("the step's cosign calls are not, in order, %q; missing %q after what came before:\n%s", testCase.wantOrdered, want, output)
				}
				rest = rest[at+len(want)+1:]
			}
		})
	}
}

// TestTheShippedMirrorCopyBudgetActuallyWaits judges the numbers the workflow
// itself carries, which every case above overrides and none can speak for.
func TestTheShippedMirrorCopyBudgetActuallyWaits(t *testing.T) {
	env := mirrorStepEnv(t, mirrorStepBlock(t))

	for _, knob := range []struct {
		name    string
		atLeast int
		why     string
	}{
		{"COPY_ATTEMPTS", 2, "one attempt is the copy the throttle refused on runs 36512010639 and 36512597460"},
		{"COPY_DELAY_SECONDS", 1, "a zero pause spends the attempts inside the same second the throttle answered in"},
	} {
		raw, ok := env[knob.name]
		if !ok {
			t.Errorf("%s, step %q declares no %s. Declared: %v", gateWorkflow, mirrorStepName, knob.name, env)
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			t.Errorf("%s, step %q: %s is %q, which the step compares numerically: %v", gateWorkflow, mirrorStepName, knob.name, raw, err)
			continue
		}
		if value < knob.atLeast {
			t.Errorf("%s, step %q ships %s=%d, want at least %d — %s", gateWorkflow, mirrorStepName, knob.name, value, knob.atLeast, knob.why)
		}
	}
}

func mirrorStepBlock(t *testing.T) string {
	t.Helper()

	return workflowfile.Step(t, gateWorkflow, mirrorJob, workflowfile.Job(t, gateWorkflow, mirrorJob), mirrorStepName)
}

// mirrorStepEnv returns the step's literal `env:` entries; expression-valued
// ones are the harness's to supply.
func mirrorStepEnv(t *testing.T, block string) map[string]string {
	t.Helper()

	marker := "        env:\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("%s, step %q: no `env:` mapping", gateWorkflow, mirrorStepName)
	}

	env := map[string]string{}
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		if !strings.HasPrefix(line, "          ") {
			break
		}
		match := envEntry.FindStringSubmatch(strings.TrimPrefix(line, "          "))
		if match == nil || strings.Contains(match[2], "${{") {
			continue
		}
		env[match[1]] = strings.TrimSpace(match[2])
	}
	return env
}
