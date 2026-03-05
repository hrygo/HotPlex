# ============================================
# HotPlex All-in-One Image
# Developer productivity tool - includes full toolchain
# ============================================
# Build: docker build -t hotplex:latest .
# Run:   docker compose up -d
# ============================================

FROM golang:1.25-alpine

WORKDIR /app

# Install complete development toolchain
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
    # Network debugging (essential for WebSocket Gateway)
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

# Install Go tools
RUN go install github.com/air-verse/air@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest

# Install websocat (WebSocket client for debugging)
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then ARCH="x86_64"; \
    elif [ "$ARCH" = "aarch64" ]; then ARCH="aarch64"; fi && \
    wget -qO /usr/local/bin/websocat \
    "https://github.com/vi/websocat/releases/download/v1.14.1/websocat.${ARCH}-unknown-linux-musl" && \
    chmod +x /usr/local/bin/websocat

# Install Claude Code
RUN npm install -g @anthropic-ai/claude-code@latest

# Build binary
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags="-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o hotplexd ./cmd/hotplexd

# Verify installations
RUN go version && \
    gh --version && \
    websocat --version && \
    nc -h 2>&1 | head -1 && \
    openssl version && \
    claude --version || claude-code --version

# Create user with Go environment in home directory
ARG HOST_UID=1000
RUN adduser -D -u ${HOST_UID} hotplex && \
    mkdir -p /home/hotplex/go/pkg/mod /home/hotplex/.cache/go-build && \
    chown -R hotplex:hotplex /home/hotplex

# Set up Go environment for hotplex user
ENV GOPATH=/home/hotplex/go
ENV GOCACHE=/home/hotplex/.cache/go-build
ENV PATH="${GOPATH}/bin:${PATH}"

USER hotplex

EXPOSE 8080
ENTRYPOINT ["/app/hotplexd"]
