# =============================================================================
# Build Stage - Compile Go binary
# =============================================================================
FROM casjaysdev/go:latest AS builder

# git: required for go mod download; bash: for build scripts
RUN apk add --no-cache git bash

ARG TARGETARCH
ARG VERSION=dev
ARG BUILD_DATE
ARG COMMIT_ID

WORKDIR /build

# Copy go.mod first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -buildvcs=false \
    -trimpath \
    -ldflags "-s -w -X 'main.Version=${VERSION}' -X 'main.CommitID=${COMMIT_ID}' -X 'main.BuildDate=${BUILD_DATE}'" \
    -o /build/bin/gitignore ./src

# =============================================================================
# Runtime Stage - Minimal Alpine image
# =============================================================================
FROM alpine:latest

# ARGs for build-time values (set by docker build --build-arg)
ARG VERSION=dev
ARG BUILD_DATE
ARG COMMIT_ID
ARG LICENSE=MIT

# No LABEL blocks — all image metadata is applied as OCI annotations at build
# time by CI (docker/metadata-action annotations output). Annotations attach to
# the manifest index and are visible on multiarch pulls; LABEL applies only to
# per-platform layers. See dockerfile_conventions.md → OCI Annotations.

# Install required packages
# NOTE: Tor binary installed but NOT configured here - the server binary owns
# all Tor setup (see PART 31).
RUN apk add --no-cache \
    git \
    curl \
    bash \
    tini \
    tor

# NO directory creation - the binary creates and owns all directories based on
# CONFIG_DIR / DATA_DIR / DATABASE_DIR. Docker volume mounts auto-create mount
# points. Baking directories here would fight user overrides.

# Copy binary from builder stage (multi-stage build)
COPY --from=builder /build/bin/gitignore /usr/local/bin/gitignore

# Copy BUILD-TIME overlay (entrypoint.sh) from docker/rootfs/ into image
COPY docker/rootfs/ /

# Copy Dockerfile to image (for reference and backup)
COPY docker/Dockerfile /root/Dockerfile

# Make all binaries/scripts in /usr/local/bin executable (755)
RUN chmod 755 /usr/local/bin/*

# Set environment
# MODE is never baked into the image — the binary defaults to production and
# compose files set MODE explicitly. Only TZ is set here; the binary derives
# all paths from CONFIG_DIR / DATA_DIR (set by entrypoint.sh).
ENV TZ=America/New_York

# Expose internal port (always 80)
EXPOSE 80

# Stop signal for graceful shutdown
STOPSIGNAL SIGRTMIN+3

# Health check (long start period for services that need initialization)
HEALTHCHECK --start-period=10m --interval=5m --timeout=15s --retries=3 \
    CMD /usr/local/bin/gitignore --status || exit 1

# Use tini as init with signal propagation
# -p SIGTERM: propagate SIGTERM to child processes
ENTRYPOINT [ "tini", "-p", "SIGTERM", "--", "/usr/local/bin/entrypoint.sh" ]
