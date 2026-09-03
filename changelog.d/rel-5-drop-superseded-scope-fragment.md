none

Housekeeping only. Drops the changelog fragment that had recorded REL-5's earlier scope
decision. That decision was reversed on 2026-09-03 when REL-5 was implemented as written
(`rel-5-e2e-cross-browser-gates-publish.md`), so the fragment now asserts the opposite of what
the tree does. Its two workflow citations were stale besides: the "Not a required status check"
line it pointed at, and the `publish-image` `needs:` range it quoted, had both moved.
