# syntax=docker/dockerfile:1

# ---- Build stage: holds the Go toolchain, discarded afterwards --------------
FROM golang:1.27-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Dependencies are copied and downloaded before the source, so editing a .go
# file does not invalidate the cached module download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a binary with no dynamic dependencies, so it runs on
# any base image regardless of libc. -ldflags="-s -w" strips symbol and debug
# tables; -trimpath keeps local paths out of the binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/hiredesk-api \
        ./cmd/api

# The development seeder, built the same way. It ships in the image so that
# `make seed` needs nothing installed on the host but Docker.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/hiredesk-seed \
        ./cmd/seed

# ---- Runtime stage: receives only the binary -------------------------------
FROM alpine:3.22

# A static Go binary carries no root CAs, so outbound HTTPS fails without these.
RUN apk add --no-cache ca-certificates

# Fixed high UID keeps ownership predictable on mounted volumes and avoids
# colliding with a host user.
RUN adduser -D -H -s /sbin/nologin -u 10001 appuser

COPY --from=builder --chown=appuser:appuser /out/hiredesk-api /usr/local/bin/hiredesk-api
COPY --from=builder --chown=appuser:appuser /out/hiredesk-seed /usr/local/bin/hiredesk-seed

# After the COPYs, since an unprivileged user cannot write to /usr/local/bin.
USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1

# Exec form, so the binary is PID 1 and receives SIGTERM directly. The shell
# form would not forward it, breaking graceful shutdown.
ENTRYPOINT ["/usr/local/bin/hiredesk-api"]
