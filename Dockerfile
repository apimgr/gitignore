# ============================================
# Build stage
# ============================================
FROM golang:alpine AS builder

# Why latest? Static binaries are self-contained, no Go runtime dependency.
# Using :latest gets latest Go features, security patches, and build optimizations.

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN apk add --no-cache git make ca-certificates tzdata

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

# Build static binary with all assets embedded
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE} -w -s" \
    -a -installsuffix cgo \
    -o gitignore \
    .

# ============================================
# Runtime stage - Alpine with minimal tools
# ============================================
FROM alpine:latest

# Why latest? Static binaries (CGO_ENABLED=0) have no runtime dependencies.
# Alpine version only affects runtime tools (curl, bash, ca-certificates).
# Using :latest ensures latest security patches without version maintenance.

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# Install runtime dependencies (curl, bash)
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    bash \
    && rm -rf /var/cache/apk/*

# Copy binary to /usr/local/bin
COPY --from=builder /build/gitignore /usr/local/bin/gitignore
RUN chmod +x /usr/local/bin/gitignore

# Environment variables
ENV PORT=80 \
    CONFIG_DIR=/config \
    DATA_DIR=/data \
    LOGS_DIR=/logs \
    ADDRESS=0.0.0.0 \
    DB_PATH=/data/db/gitignore.db

# Create directories
RUN mkdir -p /config /data /data/db /logs && \
    chown -R 65534:65534 /config /data /logs

# Metadata labels (OCI standard)
LABEL org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.authors="apimgr" \
      org.opencontainers.image.url="https://github.com/apimgr/gitignore" \
      org.opencontainers.image.source="https://github.com/apimgr/gitignore" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.vendor="apimgr" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.title="gitignore" \
      org.opencontainers.image.description="GitIgnore API Server - Comprehensive .gitignore template API - Single static binary" \
      org.opencontainers.image.documentation="https://github.com/apimgr/gitignore/blob/main/docs/README.md" \
      org.opencontainers.image.base.name="alpine:latest"

# Expose default port
EXPOSE 80

# Create mount points for volumes
VOLUME ["/config", "/data", "/logs"]

# Run as non-root user (nobody)
USER 65534:65534

# Health check using wget (alpine has wget by default)
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT:-80}/healthz || exit 1

# Run
ENTRYPOINT ["/usr/local/bin/gitignore"]
CMD []
