# Agent Runner dev/sandbox image used by local devcontainers and eval proofs.
# This is intentionally tool-heavy and is not a production runtime image.

ARG PLAYWRIGHT_VERSION=1.61.0
FROM mcr.microsoft.com/playwright:v${PLAYWRIGHT_VERSION}-noble

ARG GO_VERSION=1.26.1

USER root

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    jq \
    make \
    socat \
    tar \
    zsh \
    xz-utils \
  && rm -rf /var/lib/apt/lists/*

RUN arch="$(dpkg --print-architecture)" \
  && case "$arch" in \
    amd64) go_arch="amd64" ;; \
    arm64) go_arch="arm64" ;; \
    *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
  esac \
  && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz" -o /tmp/go.tgz \
  && tar -C /usr/local -xzf /tmp/go.tgz \
  && rm /tmp/go.tgz

ENV PATH="/usr/local/go/bin:/workspace/bin:${PATH}"

RUN npm install -g \
    @openai/codex \
    @anthropic-ai/claude-code \
    @codagent-ai/agent-plugin \
    agent-validator \
    chrome-devtools-axi@0.1.26 \
    chrome-devtools-mcp@1.5.0 \
    opencode-ai

# chrome-devtools-mcp defaults to the stable Chrome location. The Playwright
# image ships a compatible Chromium build under a versioned directory, so give
# browser-agent tools a stable path without installing a second browser.
RUN chrome_path="$(find /ms-playwright -path '*/chrome-linux/chrome' -type f -print -quit)" \
  && test -n "$chrome_path" \
  && mkdir -p /opt/google/chrome \
  && ln -s "$chrome_path" /opt/google/chrome/chrome

RUN mkdir -p \
    /workspace \
    /workspace/bin \
    /workspace/home/.codex \
    /workspace/home/.claude \
    /workspace/home/.config/git \
    /artifacts \
  && chown -R pwuser:pwuser /workspace /artifacts

USER pwuser
WORKDIR /workspace

ENV HOME=/workspace/home \
  SHELL=/usr/bin/zsh \
  npm_config_cache=/workspace/home/.npm \
  CHROME_DEVTOOLS_AXI_MCP_PATH=/usr/lib/node_modules/chrome-devtools-mcp/build/src/bin/chrome-devtools-mcp.js \
  CHROME_DEVTOOLS_AXI_CHROME_ARGS="--no-sandbox --disable-setuid-sandbox" \
  PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  CI=1

CMD ["/usr/bin/zsh"]
