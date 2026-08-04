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
    && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build \
      -buildmode=c-shared -trimpath \
      -ldflags="-s -w -X main.providerKind=copilot-catalog" \
      -o /out/cpa-copilot-model-catalog.so ./cmd/provider \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build \
      -trimpath -ldflags="-s -w" \
      -o /out/cpa-copilot-cursor ./cmd/bridge \
    && rm -f /out/*.h

FROM debian:bookworm-slim AS runtime-downloader

ARG COPILOT_CLI_VERSION=1.0.73
ARG COPILOT_CLI_SHA512=937dd722bebf3d5a7e2bee45ff3bf7368e0f3da3489af1f3ef799c6c8c3ade8c61e6289a7178e0af45aa6c1212e6cfeb9213968cd3c891ba1ffd34cf67b9908c
ARG COPILOT_CLI_URL=https://registry.npmjs.org/@github/copilot-linux-x64/-/copilot-linux-x64-${COPILOT_CLI_VERSION}.tgz
ARG CURSOR_VERSION=2026.07.23-e383d2b
ARG CURSOR_SHA256=702ad595213bee5df0268be9f80a19f29fcceaa2a42fc55e39f2b5199051f0c4
ARG CURSOR_URL=https://downloads.cursor.com/lab/${CURSOR_VERSION}/linux/x64/agent-cli-package.tar.gz

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl zlib1g \
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
    && rm -rf \
      /opt/cursor-agent/node_modules/better-sqlite3/build/Release/obj \
      /opt/cursor-agent/node_modules/better-sqlite3/build/Release/obj.target \
      /opt/cursor-agent/node_modules/better-sqlite3/build/Release/sqlite3.a \
      /opt/cursor-agent/node_modules/better-sqlite3/deps \
    && rm /tmp/cursor-agent.tar.gz

FROM cgr.dev/chainguard/glibc-dynamic:latest@sha256:57e5704e70a85b90191182eb6110d1c817df0d8e96035cb041195c5a351f0861

LABEL org.opencontainers.image.source="https://github.com/linonetwo/cpa-copilot-cursor" \
      org.opencontainers.image.description="Native CPA OAuth providers for GitHub Copilot and Cursor subscriptions" \
      org.opencontainers.image.licenses="MIT"

COPY --from=runtime-downloader /opt/copilot/ /opt/copilot/
COPY --from=runtime-downloader /opt/cursor-agent/ /opt/cursor-agent/
COPY --from=runtime-downloader /lib/x86_64-linux-gnu/libz.so.1* /lib/x86_64-linux-gnu/
COPY --from=plugin-builder /out/cpa-copilot-provider.so /plugins/cpa-copilot-provider.so
COPY --from=plugin-builder /out/cpa-cursor-provider.so /plugins/cpa-cursor-provider.so
COPY --from=plugin-builder /out/cpa-copilot-model-catalog.so /plugins/cpa-copilot-model-catalog.so
COPY --from=plugin-builder /out/cpa-copilot-cursor /usr/local/bin/cpa-copilot-cursor

ENV CPA_COPILOT_CURSOR_DATA=/data \
    COPILOT_CLI_PATH=/opt/copilot/copilot \
    COPILOT_CACHE_HOME=/data/runtime-cache/copilot \
    COPILOT_AUTO_UPDATE=false \
    CURSOR_AGENT_PATH=/opt/cursor-agent/node \
    CURSOR_AGENT_SCRIPT=/opt/cursor-agent/index.js \
    HOME=/data/runtime-home

USER 0
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/local/bin/cpa-copilot-cursor", "--healthcheck"]

ENTRYPOINT ["/usr/local/bin/cpa-copilot-cursor"]
