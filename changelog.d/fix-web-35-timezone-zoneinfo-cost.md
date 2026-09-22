### Security

- **`X-Ovumcy-Timezone` no longer costs a zoneinfo read on public or unauthenticated requests.**
  The header (and its `ovumcy_tz` cookie fallback) admits any short identifier, so a caller sending
  a distinct name on every request paid one zoneinfo file read and parse per request, on every
  route without a rate limit — every HTML page and the unlimited `/api/v1` handlers. Resolution now
  runs only once a request's session has verified, so an anonymous request (or one carrying a
  forged session cookie) takes the server's configured fallback zone instead of buying a zoneinfo
  read.
