# syntax=docker/dockerfile:1

FROM golang:1.24.13-alpine3.22@sha256:3641e0d9b931dc4f2f185dcd669c4679670e9277c8166a838ddb98a2d4389cb5 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY web/static ./web/static

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/ovumcy ./cmd/ovumcy

FROM alpine:3.22.3@sha256:55ae5d250caebc548793f321534bc6a8ef1d116f334f18f4ada1b2daad3251b2 AS runtime
WORKDIR /app

RUN apk add --no-cache tzdata ca-certificates \
    && addgroup -S ovumcy \
    && adduser -S -G ovumcy -h /app ovumcy

COPY --from=builder --chown=ovumcy:ovumcy /out/ovumcy /app/ovumcy
COPY --from=builder --chown=ovumcy:ovumcy /src/internal/templates /app/internal/templates
COPY --from=builder --chown=ovumcy:ovumcy /src/internal/i18n /app/internal/i18n
COPY --from=builder --chown=ovumcy:ovumcy /src/web/static /app/web/static

RUN mkdir -p /app/data && chown -R ovumcy:ovumcy /app

USER ovumcy:ovumcy

EXPOSE 8080
ENV DB_PATH=/app/data/ovumcy.db
CMD ["/app/ovumcy"]
