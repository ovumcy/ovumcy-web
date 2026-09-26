### Fixed

- A refusal rendered before the request's language was resolved — most visibly a POST /lang form
  submission or an HTMX request whose body or head exceeded the server's limits (413/431) —
  showed its status page with the raw machine key (`request_too_large`, `common.back`) in every
  locale instead of the owner's language. The shared status-fragment renderer now resolves the
  request's own locale catalogue before rendering, whichever layer produced the refusal, so the
  page reads in the owner's language as a refusal produced after routing already did.
