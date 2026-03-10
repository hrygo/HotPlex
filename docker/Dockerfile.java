# ============================================
# HotPlex + Java/Kotlin Stack
# ============================================
# Extends base HotPlex with Java/Kotlin productivity tools
# Build: docker build -f Dockerfile.java -t hotplex:java .
# ============================================

FROM golang:1.25-alpine

LABEL maintainer="HotPlex Team"
LABEL description="HotPlex AI Agent with Go + Java/Kotlin stack"

WORKDIR /app

# ============================================
# Core System Dependencies
# ============================================
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
    unzip \
    tar \
    # Java
    openjdk25 \
    # Font (for Java apps)
    fontconfig \
    ttf-dejavu \
    # Runtime deps
    ca-certificates

# ============================================
# Go Toolchain (HotPlex native)
# ============================================
RUN go install github.com/air-verse/air@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest && \
    go install golang.org/x/tools/cmd/goimports@latest && \
    go install mvdan.cc/gofumpt@latest && \
    go install honnef.co/go/tools/cmd/staticcheck@latest && \
    go install github.com/securego/gosec/v2/cmd/gosec@latest && \
    go install golang.org/x/vuln/cmd/govulncheck@latest && \
    go install github.com/golang/mock/mockgen@latest && \
    go install gotest.tools/gotestsum@latest && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# ============================================
# Java/Kotlin Stack Extensions
# ============================================
# Gradle
ENV GRADLE_VERSION=9.4
RUN wget -q https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip && \
    unzip gradle-${GRADLE_VERSION}-bin.zip && mv gradle-${GRADLE_VERSION} /opt/gradle && \
    rm gradle-${GRADLE_VERSION}-bin.zip
ENV GRADLE_HOME=/opt/gradle
ENV PATH="${GRADLE_HOME}/bin:${PATH}"

# Maven
ENV MAVEN_VERSION=3.9.13
RUN wget -q https://archive.apache.org/dist/maven/maven-3/${MAVEN_VERSION}/binaries/apache-maven-${MAVEN_VERSION}-bin.tar.gz && \
    tar -xzf apache-maven-${MAVEN_VERSION}-bin.tar.gz && mv apache-maven-${MAVEN_VERSION} /opt/maven && \
    rm apache-maven-${MAVEN_VERSION}-bin.tar.gz
ENV MAVEN_HOME=/opt/maven
ENV PATH="${MAVEN_HOME}/bin:${PATH}"

# Kotlin
ENV KOTLIN_VERSION=2.3.0
RUN wget -q https://github.com/JetBrains/kotlin/releases/download/v${KOTLIN_VERSION}/kotlin-compiler-${KOTLIN_VERSION}.zip && \
    unzip kotlin-compiler-${KOTLIN_VERSION}.zip && mv kotlinc /opt/kotlinc && \
    rm kotlin-compiler-${KOTLIN_VERSION}.zip
ENV KOTLIN_HOME=/opt/kotlinc
ENV PATH="${KOTLIN_HOME}/bin:${PATH}"

# ktlint (Kotlin linter)
RUN wget -qO /usr/local/bin/ktlint \
    "https://github.com/pinterest/ktlint/releases/download/1.5.0/ktlint" && \
    chmod +x /usr/local/bin/ktlint

# detekt (Kotlin static analysis)
RUN mkdir -p /opt/detekt && \
    wget -qO /opt/detekt/detekt-cli.jar \
    "https://github.com/detekt/detekt/releases/download/v1.23.7/detekt-cli-1.23.7.jar" && \
    echo '#!/bin/bash\njava -jar /opt/detekt/detekt-cli.jar "$@"' > /usr/local/bin/detekt && \
    chmod +x /usr/local/bin/detekt

# ============================================
# WebSocket & Claude Code
# ============================================
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then ARCH="x86_64"; \
    elif [ "$ARCH" = "aarch64" ]; then ARCH="aarch64"; fi && \
    wget -qO /usr/local/bin/websocat \
    "https://github.com/vi/websocat/releases/download/v1.14.1/websocat.${ARCH}-unknown-linux-musl" && \
    chmod +x /usr/local/bin/websocat

RUN npm install -g @anthropic-ai/claude-code@latest

# ============================================
# HotPlex Binary & Entrypoint
# ============================================
ARG TARGETARCH
COPY linux/${TARGETARCH}/hotplexd /app/hotplexd
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/hotplexd /app/docker-entrypoint.sh

# ============================================
# Verification
# ============================================
RUN go version && \
    java --version && \
    gradle --version && \
    mvn --version && \
    kotlinc -version && \
    websocat --version && \
    claude --version 2>/dev/null || true

# ============================================
# User Setup
# ============================================
ARG HOST_UID=1000
RUN adduser -D -u ${HOST_UID} hotplex && \
    mkdir -p /home/hotplex/go/pkg/mod \
    /home/hotplex/.cache/go-build \
    /home/hotplex/.gradle \
    /home/hotplex/.m2 && \
    chown -R hotplex:hotplex /home/hotplex

ENV GOPATH=/home/hotplex/go
ENV GOCACHE=/home/hotplex/.cache/go-build
ENV GRADLE_USER_HOME=/home/hotplex/.gradle
ENV MAVEN_OPTS="-Xmx512m"
ENV PATH="${GOPATH}/bin:${PATH}"

USER hotplex

EXPOSE 8080
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/hotplexd"]
