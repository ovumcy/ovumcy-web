### Security

- **A "page not found" no longer echoes the address it was opened at.** Every page renders the
  address it was requested at back into its own markup — the language switcher carries it as the
  `next` field, and the footer privacy link carries it into a new outgoing URL. On a page that is
  not found there is no route behind that address, so all of it comes from whoever wrote the link:
  opening `/someone@example.com/nowhere` showed that address in the page and, through the footer
  link, carried it onward into browser history, the `Referer` header and the `/privacy` entry in
  the server access log. Nothing stored was exposed and the value was never trusted for navigation
  — it came from the visitor's own link — but a value nobody typed had no business being shown or
  forwarded. The not-found page now renders a fixed address of its own, so nothing from the request
  reaches the markup. Switching language there still returns you to the same page, and the footer
  privacy link still appears; because that fixed address names no section, the navigation marks no
  current section on a page that is not one, which is how it already behaved.
