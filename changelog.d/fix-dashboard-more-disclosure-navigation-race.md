none

Test-only fix: a dashboard spec navigated on top of the page's in-flight htmx
lazy-loads, which aborted the navigation and surfaced as a flaky e2e shard. No
product code involved.
