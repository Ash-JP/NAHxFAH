# =============================================================================
# WIFI HUNTER AR — Go Server Dockerfile
# =============================================================================
# Multi-stage build:
#   Stage 1: Build the Go binary with full SDK
#   Stage 2: Minimal Alpine runtime image
# =============================================================================

# ---------------------------------------------------------------------------
# Stage 1: Builder
# ---------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Cache dependencies first (layer caching optimization)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -extldflags '-static'" \
    -o /build/wifi-hunter-server \
    ./cmd/server

# ---------------------------------------------------------------------------
# Stage 2: Runtime
# ---------------------------------------------------------------------------
FROM alpine:3.20

# Security: non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Copy timezone data and CA certs from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /build/wifi-hunter-server /usr/local/bin/wifi-hunter-server

# Switch to non-root
USER appuser

EXPOSE 8000

# Healthcheck (Docker-native)
HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- http://localhost:8000/health || exit 1

ENTRYPOINT ["/usr/local/bin/wifi-hunter-server"]
