# syntax=docker/dockerfile:1

FROM golang:1.26.4-alpine3.22@sha256:727cfc3c40be55cd1bc9a4a059406b28a059857e3be752aa9d09531e12c20c56 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY web/static ./web/static

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/ovumcy ./cmd/ovumcy

FROM alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4 AS runtime-assets
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
COPY --from=builder --chown=10001:10001 /src/internal/templates /app/internal/templates
COPY --from=builder --chown=10001:10001 /src/internal/i18n /app/internal/i18n
COPY --from=builder --chown=10001:10001 /src/web/static /app/web/static

USER 10001:10001

EXPOSE 8080
ENV DB_PATH=/app/data/ovumcy.db
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/ovumcy", "healthcheck"]
CMD ["/app/ovumcy"]
