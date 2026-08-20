# syntax=docker/dockerfile:1

# The builder tag must stay equal to the `go` directive in go.mod. Every CI Go
# job resolves its toolchain with `go-version-file: go.mod`, so a builder ahead
# of that directive ships a binary compiled by a toolchain no unit, race or e2e
# run ever exercised — GOTOOLCHAIN only ever upgrades, never downgrades, so the
# newer image simply wins. A release candidate reached this line that way. CI
# now asserts the equality (`Builder toolchain matches go.mod` in
# .github/workflows/ci.yml), and Dependabot is told to leave pre-release tags of
# this image alone.
FROM golang:1.26.6-alpine3.24@sha256:1a9c10cf505a9e6b1e96ea77ebdbfe79a0f10380181faf88bc3b51d7e4315fae AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY web ./web

# Release identity for the asset cache-bust token (?v=<token>). The build
# context carries no .git, so without this the binary cannot learn its own
# revision and falls back to a per-start timestamp token; CI passes the commit
# sha. Empty default keeps plain `docker build .` working unchanged.
ARG BUILD_REVISION=""

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w -X main.buildVersion=${BUILD_REVISION}" -o /out/ovumcy ./cmd/ovumcy

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS runtime-assets
WORKDIR /app

RUN apk add --no-cache tzdata ca-certificates \
    && addgroup -S -g 10001 ovumcy \
    && adduser -S -D -H -u 10001 -G ovumcy -h /app ovumcy \
    && mkdir -p /app/data

FROM scratch AS runtime
WORKDIR /app

COPY --from=runtime-assets /etc/passwd /etc/group /etc/
COPY --from=runtime-assets /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=runtime-assets /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=runtime-assets --chown=10001:10001 /app/data /app/data
COPY --from=builder --chown=10001:10001 /out/ovumcy /app/ovumcy

USER 10001:10001

EXPOSE 8080
ENV DB_PATH=/app/data/ovumcy.db
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/ovumcy", "healthcheck"]
CMD ["/app/ovumcy"]
