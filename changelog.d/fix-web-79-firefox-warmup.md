none

Test-only: the cross-browser smoke spec now primes the firefox Browser process once per
CI worker, in a firefox-only `beforeAll`, before any of the file's real tests run. A worker
reuses one firefox process across the file's tests, and that process can still be cold when
a later test opens its first fresh-context navigation — `test.slow()`'s per-test timeout
stretch alone did not cover that (WEB-79).
