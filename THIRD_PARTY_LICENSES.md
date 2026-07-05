# Third-Party Licenses

Ovumcy is licensed under AGPL v3 (see [LICENSE](LICENSE)). This file lists third-party
software redistributed as part of the built application (e.g. embedded via `go:embed`,
container images, and release artifacts) along with their licenses.

## htmx

- **Project:** [htmx](https://htmx.org) ([source](https://github.com/bigskysoftware/htmx))
- **Version:** 2.0.10 (`node_modules/htmx.org`, see `package.json`)
- **Files:** `web/static/js/htmx.min.js`
- **License:** 0BSD (Zero-Clause BSD), per `node_modules/htmx.org/LICENSE` and its
  `package.json` (`"license": "0BSD"`)

```
Zero-Clause BSD
=============

Permission to use, copy, modify, and/or distribute this software for
any purpose with or without fee is hereby granted.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL
WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES
OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE
FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY
DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN
AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT
OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

0BSD imposes no attribution or notice-reproduction requirement, so this entry is listed
for transparency rather than to close a compliance gap.

## Tailwind CSS (build tooling, not redistributed)

- `web/static/css/tailwind.css` is generated output produced by the Tailwind CLI
  (`@tailwindcss/cli`, MIT licensed) at build time. It is compiled utility CSS
  authored by this project, not a redistribution of Tailwind's own source, so no
  separate attribution is required. Listed here for completeness.
