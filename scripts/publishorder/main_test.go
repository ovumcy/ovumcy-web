// Package publishorder holds the publish job to the one ordering that makes
// its promises true: no public tag exists until the digest under it has been
// signed, attested, and read back.
//
// The `publish` job in .github/workflows/docker-image.yml used to push with
// `tags: ${{ steps.meta.outputs.tags }}`, which creates `vX.Y.Z`, `latest`,
// `main` and `sha-…` at the moment of the push, and only then installed Cosign,
// signed, and attested. Between them sat a check that the image pulls
// anonymously — a step that confirms the unsigned release is reachable by
// anyone. A failure in Cosign or in the attestation therefore turned the
// workflow red with the release already published, unsigned and without
// provenance. README.md tells operators the opposite in as many words: every
// published image is Cosign-signed, carries a SLSA provenance attestation, and
// ships an SBOM. A red workflow retracts nothing; nothing deletes a tag that
// already resolves.
//
// So the job pushes by DIGEST with no tag at all, signs and attests that
// digest, verifies both against the registry, and only then writes the tags —
// from the manifest bytes that already hash to the signed digest, which is what
// makes the promotion incapable of pointing a tag anywhere else.
//
// What is guarded here, each because the others alone read green over the
// defect. Deliberately not opened with a count: the list has grown four times
// and a number in front of it is one more claim to keep true.
//
//   - the ORDER, statically, and with it the STEP LIST that runs before the
//     promotion. Searching earlier steps for `steps.meta.outputs.tags` catches
//     a step that reads the derived list and misses one that spells a tag out;
//     nothing can enumerate every way a step might write a tag, so the window
//     is closed from the other end instead — a step that appears in it and is
//     not on the reviewed list fails, whatever it does;
//   - what the three steps between the push and the promotion ACT ON. The
//     order rule reads their names, and a name is all it reads: a signing step
//     emptied to `echo ok` keeps its place in the sequence and its position
//     proves nothing;
//   - the PROMOTION and the VERIFICATION, by running their real scripts over a
//     stubbed registry. A guard that only read the order would pass over a
//     promotion that writes a tag it never checked, or a verification that
//     accepts an alias resolving to a digest nobody signed;
//   - the DOCKER HUB MIRROR, at both ends. The tail of the job is pinned the
//     way the window before the promotion is, so nothing reaches a second
//     registry until the first one's release is complete; and the copy and
//     its check run over that same stub, because a mirror that resolved a tag
//     rather than the signed digest, or one whose signature never travelled
//     with it, is a tag on a public registry that no operator can verify;
//   - the TOKEN PARSE, against a real interpreter rather than that stub, which
//     shadows it everywhere else. It is the only place the line both registry
//     steps pull the bearer token out of the registry's answer with is
//     executed at all, and it holds the two steps to one spelling of it;
//   - the IDENTITY PATTERN the signature is checked against, by running the
//     verify step with `cosign` and `gh` shadowed. The repository is spliced
//     into a regular expression there, and a dot left unescaped in it matches
//     any character — in the one field that check exists to pin;
//   - the IMAGE NAME every later step reads, by running the step that derives
//     it. The fixtures below hand `IMAGE_NAME` and `IMAGE_PATH` in as values,
//     which is what makes them fixtures — and what would otherwise leave the
//     line computing them the one `run:` block in the job nothing executes;
//   - the READER this file reaches all of that through. `envConstants` takes a
//     constant off the workflow rather than restating it, and a step block
//     begins straight after the line naming the step — so a step whose first
//     key is `env:` has no newline in front of it to match on.
//
// What is NOT provable here is the end-to-end negative — breaking Cosign on a
// real tag push and observing that no public alias appears. That needs a
// registry and a release tag; the order rule below is what stands in for it in
// CI, and it is the property that experiment would be testing.
package publishorder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

const (
	publishWorkflow = ".github/workflows/docker-image.yml"
	publishJob      = "publish"

	resolveStep = "Resolve the registry image name"
	pushStep    = "Push the image by digest, under no public tag"
	signStep    = "Sign the pushed digest"
	attestStep  = "Attest build provenance"
	verifyStep  = "Verify the signature and provenance before promoting"
	promoteStep = "Promote the signed digest to its public tags"
	publicStep  = "Verify every public tag anonymously and against the signed digest"

	installCosignStep = "Install Cosign"

	mirrorLoginStep  = "Log in to Docker Hub for the mirror"
	mirrorStep       = "Mirror the signed digest to Docker Hub"
	mirrorVerifyStep = "Verify every mirrored tag anonymously and against the signed digest"
)

var (
	stepName = regexp.MustCompile(`(?m)^      - name: (.+)$`)
	envEntry = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*): (.*)$`)
)

// canonicalManifest is the exact bytes the stub answers when the signed
// digest is read back, and digest is computed from THOSE bytes rather than
// asserted independently of them: a digest fixed in isolation is a value the
// promotion and the public-check stub could each satisfy without agreeing on
// what was actually signed, which is the failure this package exists to rule
// out. otherManifest is a different stored object; the fixtures hand the stub
// that object rather than a "wrong" digest chosen by the test, and the stub
// hashes it for real, because a mismatch the test wrote out itself proves
// nothing about a registry comparison.
const (
	canonicalManifest = `{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":1}]}`
	otherManifest     = `{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`
)

var digest = "sha256:" + sha256Hex(canonicalManifest)

// sha256Hex is the same computation the stub performs in shell (`sha256sum`)
// over a tag's actually-stored body, so a package-level `digest` and the
// stub's own runtime answer are the same function applied to the same bytes,
// not two independently maintained values that happen to agree today.
func sha256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// The image the fixtures run against. The workflow derives both from
// `github.repository`, lowercased; the two spellings are what the steps
// consume — the full reference a tag is checked to belong to, and the registry
// path an API call is made against.
const (
	imageName = "ghcr.io/ovumcy/ovumcy-web"
	imagePath = "ovumcy/ovumcy-web"

	// The Docker Hub mirror is the same path under the other registry, which
	// is what lets the pull-count badge in README.md name it, and what makes
	// `docker.io/<path>` derivable rather than a second spelling to maintain.
	mirrorName = "docker.io/" + imagePath

	// The signer identity the mirror's check is handed. The workflow builds it
	// once, in the step that verifies GHCR, and passes it on as an output —
	// `TestTheIdentityPatternPinsEveryCharacterOfTheRepository` is what holds
	// that construction honest, and all the mirror owes is that it passes ON
	// what it was given rather than deciding for itself.
	//
	// Which is why this is deliberately NOT the pattern that step produces for
	// this repository. Handed the real one, a mirror step that ignored its
	// input and spelled the identity out as a literal would satisfy every
	// assertion here — and would then pin a hard-coded owner in any fork.
	// A value no correct step could have invented cannot be satisfied that way.
	identityRegexp = `^stub-identity-the-mirror-must-pass-through@`

	// The attempt budget the mirror fixtures run under. Deliberately smaller
	// than the workflow's, and not read from it: what the step owes is to wait
	// out a registry that answers late and to refuse one that never answers,
	// and a fixture that took the number off the workflow would agree with
	// whatever that number became — including zero, which is no waiting at all.
	verifyAttempts = 3

	// What cosign prints when the registry holds no signature for the digest
	// YET — the one refusal the step may ask about again. Spelled here as
	// cosign spells it (`ErrNoSignaturesFound`, `pkg/cosign/verify.go`),
	// because the step greps for it and a fixture inventing its own wording
	// would prove the retry works on a string nothing ever emits.
	listingNotReady = "no signatures found"
)

// TestNoPublicTagIsCreatedBeforeTheSignature is the order rule. It is written
// against the step LIST rather than against any one step's text: the defect was
// not a wrong step, it was three correct steps in the wrong order.
func TestNoPublicTagIsCreatedBeforeTheSignature(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	steps := stepNames(t, job)
	index := func(name string) int {
		for i, step := range steps {
			if step == name {
				return i
			}
		}
		t.Fatalf("%s, job %q: no step named %q. The publish order is what this package guards, and a renamed step leaves it guarding nothing — rename it here too, deliberately.", publishWorkflow, publishJob, name)
		return -1
	}

	push := index(pushStep)
	sign := index(signStep)
	attest := index(attestStep)
	verify := index(verifyStep)
	promote := index(promoteStep)
	public := index(publicStep)

	for _, ordered := range []struct {
		earlier, later int
		earlierName    string
		laterName      string
		because        string
	}{
		{push, sign, pushStep, signStep, "there is nothing to sign until the digest is pushed"},
		{sign, promote, signStep, promoteStep, "a tag written before the signature is an unsigned public release for as long as the signing step takes, and forever if it fails"},
		{attest, promote, attestStep, promoteStep, "provenance is promised for every published image, so the alias must not exist before the attestation does"},
		{verify, promote, verifyStep, promoteStep, "the signature and the attestation are two API calls that reported success; the promotion is gated on reading them back, not on their exit codes"},
		{promote, public, promoteStep, publicStep, "the public check reads the tags the promotion writes"},
	} {
		if ordered.earlier >= ordered.later {
			t.Errorf("%s, job %q: %q runs at or after %q, and %s",
				publishWorkflow, publishJob, ordered.earlierName, ordered.laterName, ordered.because)
		}
	}

	// The push must carry no tag of its own. `push-by-digest` is what leaves the
	// pushed index unnamed; a `tags:` entry beside it puts the public alias back
	// in front of the signature no matter what order the steps are in, and would
	// read green above.
	// Comments stripped here too, and for a sharper reason than below: the
	// comment above this step explains push-by-digest at length, so a check
	// reading it would stay green over an `outputs:` line that had lost the
	// option — the guard would then be satisfied by its own rationale.
	block := withoutComments(stepBlock(t, job, pushStep))
	if !strings.Contains(block, "push-by-digest=true") {
		t.Errorf("%s, step %q does not push by digest, so the push itself names the image and the ordering above buys nothing", publishWorkflow, pushStep)
	}
	for _, forbidden := range []string{"\n          tags:", "\n          push: true"} {
		if strings.Contains(block, forbidden) {
			t.Errorf("%s, step %q carries `%s`, which creates the public alias at push time — before the signature exists",
				publishWorkflow, pushStep, strings.TrimSpace(forbidden))
		}
	}

	// And no step before the promotion may reach the tag list at all. This is
	// the rule that generalises: it is not about `build-push-action`, it is
	// about anything at all learning the public names early enough to write one.
	//
	// Comments are stripped first. The steps here carry long ones, and the
	// comment above the push names `steps.meta.outputs.tags` precisely because
	// that is what it used to carry — a rule that read comments would fire on
	// the sentence explaining why the code does not do the thing.
	for i, name := range steps {
		if i >= promote {
			break
		}
		if strings.Contains(withoutComments(stepBlock(t, job, name)), "steps.meta.outputs.tags") {
			t.Errorf("%s, job %q: step %q reads the public tag list and runs before %q. Only the promotion may hold those names",
				publishWorkflow, publishJob, name, promoteStep)
		}
	}
}

