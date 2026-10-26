package publishorder

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The behaviour of promote-latest.yml's own steps: the copy of a release onto
// Docker Hub, the re-check right before the first write, the names the Docker
// Hub write puts the digest under, the reporting step, and what the workflow
// is not allowed to do at all. The order of the steps, the refusals, the signer
// pin and the byte-exact tag writes are in latest_test.go.

const (
	signedRef = imageName + "@" + signedDigest

	// The destination is the bare repository, the form `oras copy` documents:
	// the source is a digest, so the content lands under that digest and
	// nothing gets a tag.
	mirrorRepository = mirrorName

	// What oras and cosign print for Docker Hub's rate limit. oras words it in
	// lower case after the status; cosign keeps the registry's own code.
	orasRateLimit   = "Error response from registry: PUT \"https://registry-1.docker.io/v2/x\": response status code 429: toomanyrequests: You have reached your rate limit"
	cosignRateLimit = "Error: TOOMANYREQUESTS: You have reached your pull rate limit"
)

// TestTheReleaseIsCopiedOntoDockerHubByteForByteOrNotAtAll runs the copy step.
// The copy runs whether the mirror lacks the digest or already holds it (a
// previous copy can have left the index and not the signature bundle), between
// the source DIGEST and the bare destination repository and nothing else; a
// mirror that answers another digest for these bytes, or none, is not guessed
// at; only a rate limit is asked again.
func TestTheReleaseIsCopiedOntoDockerHubByteForByteOrNotAtAll(t *testing.T) {
	const orasStub = `n=0
sleep() { :; }
oras() { printf 'ORAS %s\n' "$*" >&2; n=$(( n + 1 )); if [ "$n" -le "${ORAS_FAILS:-0}" ]; then printf '%s\n' "${ORAS_MESSAGE:-boom}" >&2; return 1; fi; }`
	const wantCopy = "ORAS copy --recursive " + signedRef + " " + mirrorRepository

	for _, testCase := range []struct {
		name        string
		env         map[string]string
		wantCopies  int
		wantRefusal string
	}{
		{name: "the mirror lacks the digest", env: map[string]string{"STUB_DIGEST_STATUS": "404"}, wantCopies: 1},
		{name: "the mirror already holds it, and may lack its signature", env: map[string]string{"STUB_DIGEST": signedDigest}, wantCopies: 1},
		{name: "the mirror answers another digest for it", env: map[string]string{"STUB_DIGEST": hubTagDigest}, wantRefusal: "which is not that digest"},
		{name: "the mirror cannot say", env: map[string]string{"STUB_DIGEST_STATUS": "500"}, wantRefusal: "is not known"},
		{name: "a rate limit on the digest query lifts", env: map[string]string{"STUB_DIGEST_STATUS": "404", "STUB_RATE_LIMITS": "2", "STUB_RATE_URL": "/manifests/sha256:"}, wantCopies: 1},
		{name: "a rate limit on the digest query that never lifts", env: map[string]string{"STUB_DIGEST_STATUS": "404", "STUB_RATE_LIMITS": "99", "STUB_RATE_URL": "/manifests/sha256:"}, wantRefusal: "answered HTTP 429"},
		{name: "a rate limit on the copy lifts", env: map[string]string{"STUB_DIGEST_STATUS": "404", "ORAS_FAILS": "2", "ORAS_MESSAGE": orasRateLimit}, wantCopies: 3},
		{name: "a rate limit on the copy in cosign's words lifts", env: map[string]string{"STUB_DIGEST_STATUS": "404", "ORAS_FAILS": "1", "ORAS_MESSAGE": cosignRateLimit}, wantCopies: 2},
		{name: "a rate limit on the copy that never lifts", env: map[string]string{"STUB_DIGEST_STATUS": "404", "ORAS_FAILS": "99", "ORAS_MESSAGE": orasRateLimit}, wantCopies: 3, wantRefusal: "was refused by a registry's rate limit on all 3 attempts"},
		{name: "any other failure of the copy is an answer, asked once", env: map[string]string{"STUB_DIGEST_STATUS": "404", "ORAS_FAILS": "99", "ORAS_MESSAGE": "Error: unauthorized: authentication required"}, wantCopies: 1, wantRefusal: "and not on a registry's rate limit"},
		{name: "a digest that merely contains 429 is not a rate limit", env: map[string]string{"STUB_DIGEST_STATUS": "404", "ORAS_FAILS": "99", "ORAS_MESSAGE": "Error: manifest unknown: sha256:429429429429429429429429429429429429429429429429429429429429abcd"}, wantCopies: 1, wantRefusal: "and not on a registry's rate limit"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{
				"DIGEST": signedDigest, "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x",
				"RATE_LIMIT_ATTEMPTS": "3", "RATE_LIMIT_DELAY_SECONDS": "0",
				"STUB_COUNTER": filepath.ToSlash(filepath.Join(t.TempDir(), "counter")),
			}
			for key, value := range testCase.env {
				env[key] = value
			}
			output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteHubCopyStep, hubCurl+"\n"+orasStub, env)

			if got := strings.Count(output, "ORAS "); got != testCase.wantCopies {
				t.Errorf("the step ran oras %d time(s), want %d:\n%s", got, testCase.wantCopies, output)
			}
			if testCase.wantCopies > 0 && !strings.Contains(output, wantCopy+"\n") {
				t.Errorf("the copy was not %q:\n%s", wantCopy, output)
			}
			if testCase.wantRefusal != "" {
				if err == nil {
					t.Fatalf("the step went on.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantRefusal)
				return
			}
			if err != nil {
				t.Fatalf("the step failed: %v\n%s", err, output)
			}
		})
	}
}

