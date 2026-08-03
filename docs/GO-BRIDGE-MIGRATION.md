# Go Bridge Migration

## Decision

Replace the Python bridge with a standalone Go bridge while keeping the two CPA
native plugins as thin Go shared libraries.

Use:

- `github.com/github/copilot-sdk/go` pinned to the stable Copilot SDK release.
- The SDK's bundled, integrity-verified Copilot CLI with the default child-process
  JSON-RPC transport.
- The official `cursor-agent` CLI for Cursor subscription login and execution.
- A Go HTTP server bound to `127.0.0.1:8789`, preserving the existing bridge API.

Do not use:

- The Copilot Node.js SDK under Bun or Deno. The package declares Node.js runtime
  requirements and includes native runtime integration; Bun and Deno are not
  supported deployment targets.
- The Cursor TypeScript SDK for subscription accounts. Its public API uses API
  keys and token-based billing rather than the Cursor subscription OAuth state
  managed by `cursor-agent login`.
- The Rust Copilot SDK for this bridge. It is viable, but adds a second systems
  language without reducing the required Cursor CLI process.
- The experimental in-process Copilot transport for the first Go release.

## Why Go

The official Go SDK exposes the capabilities used by the bridge:

- authenticated-user sessions;
- model discovery;
- `account.getQuota`;
- explicit base directories for per-account OAuth state;
- bundled Copilot CLI support.

It also matches the existing CPA plugin implementation and removes the Python
interpreter, `aiohttp`, and Python SDK from the runtime image.

The current local image measurement is approximately:

- total image: 583 MB;
- Python runtime and installed packages: 69 MB;
- bundled Copilot CLI: 161 MB;
- Cursor Agent package: 225 MB;
- CPA shared libraries: 14 MB.

The migration should reduce the bridge process and image overhead, but the two
official CLI runtimes will remain the dominant image components.

## Compatibility Contract

The Go bridge must preserve:

- `GET /healthz`;
- `POST /v1/oauth/start`;
- `POST /v1/oauth/poll`;
- `POST /v1/models`;
- `POST /v1/execute`;
- `POST /v1/quota`;
- `CPA_SUBSCRIPTION_BRIDGE_SECRET`;
- `CPA_SUBSCRIPTION_BRIDGE_DATA`;
- opaque CPA auth handles and the existing PVC layout;
- Copilot and Cursor login output parsing through a pseudo-terminal;
- OpenAI chat-completions response and streaming shapes.

No CPA plugin ABI or stored authentication JSON migration should be required.

## Implementation Plan

1. Add `cmd/bridge` and internal Go packages for HTTP, storage, device flow,
   OpenAI payload conversion, and runtime execution.
2. Bundle the Copilot CLI with the official Go SDK bundler and expose its path to
   both SDK clients and `copilot login`.
3. Port Cursor PTY login, model discovery, status, and print execution.
4. Add API contract tests that run against both the Python and Go bridges.
5. Build a candidate image without Python and compare image size and idle memory.
6. Run real Copilot and Cursor OAuth, model, chat, and quota integration tests.
7. Publish a versioned candidate only after parity is confirmed.

## Deployment

This migration is source preparation only. Do not update the GitOps image pin or
restart the CPA Pod until the maintenance window and integration checks are
complete.
