# ============================================
# Stage 1: Build binary
# ============================================
FROM golang:1.25-alpine AS builder

WORKDIR /build
RUN apk add --no-cache git make curl
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags="-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o hotplexd ./cmd/hotplexd

# ============================================
# Stage 2: Install Claude Code
# ============================================
FROM alpine:3.19 AS claude-installer
RUN apk add --no-cache curl jq npm nodejs

ARG CLAUDE_VERSION="latest"
ENV CLAUDE_VERSION=${CLAUDE_VERSION}

# 安装 Claude Code 到临时位置
RUN npm install -g @anthropic-ai/claude-code

# ============================================
# Stage 3: Final runtime image
# ============================================
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache ca-certificates nodejs npm git

# Copy binary
COPY --from=builder /build/hotplexd /usr/local/bin/

# Copy Claude Code directly using npm
RUN npm install -g @anthropic-ai/claude-code@latest

# Verify CLI
RUN /usr/local/bin/claude --version || /usr/local/bin/claude-code --version

# Create user matching host UID (for config file access)
ARG HOST_UID=1000
RUN adduser -D -u ${HOST_UID} hotplex
USER hotplex

EXPOSE 8080
ENTRYPOINT ["hotplexd"]