// TestThePromotionAsksAgainBeforeItWritesLatest runs the re-check that stands
// right before the first write. A higher stable tag pushed during the minutes
// the verification takes keeps `latest`; this run then writes nothing.
func TestThePromotionAsksAgainBeforeItWritesLatest(t *testing.T) {
	const stubs = `gh() { printf 'GH %s\n' "$*" >&2; if [ -n "${GH_FAIL:-}" ]; then return 1; fi; printf '%s\n' "$GH_REFS"; }
sleep() { :; }
` + highestTagExports

	for _, testCase := range []struct {
		name        string
		refs        string
		ghFails     bool
		wantRefusal string
	}{
		{name: "still the highest", refs: refsOf("v1.9.1", "v1.9.2")},
		{name: "a higher pre-release does not count", refs: refsOf("v1.9.2", "v2.0.0-rc.1")},
		{name: "a higher stable tag was pushed meanwhile", refs: refsOf("v1.9.2", "v1.10.0"), wantRefusal: "v1.10.0 was tagged while this run verified v1.9.2"},
		{name: "a listing that cannot be read", ghFails: true, wantRefusal: "could not be listed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{"TAG": "v1.9.2", "GITHUB_REPOSITORY": "ovumcy/ovumcy-web", "GH_REFS": testCase.refs}
			if testCase.ghFails {
				env["GH_FAIL"] = "1"
			}
			output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteRecheckStep, stubs, env)

			if want := "GH api --paginate repos/ovumcy/ovumcy-web/git/matching-refs/tags/v "; !strings.Contains(output, want) {
				t.Errorf("the step did not list the repository's `v*` tags (wanted %q):\n%s", want, output)
			}
			if testCase.wantRefusal == "" {
				if err != nil {
					t.Fatalf("the step refused the highest tag: %v\n%s", err, output)
				}
				return
			}
			if err == nil {
				t.Fatalf("the step let a tag through that is no longer the highest.\n%s", output)
			}
			requireRefusalReason(t, output, testCase.wantRefusal)
		})
	}
}

