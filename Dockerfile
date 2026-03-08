# ============================================
# HotPlex All-in-One Image (Multi-Stage Build)
# Developer productivity tool - includes full toolchain
# ============================================
# Build: docker build --build-arg CACHEBUST=$(date +%s) -t hotplex:latest .
# Or:    make docker-build
# Run:   docker compose up -d
# ============================================

# ============================================
# Stage 1: Builder (NO CACHE - always rebuild)
# ============================================
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go.mod/go.sum first for dependency caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build (this layer always rebuilds)
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o hotplexd ./cmd/hotplexd

# ============================================
# Stage 2: Runtime Base (cached)
# ============================================
FROM golang:1.25-alpine AS runtime-base

WORKDIR /app

# System packages (cached)
RUN apk add --no-cache \
    # Version control
    git \
    github-cli \
    # Build tools
    make \
    bash \
    # Modern file tools (Claude Code recommended)
    ripgrep \
    fd \
    bat \
    fzf \
    eza \
    # Network debugging
    curl \
    jq \
    wget \
    netcat-openbsd \
    openssl \
    # Node.js (for Claude Code)
    nodejs \
    npm \
    # Code quality
    golangci-lint \
    # Process management
    procps \
    htop \
    # Config processing
    yq \
    # Script runtime
    python3 \
    # Editor
    vim \
    # Timezone support
    tzdata \
    # Archive tools
    zip \
    # Runtime deps
    ca-certificates

# ============================================
# Stage 3: Go tools (cached)
# ============================================
FROM runtime-base AS runtime-tools

RUN go install github.com/air-verse/air@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest

# ============================================
# Stage 4: Third-party tools (cached)
# ============================================
FROM runtime-tools AS runtime-all

RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then ARCH="x86_64"; \
    elif [ "$ARCH" = "aarch64" ]; then ARCH="aarch64"; fi && \
    wget -qO /usr/local/bin/websocat \
    "https://github.com/vi/websocat/releases/download/v1.14.1/websocat.${ARCH}-unknown-linux-musl" && \
    chmod +x /usr/local/bin/websocat

# Install Claude Code via npm
RUN npm install -g @anthropic-ai/claude-code@latest

# ============================================
# Stage 5: Final Image
# ============================================
FROM runtime-all AS final

# Copy binary from builder (always fresh)
COPY --from=builder /build/hotplexd /app/hotplexd

# Copy and setup entrypoint script
COPY docker-entrypoint.sh /app/docker-entrypoint.sh

# User setup
ARG HOST_UID=1000
RUN adduser -D -u ${HOST_UID} hotplex && \
    mkdir -p /home/hotplex/go/pkg/mod /home/hotplex/.cache/go-build && \
    chmod +x /app/docker-entrypoint.sh && \
    chown -R hotplex:hotplex /home/hotplex /app

# Set up Go environment for hotplex user
ENV GOPATH=/home/hotplex/go
ENV GOCACHE=/home/hotplex/.cache/go-build
ENV PATH="${GOPATH}/bin:${PATH}"

# Verify installations
RUN go version && \
    gh --version && \
    websocat --version && \
    claude --version || claude-code --version

USER hotplex

EXPOSE 8080
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/hotplexd"]
