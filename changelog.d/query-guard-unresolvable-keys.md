none

Tests and a code comment only: the guard that keeps auth inputs out of the query string now also
flags a FormValue, Query or Params read whose key it cannot resolve to a literal, so a wrapper that
takes the field name as a parameter no longer passes it; the OIDC callback, which reads provider
parameters from the query by design, is exempted by name. No runtime behavior changes.
