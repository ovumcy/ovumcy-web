### Security

- **A crafted link can no longer plant its query parameters in the page it opens.** Every page
  renders the address it was requested at back into its own markup — the language switcher carries
  it as the `next` field, and the footer privacy link carries it into a new outgoing URL. That
  address was taken from the request verbatim, so opening `/login?email=someone@example.com` echoed
  that address into the page, and following the footer link carried it onward into browser history,
  the `Referer` header and the `/privacy` entry in the server access log. Nothing stored was
  exposed and the value was never trusted for navigation, but a value the visitor never typed had
  no business being shown or forwarded. The rendered address now keeps only the parameters the
  pages actually read — the calendar's `month`, `day`, `selected` and `edit`, the onboarding
  `step`, and the privacy page's `back` — and drops everything else whatever it contains. Because
  `back` holds a path of its own, its query is filtered by the same rule one level deeper, and the
  privacy page's back link is filtered alongside it. A query the server cannot parse now yields the
  bare path rather than being passed through. Normal navigation is unchanged: the calendar keeps
  its month while you switch languages, and the privacy page still returns you where you came from.
