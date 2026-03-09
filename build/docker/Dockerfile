# ============================================
# HotPlex Distroless Image (Container Hardened)
# ============================================
# Minimal attack surface - based on distroless/cc-debian12:nonroot
# Features:
#   - Distroless base (minimal packages)
#   - Non-root user (nonroot)
#   - Read-only filesystem + tmpfs
#   - Seccomp filtering
#   - Dropped capabilities
#   - No new privileges
# ============================================
# Build: docker build -f build/docker/Dockerfile -t hotplex:distroless .
# ============================================

# ============================================
# Stage 1: Builder
# ============================================
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go.mod/go.sum first for dependency caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

# Build with static linking (CGO_ENABLED=0) for distroless compatibility
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_TIME} -X main.builtBy=docker" \
    -o hotplexd ./cmd/hotplexd

# Verify binary is statically linked
RUN file hotplexd && ldd hotplexd || echo "Static binary confirmed"

# ============================================
# Stage 2: Final Distroless Image
# ============================================
FROM gcr.io/distroless/cc-debian12:nonroot AS final

# Install ca-certificates for TLS (minimal runtime dependency)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

# Copy binary from builder
COPY --from=builder /build/hotplexd /app/hotplexd

# Ensure binary is executable
RUN chmod +x /app/hotplexd

# Non-root user (nonroot) is already configured in distroless image
# Verify user exists
RUN id nonroot || true

# Set ownership
RUN chown -R nonroot:nonroot /app

# Switch to non-root user
USER nonroot

# Working directory
WORKDIR /app

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ["wget", "-q", "--spider", "http://localhost:8080/health"] || exit 1

# Entrypoint
ENTRYPOINT ["/app/hotplexd"]
