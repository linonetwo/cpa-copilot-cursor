FROM golang:1.26-bookworm AS plugin-builder

ENV PATH="/usr/local/go/bin:${PATH}"
WORKDIR /src
COPY go.mod go.sum* ./
RUN /usr/local/go/bin/go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN mkdir -p /out \
    && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build \
      -buildmode=c-shared -trimpath \
      -ldflags="-s -w -X main.providerKind=copilot" \
      -o /out/cpa-copilot-provider.so ./cmd/provider \
    && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build \
      -buildmode=c-shared -trimpath \
      -ldflags="-s -w -X main.providerKind=cursor" \
      -o /out/cpa-cursor-provider.so ./cmd/provider \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build \
      -trimpath -ldflags="-s -w" \
      -o /out/cpa-subscription-bridge ./cmd/bridge \
    && rm -f /out/*.h

FROM debian:bookworm-slim AS runtime-downloader

ARG COPILOT_CLI_VERSION=1.0.73
ARG COPILOT_CLI_SHA512=937dd722bebf3d5a7e2bee45ff3bf7368e0f3da3489af1f3ef799c6c8c3ade8c61e6289a7178e0af45aa6c1212e6cfeb9213968cd3c891ba1ffd34cf67b9908c
ARG COPILOT_CLI_URL=https://registry.npmjs.org/@github/copilot-linux-x64/-/copilot-linux-x64-${COPILOT_CLI_VERSION}.tgz
ARG CURSOR_VERSION=2026.07.23-e383d2b
ARG CURSOR_SHA256=702ad595213bee5df0268be9f80a19f29fcceaa2a42fc55e39f2b5199051f0c4
ARG CURSOR_URL=https://downloads.cursor.com/lab/${CURSOR_VERSION}/linux/x64/agent-cli-package.tar.gz

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/copilot \
    && curl --fail --location --retry 3 "$COPILOT_CLI_URL" --output /tmp/copilot.tgz \
    && echo "$COPILOT_CLI_SHA512  /tmp/copilot.tgz" | sha512sum --check - \
    && tar -xzf /tmp/copilot.tgz -C /opt/copilot --strip-components=1 package/copilot \
    && chmod +x /opt/copilot/copilot \
    && rm /tmp/copilot.tgz

RUN mkdir -p /opt/cursor-agent \
    && curl --fail --location --retry 3 "$CURSOR_URL" --output /tmp/cursor-agent.tar.gz \
    && echo "$CURSOR_SHA256  /tmp/cursor-agent.tar.gz" | sha256sum --check - \
    && tar -xzf /tmp/cursor-agent.tar.gz --strip-components=1 -C /opt/cursor-agent \
    && chmod +x /opt/cursor-agent/cursor-agent \
    && rm /tmp/cursor-agent.tar.gz

FROM debian:bookworm-slim

LABEL org.opencontainers.image.source="https://github.com/linonetwo/cpa-subscription-bridge" \
      org.opencontainers.image.description="Native CPA OAuth providers for GitHub Copilot and Cursor subscriptions" \
      org.opencontainers.image.licenses="MIT"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tini \
    && rm -rf /var/lib/apt/lists/*

COPY --from=runtime-downloader /opt/copilot/ /opt/copilot/
COPY --from=runtime-downloader /opt/cursor-agent/ /opt/cursor-agent/
COPY --from=plugin-builder /out/ /plugins/
RUN install -m 0755 /plugins/cpa-subscription-bridge /usr/local/bin/cpa-subscription-bridge \
    && rm /plugins/cpa-subscription-bridge

ENV CPA_SUBSCRIPTION_BRIDGE_DATA=/data \
    COPILOT_CLI_PATH=/opt/copilot/copilot \
    CURSOR_AGENT_PATH=/opt/cursor-agent/cursor-agent \
    HOME=/data/runtime-home

VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/local/bin/cpa-subscription-bridge", "--healthcheck"]

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/cpa-subscription-bridge"]
