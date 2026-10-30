none

Tests only: the doc-read guard probes only files a pull request can carry — tracked, or untracked
and not ignored — so ignored local artifacts (an e2e run's logs under .tmp) no longer push its probe
count past the budget on the machine that holds them.
