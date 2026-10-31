### Security

- **A calendar subscribe link spelled with an upper-case `.ICS` is masked in the logs too.** The
  log redaction that hides the feed token in a request path matched only a lower-case `.ics`
  suffix, while the feed itself and its rate limit accept any case, so a path that fell back to
  the raw request URL could carry the token verbatim. The mask now ignores the suffix's case.
