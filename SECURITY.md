# Security Policy

## Credential boundary

CPA auth files contain only an opaque handle and non-secret account metadata. Copilot and Cursor credential state is stored under `/data/<provider>/<handle>/home` in the bridge sidecar's dedicated persistent volume.

The bridge binds to `127.0.0.1:8789`. Do not expose it through a Kubernetes Service, Ingress, host port, or public Docker port. Set a high-entropy `CPA_COPILOT_CURSOR_SECRET` in both the CPA container and bridge sidecar.

## Management resources

CPA resource routes are not management-key authenticated by CPA itself. The quota resource intentionally returns only provider labels, runtime status, and quota summaries. Protect the whole CPA management UI with network policy and external authentication.

## Upstream software

- GitHub Copilot requests use the official Go SDK and a pinned, checksum-verified official Copilot CLI package.
- Cursor requests use the official Cursor Agent CLI archive with a pinned SHA-256.
- The final runtime uses a shell-free Chainguard `glibc-dynamic` image pinned by OCI digest.
- The project does not implement account farming, hardware identity reset, anti-ban logic, or subscription-limit bypasses.

## Reporting

Please report vulnerabilities privately through GitHub Security Advisories for this repository. Do not include live OAuth tokens, cookies, device codes, or bridge secrets in an issue.
