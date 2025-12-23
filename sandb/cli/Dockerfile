# AI CLI Sandbox - Isolated Docker environment for AI coding assistants
# Base image: Go + Node.js for Claude Code and Go development
#
# NOTE: Go version is managed by install.sh - it will update this file
# to match your host's Go version. After upgrading Go on your host,
# run: _sandb/install.sh && _sandb/cli/build.sh

# Stage 1: Get Node.js from official image
FROM node:lts-bookworm AS node

# Stage 2: Build final image with Go
# GO_VERSION is updated by install.sh to match host
FROM golang:1.24.10-bookworm

# Copy Node.js from official image
COPY --from=node /usr/local/bin/node /usr/local/bin/
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm && \
    ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

# ============================================================================
# Base tools installation
# ============================================================================
RUN apt-get update && apt-get install -y --no-install-recommends \
    socat \
    git \
    curl \
    postgresql-client \
    netcat-openbsd \
    ca-certificates \
    xz-utils \
    && rm -rf /var/lib/apt/lists/*

# ============================================================================
# User setup - matching host UID/GID for file permissions
# ============================================================================
ARG USER_UID=1000
ARG USER_GID=1000
RUN groupadd --gid ${USER_GID} user 2>/dev/null || true && \
    useradd --uid ${USER_UID} --gid ${USER_GID} -m user 2>/dev/null || true && \
    mkdir -p /go && chown -R user:user /go

# Setup directories for user
RUN mkdir -p /home/user/.claude /home/user/.copilot /home/user/.npm-global /home/user/.config /home/user/.local/bin && \
    chown -R user:user /home/user

# Copy entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

USER user

# Configure npm to use user-writable directory for global packages
ENV NPM_CONFIG_PREFIX=/home/user/.npm-global
ENV PATH=/home/user/.local/bin:/home/user/.npm-global/bin:$PATH

# ============================================================================
# Claude Code installation (native - enables auto-updates)
# ============================================================================
RUN curl -fsSL https://claude.ai/install.sh | bash

# ============================================================================
# GitHub Copilot CLI installation (optional)
# ============================================================================
RUN npm install -g @github/copilot

# ============================================================================
# Proxy environment variables
# ============================================================================
ENV HTTP_PROXY=http://host.docker.internal:3128
ENV HTTPS_PROXY=http://host.docker.internal:3128
ENV NO_PROXY=localhost,127.0.0.1

ENTRYPOINT ["/entrypoint.sh"]
CMD ["bash"]
