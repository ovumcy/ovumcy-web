### Fixed

- **Onboarding step 2's refusal of the removed `age_group` field now covers every shape the
  endpoint accepts, not only a form body and a JSON string.** The presence check told "the field is
  not there" apart from "the field is there but malformed" by inspecting the outcome of a typed
  decode, which reads a JSON number or `null` for `age_group` as absent (the typed probe fails with
  a type error, not a missing-field error), and only ever inspected `PostArgs`, which fasthttp never
  populates for a multipart body or for a value the client put in the URL's own query string. A
  client using any of those four shapes was told `200` with the field silently dropped — the exact
  removed-field-reads-as-success outcome the original refusal exists to prevent, one spelling over.
  Presence is now decided from the raw request the same way for every shape: the parsed JSON object's
  own keys for a JSON body, and the URL query string, the urlencoded body, and the multipart form in
  that order otherwise — the same three sources `FormValue` itself reads from. Two narrower routes
  around the same guard are closed alongside it: the query-string check now runs unconditionally
  before branching on Content-Type, so a JSON request carrying `age_group` only in its URL (not its
  body) is caught too; and the JSON-key match is now case-insensitive, matching the fallback
  `Bind().Body` itself uses, so a body naming `Age_Group` or `AGE_GROUP` — exactly the off-spec
  casing a client still on the old contract might send — is refused rather than silently dropped.
