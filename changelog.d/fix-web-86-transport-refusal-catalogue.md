### Fixed

- A transport-level refusal produced before the request's language was resolved — a POST /lang
  form submission or an HTMX fragment refused by the request-body cap, the request-head cap, an
  expired request budget, or a CSRF denial — rendered its status page in the raw machine key
  (`request_too_large`, `common.back`) in every locale instead of the owner's language. The shared
  status-fragment renderer now resolves the request's own locale catalogue before rendering,
  whichever layer produced the refusal, so the page reads in the owner's language exactly as a
  refusal produced after routing already did.