// TestOnlyReviewedStepsRunBeforeThePromotion closes the window from the other
// end. The rule above asks whether a step READS the derived tag list, which is
// how the defect was actually written; a step that spells `<image>:latest` out
// and pushes it reads nothing and creates the same public alias before the
// signature. No pattern can enumerate every way a step might write a tag, so
// what is pinned instead is which steps may run there at all: a step added to
// that window fails here until it is put on this list deliberately, and putting
// it on the list is the review this guard exists to force.
func TestOnlyReviewedStepsRunBeforeThePromotion(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	steps := stepNames(t, job)

	promote := slices.Index(steps, promoteStep)
	if promote < 0 {
		t.Fatalf("%s, job %q: no step named %q, so this guard cannot find the window it is meant to hold", publishWorkflow, publishJob, promoteStep)
	}

	want := []string{
		"Checkout",
		"Set up QEMU",
		"Set up Docker Buildx",
		"Log in to Docker Hub",
		"Log in to GHCR",
		resolveStep,
		"Extract Docker metadata",
		"Build runtime image for the pre-publish scan",
		"Pull Trivy image",
		"Scan the image before publishing it",
		pushStep,
		installCosignStep,
		signStep,
		attestStep,
		verifyStep,
	}

	if got := steps[:promote]; !slices.Equal(got, want) {
		t.Errorf("%s, job %q runs these steps before %q:\n  %v\nand this guard has reviewed:\n  %v\nEvery step in that window runs while the digest is signed but nothing public points at it, so a new one there has to be read for whether it can create a tag — then added here. Reordering or removing one is the same question in reverse",
			publishWorkflow, publishJob, promoteStep, got, want)
	}
}

// TestTheDockerHubMirrorRunsAfterTheWholeGhcrReleaseIsVerified holds the
// second registry to the far side of every check the first one gets. The
// window guarded above is the one before the promotion; this is its mirror
// image at the other end of the job, and it exists because a mirror is the
// cheapest way to reintroduce the defect this package was written for — an
// alias on a registry nothing in this run has read back, carrying whatever
// bytes a second build produced.
//
// The tail is pinned whole rather than by a pairwise order, for the reason the
// pre-promotion window is: a step appended after the public check runs with
// this job's registry credentials in the environment and can write a tag
// anywhere, so a new one there has to be read for that and then added here.
func TestTheDockerHubMirrorRunsAfterTheWholeGhcrReleaseIsVerified(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	steps := stepNames(t, job)

	public := slices.Index(steps, publicStep)
	if public < 0 {
		t.Fatalf("%s, job %q: no step named %q, so this guard cannot find the window it is meant to hold", publishWorkflow, publishJob, publicStep)
	}

	want := []string{publicStep, mirrorLoginStep, mirrorStep, mirrorVerifyStep}
	if got := steps[public:]; !slices.Equal(got, want) {
		t.Errorf("%s, job %q ends on these steps:\n  %v\nand this guard has reviewed:\n  %v\nNothing may reach Docker Hub before %q has resolved every GHCR alias to the signed digest, and the mirror is not published until %q has resolved every Docker Hub alias to the same one",
			publishWorkflow, publishJob, got, want, publicStep, mirrorVerifyStep)
	}
}