// TestTheDockerHubWriteNamesTheReleaseOnlyWhereTheVerificationFoundItAbsent runs
// the write with each answer the verification can hand it. The release tag goes
// first and `latest` second, a mirror that already names the release gets only
// `latest`, and an answer that is neither is a refusal before any request.
func TestTheDockerHubWriteNamesTheReleaseOnlyWhereTheVerificationFoundItAbsent(t *testing.T) {
	const tagPut = "CURL PUT https://registry-1.docker.io/v2/" + imagePath + "/manifests/v1.9.2\n"
	const latestPut = "CURL PUT https://registry-1.docker.io/v2/" + imagePath + "/manifests/latest\n"

	for _, testCase := range []struct {
		name        string
		tagOnHub    string
		wantPuts    []string
		wantRefusal string
	}{
		{name: "the release is absent from the mirror", tagOnHub: "absent", wantPuts: []string{tagPut, latestPut}},
		{name: "the mirror already names the release", tagOnHub: "present", wantPuts: []string{latestPut}},
		{name: "no answer arrived", tagOnHub: "", wantRefusal: "did not say whether the mirror holds"},
		{name: "an answer nobody defined", tagOnHub: "maybe", wantRefusal: "did not say whether the mirror holds"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteHubMoveStep, putCurl, map[string]string{
				"DIGEST": digest, "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x", "STUB_BODY": canonicalManifest,
				"REGISTRY_USER": "publisher", "REGISTRY_PASSWORD": "secret", "TAG": "v1.9.2", "TAG_ON_HUB": testCase.tagOnHub,
				"RATE_LIMIT_ATTEMPTS": "3", "RATE_LIMIT_DELAY_SECONDS": "0",
			})

			if testCase.wantRefusal != "" {
				if err == nil {
					t.Fatalf("the step wrote without knowing which names to write.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantRefusal)
				if strings.Contains(output, "CURL ") {
					t.Errorf("the step reached the registry before it knew what to write:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("the step failed: %v\n%s", err, output)
			}
			if got := strings.Count(output, "CURL PUT "); got != len(testCase.wantPuts) {
				t.Errorf("the step wrote %d name(s), want %d:\n%s", got, len(testCase.wantPuts), output)
			}
			requireOrderedLines(t, output, trimNewlines(testCase.wantPuts))
		})
	}
}

func trimNewlines(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.TrimSuffix(line, "\n")
	}
	return out
}

// TestTheDockerHubCheckAsksAboutTheReleaseTagToo: the mirror now carries the
// release under its own name, and that name has to be the signed digest.
func TestTheDockerHubCheckAsksAboutTheReleaseTagToo(t *testing.T) {
	env := func(extra map[string]string) map[string]string {
		base := map[string]string{"DIGEST": signedDigest, "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x", "TAG": "v1.9.2", "STUB_DIGEST": signedDigest, "RATE_LIMIT_ATTEMPTS": "3", "RATE_LIMIT_DELAY_SECONDS": "0"}
		for key, value := range extra {
			base[key] = value
		}
		return base
	}

	output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteHubCheckStep, registryCurl, env(nil))
	if err != nil {
		t.Fatalf("the check refused a mirror that is right: %v\n%s", err, output)
	}
	for _, name := range []string{"latest", "v1.9.2"} {
		if want := "CURL https://registry-1.docker.io/v2/" + imagePath + "/manifests/" + name; !strings.Contains(output, want) {
			t.Errorf("the check never read %s:\n%s", want, output)
		}
	}

	output, _, err = runWorkflowStep(t, promoteWorkflow, promoteJob, promoteHubCheckStep, registryCurl, env(map[string]string{"STUB_TAG_DIGEST": hubTagDigest}))
	if err == nil {
		t.Fatalf("the check accepted a release tag that names another digest.\n%s", output)
	}
	requireRefusalReason(t, output, "docker.io/"+imagePath+":v1.9.2 resolves to "+hubTagDigest)
}

// TestTheFailureReportNeverFailsAndNamesBothRegistries runs the step that
// prints where `latest` points after a failed run. It reads anonymously, says
// what each registry answered, and no answer — a refused token, a curl that
// dies — may turn it into a second failure.
func TestTheFailureReportNeverFailsAndNamesBothRegistries(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		preamble string
		env      map[string]string
		want     []string
	}{
		{
			name: "both registries answer", preamble: registryCurl, env: map[string]string{"STUB_DIGEST": signedDigest},
			want: []string{"ghcr.io/" + imagePath + ":latest -> " + signedDigest + " (HTTP 200)", "docker.io/" + imagePath + ":latest -> " + signedDigest + " (HTTP 200)"},
		},
		{
			name: "the token requests are refused", preamble: registryCurl, env: map[string]string{"STUB_TOKEN_STATUS": "500"},
			want: []string{"ghcr.io/" + imagePath + ":latest -> unreadable", "docker.io/" + imagePath + ":latest -> unreadable"},
		},
		{
			name: "curl itself dies", preamble: "curl() { return 7; }",
			want: []string{"ghcr.io/" + imagePath + ":latest -> unreadable", "docker.io/" + imagePath + ":latest -> unreadable"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{"IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x"}
			for key, value := range testCase.env {
				env[key] = value
			}
			output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteReportStep, testCase.preamble, env)
			if err != nil {
				t.Fatalf("the report failed the job it reports on: %v\n%s", err, output)
			}
			for _, want := range testCase.want {
				if !strings.Contains(output, want) {
					t.Errorf("the report never said %q:\n%s", want, output)
				}
			}
		})
	}

	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	if block := "\n" + stepBlockOf(t, job, promoteReportStep); !strings.Contains(block, "\n        if: failure()\n") {
		t.Errorf("%s, step %q does not run `if: failure()`, so a failed run prints nothing", promoteWorkflow, promoteReportStep)
	}
}

func stepBlockOf(t *testing.T, job, name string) string {
	t.Helper()

	return withoutComments(workflowfile.Step(t, promoteWorkflow, promoteJob, job, name))
}

