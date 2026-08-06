### Internal

- **Config entries that can no longer match anything are gone.** Trivy's filesystem scan skipped a
  `.tmp_docker_ctx` directory nothing has ever created; `.dockerignore` and `.github/CODEOWNERS`
  still named a `docker/` tree removed in `90d85a4`; `.dockerignore` excluded a
  `tailwind.config.js` that Tailwind v4 does not use; and both ignore files carried `.tmp_*/`
  alongside the `.tmp*/` that already subsumes it.
- **Two build outputs are now marked generated.** `web/static/js/theme-bootstrap.js` and
  `timezone-bootstrap.js` are written by `scripts/build-js.mjs` from `web/src/js/`, and were the
  only build products missing `linguist-generated` in `.gitattributes`.
- **The mutation shard guard's comment now describes the failure it prevents.** It cited a
  `.mutation/<slug>.md` "pending first weekly CI run" state that exists in no file and under no
  slug spelling; what the guard actually stops is a shard passing green having uploaded nothing.
