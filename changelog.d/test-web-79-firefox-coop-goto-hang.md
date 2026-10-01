none

Test-harness change only: the firefox cross-browser lane turns off COOP browsing-context swaps in its test browser, which made a fresh context's first navigation hang, and drops the 90s timeout and video-off workarounds that did not help.
