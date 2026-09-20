none

Internal hardening with no behaviour change. The OIDC step-up callback state
was matched in five places: once at the callback, before the one-time step-up
cookie is spent, once in the cross-site bounce before it parks anything for the
continue leg, and once again inside each of the three per-purpose completions.
The three per-purpose copies are removed and one check now stands at the
dispatch seam every completion passes through, so a purpose added later
inherits it rather than having to remember it.

Nothing a user can reach changes. The callback's own match still runs first and
still gates spending the cookie, so no request that was refused is now accepted
and none that completed now fails; the seam is only reached by a callback whose
state already matched, or by the same-site continue leg, which rebuilds the
exchange from the state it is checked against. A test guard pins that the check
stands ahead of the dispatch switch and that no completion carries a copy.
