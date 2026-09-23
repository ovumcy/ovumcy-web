none

Test-only: the `publishorder`, `releasegate` and `backuprestoredoc` Go test
harnesses ran the scripts they extract from `docker-image.yml` and the
self-hosting runbook by handing them to `bash -c "<script>"`, which on
Windows silently truncates a long enough command-line argument — the cut can
land inside a comment, so the shell exits 0 having never reached the script's
later output writes — and which never applies the errexit the real steps run
under. Each harness now writes the script to a file under `t.TempDir()` and
runs that file. The two workflow harnesses read the flags off the step's own
`shell:` key (`shell: bash` compiles to `bash --noprofile --norc -eo pipefail
{0}`) and fail on a step that declares any other shell or none, which runs as
`bash -e {0}` without pipefail; the runbook harness keeps its documented
`set -euo pipefail` prefix. A positive control per harness pins errexit and
pipefail through the real helper, the release-gate refusal cases now also
refuse a refusal that came from a harness stub, and a source scan of every
package under `scripts/` fails on any call that passes a shell a `-c` flag
spelled as a literal anywhere, built by concatenation, or held in a variable
or a named constant, unless the site is named in the scan's own
allowlist as a fixed one-line probe the test wrote itself, one `-c` site per
allowlist entry.
