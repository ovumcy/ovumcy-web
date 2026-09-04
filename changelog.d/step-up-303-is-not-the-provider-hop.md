### Fixed

`docs/openapi.yaml` no longer promises a `303` redirect to the provider's
authorize URL from the three OIDC step-up operations
(`POST /api/v1/users/current/password/step-up`, `.../data-wipe/step-up`,
`.../deletion/step-up`). That hop stopped being a redirect when the settings
page's CSP began pinning `form-action` to `'self'`: Chromium enforces the pin
across a form navigation's whole redirect chain, so a cross-origin `3xx` aborts
in the browser. The server hands back a same-origin `200` page whose
meta-refresh performs the hop instead — the behaviour the fourth operation of
the same class, `.../oidc/link/step-up`, already documented. A JSON caller was
never affected: it receives `200 {ok, redirect_url}` and always did.

The `303` these operations really can answer a browser with is the refusal
bounce back to `/settings`, which every `/api/v1/users/current` mutation shares
and none of the ~30 others declares; it is the cross-cutting HTML-surface
answer the spec's own preamble puts outside this contract, not a per-operation
outcome, so it is not declared here either.

A new guard keeps the class closed. Over-declaration in an OpenAPI spec is
generally undecidable — proving no path reaches a status is proving a negative,
which is why the existing per-operation reachability walk only reports
under-declaration. This one asks the opposite, positive question: does the
route's handler chain reach the same-origin interstitial helper? A hit is a
witness, and it settles the browser answer, so an operation that serves the
interstitial may not declare a `303`. Neither status guard could have caught
this on its own: the whole-server sweep passes because `303` is emitted all
over `internal/`, and the per-operation walk passes because `303` really is
reachable here — just not for the reason the spec gave. The falsehood lived in
the response description, and nothing in the suite reads one.
