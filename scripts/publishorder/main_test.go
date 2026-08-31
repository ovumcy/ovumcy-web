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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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
)

var (
	stepName = regexp.MustCompile(`(?m)^      - name: (.+)$`)
	envEntry = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*): (.*)$`)
)

// digest is what the fixtures sign and promote; otherDigest is what a public
// alias must never be allowed to resolve to instead. The stubbed manifest body
// carries `digest`, so a tag written from anything but the bytes read back by
// digest shows up in the stub's own log.
const (
	digest      = "sha256:1f0c1cb3f0e0c9c2a8d2e0a1b3c4d5e6f70819202a2b3c4d5e6f708192a2b3c4d"
	otherDigest = "sha256:99999999999999999999999999999999999999999999999999999999deadbeef"
)

// The image the fixtures run against. The workflow derives both from
// `github.repository`, lowercased; the two spellings are what the steps
// consume — the full reference a tag is checked to belong to, and the registry
// path an API call is made against.
const (
	imageName = "ghcr.io/ovumcy/ovumcy-web"
	imagePath = "ovumcy/ovumcy-web"
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
		"Install Cosign",
		signStep,
		attestStep,
		verifyStep,
	}

	if got := steps[:promote]; !slices.Equal(got, want) {
		t.Errorf("%s, job %q runs these steps before %q:\n  %v\nand this guard has reviewed:\n  %v\nEvery step in that window runs while the digest is signed but nothing public points at it, so a new one there has to be read for whether it can create a tag — then added here. Reordering or removing one is the same question in reverse",
			publishWorkflow, publishJob, promoteStep, got, want)
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
	// resolves is what each tag resolves to when read back anonymously, and
	// headStatus what that read returns.
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
				if rest, ok := strings.CutPrefix(line, "PUT-BODY "); ok {
					bodies++
					if !strings.Contains(rest, digest) {
						t.Fatalf("a tag was written from bytes other than the signed manifest: %q", line)
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
				r.resolves = map[string]string{"v2.0.0": digest, "latest": digest}
				return r
			}(),
		},
		{
			name: "one alias resolves to something else",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": digest, "latest": otherDigest}
				return r
			}(),
			wantRefusal: true,
			wantError:   "not to the signed digest",
		},
		{
			name: "an alias is not anonymously readable",
			registry: func() registry {
				r := defaultRegistry()
				r.resolves = map[string]string{"v2.0.0": digest, "latest": digest}
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
				r.resolves = map[string]string{"v2.0.0": digest, "latest": digest}
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
				r.resolves = map[string]string{"v2.0.0": digest, "latest": digest}
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
				r.resolves = map[string]string{"v2.0.0": digest, "latest": digest}
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
				r.resolves = map[string]string{"v2.0.0": digest, "latest": digest}
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
				r.resolves = map[string]string{"v2.0.0": digest, "latest": digest}
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
			command := exec.Command(bash, "-c", preamble+"\n"+script)
			command.Env = append(os.Environ(),
				"GITHUB_REPOSITORY="+testCase.repository,
				"IMAGE_DIGEST="+imageName+"@"+digest,
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

			command := exec.Command(bash, "-c", script)
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

			output, err := exec.Command(bash, "-c", "set -euo pipefail\ntoken_body="+shellQuote(path)+"\n"+parse).Output()
			if err != nil {
				t.Fatalf("the token parse failed on %s: %v", testCase.body, err)
			}
			if got := strings.TrimSpace(string(output)); got != testCase.want {
				t.Errorf("the token parse read %q out of %s, want %q", got, testCase.body, testCase.want)
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
	return path
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
	resolves := ""
	for tag, resolved := range reg.resolves {
		resolves += tag + " " + resolved + "\n"
	}
	preamble := stubRegistry(dir, reg, resolves)

	command := exec.Command(bash, "-c", preamble+"\n"+script)
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
func stubRegistry(dir string, reg registry, resolves string) string {
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
		`printf '{"signed":"` + digest + `"}' > "$STUB_MANIFEST"`,
		`printf '%s' ` + shellQuote(token) + ` > ` + shellQuote(dir+"/token.json"),
		`printf '%s' ` + shellQuote(resolves) + ` > ` + shellQuote(dir+"/resolves.txt"),
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
		`        printf 'PUT-BODY %s\n' "$(cat "$body")" >&2`,
		`        printf 'PUT-CT %s\n' "$content_type" >&2`,
		`        printf '%s' ` + shellQuote(reg.putStatus) + `; return 0`,
		`      fi`,
		`      resolved="$(awk -v t="$tag" '$1 == t { print $2 }' "$STUB_DIR/resolves.txt")"`,
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

// stepBlock returns the text of one step, from its `- name:` line to the next
// step at the same indentation.
func stepBlock(t *testing.T, job, name string) string {
	t.Helper()

	header := "      - name: " + name + "\n"
	start := strings.Index(job, header)
	if start < 0 {
		t.Fatalf("%s, job %q: no step named %q", publishWorkflow, publishJob, name)
	}
	rest := job[start+len(header):]

	if next := stepName.FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
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