// TestTheMirrorCopiesTheSignedDigestUnderOnlyItsOwnTags runs the copy step
// with `cosign` shadowed. The order rule above reads step names, and a name is
// all it reads: a mirror step that resolved `<image>:latest` and pushed
// whatever came back would keep its place in the sequence and put unsigned
// bytes under a public tag on the other registry.
func TestTheMirrorCopiesTheSignedDigestUnderOnlyItsOwnTags(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	script := stepScript(t, job, mirrorStep)

	// `cosign` is shadowed rather than stubbed per endpoint: what this step
	// owes is WHICH invocations it makes and IN WHICH ORDER, and the arguments
	// are the whole of that.
	//
	// `verifyFailures` is the count of `verify` calls the shim refuses before
	// answering — the registry's own behaviour, not the step's. Docker Hub
	// accepts a signature before it lists it, so "not there yet" and "not
	// there at all" reach this step as the same verdict and are told apart
	// only by asking again; a fixture that could not distinguish them would
	// pass a step that waits forever and a step that does not wait at all.
	//
	// `failureMessage` is what cosign PRINTS when it refuses, and it decides
	// whether the step may ask again at all: only a missing listing earns a
	// retry. A shim that refused silently would let a step retry every
	// refusal and still read green here.
	preamble := func(verifyFailures int, failureMessage string) string {
		return `n=0` + "\n" +
			`cosign() { printf 'COSIGN %s\n' "$*" >&2; if [ "$1" = verify ]; then n=$(( n + 1 )); ` +
			`if [ "$n" -gt ` + strconv.Itoa(verifyFailures) + ` ]; then return 0; fi; ` +
			`printf '%s\n' ` + shellQuote(failureMessage) + ` >&2; return 1; fi; }`
	}

	for _, testCase := range []struct {
		name    string
		tagRefs string
		// identity is what the step is handed as the signer pattern. The
		// zero value is the fixture's pattern; emptyIdentity asks for the
		// value a workflow expression resolves to when the step that builds
		// it did not run.
		emptyIdentity bool
		// verifyFailures is how many `cosign verify` calls the registry
		// refuses before it answers. Below `verifyAttempts` the step must
		// wait it out and publish; at or above it the step must refuse.
		verifyFailures int
		// failureMessage is what those refusals print. The zero value is the
		// listing-not-ready verdict, the only one a retry is for.
		failureMessage string
		// wantVerifyCalls, when set, is exactly how many times `cosign verify`
		// may run. It is what separates waiting from retrying blindly: a
		// refusal that is an ANSWER must be asked once and no more.
		wantVerifyCalls int
		// wantCosign is the ORDERED sequence of invocations. Order is the
		// property, not a detail of it: an alias written before the signature
		// is read back is a public tag on a registry nobody can verify for as
		// long as the signing call keeps failing, which is the defect this
		// package exists to refuse — on the mirror it would be the same defect.
		wantCosign  []string
		wantRefusal string
	}{
		{
			name:    "the tags this run derived",
			tagRefs: imageName + ":v2.0.0\n" + imageName + ":latest",
			// The bytes cross once, to a DIGEST destination, so Docker Hub
			// holds the manifest under no alias at all; the mirror is signed
			// and read back there; only then is an alias written, and each
			// from the mirror's own digest, which is the same manifest and
			// costs a manifest write rather than a second transfer. Every
			// source is a digest, which is the part that matters.
			wantCosign: []string{
				"COSIGN copy --force " + imageName + "@" + digest + " " + mirrorName + "@" + digest,
				"COSIGN sign --yes " + mirrorName + "@" + digest,
				"COSIGN verify --certificate-identity-regexp " + identityRegexp + " --certificate-oidc-issuer https://token.actions.githubusercontent.com " + mirrorName + "@" + digest,
				"COSIGN copy --force " + mirrorName + "@" + digest + " " + mirrorName + ":v2.0.0",
				"COSIGN copy --force " + mirrorName + "@" + digest + " " + mirrorName + ":latest",
			},
		},
		{
			// An empty pattern is not a loose check, it is no check: cosign
			// matches every certificate identity against it. Refused before
			// cosign is reached, so a mirror signed by another workflow
			// cannot be accepted while this reads green.
			name:          "the signer identity pattern arrived empty",
			tagRefs:       imageName + ":v2.0.0",
			emptyIdentity: true,
			wantRefusal:   "identity pattern reached this step empty",
		},
		{
			// The registry answers late, which is the ordinary case and not a
			// failure: Docker Hub accepted the signature and had not listed it
			// yet. The step waits and publishes — and the aliases appearing
			// after the LAST verify is what says it waited rather than
			// publishing on a verdict it never got.
			name:           "the referrers listing catches up before the attempts run out",
			tagRefs:        imageName + ":v2.0.0",
			verifyFailures: verifyAttempts - 1,
			wantCosign: []string{
				"COSIGN sign --yes " + mirrorName + "@" + digest,
				"COSIGN verify --certificate-identity-regexp " + identityRegexp + " --certificate-oidc-issuer https://token.actions.githubusercontent.com " + mirrorName + "@" + digest,
				"COSIGN verify --certificate-identity-regexp " + identityRegexp + " --certificate-oidc-issuer https://token.actions.githubusercontent.com " + mirrorName + "@" + digest,
				"COSIGN copy --force " + mirrorName + "@" + digest + " " + mirrorName + ":v2.0.0",
			},
		},
		{
			// The signing call reported success and left nothing verifiable
			// behind it, and waiting did not change that. The manifest is on
			// Docker Hub by then — under no alias, which is what makes this a
			// red run rather than a published mirror nobody can verify.
			name:           "the mirrored signature never appears",
			tagRefs:        imageName + ":v2.0.0\n" + imageName + ":latest",
			verifyFailures: verifyAttempts,
			wantRefusal:    "carries no signature this repository issued",
		},
		{
			// A refusal that is an ANSWER. cosign found something and would
			// not accept it, which the budget cannot change: asking again
			// spends the whole wait and then reports an absence, so a mirror
			// signed by a stranger would reach the log wearing the wording a
			// slow registry produces. Asked once, refused on its own terms.
			name:            "the mirror is signed by an identity this repository did not issue",
			tagRefs:         imageName + ":v2.0.0",
			verifyFailures:  verifyAttempts * 10,
			failureMessage:  "none of the signatures were verified against the certificate identity",
			wantVerifyCalls: 1,
			wantRefusal:     "did not verify against this repository's signer identity",
		},
		{
			// The same list the promotion and the public check refuse, refused
			// here for the same reason and before the first copy: a foreign
			// reference found halfway would leave the mirror holding the tags
			// written ahead of it.
			name:        "a reference from another repository",
			tagRefs:     imageName + ":v2.0.0\ndocker.io/someone/else:latest",
			wantRefusal: "not a tag of",
		},
		{
			// An empty list is not a mirror of nothing, it is a run that would
			// report a mirror published having copied nothing at all.
			name:        "no tag at all",
			tagRefs:     "\n   \n",
			wantRefusal: "no tag to mirror",
		},
		{
			// Same fixture the promotion step guards against: a reference
			// with nothing after the colon matches the "is this image"
			// prefix check above and, absent this guard, would queue a
			// blank tag — landing a dangling `${MIRROR_NAME}:` copy
			// destination instead of failing loud.
			name:        "the metadata step derived no tag at all",
			tagRefs:     imageName + ":",
			wantRefusal: "names this image and no tag",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			identity := identityRegexp
			if testCase.emptyIdentity {
				identity = ""
			}

			failureMessage := testCase.failureMessage
			if failureMessage == "" {
				failureMessage = listingNotReady
			}

			command := runBashScript(t, bash, job, mirrorStep, preamble(testCase.verifyFailures, failureMessage)+"\n"+script)
			command.Env = append(os.Environ(),
				"DIGEST="+digest,
				"TAG_REFS="+testCase.tagRefs,
				"IMAGE_NAME="+imageName,
				"MIRROR_NAME="+mirrorName,
				"IDENTITY_REGEXP="+identity,
				// The attempt budget is the fixture's, not the workflow's:
				// the real one waits minutes on purpose. The DELAY is what a
				// test cannot afford, and driving it to zero is why these are
				// plain constants in the step's `env:` rather than an
				// expression nothing local can override. What the SHIPPED
				// numbers must be is
				// `TestTheShippedReadBackBudgetActuallyWaits`'s subject.
				"VERIFY_ATTEMPTS="+strconv.Itoa(verifyAttempts),
				"VERIFY_DELAY_SECONDS=0",
			)
			output, err := command.CombinedOutput()

			if testCase.wantVerifyCalls > 0 {
				if got := strings.Count(string(output), "COSIGN verify "); got != testCase.wantVerifyCalls {
					t.Errorf("the step ran `cosign verify` %d time(s), want %d — a refusal that is an answer must not be asked again:\n%s",
						got, testCase.wantVerifyCalls, output)
				}
			}

			if testCase.wantRefusal != "" {
				if err == nil {
					t.Fatalf("the step mirrored a tag list it should have refused.\n%s", output)
				}
				requireRefusalReason(t, string(output), testCase.wantRefusal)

				// Every refusal but one is over an INPUT — the tag list, the
				// identity pattern — and the step judges all of them before it
				// reaches a registry, so nothing at all may have been run. The
				// weaker "no alias was written" would pass a step that moved
				// the cross-registry copy above the list it has not judged yet.
				if testCase.verifyFailures == 0 {
					if strings.Contains(string(output), "COSIGN") {
						t.Errorf("the step reached a registry before it had judged its own inputs:\n%s", output)
					}
					return
				}

				// The one refusal that happens after the copy and the signing
				// call: those write nothing an operator can name, and a `:tag`
				// destination is the thing a red run cannot retract.
				if strings.Contains(string(output), "COSIGN copy --force "+mirrorName+"@"+digest+" "+mirrorName+":") {
					t.Errorf("the step wrote a mirrored alias on a run it refused, and a red run retracts no tag:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("the step failed on %q: %v\n%s", testCase.tagRefs, err, output)
			}
			requireOrderedLines(t, string(output), testCase.wantCosign)
		})
	}
}

// TestTheShippedReadBackBudgetActuallyWaits judges the numbers the workflow
// ITSELF carries, which every fixture above overrides and therefore none of
// them can speak for. The defect it refuses is a quiet one: `VERIFY_ATTEMPTS`
// trimmed to 1 restores the one-shot read-back that lost the race against
// Docker Hub's referrers listing on run 35606394489, and the whole package
// stays green, because each case hands the step a budget of its own.
//
// The floor is "more than once, and with a pause worth having" rather than a
// figure copied off the workflow — a bound restating the value it guards
// agrees with whatever that value becomes.
func TestTheShippedReadBackBudgetActuallyWaits(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	env := declaredEnv(t, job, mirrorStep)

	for _, knob := range []struct {
		name    string
		atLeast int
		why     string
	}{
		{
			name:    "VERIFY_ATTEMPTS",
			atLeast: 2,
			why:     "one attempt is the one-shot check that asked before the listing existed",
		},
		{
			name:    "VERIFY_DELAY_SECONDS",
			atLeast: 1,
			why:     "a zero pause spends the attempts inside the same second and waits for nothing",
		},
	} {
		t.Run(knob.name, func(t *testing.T) {
			raw, ok := env[knob.name]
			if !ok {
				t.Fatalf("the mirror step declares no %s, so the read-back's budget is whatever `set -u` refuses. Declared: %v", knob.name, env)
			}
			value, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				t.Fatalf("%s is %q, which the step compares numerically: %v", knob.name, raw, err)
			}
			if value < knob.atLeast {
				t.Errorf("the mirror step ships %s=%d, want at least %d — %s", knob.name, value, knob.atLeast, knob.why)
			}
		})
	}
}

