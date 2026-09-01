### Security

- **The patch-coverage gate now runs in the merge queue, not only on the pull request.** It is a
  required branch-protection check, but it carried `if: github.event_name == 'pull_request'`, so
  the merge-queue suite that actually decides admission reported it `skipped` — and a skipped
  required check counts as satisfied. A queued change could merge having never had its patch
  coverage judged there. The job now also runs on `merge_group`, with its base-ref resolution
  branched the same way the `changes` job already splits it (`pull_request.base.ref` does not
  exist under `merge_group`; the queue's base lives in `merge_group.base_sha` instead).

- **The three Trivy invocations that gate a build no longer hide unfixed CRITICAL/HIGH findings
  from themselves or from the Security tab.** `--ignore-unfixed` was set on the filesystem scan and
  the image scan (both required checks, both writing SARIF that gets uploaded), and on the
  pre-publish re-scan before `:latest`/a release tag goes out. A CRITICAL vulnerability with no
  upstream fix yet — the ordinary case for a fresh CVE — tripped none of them and never reached the
  Security tab either. All three now scan and gate on the full HIGH/CRITICAL set.
