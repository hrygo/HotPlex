# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build tools
RUN apk add --no-cache make git

# Copy dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the gateway binary
RUN make build

# Final stage
FROM alpine:3.21

WORKDIR /app

# Install runtime dependencies (e.g., node for Claude Code, or other agents)
RUN apk add --no-cache \
    ca-certificates \
    sqlite-libs \
    libc6-compat \
    curl

# Create necessary directories
RUN mkdir -p /app/bin /app/configs /app/data

# Copy binary from builder
COPY --from=builder /app/bin/gateway /app/bin/gateway

# Expose gateway port
EXPOSE 9080 9081

# Set default environment variables
ENV HOTPLEX_CONFIG=/app/configs/config.yaml

# Set entrypoint
ENTRYPOINT ["/app/bin/gateway"]
CMD ["--config", "/app/configs/config.yaml"]