// TestTheMirrorCheckRefusesAMirrorThatIsNotTheSignedDigest runs the mirror's
// public check against the stub, as the GHCR one is run against it. An answer
// from GHCR is not an answer about Docker Hub: the copy is a second registry's
// account of the same bytes, and this is the step that says the account agrees
// — every mirrored alias resolving anonymously, and resolving to the digest
// this run signed, or no mirror reported published at all. The mirror's own
// signature is not this step's subject: it is issued and read back before the
// first alias exists, which `TestTheMirrorCopiesTheSignedDigestUnderOnlyItsOwnTags`
// is what holds to that order.
func TestTheMirrorCheckRefusesAMirrorThatIsNotTheSignedDigest(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	tags := imageName + ":v2.0.0\n" + imageName + ":latest"

	bothResolve := func(mutate func(*registry)) registry {
		r := defaultRegistry()
		r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
		if mutate != nil {
			mutate(&r)
		}
		return r
	}

	for _, testCase := range []struct {
		name     string
		registry registry
		// tags overrides the pair above when a case is about the references
		// themselves rather than about what they resolve to.
		tags        string
		wantRefusal bool
		// wantError names the branch, so a refusal for another reason is not
		// mistaken for this one.
		wantError string
	}{
		{
			name:     "both mirrored aliases resolve to the signed digest",
			registry: bothResolve(nil),
		},
		{
			// The copy landed, and landed different. Whatever produced those
			// bytes, the signature this run issued does not cover them.
			name: "one mirrored alias resolves to something else",
			registry: bothResolve(func(r *registry) {
				r.resolves["latest"] = otherManifest
			}),
			wantRefusal: true,
			wantError:   "not to the signed digest",
		},
		{
			name: "a mirrored alias is not anonymously readable",
			registry: bothResolve(func(r *registry) {
				r.headStatus = "404"
			}),
			wantRefusal: true,
			wantError:   "Anonymous Docker Hub manifest request",
		},
		{
			name: "the anonymous token request is refused",
			registry: bothResolve(func(r *registry) {
				r.tokenStatus = "403"
			}),
			wantRefusal: true,
			wantError:   "Anonymous Docker Hub token request",
		},
		{
			name: "the anonymous token response carries no token",
			registry: bothResolve(func(r *registry) {
				r.emptyToken = true
			}),
			wantRefusal: true,
			wantError:   "did not include a bearer token",
		},
		{
			// The same header-shape case the GHCR check carries. Both read a
			// digest header, and a reader that stops finding one calls a
			// correct mirror unsigned.
			name: "the registry writes the digest header with no space",
			registry: bothResolve(func(r *registry) {
				r.terseHeaders = true
			}),
		},
		{
			// The copy step refuses an empty list before it copies anything,
			// and this step does not rely on that having happened: handed one,
			// it would loop zero times and report a mirror published having
			// resolved nothing.
			name:        "no mirrored tag to verify",
			registry:    bothResolve(nil),
			tags:        "\n   \n",
			wantRefusal: true,
			wantError:   "no mirrored tag to verify",
		},
		{
			// The same prefix rule the copy and the promotion apply. Asked on
			// its own, this step would otherwise report on a tag of the signed
			// image while the metadata step had named someone else's registry.
			name:        "a reference from another repository",
			registry:    bothResolve(nil),
			tags:        imageName + ":v2.0.0\ndocker.io/someone/else:latest",
			wantRefusal: true,
			wantError:   "not a tag of",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tagRefs := tags
			if testCase.tags != "" {
				tagRefs = testCase.tags
			}

			output, _, err := runStep(t, bash, job, mirrorVerifyStep, map[string]string{
				"DIGEST":      digest,
				"TAG_REFS":    tagRefs,
				"IMAGE_NAME":  imageName,
				"IMAGE_PATH":  imagePath,
				"MIRROR_NAME": mirrorName,
			}, testCase.registry)

			if testCase.wantRefusal {
				if err == nil {
					t.Fatalf("the check reported a mirror published that it should have refused.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantError)
				return
			}
			if err != nil {
				t.Fatalf("the check refused a mirror that is the signed digest: %v\n%s", err, output)
			}
		})
	}
}

// TestTheMirrorNameIsSpelledOnceAcrossTheRepository compares the name the
// workflow DERIVES the mirror from against every place the documentation
// SPELLS it out. The two are independent by construction: the workflow builds
// `docker.io/<path>` out of `github.repository`, while a badge URL and a
// verification command are literal text, and nothing else in this repository
// puts the two side by side.
//
// The failure that needs is quiet and total. Registered under a namespace that
// is not the GitHub owner — the obvious reason being that the owner's name was
// taken on Docker Hub — the mirror pushes to one repository while the badge
// counts another and the documented command verifies a third, with every step
// in the publish job and every other test in this package green.
func TestTheMirrorNameIsSpelledOnceAcrossTheRepository(t *testing.T) {
	// Each site keys on its own surroundings rather than on the name's shape,
	// for readmeversion's reason: a pattern anchored to what it expects to
	// find skips a site written any other way, and a skipped site is the one
	// that drifts. Each is required to match, so a reworded sentence is a
	// failure here rather than a site silently leaving the comparison.
	for _, site := range []struct {
		file string
		what string
		re   *regexp.Regexp
	}{
		{"README.md", "the pull-count badge", regexp.MustCompile(`img\.shields\.io/docker/pulls/(\S+?)"`)},
		{"README.md", "the badge's link", regexp.MustCompile(`hub\.docker\.com/r/(\S+?)"`)},
		{"README.md", "the Quick Start reference", regexp.MustCompile("docker\\.io/(\\S+?)`")},
		{"SECURITY.md", "the verification note", regexp.MustCompile("docker\\.io/([^:`\\s]+)")},
	} {
		t.Run(site.file+", "+site.what, func(t *testing.T) {
			matches := site.re.FindAllStringSubmatch(workflowfile.Read(t, site.file), -1)
			if len(matches) == 0 {
				t.Fatalf("%s no longer carries %s, so the name the mirror writes to is stated there and compared against nothing", site.file, site.what)
			}
			for _, match := range matches {
				if match[1] != imagePath {
					t.Errorf("%s %s names %q while the publish job mirrors to %q. A reader following the documentation reaches a repository this workflow never writes to",
						site.file, site.what, match[1], mirrorName)
				}
			}
		})
	}
}

// TestTheMirrorsToolIsPinnedRatherThanInherited holds the cosign version to
// this file. `cosign copy` is deprecated as of v3.0.6 — the command names its
// own replacements — so the version that still carries it is a fact this
// workflow depends on and does not otherwise state. Left to the action's
// default, the version moves whenever the action's SHA is bumped, and the
// release that discovers `copy` is gone fails after the GHCR release is
// already public, on a job none of the branch protection's required checks
// cover.
func TestTheMirrorsToolIsPinnedRatherThanInherited(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)

	if block := withoutComments(stepBlock(t, job, installCosignStep)); !strings.Contains(block, "cosign-release:") {
		t.Errorf("%s, step %q takes its cosign version from the action's default, so a bump of the action's SHA changes the tool the mirror runs. `cosign copy` is deprecated; pin the version that still has it",
			publishWorkflow, installCosignStep)
	}
}

// TestTheSigningStepsActOnThePushedDigest reads what the three steps between
// the push and the promotion actually do. Their ORDER is held above, and order
// is all it holds: `Verify the signature and provenance before promoting`
// emptied to an `echo` keeps its name and its place, and the promotion then
// runs gated on nothing while every ordering assertion still passes.
func TestTheSigningStepsActOnThePushedDigest(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)

	for _, want := range []struct {
		step     string
		contains []string
		because  string
	}{
		{
			step:     signStep,
			contains: []string{"cosign sign", "steps.build.outputs.digest"},
			because:  "the signature has to cover the digest this run pushed, and nothing else names it",
		},
		{
			step:     attestStep,
			contains: []string{"subject-digest: ${{ steps.build.outputs.digest }}"},
			because:  "provenance attested for another subject is provenance the published digest does not carry",
		},
		{
			step:     verifyStep,
			contains: []string{"cosign verify", "gh attestation verify", "steps.build.outputs.digest"},
			because:  "signing and attesting are two API calls that reported success; this step is the only one that reads either back, and the promotion below is gated on it",
		},
	} {
		block := withoutComments(stepBlock(t, job, want.step))
		for _, needle := range want.contains {
			if !strings.Contains(block, needle) {
				t.Errorf("%s, step %q does not carry %q. %s", publishWorkflow, want.step, needle, want.because)
			}
		}
	}
}

