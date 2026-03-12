# ==============================================================================
# HotPlex Java Development Stack (2026)
# ==============================================================================

# 1. Source the binary provider
FROM hotplex:artifacts AS binary-provider

# 2. SDK Layer
FROM eclipse-temurin:25-jdk-bookworm AS sdk-source

# 3. Target Foundation
FROM hotplex:base

# Inject JDK (Cached)
USER root
COPY --from=sdk-source /opt/java/openjdk /opt/java/openjdk
ENV JAVA_HOME=/opt/java/openjdk
ENV PATH="${JAVA_HOME}/bin:${PATH}"

# Build Tools (Cached)
RUN ENV_GRADLE_VERSION=8.14 && \
    wget -q https://services.gradle.org/distributions/gradle-${ENV_GRADLE_VERSION}-bin.zip && \
    unzip gradle-${ENV_GRADLE_VERSION}-bin.zip -d /opt && rm gradle-${ENV_GRADLE_VERSION}-bin.zip && \
    ENV_MAVEN_VERSION=3.9.13 && \
    wget -q https://archive.apache.org/dist/maven/maven-3/${ENV_MAVEN_VERSION}/binaries/apache-maven-${ENV_MAVEN_VERSION}-bin.tar.gz && \
    tar -xzf apache-maven-${ENV_MAVEN_VERSION}-bin.tar.gz -C /opt && rm apache-maven-${ENV_MAVEN_VERSION}-bin.tar.gz

ENV GRADLE_HOME=/opt/gradle-8.14
ENV MAVEN_HOME=/opt/apache-maven-3.9.13
ENV PATH="${GRADLE_HOME}/bin:${MAVEN_HOME}/bin:${PATH}"

# ==============================================================================
# 🔥 Late Injection: The Binary (Changes frequently)
# ==============================================================================
COPY --from=binary-provider /hotplexd /usr/local/bin/hotplexd
# ==============================================================================

USER hotplex
CMD ["/usr/local/bin/hotplexd"]
LABEL org.opencontainers.image.title="HotPlex Java"
