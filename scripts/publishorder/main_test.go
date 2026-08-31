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
// Two things are guarded here, because either one alone reads green over the
// defect:
//
//   - the ORDER, statically: nothing that can create a public tag may precede
//     the signature. A guard that only tested the promotion script would pass
//     over a `tags:` quietly restored to the push;
//   - the PROMOTION and the VERIFICATION, by running their real scripts over a
//     stubbed registry. A guard that only read the order would pass over a
//     promotion that writes a tag it never checked, or a verification that
//     accepts an alias resolving to a digest nobody signed.
//
// What is NOT provable here is the end-to-end negative — breaking Cosign on a
// real tag push and observing that no public alias appears. That needs a
// registry and a release tag; the order rule below is what stands in for it in
// CI, and it is the property that experiment would be testing.
package publishorder

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	publishWorkflow = ".github/workflows/docker-image.yml"
	publishJob      = "publish"

	pushStep    = "Push the image by digest, under no public tag"
	signStep    = "Sign the pushed digest"
	attestStep  = "Attest build provenance"
	verifyStep  = "Verify the signature and provenance before promoting"
	promoteStep = "Promote the signed digest to its public tags"
	publicStep  = "Verify every public tag anonymously and against the signed digest"
)

var (
	jobHeader = regexp.MustCompile(`(?m)^  [A-Za-z0-9_.-]+:[ \t]*$`)
	stepName  = regexp.MustCompile(`(?m)^      - name: (.+)$`)
	envEntry  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*): (.*)$`)
)

// digest is what the fixtures sign and promote; otherDigest is what a public
// alias must never be allowed to resolve to instead. The stubbed manifest body
// carries `digest`, so a tag written from anything but the bytes read back by
// digest shows up in the stub's own log.
const (
	digest      = "sha256:1f0c1cb3f0e0c9c2a8d2e0a1b3c4d5e6f70819202a2b3c4d5e6f708192a2b3c4d"
	otherDigest = "sha256:99999999999999999999999999999999999999999999999999999999deadbeef"
)

// TestNoPublicTagIsCreatedBeforeTheSignature is the order rule. It is written
// against the step LIST rather than against any one step's text: the defect was
// not a wrong step, it was three correct steps in the wrong order.
func TestNoPublicTagIsCreatedBeforeTheSignature(t *testing.T) {
	steps := stepNames(t)
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
	block := withoutComments(stepBlock(t, pushStep))
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
		if strings.Contains(withoutComments(stepBlock(t, name)), "steps.meta.outputs.tags") {
			t.Errorf("%s, job %q: step %q reads the public tag list and runs before %q. Only the promotion may hold those names",
				publishWorkflow, publishJob, name, promoteStep)
		}
	}
}

// withoutComments drops whole-line YAML comments. It is deliberately not a
// shell-comment stripper: a `#` inside a `run:` script is the script's, and
// nothing here judges those.
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
	for _, testCase := range []struct {
		name        string
		tags        string
		registry    registry
		wantRefusal bool
		wantWrites  []string
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
		},
		{
			name:        "a reference outside the repository this run signed",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0\ndocker.io/someone/else:latest",
			registry:    defaultRegistry(),
			wantRefusal: true,
		},
		{
			name:        "the registry refuses the tag write",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.putStatus = "400"; return r }(),
			wantRefusal: true,
		},
		{
			name:        "the signed manifest cannot be read back",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.manifestStatus = "404"; return r }(),
			wantRefusal: true,
		},
		{
			name:        "the registry returns the manifest with no media type",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.contentType = ""; return r }(),
			wantRefusal: true,
		},
		{
			name:        "GHCR issues no push token",
			tags:        "ghcr.io/ovumcy/ovumcy-web:v2.0.0",
			registry:    func() registry { r := defaultRegistry(); r.emptyToken = true; return r }(),
			wantRefusal: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, writes, err := runStep(t, promoteStep, map[string]string{
				"DIGEST":            digest,
				"TAG_REFS":          testCase.tags,
				"REGISTRY_USER":     "github-actions",
				"REGISTRY_PASSWORD": "stub",
			}, testCase.registry)

			if testCase.wantRefusal {
				if err == nil {
					t.Fatalf("the promotion published this and owed a refusal.\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("the promotion refused a release it owes: %v\n%s", err, output)
			}

			if strings.Join(writes, ",") != strings.Join(testCase.wantWrites, ",") {
				t.Fatalf("wrote tags %v, want %v\n%s", writes, testCase.wantWrites, output)
			}
			// Every write must carry the manifest read back by digest. A
			// promotion that PUTs anything else would pass the check above and
			// still point the alias at an unsigned object.
			for _, line := range strings.Split(output, "\n") {
				if strings.HasPrefix(line, "PUT-BODY ") && !strings.Contains(line, digest) {
					t.Fatalf("a tag was written from bytes other than the signed manifest: %q", line)
				}
			}
		})
	}
}

