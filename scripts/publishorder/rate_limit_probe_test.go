package publishorder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// TestTheRateLimitProbeNeverFailsTheJobNorPrintsTheCredential runs the probe
// step's real script under the flags its own `shell:` gives it, with `curl`
// shadowed. The step says two things and nothing else holds it to either: no
// answer from Docker Hub ends it — it sits ahead of the mirror, so a probe that
// failed would skip the mirror after GHCR had already published — and the
// login it reads reaches curl on stdin, never in an argument or the log.
func TestTheRateLimitProbeNeverFailsTheJobNorPrintsTheCredential(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	script := stepScript(t, job, mirrorProbeStep)
	python := requirePython(t)

	const (
		credential = "U0VDUkVUQjY0"
		token      = "TOKEN-XYZ"
	)
	goodConfig := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + credential + `"}}}`

	for _, testCase := range []struct {
		name string
		// mode is how the shadowed Docker Hub answers: `unreachable` never
		// answers, `head-fails` issues a token and drops the HEAD, `not-json`
		// answers the token request 200 with a page, `answers` serves both.
		mode   string
		config string
		want   []string
	}{
		{
			name:   "Docker Hub answers both requests",
			mode:   "answers",
			config: goodConfig,
			want: []string{
				"inline auth under https://index.docker.io/v1/: True",
				"anonymous: ratelimit-remaining: 42;w=3600",
				"mirror-login: ratelimit-remaining: 42;w=3600",
				"mirror-login: docker-ratelimit-source: account-id",
				"CREDENTIAL ON STDIN", "TOKEN ON STDIN",
			},
		},
		{
			name:   "Docker Hub never answers",
			mode:   "unreachable",
			config: goodConfig,
			want:   []string{"anonymous: the token request got no answer", "mirror-login: the token request got no answer"},
		},
		{
			name:   "the HEAD fails after a token is issued",
			mode:   "head-fails",
			config: goodConfig,
			want:   []string{"CREDENTIAL ON STDIN", "TOKEN ON STDIN"},
		},
		{
			name:   "the token response is not JSON",
			mode:   "not-json",
			config: goodConfig,
			want:   []string{"mirror-login: the token response carried no token"},
		},
		{
			name:   "the docker config is not JSON",
			mode:   "answers",
			config: "{",
			want:   []string{"mirror-login: no inline auth"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dockerConfig := t.TempDir()
			if err := os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(testCase.config), 0o600); err != nil {
				t.Fatalf("write the docker config: %v", err)
			}

			preamble := `mode=` + shellQuote(testCase.mode) + "\n" +
				`python3() { ` + shellQuote(filepath.ToSlash(python)) + ` "$@"; }` + "\n" +
				`curl() { local in out='' prev='' a; in="$(cat)"; printf 'CURL %s\n' "$*" >&2; ` +
				`case "$in" in *` + credential + `*) printf 'CREDENTIAL ON STDIN\n' >&2;; esac; ` +
				`case "$in" in *` + token + `*) printf 'TOKEN ON STDIN\n' >&2;; esac; ` +
				`for a in "$@"; do [ "$prev" = -o ] && out="$a"; prev="$a"; done; ` +
				`[ "$mode" = unreachable ] && return 28; ` +
				`if [ -n "$out" ]; then if [ "$mode" = not-json ]; then printf '<html>' > "$out"; else printf '{"token":"%s"}' ` + shellQuote(token) + ` > "$out"; fi; printf 200; return 0; fi; ` +
				`[ "$mode" = head-fails ] && return 28; ` +
				`printf 'HTTP/2 200\r\nratelimit-limit: 100;w=3600\r\nratelimit-remaining: 42;w=3600\r\ndocker-ratelimit-source: account-id\r\n\r\n'; }`

			command := runBashScript(t, bash, job, mirrorProbeStep, preamble+"\n"+script)
			command.Env = append(os.Environ(),
				"DOCKER_CONFIG="+filepath.ToSlash(dockerConfig),
				"MIRROR_PATH=example/app",
				"MANIFEST_ACCEPT=application/vnd.oci.image.index.v1+json",
			)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("the probe failed the job (%v), which would skip the mirror after GHCR published:\n%s", err, output)
			}
			for _, want := range testCase.want {
				if !strings.Contains(string(output), want) {
					t.Errorf("the probe did not print %q:\n%s", want, output)
				}
			}
			for _, secret := range []string{credential, token} {
				if strings.Contains(string(output), secret) {
					t.Errorf("the probe put %q in an argument or the log:\n%s", secret, output)
				}
			}
		})
	}
}

// requirePython resolves the interpreter the step's `python3` stands for: a
// runner has `python3`, a Windows checkout may have only `python`, and a
// `python3` that is a store advert runs nothing.
func requirePython(t *testing.T) string {
	t.Helper()

	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if output, err := exec.Command(path, "--version").CombinedOutput(); err == nil && strings.HasPrefix(string(output), "Python 3") {
			return path
		}
	}
	t.Skip("no working python3 or python on PATH")
	return ""
}
