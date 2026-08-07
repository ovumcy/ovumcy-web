### Internal

- **Removed `docs/screenshots/login.png`** (307 KB), which nothing referenced.
  The README shows six screenshots and the login screen is not among them, and
  the file sits outside the reusable asset set `docs/hero-demo.md` defines. The
  directory's other screenshots are all rendered by the README; the two files that
  are not, `ovumcy-promo-card.svg` and `ovumcy-social-preview.png`, are outbound
  assets consumed outside the repository and carry no in-repo reference by design.
