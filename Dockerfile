# ==============================================================================
# Multi-Stage Production Dockerfile for CAMS TPS Monitoring System
# Target Architecture: linux/amd64
# ==============================================================================

# Stage 1: Build stage
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache ca-certificates tzdata git

WORKDIR /build

# Cache Go modules layer
COPY go.mod go.sum ./
RUN go mod download

# Copy application source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/

# Compile standalone static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -installsuffix cgo \
    -ldflags="-w -s -X main.version=1.0.0" \
    -o /build/aggregator ./cmd/aggregator

# Stage 2: Minimal hardened runtime stage
FROM alpine:3.19

# Security: Install CA certificates and timezone database
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 -s /sbin/nologin -g appuser appuser

WORKDIR /app

# Copy binary and database migrations
COPY --from=builder /build/aggregator /usr/local/bin/aggregator
COPY --from=builder /build/migrations /app/migrations

# Set file ownership to unprivileged user
RUN chown -R appuser:appuser /app

# Security: Drop root privileges
USER appuser:appuser

# Expose HTTP health and metrics port
EXPOSE 8080

# Container healthcheck
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --spider -q http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/aggregator"]