// TestThePromotionNeverSignsAndCopiesOnlyWithOras holds what makes the Docker
// Hub copy the release and not a look-alike: this workflow's own identity signs
// nothing, builds nothing, and the one tool that moves bytes is `oras copy
// --recursive` between two digest references, in the copy step.
func TestThePromotionNeverSignsAndCopiesOnlyWithOras(t *testing.T) {
	text := withoutComments(workflowfile.Read(t, promoteWorkflow))

	for _, forbidden := range []string{
		"cosign sign", "cosign attest", "cosign copy", "cosign clean", "cosign save", "cosign load",
		"attest-build-provenance", "id-token", "imagetools", "buildx", "docker build", "docker push", "build-push-action",
		"oras push", "oras tag", "oras attach", "oras manifest", "oras blob",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("%s names %q outside a comment: this workflow signs nothing and rebuilds no manifest", promoteWorkflow, forbidden)
		}
	}

	// A call is `oras` followed by one of the tool's subcommands, wherever it
	// stands on the line (it runs behind the rate-limit wrapper); the words
	// "the oras output" in a message are not one.
	orasCall := regexp.MustCompile(`(?m)\boras\s+(copy|cp|push|pull|tag|attach|discover|resolve|login|logout|manifest|blob|repo|backup|restore)\b(.*)$`)
	calls := orasCall.FindAllStringSubmatch(text, -1)
	if len(calls) != 1 {
		t.Fatalf("%s runs oras %d times, want exactly the one copy", promoteWorkflow, len(calls))
	}
	// The destination is the bare repository `oras copy` documents (`<to>[:<tag>]`):
	// a digest source lands under its own digest, with no tag.
	if calls[0][1] != "copy" || !strings.HasPrefix(strings.TrimSpace(calls[0][2]), `--recursive "ghcr.io/${IMAGE_PATH}@${DIGEST}" "docker.io/${IMAGE_PATH}";`) {
		t.Errorf("%s: the oras call is %q, want `oras copy --recursive` from the GHCR digest to the bare Docker Hub repository", promoteWorkflow, strings.TrimSpace(calls[0][0]))
	}
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	if !strings.Contains(stepBlockOf(t, job, promoteHubCopyStep), "oras copy --recursive") {
		t.Errorf("%s: the oras copy is not in step %q", promoteWorkflow, promoteHubCopyStep)
	}
}

// TestNoPromotionStepPutsABearerTokenOnACommandLine enumerates the steps from
// the workflow and refuses a bearer header given to curl as an argument. The
// behavioural half is in runWorkflowStep, which fails any step run there whose
// curl sees one; this half reads every step, the ones no fixture runs included,
// and holds the allowed spelling (`-H "@${auth}"`) to the steps that need it.
func TestNoPromotionStepPutsABearerTokenOnACommandLine(t *testing.T) {
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	argvBearer := regexp.MustCompile(`-H\s+"?Authorization:\s*Bearer`)

	for _, name := range promoteStepNames(t) {
		if argvBearer.MatchString(stepBlockOf(t, job, name)) {
			t.Errorf("%s, step %q gives curl a bearer token as an argument, where the process table shows it", promoteWorkflow, name)
		}
	}
	for _, name := range []string{promoteResolveStep, promoteHubCopyStep, promoteHubVerifyStep, promoteGhcrMoveStep, promoteGhcrCheckStep, promoteHubMoveStep, promoteHubCheckStep, promoteReportStep} {
		if !strings.Contains(stepBlockOf(t, job, name), `-H "@${auth}"`) {
			t.Errorf("%s, step %q reads the registry without the header file, so this guard judged nothing about it", promoteWorkflow, name)
		}
	}
}

// TestNoPromotionScriptSplicesAnExpression: the dispatch input reaches a script
// only through a quoted `env:` entry. An expression inside `run:` is text the
// shell parses, and the tag is whatever the dispatcher typed.
func TestNoPromotionScriptSplicesAnExpression(t *testing.T) {
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)

	for _, name := range promoteStepNames(t) {
		block := stepBlockOf(t, job, name)
		_, script, found := strings.Cut(block, "        run: |\n")
		if found && strings.Contains(script, "${{") {
			t.Errorf("%s, step %q has a `${{ … }}` expression inside its script; it belongs in `env:`", promoteWorkflow, name)
		}
	}
}

var (
	outputReference = regexp.MustCompile(`steps\.([A-Za-z_][A-Za-z0-9_]*)\.outputs\.([A-Za-z_][A-Za-z0-9_]*)`)
	stepIDLine      = regexp.MustCompile(`(?m)^        id: ([A-Za-z_][A-Za-z0-9_]*)$`)
)