// withoutComments drops every whole-line comment, YAML and shell alike. Both
// are stripped for one reason: these steps carry long ones, and the comment
// above the push names `steps.meta.outputs.tags` precisely because that is what
// it used to carry — a rule that read comments would fire on the sentence
// explaining why the code does not do the thing. An inline `#` is left where it
// is; it belongs to the command on that line, and nothing here judges those.
func withoutComments(block string) string {
	var kept []string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// registry is the stubbed GHCR the two scripts below run against.
type registry struct {
	// tokenStatus and pushToken decide whether the scripts get an authorised
	// session at all.
	tokenStatus string
	emptyToken  bool
	// manifestStatus is what reading the signed manifest back by digest returns.
	manifestStatus string
	contentType    string
	// putStatus is what a tag write returns; 201 is the registry's success.
	putStatus string
	// resolves is the body each tag is stored under, as if an earlier PUT had
	// written it. The stub hashes that body for real (`sha256sum`, the same
	// function `sha256Hex` runs in Go) to answer an anonymous read, so a case
	// controls what got STORED, not the digest the check is told to see —
	// a stub that returned a digest a test merely asserted would confirm
	// itself rather than the registry's own comparison. headStatus is what
	// that read returns.
	resolves   map[string]string
	headStatus string
	// terseHeaders drops the space after a header name. A server may write one
	// that way, and a reader matching the name as a whitespace-delimited field
	// silently stops finding the header at all — which reads as the header
	// being absent rather than as the reader being wrong.
	terseHeaders bool
}

func defaultRegistry() registry {
	return registry{
		tokenStatus:    "200",
		manifestStatus: "200",
		contentType:    "application/vnd.oci.image.index.v1+json",
		putStatus:      "201",
		headStatus:     "200",
	}
}

// TestPromotionWritesOnlyTheSignedDigestUnderOnlyItsOwnTags runs the promotion
// script against the stub. The failure it exists to refuse is quiet: a tag
// written from something other than the bytes that were signed, or written at
// all for a reference outside the repository this run signed.
func TestPromotionWritesOnlyTheSignedDigestUnderOnlyItsOwnTags(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	// Asked for HERE rather than inside each case: a skip taken per subtest
	// leaves this test reporting PASS with every case skipped, and the package
	// line reading `ok` over assertions none of which were made.
	bash := requireBash(t)

	for _, testCase := range []struct {
		name        string
		tags        string
		registry    registry
		wantRefusal bool
		// wantError is the substring of the step's own `::error::` line that
		// names the branch this fixture is about. A refusal fixture that only
		// asked whether the step failed would pass on a failure for any other
		// reason, which is the guard reporting green over the branch it exists
		// to hold.
		wantError  string
		wantWrites []string
		// wantContentType is what the tag write must declare when that is not
		// simply what the registry answered with.
		wantContentType string
	}{
		{
			name:       "a release tag and its aliases",
			tags:       "ghcr.io/ovumcy/ovumcy-web:v2.0.0\nghcr.io/ovumcy/ovumcy-web:latest\nghcr.io/ovumcy/ovumcy-web:sha-5049126",
			registry:   defaultRegistry(),
			wantWrites: []string{"v2.0.0", "latest", "sha-5049126"},
		},
		{
			// A digest pushed and signed with nothing pointing at it is not a
			// release, and a green run saying otherwise is the worse outcome.
			name:        "the metadata step derived no tag at all",
			tags:        "\n",
			registry:    defaultRegistry(),
			wantRefusal: true,
			wantError:   "produced no tag to promote",
		},
		{
			name:        "a reference outside the repository this run signed",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0\ndocker.io/someone/else:latest",
			registry:    defaultRegistry(),
			wantRefusal: true,
			wantError:   "is not a tag of",
		},
		{
			name:        "the registry refuses the tag write",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.putStatus = "400"; return r }(),
			wantRefusal: true,
			wantError:   "returned HTTP 400",
			wantWrites:  []string{"v2.0.0"},
		},
		{
			name:        "the signed manifest cannot be read back",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.manifestStatus = "404"; return r }(),
			wantRefusal: true,
			wantError:   "reading the signed manifest",
		},
		{
			name:        "the registry returns the manifest with no media type",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.contentType = ""; return r }(),
			wantRefusal: true,
			wantError:   "no Content-Type",
		},
		{
			name:        "GHCR issues no push token",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.emptyToken = true; return r }(),
			wantRefusal: true,
			wantError:   "returned no push token",
		},
		{
			// 200 and a token is the only shape that may proceed. A status the
			// step does not read is a token it would go on to use.
			name:        "GHCR refuses the push token request",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.tokenStatus = "403"; return r }(),
			wantRefusal: true,
			wantError:   "HTTP 403 for a push token",
		},
		{
			// The media type is not a constant, and the tag write has to echo
			// whatever the read-back returned: a hardcoded one stores a
			// different object under the tag, and the digest changes with it.
			// This fixture is the one that fails if the PUT stops carrying the
			// GET's Content-Type.
			name: "the registry stores the manifest as a Docker v2 list",
			tags: "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry: func() registry {
				r := defaultRegistry()
				r.contentType = "application/vnd.docker.distribution.manifest.list.v2+json"
				return r
			}(),
			wantWrites: []string{"v2.0.0"},
		},
		{
			// A media type may arrive with a parameter. The registry stores the
			// object under the type alone, so a tag written with the parameter
			// still attached names a different object at a different digest —
			// which is what echoing the media type was supposed to prevent.
			name: "the registry answers with a charset parameter",
			tags: "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry: func() registry {
				r := defaultRegistry()
				r.contentType = "application/vnd.oci.image.index.v1+json; charset=utf-8"
				return r
			}(),
			wantWrites:      []string{"v2.0.0"},
			wantContentType: "application/vnd.oci.image.index.v1+json",
		},
		{
			// This image, and no tag. It passes the prefix check, yields an
			// empty tag, and would be dropped in silence between the two
			// passes — a release published under fewer names than the run goes
			// on to report.
			name:        "a reference naming this image and no tag",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0\nghcr.io/ovumcy/ovumcy-web:",
			registry:    defaultRegistry(),
			wantRefusal: true,
			wantError:   "names this image and no tag",
		},
		{
			// No space after the header name. A reader that matches the name as
			// a whitespace field stops finding it, and the step then refuses a
			// manifest whose media type is right there.
			name: "the registry writes the header with no space after its name",
			tags: "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry: func() registry {
				r := defaultRegistry()
				r.terseHeaders = true
				return r
			}(),
			wantWrites: []string{"v2.0.0"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, writes, err := runStep(t, bash, job, promoteStep, map[string]string{
				"DIGEST":            digest,
				"TAG_REFS":          testCase.tags,
				"IMAGE_NAME":        imageName,
				"IMAGE_PATH":        imagePath,
				"REGISTRY_USER":     "github-actions",
				"REGISTRY_PASSWORD": "stub",
			}, testCase.registry)

			// Judged for every case, refusal included: what the step wrote
			// before it gave up is the difference between a failure that
			// published nothing and one that left a half-promoted release.
			if strings.Join(writes, ",") != strings.Join(testCase.wantWrites, ",") {
				t.Fatalf("wrote tags %v, want %v\n%s", writes, testCase.wantWrites, output)
			}

			if testCase.wantRefusal {
				if err == nil {
					t.Fatalf("the promotion published this and owed a refusal.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantError)
				return
			}
			if err != nil {
				t.Fatalf("the promotion refused a release it owes: %v\n%s", err, output)
			}
			// Every write must carry the manifest read back by digest, under
			// the media type it was read back with. A promotion that PUTs other
			// bytes points the alias at an unsigned object; one that PUTs the
			// right bytes under the wrong media type has the registry store a
			// different object, which is the same failure by another route.
			// Both pass every check above.
			wantContentType := testCase.wantContentType
			if wantContentType == "" {
				wantContentType = testCase.registry.contentType
			}

			bodies, types := 0, 0
			for _, line := range strings.Split(output, "\n") {
				if rest, ok := strings.CutPrefix(line, "PUT-BODY-SHA "); ok {
					bodies++
					// The stub hashes the file it is about to send and logs the
					// digest, rather than logging the body for this side to
					// compare: `$(cat "$body")` strips every trailing newline
					// before the text leaves the shell, so a promotion that
					// added or dropped one would arrive here already
					// normalised and compare equal — invisible to a check
					// whose whole claim is that it is byte-exact. A digest is
					// also what the registry itself compares: the substring
					// check this replaces was satisfied by any body that
					// merely mentioned the digest somewhere in it, including
					// the signed manifest with a byte changed anywhere the
					// digest string does not sit.
					if got := strings.TrimSpace(rest); got != digest {
						t.Fatalf("a tag was written from bytes other than the signed manifest.\ngot:  %s\nwant: %s (sha256 of the manifest the stub served)", got, digest)
					}
				}
				if rest, ok := strings.CutPrefix(line, "PUT-CT "); ok {
					types++
					if strings.TrimSpace(rest) != wantContentType {
						t.Fatalf("a tag was written declaring %q, and the signed manifest was stored as %q. The registry stores what the write declares, so a tag written under another media type is a different object at a different digest", strings.TrimSpace(rest), wantContentType)
					}
				}
			}
			if bodies != len(testCase.wantWrites) || types != len(testCase.wantWrites) {
				t.Fatalf("the stub logged %d bodies and %d media types for %d tag writes, so the two assertions above judged fewer writes than happened.\n%s", bodies, types, len(testCase.wantWrites), output)
			}
		})
	}
}

// TestThePublicCheckRefusesAnAliasThatIsNotTheSignedDigest runs the final
// verification. Anonymous reachability was already checked before this change;
// the digest comparison is the new half, and it is the one that says the alias
// an operator resolves is the artifact the signature covers.
func TestThePublicCheckRefusesAnAliasThatIsNotTheSignedDigest(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	tags := "ghcr.io/ovumcy/ovumcy-web:v2.0.0\nghcr.io/ovumcy/ovumcy-web:latest"

	for _, testCase := range []struct {
		name     string
		registry registry
		// tags overrides the pair above when a case is about the references
		// themselves rather than about what they resolve to.
		tags        string
		wantRefusal bool
		// wantError names the branch, so a refusal for another reason is not
		// mistaken for this one.
		wantError string
	}{
		{
			name: "both aliases resolve to the signed digest",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
				return r
			}(),
		},
		{
			name: "one alias resolves to something else",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": otherManifest}
				return r
			}(),
			wantRefusal: true,
			wantError:   "not to the signed digest",
		},
		{
			name: "an alias is not anonymously readable",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
				r.headStatus = "404"
				return r
			}(),
			wantRefusal: true,
			wantError:   "Anonymous GHCR manifest request",
		},
		{
			name: "the anonymous token request is refused",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
				r.tokenStatus = "403"
				return r
			}(),
			wantRefusal: true,
			wantError:   "Anonymous GHCR token request",
		},
		{
			// 200 with no token in the body. The status alone is not the
			// answer, and the step has a branch for it.
			name: "the anonymous token response carries no token",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
				r.emptyToken = true
				return r
			}(),
			wantRefusal: true,
			wantError:   "did not include a bearer token",
		},
		{
			// No space after the header name. The promotion reads its own
			// header this way already; a check that reads its one differently
			// finds nothing and calls a correct tag unsigned.
			name: "the registry writes the digest header with no space",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
				r.terseHeaders = true
				return r
			}(),
		},
		{
			// An empty list. The promotion refuses one before it writes
			// anything; asked on its own, this step would loop zero times and
			// report the release verified having resolved no alias at all.
			name: "no public tag to verify",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
				return r
			}(),
			tags:        "\n",
			wantRefusal: true,
			wantError:   "no public tag to verify",
		},
		{
			// This check answers about the image the run signed. Nothing
			// foreign reaches it while the promotion runs first and refuses
			// one, and that is a fact about the step before it.
			name: "a reference outside the image this run signed",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": canonicalManifest}
				return r
			}(),
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0\ndocker.io/someone/else:latest",
			wantRefusal: true,
			wantError:   "is not a tag of",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			refs := testCase.tags
			if refs == "" {
				refs = tags
			}

			output, _, err := runStep(t, bash, job, publicStep, map[string]string{
				"DIGEST":     digest,
				"TAG_REFS":   refs,
				"IMAGE_NAME": imageName,
				"IMAGE_PATH": imagePath,
			}, testCase.registry)

			if testCase.wantRefusal {
				if err == nil {
					t.Fatalf("the check passed a release it owes a refusal.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantError)
				return
			}
			if err != nil {
				t.Fatalf("the check refused a release it owes: %v\n%s", err, output)
			}
		})
	}
}

