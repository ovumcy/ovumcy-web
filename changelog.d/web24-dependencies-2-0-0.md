### Dependencies

- **Everything that moved between v1.9.2 and v2.0.0, in one place.** The advisory-driven bumps
  have their own entries (the Go toolchain under *Security*; `go-jose`, `brace-expansion`,
  `@parcel/watcher`, `minimatch` and `@humanfs/node` in this section). The rest came in as routine
  Dependabot group updates; the number of those pull requests says nothing about how many
  advisories were fixed.
  - **Runtime — what ships in the image.** Go 1.26.5 → 1.27.3 (`go.mod` and the
    `golang:1.27.3-alpine3.24` builder stage). Direct modules: `gofiber/fiber/v3` 3.4.0 → 3.6.1
    (with `valyala/fasthttp` now a direct dependency at 1.77.0), `coreos/go-oidc/v3` 3.20.0 →
    3.21.0, `jackc/pgx/v5` 5.10.0 → 5.11.0, `gorm.io/driver/postgres` 1.6.0 → 1.6.3,
    `golang.org/x/crypto` 0.54.0 → 0.58.0, `x/net` 0.57.0 → 0.62.0, `x/oauth2` 0.36.0 → 0.37.0,
    `x/sys` 0.47.0 → 0.49.0; indirect `go-jose/v4` 4.1.4 → 4.1.6, `klauspost/compress` 1.19.0 →
    1.20.3, `x/text` 0.40.0 → 0.43.0. The `runtime-assets` stage moves from Alpine 3.24.1 to
    3.24.3 with `tzdata` and `ca-certificates` now version-pinned; the final stage is still
    `scratch`. `THIRD_PARTY_LICENSES.md` follows every module move.
  - **Example deployments.** The Caddy reverse-proxy examples move from `caddy:2.11.4-alpine` to
    `caddy:2.11.7-alpine`.
  - **Frontend build and test toolchain — development only, never in the image.**
    `htmx.org` 2.0.10 → 2.0.11 (the committed `htmx.min.js` is regenerated from it),
    `@playwright/test` 1.61.1 → 1.64.1, `eslint` 10.7.0 → 10.13.0, `jsdom` 29.1.1 → 30.1.3,
    `otplib` 13.4.1 → 13.5.0, `globals` 17.7.0 → 17.13.0; `typescript`, `typescript-eslint` and
    `@types/node` are new, for the type check over the e2e suite.
  - **CI tooling and actions.** `golang.org/x/tools` v0.52.0 is now a module dependency, and the
    analyzers (staticcheck v0.8.1, golangci-lint v2.13.2, gosec v2.29.0) are built against it
    because Go 1.27 export data needs it. Pinned actions moved by minor and patch releases only:
    `actions/checkout`, `setup-node`, `upload-artifact`, `download-artifact`,
    `attest-build-provenance` (4.1.1 → 4.2.2), the `docker/*` build, login, Buildx and QEMU
    actions, `github/codeql-action` (4.37.1 → 4.39.1), `codecov/codecov-action` and
    `ossf/scorecard-action` (2.4.3 → 2.5.0); `oras-project/setup-oras` 2.0.3 is new. Every
    action stays pinned by commit SHA.
