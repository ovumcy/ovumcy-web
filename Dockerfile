# syntax=docker/dockerfile:1

FROM golang:1.25.9-alpine3.22@sha256:2c16ac01b3d038ca2ed421d66cea489e3cb670c251b4f8bbcfad2ebfb75f884c AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY web/static ./web/static

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/ovumcy ./cmd/ovumcy

FROM alpine:3.22.3@sha256:55ae5d250caebc548793f321534bc6a8ef1d116f334f18f4ada1b2daad3251b2 AS runtime-assets
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
CMD ["/app/ovumcy"]
