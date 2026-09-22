none

Test-only: the `publishorder`, `releasegate` and `backuprestoredoc` Go test
harnesses ran the scripts they extract from `docker-image.yml` and the
self-hosting runbook by handing them to `bash -c "<script>"`, which on
Windows silently truncates a long enough command-line argument — the cut can
land inside a comment, so the shell exits 0 having never reached the script's
later output writes — and which never applies the errexit the real steps run
under (`shell: bash` compiles to `bash --noprofile --norc -eo pipefail {0}`).
Every such site now writes the script to a file under `t.TempDir()` and runs
it as a file, the way the workflow (and, for the runbook harness, the
harness's own documented fail-fast convention) actually does. Short
tool-probe one-liners are unchanged.
