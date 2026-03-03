# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY web/static ./web/static

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/ovumcy ./cmd/ovumcy

FROM alpine:3.21 AS runtime
WORKDIR /app

RUN apk add --no-cache tzdata ca-certificates

COPY --from=builder /out/ovumcy /app/ovumcy
COPY --from=builder /src/internal/templates /app/internal/templates
COPY --from=builder /src/internal/i18n /app/internal/i18n
COPY --from=builder /src/web/static /app/web/static

RUN mkdir -p /app/data

EXPOSE 8080
ENV DB_PATH=/app/data/ovumcy.db
CMD ["/app/ovumcy"]