// promoteStepEnvironment returns one step's whole `env:` mapping, expressions
// included — the wiring this file judges.
func promoteStepEnvironment(t *testing.T, name string) map[string]string {
	t.Helper()

	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	prefixed := "\n" + stepBlockOf(t, job, name)
	_, rest, found := strings.Cut(prefixed, "\n        env:\n")
	if !found {
		t.Fatalf("%s, step %q declares no `env:`", promoteWorkflow, name)
	}
	env := map[string]string{}
	for _, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		if match := envEntry.FindStringSubmatch(strings.TrimPrefix(line, "          ")); match != nil {
			env[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return env
}

// TestThePromotionStepsAreWiredToTheValuesTheyJudge reads the exact expression
// each step's `env:` hands its script. The scripts are exercised with values
// the fixtures supply, so a variable wired to the wrong output — the digest
// from a step that never writes one, the identity pattern from nowhere — runs
// green there and empty or wrong in the real run.
func TestThePromotionStepsAreWiredToTheValuesTheyJudge(t *testing.T) {
	const (
		tagExpr     = "${{ inputs.tag }}"
		digestExpr  = "${{ steps.release.outputs.digest }}"
		pathExpr    = "${{ steps.image.outputs.path }}"
		patternExpr = "${{ steps.verify.outputs.identity_regexp }}"
	)

	for _, wiring := range []struct {
		step string
		env  map[string]string
	}{
		{promoteRefuseStep, map[string]string{"TAG": tagExpr}},
		{promoteResolveStep, map[string]string{"TAG": tagExpr, "IMAGE_PATH": pathExpr}},
		{promoteVerifyStep, map[string]string{"TAG": tagExpr, "IMAGE_DIGEST": "${{ steps.image.outputs.name }}@${{ steps.release.outputs.digest }}"}},
		{promoteHubCopyStep, map[string]string{"DIGEST": digestExpr, "IMAGE_PATH": pathExpr}},
		{promoteHubVerifyStep, map[string]string{"TAG": tagExpr, "DIGEST": digestExpr, "IMAGE_PATH": pathExpr, "IDENTITY_REGEXP": patternExpr}},
		{promoteRecheckStep, map[string]string{"TAG": tagExpr}},
		{promoteGhcrMoveStep, map[string]string{"DIGEST": digestExpr, "IMAGE_PATH": pathExpr}},
		{promoteGhcrCheckStep, map[string]string{"DIGEST": digestExpr, "IMAGE_PATH": pathExpr}},
		{promoteHubMoveStep, map[string]string{"DIGEST": digestExpr, "IMAGE_PATH": pathExpr, "TAG": tagExpr, "TAG_ON_HUB": "${{ steps.hubverify.outputs.tag_on_hub }}"}},
		{promoteHubCheckStep, map[string]string{"DIGEST": digestExpr, "IMAGE_PATH": pathExpr, "TAG": tagExpr}},
	} {
		got := promoteStepEnvironment(t, wiring.step)
		for key, want := range wiring.env {
			if got[key] != want {
				t.Errorf("%s, step %q wires %s to %q, want %q", promoteWorkflow, wiring.step, key, got[key], want)
			}
		}
	}

	// Every output a step reads must be one an earlier step of that id writes,
	// so a renamed output cannot leave a reader on the empty string.
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	text := withoutComments(job)
	blocks := strings.Split(text, "\n      - name: ")
	writers := map[string]string{}
	for _, block := range blocks[1:] {
		if match := stepIDLine.FindStringSubmatch(block); match != nil {
			writers[match[1]] = block
		}
	}
	seen := map[string]bool{}
	for _, match := range outputReference.FindAllStringSubmatch(text, -1) {
		id, output := match[1], match[2]
		seen[id+"."+output] = true
		block, ok := writers[id]
		if !ok {
			t.Errorf("%s reads steps.%s.outputs.%s, and no step has `id: %s`", promoteWorkflow, id, output, id)
			continue
		}
		if !regexp.MustCompile(`[^A-Za-z0-9_]` + output + `=`).MatchString(block) {
			t.Errorf("%s reads steps.%s.outputs.%s, and that step writes no `%s=` to its output file", promoteWorkflow, id, output, output)
		}
	}
	for _, want := range []string{"release.digest", "image.path", "image.name", "verify.identity_regexp", "hubverify.tag_on_hub"} {
		if !seen[want] {
			t.Errorf("%s reads no steps.%s output, so the wiring guard above judged less than it claims", promoteWorkflow, strings.Replace(want, ".", ".outputs.", 1))
		}
	}
}