// TestThePublicCheckRefusesAnAliasThatIsNotTheSignedDigest runs the final
// verification. Anonymous reachability was already checked before this change;
// the digest comparison is the new half, and it is the one that says the alias
// an operator resolves is the artifact the signature covers.
func TestThePublicCheckRefusesAnAliasThatIsNotTheSignedDigest(t *testing.T) {
	tags := "ghcr.io/ovumcy/ovumcy-web:v2.0.0\nghcr.io/ovumcy/ovumcy-web:latest"

	for _, testCase := range []struct {
		name        string
		registry    registry
		wantRefusal bool
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
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, _, err := runStep(t, publicStep, map[string]string{
				"DIGEST":     digest,
				"TAG_REFS":   tags,
				"IMAGE_NAME": "ghcr.io/ovumcy/ovumcy-web",
			}, testCase.registry)

			if testCase.wantRefusal && err == nil {
				t.Fatalf("the check passed a release it owes a refusal.\n%s", output)
			}
			if !testCase.wantRefusal && err != nil {
				t.Fatalf("the check refused a release it owes: %v\n%s", err, output)
			}
		})
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
func runStep(t *testing.T, step string, env map[string]string, reg registry) (string, []string, error) {
	t.Helper()

	script := stepScript(t, step)

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is required to run the publish steps as the workflow runs them: %v", err)
	}

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
	// The step's own `env:` first, so a constant it declares there — the Accept
	// header listing the manifest media types, say — reaches the script exactly
	// as the workflow supplies it rather than being restated here. The fixture's
	// values come after and win, since Go's exec keeps the last binding.
	for key, value := range declaredEnv(t, step) {
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

	return strings.Join([]string{
		`STUB_MANIFEST=` + shellQuote(dir+"/manifest.json"),
		`printf '{"signed":"` + digest + `"}' > "$STUB_MANIFEST"`,
		`printf '%s' ` + shellQuote(token) + ` > ` + shellQuote(dir+"/token.json"),
		`printf '%s' ` + shellQuote(resolves) + ` > ` + shellQuote(dir+"/resolves.txt"),
		`printf '%s' ` + shellQuote(tokenValue) + ` > ` + shellQuote(dir+"/token_value.txt"),
		`python3() { cat > /dev/null 2>&1 || true; cat "$STUB_DIR/token_value.txt"; printf '\n'; }`,
		`curl() {`,
		`  local out="" dump="" method=GET url="" body=""`,
		`  while [ $# -gt 0 ]; do`,
		`    case "$1" in`,
		`      -sSLo|-o|--output) out="$2"; shift 2 ;;`,
		`      -D|--dump-header) dump="$2"; shift 2 ;;`,
		`      -X) method="$2"; shift 2 ;;`,
		`      -I) method=HEAD; shift ;;`,
		`      -w|-u|-H) shift 2 ;;`,
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
		`      [ -n "$dump" ] && printf 'HTTP/2 %s\r\nContent-Type: %s\r\n' ` + shellQuote(reg.manifestStatus) + ` ` + shellQuote(reg.contentType) + ` > "$dump"`,
		`      printf '%s' ` + shellQuote(reg.manifestStatus) + `; return 0 ;;`,
		`    *"/manifests/"*)`,
		`      tag="${url##*/manifests/}"`,
		`      if [ "$method" = PUT ]; then`,
		`        printf 'PUT-TAG %s\n' "$tag" >&2`,
		`        printf 'PUT-BODY %s\n' "$(cat "$body")" >&2`,
		`        printf '%s' ` + shellQuote(reg.putStatus) + `; return 0`,
		`      fi`,
		`      resolved="$(awk -v t="$tag" '$1 == t { print $2 }' "$STUB_DIR/resolves.txt")"`,
		`      [ -n "$dump" ] && printf 'HTTP/2 %s\r\nDocker-Content-Digest: %s\r\n' ` + shellQuote(reg.headStatus) + ` "$resolved" > "$dump"`,
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
func stepNames(t *testing.T) []string {
	t.Helper()

	var names []string
	for _, match := range stepName.FindAllStringSubmatch(jobBlock(t, publishWorkflow, publishJob), -1) {
		names = append(names, strings.TrimSpace(match[1]))
	}
	if len(names) == 0 {
		t.Fatalf("%s, job %q: no steps found", publishWorkflow, publishJob)
	}
	return names
}

// stepBlock returns the text of one step, from its `- name:` line to the next
// step at the same indentation.
func stepBlock(t *testing.T, name string) string {
	t.Helper()

	block := jobBlock(t, publishWorkflow, publishJob)
	header := "      - name: " + name + "\n"
	start := strings.Index(block, header)
	if start < 0 {
		t.Fatalf("%s, job %q: no step named %q", publishWorkflow, publishJob, name)
	}
	rest := block[start+len(header):]

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
func declaredEnv(t *testing.T, step string) map[string]string {
	t.Helper()

	block := stepBlock(t, step)
	marker := "        env:\n"
	start := strings.Index(block, marker)
	if start < 0 {
		return nil
	}

	env := map[string]string{}
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		entry := strings.TrimPrefix(line, "          ")
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
func stepScript(t *testing.T, name string) string {
	t.Helper()

	block := stepBlock(t, name)
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

// jobBlock returns the text of one job, from its header to the next job header
// at the same indentation. It fails closed: a renamed job is a failure here,
// never a silently empty search.
func jobBlock(t *testing.T, workflow, job string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(workflow)))
	if err != nil {
		t.Fatalf("read %s: %v", workflow, err)
	}
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")

	header := "\n  " + job + ":\n"
	start := strings.Index(content, header)
	if start < 0 {
		t.Fatalf("%s: no job named %q", workflow, job)
	}
	rest := content[start+len(header):]

	if next := jobHeader.FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
}

// repoRoot walks up from the test's working directory to the module root.
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