// TestTheIdentityPatternPinsEveryCharacterOfTheRepository runs the verify step
// with `cosign` and `gh` shadowed, which is the only way to read the pattern
// this workflow hands its own signer-identity check.
//
// The escape is the reason: `\.` needs four backslashes to survive a
// double-quoted expansion and produced a bare dot at every smaller count, so
// the substitution turns each one into `[.]` instead — the same regex with
// nothing to decay. That reasoning lived in a comment with no test under it,
// and someone simplifying it back reinstates an unescaped dot in the field that
// says which workflow signed the image.
func TestTheIdentityPatternPinsEveryCharacterOfTheRepository(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	script := stepScript(t, job, verifyStep)

	// Shadowed the way `curl` and `python3` are: a function reaches the script
	// wherever it could reach the real command, and logging the arguments is
	// what makes the pattern readable at all.
	preamble := strings.Join([]string{
		`cosign() { printf 'COSIGN %s\n' "$*" >&2; }`,
		`gh() { printf 'GH %s\n' "$*" >&2; }`,
	}, "\n")

	for _, testCase := range []struct {
		name        string
		repository  string
		wantPattern string
		wantRefusal bool
	}{
		{
			name:        "this repository",
			repository:  "ovumcy/ovumcy-web",
			wantPattern: `^https://github\.com/ovumcy/ovumcy-web/\.github/workflows/docker-image\.yml@`,
		},
		{
			// GitHub allows a dot in a repository name. Unescaped it matches
			// any character, so a certificate issued for `ovumcy-webXv2` would
			// satisfy a check meant to accept only `ovumcy-web.v2`.
			name:        "a repository name carrying a dot",
			repository:  "ovumcy/ovumcy-web.v2",
			wantPattern: `^https://github\.com/ovumcy/ovumcy-web[.]v2/\.github/workflows/docker-image\.yml@`,
		},
		{
			// An owner with capitals, which is the only shape that can tell the
			// identity apart from the lowercased image path. Both assertions
			// below are about it: the pattern and the attestation lookup carry
			// the repository as GitHub records it, not as GHCR requires it.
			name:        "an owner with capitals",
			repository:  "MyOrg/Ovumcy-Web",
			wantPattern: `^https://github\.com/MyOrg/Ovumcy-Web/\.github/workflows/docker-image\.yml@`,
		},
		{
			// Anything the substitution cannot reach is refused rather than
			// spliced in and matched loosely.
			name:        "a name this pattern cannot escape",
			repository:  "ovumcy/ovumcy web",
			wantRefusal: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// The step writes the pattern to `$GITHUB_OUTPUT`, which is a file
			// the runner provides. It is read back below rather than merely
			// tolerated: the mirror's own check consumes that output, so a
			// pattern computed correctly and exported wrongly — under another
			// key, or not at all — leaves the second registry verified against
			// an empty identity, which is no identity.
			outputFile := filepath.Join(t.TempDir(), "github_output")

			command := runBashScript(t, bash, job, verifyStep, preamble+"\n"+script)
			command.Env = append(os.Environ(),
				"GITHUB_REPOSITORY="+testCase.repository,
				"IMAGE_DIGEST="+imageName+"@"+digest,
				"GITHUB_OUTPUT="+outputFile,
			)
			output, err := command.CombinedOutput()

			if testCase.wantRefusal {
				if err == nil {
					t.Fatalf("the step accepted a repository name it cannot escape.\n%s", output)
				}
				requireRefusalReason(t, string(output), "cannot escape")
				return
			}
			if err != nil {
				t.Fatalf("the step failed on %q: %v\n%s", testCase.repository, err, output)
			}
			if !strings.Contains(string(output), testCase.wantPattern) {
				t.Errorf("the identity handed to cosign for %q carries no %q:\n%s",
					testCase.repository, testCase.wantPattern, output)
			}

			exported, readErr := os.ReadFile(outputFile)
			if readErr != nil {
				t.Fatalf("the step wrote no step output for %q: %v", testCase.repository, readErr)
			}
			if want := "identity_regexp=" + testCase.wantPattern + "\n"; string(exported) != want {
				t.Errorf("the step exported %q for %q, want %q", exported, testCase.repository, want)
			}

			// The attestation is looked up by the repository in its own case,
			// deliberately: the OIDC subject carries it that way, while five of
			// the six steps around this one read the lowercased path. Tidying
			// that difference away would fail the lookup in a fork under an
			// owner with capitals, with an error naming the attestation rather
			// than the case.
			want := "GH attestation verify oci://" + imageName + "@" + digest + " --repo " + testCase.repository
			if !strings.Contains(string(output), want) {
				t.Errorf("the attestation lookup for %q is not %q:\n%s", testCase.repository, want, output)
			}
		})
	}
}

// TestTheImageNameIsDerivedOnceAndLowercased runs the step the push, both
// cosign steps, the promotion and the public check all read their image from.
// Every other test supplies that name rather than deriving it, so without this
// one a change to the formula — a stray suffix, the wrong variable, the
// repository left in its own case — would be reported by the registry and by
// nothing here.
func TestTheImageNameIsDerivedOnceAndLowercased(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	script := stepScript(t, job, resolveStep)

	for _, testCase := range []struct {
		name       string
		repository string
		wantPath   string
		wantName   string
	}{
		{
			name:       "this repository",
			repository: "ovumcy/ovumcy-web",
			wantPath:   imagePath,
			wantName:   imageName,
		},
		{
			// GHCR refuses an uppercase path and `docker/metadata-action`
			// lowercases the name it is handed, so a fork under an owner with
			// capitals is where a raw `github.repository` and the derived tags
			// would name two different images.
			name:       "an owner with capitals",
			repository: "MyOrg/Ovumcy-Web",
			wantPath:   "myorg/ovumcy-web",
			wantName:   "ghcr.io/myorg/ovumcy-web",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			outputs := filepath.ToSlash(filepath.Join(t.TempDir(), "outputs"))

			command := runBashScript(t, bash, job, resolveStep, script)
			command.Env = append(os.Environ(),
				"GITHUB_REPOSITORY="+testCase.repository,
				"GITHUB_OUTPUT="+outputs,
			)
			if combined, err := command.CombinedOutput(); err != nil {
				t.Fatalf("the step failed on %q: %v\n%s", testCase.repository, err, combined)
			}

			written, err := os.ReadFile(outputs)
			if err != nil {
				t.Fatalf("the step wrote no outputs at all: %v", err)
			}
			for _, want := range []string{"path=" + testCase.wantPath, "name=" + testCase.wantName} {
				if !strings.Contains(string(written), want) {
					t.Errorf("the step derived %q from %q, and no line of it is %q",
						strings.TrimSpace(string(written)), testCase.repository, want)
				}
			}
		})
	}
}

// TestTheTokenParseReadsWhatTheRegistryReturned runs the one-liner both
// registry steps pull the bearer token out of the registry's answer with.
// `stubRegistry` shadows `python3` deliberately — what those fixtures decide is
// which requests each step makes and how it judges the answers — so this is the
// only place the parse itself executes. It also holds the two steps to ONE
// spelling of it: two implementations of one job in one job drift, and no
// fixture that stubs them both can tell.
func TestTheTokenParseReadsWhatTheRegistryReturned(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)

	parse := tokenParseLine(t, job, promoteStep)
	if public := tokenParseLine(t, job, publicStep); public != parse {
		t.Fatalf("the two registry steps parse the token differently:\n  %s\n  %s", parse, public)
	}

	bash := requireBash(t)
	requireShellTool(t, bash, "python3", `python3 -c "print('ok')"`, "ok")

	for _, testCase := range []struct {
		name string
		body string
		want string
	}{
		{name: "the registry issues a token", body: `{"token": "stub-token"}`, want: "stub-token"},
		// 200 with no token in it. Both steps branch on the empty string, so
		// what the parse returns here is what arms that branch.
		{name: "the answer carries no token", body: `{}`, want: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.ToSlash(filepath.Join(t.TempDir(), "token.json"))
			if err := os.WriteFile(path, []byte(testCase.body), 0o600); err != nil {
				t.Fatalf("write the fixture answer: %v", err)
			}

			for _, step := range []string{promoteStep, publicStep} {
				output, err := runBashScript(t, bash, job, step, "set -euo pipefail\ntoken_body="+shellQuote(path)+"\n"+parse).Output()
				if err != nil {
					t.Fatalf("the token parse failed on %s under %q's shell: %v", testCase.body, step, err)
				}
				if got := strings.TrimSpace(string(output)); got != testCase.want {
					t.Errorf("the token parse read %q out of %s under %q's shell, want %q", got, testCase.body, step, testCase.want)
				}
			}
		})
	}
}

