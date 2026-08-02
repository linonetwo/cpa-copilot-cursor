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
    && rm -f /out/*.h

FROM python:3.13-slim-bookworm

ARG CURSOR_VERSION=2026.07.23-e383d2b
ARG CURSOR_SHA256=702ad595213bee5df0268be9f80a19f29fcceaa2a42fc55e39f2b5199051f0c4
ARG CURSOR_URL=https://downloads.cursor.com/lab/2026.07.23-e383d2b/linux/x64/agent-cli-package.tar.gz

LABEL org.opencontainers.image.source="https://github.com/linonetwo/cpa-subscription-bridge" \
      org.opencontainers.image.description="Native CPA OAuth providers for GitHub Copilot and Cursor subscriptions" \
      org.opencontainers.image.licenses="MIT"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tini \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY bridge/requirements.txt ./requirements.txt
RUN pip install --no-cache-dir -r requirements.txt \
    && python -m copilot download-runtime

RUN mkdir -p /opt/cursor-agent \
    && curl --fail --location --retry 3 "$CURSOR_URL" --output /tmp/cursor-agent.tar.gz \
    && echo "$CURSOR_SHA256  /tmp/cursor-agent.tar.gz" | sha256sum --check - \
    && tar -xzf /tmp/cursor-agent.tar.gz --strip-components=1 -C /opt/cursor-agent \
    && chmod +x /opt/cursor-agent/cursor-agent \
    && rm /tmp/cursor-agent.tar.gz

COPY --from=plugin-builder /out/ /plugins/
COPY bridge/ /app/

ENV CPA_SUBSCRIPTION_BRIDGE_DATA=/data \
    CURSOR_AGENT_PATH=/opt/cursor-agent/cursor-agent \
    PYTHONUNBUFFERED=1

VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl --fail http://127.0.0.1:8789/healthz || exit 1

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["python", "/app/app.py"]

