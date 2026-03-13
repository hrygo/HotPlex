# ==============================================================================
# HotPlex Java Development Stack (2026)
# ==============================================================================

# 1. Source the binary provider
FROM hotplex:artifacts AS binary-provider

# 2. SDK Layer
FROM bellsoft/liberica-openjdk-debian:25 AS sdk-source

# 3. Target Foundation
FROM hotplex:base

# Inject JDK (Cached)
USER root
COPY --from=sdk-source /usr/lib/jvm/jdk /opt/java/openjdk
ENV JAVA_HOME=/opt/java/openjdk
ENV PATH="${JAVA_HOME}/bin:${PATH}"

# 4. Build & Diagnostic Tools (Cached)
RUN ENV_GRADLE_VERSION=8.14 && \
    wget -q https://services.gradle.org/distributions/gradle-${ENV_GRADLE_VERSION}-bin.zip && \
    unzip -q gradle-${ENV_GRADLE_VERSION}-bin.zip -d /opt && rm gradle-${ENV_GRADLE_VERSION}-bin.zip && \
    ENV_MAVEN_VERSION=3.9.13 && \
    wget -q https://archive.apache.org/dist/maven/maven-3/${ENV_MAVEN_VERSION}/binaries/apache-maven-${ENV_MAVEN_VERSION}-bin.tar.gz && \
    tar -xzf apache-maven-${ENV_MAVEN_VERSION}-bin.tar.gz -C /opt && rm apache-maven-${ENV_MAVEN_VERSION}-bin.tar.gz && \
    # Spring Boot CLI
    ENV_SPRING_VERSION=4.0.3 && \
    wget -q https://repo.maven.apache.org/maven2/org/springframework/boot/spring-boot-cli/${ENV_SPRING_VERSION}/spring-boot-cli-${ENV_SPRING_VERSION}-bin.tar.gz && \
    tar -xzf spring-boot-cli-${ENV_SPRING_VERSION}-bin.tar.gz -C /opt && rm spring-boot-cli-${ENV_SPRING_VERSION}-bin.tar.gz && \
    # JBang (Install to /opt/jbang for global access)
    curl -Ls https://sh.jbang.dev | bash -s - --prefix /opt/jbang app setup && \
    ln -s /opt/jbang/bin/jbang /usr/local/bin/jbang && \
    # Arthas
    curl -sLk https://arthas.aliyun.com/install.sh | sh && \
    mv arthas /opt/arthas && \
    # Async Profiler
    ENV_ASPROF_VERSION=4.3 && \
    ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then ASPROF_BIN="linux-x64"; \
    elif [ "$ARCH" = "aarch64" ]; then ASPROF_BIN="linux-arm64"; fi && \
    wget -q https://github.com/async-profiler/async-profiler/releases/download/v${ENV_ASPROF_VERSION}/async-profiler-${ENV_ASPROF_VERSION}-${ASPROF_BIN}.tar.gz && \
    mkdir -p /opt/async-profiler && tar -xzf async-profiler-${ENV_ASPROF_VERSION}-${ASPROF_BIN}.tar.gz -C /opt/async-profiler --strip-components=1 && \
    rm async-profiler-${ENV_ASPROF_VERSION}-${ASPROF_BIN}.tar.gz

ENV GRADLE_HOME=/opt/gradle-8.14
ENV MAVEN_HOME=/opt/apache-maven-3.9.13
ENV SPRING_HOME=/opt/spring-4.0.3
ENV PATH="${GRADLE_HOME}/bin:${MAVEN_HOME}/bin:${SPRING_HOME}/bin:/opt/jbang/bin:/opt/arthas:/opt/async-profiler/bin:${PATH}"

# ==============================================================================
# 🔥 Late Injection: The Binary (Changes frequently)
# ==============================================================================
COPY --from=binary-provider /hotplexd /usr/local/bin/hotplexd
# ==============================================================================

USER hotplex
CMD ["/usr/local/bin/hotplexd"]
LABEL org.opencontainers.image.title="HotPlex Java"