// tokenParseLine returns the line of a step's script that parses the token,
// read off the workflow rather than restated here.
func tokenParseLine(t *testing.T, job, step string) string {
	t.Helper()

	for _, line := range strings.Split(stepScript(t, job, step), "\n") {
		if strings.Contains(line, "python3 -c") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("%s, step %q no longer parses the token with `python3 -c`, so this guard would run nothing", publishWorkflow, step)
	return ""
}

// requireBash returns a bash that has answered for itself. Being on PATH is not
// the test — every fixture in this package runs through it, so it is asked to
// produce a known byte before anything is judged by what it does.
func requireBash(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("bash")
	if err == nil {
		output, probeErr := exec.Command(path, "-c", "printf ok").Output()
		if got := strings.TrimSpace(string(output)); probeErr != nil || got != "ok" {
			err = fmt.Errorf("%s answered %q, not \"ok\": %v", path, got, probeErr)
		}
	}
	requireOrSkip(t, "bash", err)

	// The registry stub answers an anonymous read by hashing the body a case
	// stored, so `sha256sum` (and the `awk` that cuts its output) is now part
	// of what these steps run through. Absent, the command substitution around
	// it yields nothing, every tag resolves to a bare "sha256:", and the run
	// reports a digest mismatch that belongs to the probe rather than to the
	// step — a wrong diagnosis where this package's rule is that a guard which
	// could not look must say so. The expected value is computed by the same
	// function the fixtures use, so the two cannot drift apart.
	requireShellTool(t, path, "sha256sum", `printf '' | sha256sum | awk '{print $1}'`, sha256Hex(""))

	return path
}

// runBashScript writes script to a file under t.TempDir() and returns a Cmd
// that runs it the way the runner runs the named step: as a FILE, never `-c`,
// under the flags that step's own `shell:` compiles to. Not every step under
// `publish` declares `shell: bash` — `Scan the image before publishing it` and
// `Sign the pushed digest` declare none and run as `bash -e {0}`, without
// pipefail — so the flags are read off the step rather than assumed, and a
// step naming any other shell fails here instead of running. A
// script long enough to hold one of these steps also truncates silently on
// Windows when handed to `-c` as a command-line argument.
func runBashScript(t *testing.T, bash, job, step, script string) *exec.Cmd {
	t.Helper()

	flags := workflowfile.BashStepFlags(t, publishWorkflow, step, stepBlock(t, job, step))
	scriptFile := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0o644); err != nil {
		t.Fatalf("write the step script: %v", err)
	}
	return exec.Command(bash, append(flags, filepath.ToSlash(scriptFile))...)
}

// TestRunBashScriptStopsWhereTheStepWould is the positive control for the
// flags runBashScript reads off a step: under `shell: bash` a failing command
// ends the script, and so does a pipeline whose first stage fails. A harness
// running without either carries a fixture past the line where the real step
// stopped. The first case is the anchor: a script that fails nowhere has to
// reach its end, or the other two would pass on a helper that runs nothing.
func TestRunBashScriptStopsWhereTheStepWould(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)

	for _, testCase := range []struct {
		name        string
		script      string
		wantReached bool
	}{
		{name: "nothing fails", script: "true | true\necho reached\n", wantReached: true},
		{name: "a failing command (errexit)", script: "false\necho reached\n"},
		{name: "a pipeline whose first stage fails (pipefail)", script: "false | true\necho reached\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runBashScript(t, bash, job, promoteStep, testCase.script).CombinedOutput()
			reached := strings.Contains(string(output), "reached")

			if testCase.wantReached && (!reached || err != nil) {
				t.Fatalf("a script that fails nowhere did not run to its end under %q's shell: %v\n%s", promoteStep, err, output)
			}
			if !testCase.wantReached && (reached || err == nil) {
				t.Fatalf("the script ran past a failure under %q's shell, which `shell: bash` stops at (exit: %v):\n%s", promoteStep, err, output)
			}
		})
	}
}

// requireShellTool checks a tool THROUGH the shell that will reach it, which is
// the only resolution that answers the question being asked. Go's `LookPath`
// and bash's own lookup genuinely disagree on Windows — Go honours `PATHEXT`
// and finds a `.bat`, bash wants an extensionless file — so a tool validated
// through Go need not be the one a step's script runs. Windows also ships a
// `python3` that is a store advert and answers a lookup exactly as an
// interpreter does, which is why answering at all is not the test: it has to
// say the right thing.
func requireShellTool(t *testing.T, bash, name, probe, want string) {
	t.Helper()

	output, err := exec.Command(bash, "-c", probe).Output()
	got := strings.TrimSpace(string(output))

	// Two outcomes, and the message says which: a tool that is absent or dies
	// is not a tool that ran and answered wrongly, and one report carrying both
	// appends a `<nil>` error to the second.
	switch {
	case err != nil:
		requireOrSkip(t, name, fmt.Errorf("`%s` did not run: %v", probe, err))
	case got != want:
		requireOrSkip(t, name, fmt.Errorf("`%s` answered %q, not %q", probe, got, want))
	}
}

// requireOrSkip is this package's one rule about a tool the publish steps run
// through: a guard that reports green because it could not look is worse than
// none, so only a Windows developer machine may skip, and anywhere a verdict
// decides a merge a missing tool is a failure.
func requireOrSkip(t *testing.T, name string, err error) {
	t.Helper()

	if err == nil {
		return
	}
	if runtime.GOOS != "windows" || os.Getenv("CI") != "" {
		t.Fatalf("%s is required to run the publish steps as the workflow runs them, and this guard proves nothing without it: %v", name, err)
	}
	t.Skipf("%s is required to run the publish steps as the workflow runs them: %v", name, err)
}

// requireRefusalReason holds a refusal to the branch its fixture is about. A
// case that asked only whether the step failed would pass on a failure for any
// other reason — a stub endpoint that stopped being served, an `mktemp` that
// did not, a variable `set -u` no longer finds — and eleven of these fixtures
// exist to pin one branch each. An empty expectation is refused rather than
// matched, because `strings.Contains(anything, "")` is true.
func requireRefusalReason(t *testing.T, output, want string) {
	t.Helper()

	if want == "" {
		t.Fatalf("this fixture expects a refusal and names no message, so it would pass on a refusal for any reason at all.\n%s", output)
	}
	if !strings.Contains(output, want) {
		t.Fatalf("the step refused, but not for the reason this fixture is about: no message carrying %q.\n%s", want, output)
	}
}

// requireOrderedLines asserts that every wanted line appears in output, in the
// order given. Presence alone is the weaker claim and the wrong one where a
// publish step is concerned: signing a digest and writing an alias to it are
// both present in a step that does them the wrong way round, and it is the
// order that decides whether a failure can leave a public alias standing on an
// image nothing verifies.
func requireOrderedLines(t *testing.T, output string, want []string) {
	t.Helper()

	if len(want) == 0 {
		t.Fatalf("this fixture asserts on no invocation at all, so it would pass on a step that made none.\n%s", output)
	}

	rest := output
	for _, line := range want {
		index := strings.Index(rest, line)
		if index < 0 {
			if strings.Contains(output, line) {
				t.Fatalf("the step ran %q, but out of order — it belongs after the lines named before it.\n%s", line, output)
			}
			t.Fatalf("the step did not run %q:\n%s", line, output)
		}
		rest = rest[index+len(line):]
	}
}

// runStep executes one extracted step with `curl` and `python3` shadowed by
// shell functions serving the stubbed registry. Functions rather than stub
// executables on PATH: a bash function shadows an external command everywhere
// the script could reach one, and needs no executable bit, which Windows does
// not carry.
//
// It returns the tags the script wrote, in the order it wrote them, read off
// the stub's own log.
func runStep(t *testing.T, bash, job, step string, env map[string]string, reg registry) (string, []string, error) {
	t.Helper()

	script := stepScript(t, job, step)

	dir := filepath.ToSlash(t.TempDir())
	// Each tag's fixture body is written to its own file rather than into one
	// shared index: the body is a JSON manifest, not a bare digest, so a
	// whitespace-delimited lookup (as the digest version of this fixture used)
	// would read only the first field of it. The stub hashes the file's bytes
	// for real, so what a case controls is what got stored, not the digest a
	// check is told to see.
	for tag, body := range reg.resolves {
		if err := os.WriteFile(filepath.Join(dir, "resolve-"+tag+".body"), []byte(body), 0o600); err != nil {
			t.Fatalf("write the resolve fixture for tag %q: %v", tag, err)
		}
	}
	preamble := stubRegistry(dir, reg)

	command := runBashScript(t, bash, job, step, preamble+"\n"+script)
	command.Env = append(os.Environ(),
		"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
		"GITHUB_SERVER_URL=https://github.com",
		"STUB_DIR="+dir,
	)
	// The workflow's own constants first — the job's `env:`, then the step's —
	// so a constant declared there, the Accept header listing the manifest
	// media types being the one that matters, reaches the script exactly as the
	// workflow supplies it rather than being restated here. The fixture's
	// values come after and win, since Go's exec keeps the last binding.
	for key, value := range jobEnv(job) {
		command.Env = append(command.Env, key+"="+value)
	}
	for key, value := range declaredEnv(t, job, step) {
		command.Env = append(command.Env, key+"="+value)
	}
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}

	output, runErr := command.CombinedOutput()

	var writes []string
	for _, line := range strings.Split(string(output), "\n") {
		if rest, ok := strings.CutPrefix(line, "PUT-TAG "); ok {
			writes = append(writes, strings.TrimSpace(rest))
		}
	}
	return string(output), writes, runErr
}

// stubRegistry is the shell preamble: a `curl` that answers the four calls the
// two steps make, and a `python3` that stands in for the token extraction. It
// answers the ENDPOINT rather than parsing HTTP, so what these fixtures prove is
// which requests each step makes and how it judges the answers — not that curl
// is invoked with the right flags.
func stubRegistry(dir string, reg registry) string {
	// The stub stands in for the token extraction as well as for the endpoint:
	// both steps pipe the registry's JSON through `python3` to pull `.token`
	// out of it, and what matters to them is the value that comes back, not how
	// it was parsed. So the fixture writes the extracted value beside the body,
	// and a registry that answers without a token yields an empty one here
	// exactly as it would there.
	token, tokenValue := `{"token": "stub-token"}`, "stub-token"
	if reg.emptyToken {
		token, tokenValue = `{}`, ""
	}

	// The space after a header name is optional in HTTP, and both steps read a
	// header, so the fixture can write one either way.
	space := " "
	if reg.terseHeaders {
		space = ""
	}

	return strings.Join([]string{
		`STUB_MANIFEST=` + shellQuote(dir+"/manifest.json"),
		`printf '%s' ` + shellQuote(canonicalManifest) + ` > "$STUB_MANIFEST"`,
		`printf '%s' ` + shellQuote(token) + ` > ` + shellQuote(dir+"/token.json"),
		`printf '%s' ` + shellQuote(tokenValue) + ` > ` + shellQuote(dir+"/token_value.txt"),
		`python3() { cat > /dev/null 2>&1 || true; cat "$STUB_DIR/token_value.txt"; printf '\n'; }`,
		`curl() {`,
		`  local out="" dump="" method=GET url="" body="" content_type=""`,
		`  while [ $# -gt 0 ]; do`,
		`    case "$1" in`,
		`      -sSLo|-o|--output) out="$2"; shift 2 ;;`,
		`      -D|--dump-header) dump="$2"; shift 2 ;;`,
		`      -X) method="$2"; shift 2 ;;`,
		`      -I) method=HEAD; shift ;;`,
		// `-H` is read rather than discarded: the media type the tag write
		// declares is the difference between storing the signed manifest and
		// storing a different object under the same tag, and it travels in a
		// header. The credential and the bounds are consumed and ignored.
		`      -H) case "$2" in Content-Type:*) content_type="${2#Content-Type: }" ;; esac; shift 2 ;;`,
		`      -w|-u|-K|--connect-timeout|--max-time|--retry|--retry-delay) shift 2 ;;`,
		`      --data-binary) body="${2#@}"; shift 2 ;;`,
		`      -*) shift ;;`,
		`      *) url="$1"; shift ;;`,
		`    esac`,
		`  done`,
		`  case "$url" in`,
		`    *"/token?"*)`,
		`      if [ -n "$out" ]; then cp "$STUB_DIR/token.json" "$out"; else cat "$STUB_DIR/token.json"; fi`,
		`      printf '%s' ` + shellQuote(reg.tokenStatus) + `; return 0 ;;`,
		`    *"/manifests/sha256:"*)`,
		`      [ -n "$out" ] && cp "$STUB_MANIFEST" "$out"`,
		`      [ -n "$dump" ] && printf 'HTTP/2 %s\r\nContent-Type:` + space + `%s\r\n' ` + shellQuote(reg.manifestStatus) + ` ` + shellQuote(reg.contentType) + ` > "$dump"`,
		`      printf '%s' ` + shellQuote(reg.manifestStatus) + `; return 0 ;;`,
		`    *"/manifests/"*)`,
		`      tag="${url##*/manifests/}"`,
		`      if [ "$method" = PUT ]; then`,
		`        printf 'PUT-TAG %s\n' "$tag" >&2`,
		// The digest of the file about to be sent, not its text: a command
		// substitution around `cat` drops every trailing newline, so a body
		// that differs from the signed manifest only there would reach the
		// assertion already normalised and pass a byte-exact check.
		`        printf 'PUT-BODY-SHA sha256:%s\n' "$(sha256sum "$body" | awk '{print $1}')" >&2`,
		`        printf 'PUT-CT %s\n' "$content_type" >&2`,
		`        printf '%s' ` + shellQuote(reg.putStatus) + `; return 0`,
		`      fi`,
		// The digest is computed here, over the tag's fixture body, rather
		// than read back from a value the fixture handed the stub directly —
		// a case controls what got STORED under the tag, and this line is
		// what turns that into the digest the check reads, exactly as GHCR's
		// own content-addressing would. A stub that instead returned a
		// digest string a test merely asserted would confirm itself, not the
		// step's own comparison.
		`      resolved=""`,
		`      if [ -f "$STUB_DIR/resolve-$tag.body" ]; then`,
		`        resolved="sha256:$(sha256sum "$STUB_DIR/resolve-$tag.body" | awk '{print $1}')"`,
		`      fi`,
		`      [ -n "$dump" ] && printf 'HTTP/2 %s\r\nDocker-Content-Digest:` + space + `%s\r\n' ` + shellQuote(reg.headStatus) + ` "$resolved" > "$dump"`,
		`      printf '%s' ` + shellQuote(reg.headStatus) + `; return 0 ;;`,
		`  esac`,
		`  printf 'the step called an endpoint this fixture does not serve: %s\n' "$url" >&2`,
		`  return 1`,
		`}`,
	}, "\n")
}

// shellQuote makes a path or a fixture body safe to paste into the stub
// preamble, which is assembled as text and handed to bash whole.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// stepNames lists the publish job's steps in the order the workflow declares
// them, which is the order they run in.
func stepNames(t *testing.T, job string) []string {
	t.Helper()

	var names []string
	for _, match := range stepName.FindAllStringSubmatch(job, -1) {
		names = append(names, strings.TrimSpace(match[1]))
	}
	if len(names) == 0 {
		t.Fatalf("%s, job %q: no steps found", publishWorkflow, publishJob)
	}
	return names
}

// stepBlock returns the text of one step of the publish job.
func stepBlock(t *testing.T, job, name string) string {
	t.Helper()

	return workflowfile.Step(t, publishWorkflow, publishJob, job, name)
}

// declaredEnv returns one step's `env:` mapping, minus the entries whose value
// is a workflow expression — those are the run's own context and the fixtures
// supply them instead. What is left is the constants the step declares, which
// belong to the step and not to this file: restating them here would let the
// workflow's Accept header or its tag list drift away from what the fixtures
// exercise, with both sides still green.
func declaredEnv(t *testing.T, job, step string) map[string]string {
	t.Helper()

	return envConstants(stepBlock(t, job, step), "\n        env:\n", "          ")
}

// jobEnv returns the same for the job's own `env:`, which is where a constant
// both registry steps must agree on belongs — one of them declaring a media
// type the other does not accept is a promotion the public check will not
// resolve, with both halves reading green here.
func jobEnv(job string) map[string]string {
	return envConstants(job, "\n    env:\n", "      ")
}

// TestEnvConstantsReadsABlockThatOpensOnEnv is the shape `Sign the pushed
// digest` is written in: a step block begins straight after the line naming the
// step, so its first key has no newline in front of it. A reader that missed
// one would hand a fixture none of the workflow's constants, and `set -u` would
// then report it as the script's fault rather than the reader's.
func TestEnvConstantsReadsABlockThatOpensOnEnv(t *testing.T) {
	const accept = "application/vnd.oci.image.index.v1+json"
	block := "        env:\n          MANIFEST_ACCEPT: " + accept + "\n        run: |\n"

	if got := envConstants(block, "\n        env:\n", "          ")["MANIFEST_ACCEPT"]; got != accept {
		t.Errorf("envConstants read %q out of a block whose first key is `env:`, want %q", got, accept)
	}
}

// envConstants reads one `env:` mapping out of a block, minus the entries whose
// value is a workflow expression — those are the run's own context and the
// fixtures supply them instead. What is left is the constants the workflow
// declares, which belong to it and not to this file: restating them here would
// let the Accept header drift away from what the fixtures exercise, with both
// sides still green.
func envConstants(block, marker, keyIndent string) map[string]string {
	// The marker carries a leading newline so that an `env:` at a deeper
	// indentation cannot match, and the block is prefixed with one so that a
	// block whose FIRST key is `env:` still can — a step block begins straight
	// after the line naming the step, and `Sign the pushed digest` is written
	// exactly that way.
	prefixed := "\n" + block

	start := strings.Index(prefixed, marker)
	if start < 0 {
		return nil
	}

	env := map[string]string{}
	for _, line := range strings.Split(prefixed[start+len(marker):], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, keyIndent) {
			break
		}
		entry := strings.TrimPrefix(line, keyIndent)
		if strings.HasPrefix(entry, "#") || strings.HasPrefix(entry, " ") {
			continue
		}
		match := envEntry.FindStringSubmatch(entry)
		if match == nil || strings.Contains(match[2], "${{") {
			continue
		}
		env[match[1]] = strings.TrimSpace(match[2])
	}
	return env
}

// stepScript returns one step's `run:` block, dedented the way GitHub hands it
// to bash.
func stepScript(t *testing.T, job, name string) string {
	t.Helper()

	block := stepBlock(t, job, name)
	marker := "        run: |\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("%s, step %q: no `run: |` block, so this guard would run nothing", publishWorkflow, name)
	}

	var script []string
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		if strings.TrimSpace(line) == "" {
			script = append(script, "")
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		script = append(script, strings.TrimPrefix(line, "          "))
	}
	return strings.Join(script, "\n")
}
