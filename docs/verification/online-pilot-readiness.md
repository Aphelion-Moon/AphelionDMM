# Online pilot readiness

September 27 update: the reference service, Discord configuration and isolated
backup/rollback rehearsal are now established. See [current evidence and remaining
gates](2026-09-27-hosted-and-ci-followup.md). The August checklist below is historical.

Status date: 2026-08-31

Decision: not ready to start a private Internet pilot. The portable single-replica image is locally qualified, but no reference endpoint, external identity provider, production telemetry destination, or named-user exercise was configured.

## Fresh local evidence

- Built `apheliondmm-hosted:audit-20260831` from base revision `c3469c60d5cd39c21268e14ee38643910601b2e9` plus the remediation working tree.
- Image ID: `sha256:a4f9e3a0176a8ee412e40c3f86f8d9d6e2dd5c96baf21ee6b1536555f0f4b9ed`.
- Docker-tagged `TestHostedImageLifecycle` passed in 8.23 seconds, including PostgreSQL-backed service lifecycle, local TLS OIDC fixture, authentication, durable recovery/readiness behavior, and OTLP trace/metric export.
- Trivy 0.74.0 reported zero HIGH or CRITICAL vulnerabilities for Debian and both shipped Go binaries.
- Windows file-secret DACL regressions and hosted server race tests passed.

## Required before pilot

- Configure the reference single-replica service, PostgreSQL storage, backup destination, and rollback artifact.
- Run live PostgreSQL conformance and logical backup/restore with configured tools.
- Configure and verify the selected external OIDC provider, logout, expiry, revocation, and named-user access.
- Configure the external OTLP destination and alerts.
- Run the 25-editor profile plus slow-consumer, sustained-presence, database-interruption, reconnect, and zero-loss convergence checks; record p50/p95/p99.
- Complete the two-window human desktop procedure against the reference endpoint.
- Assign updater minisign key ownership before enabling downloads.

Multi-replica operation remains unsupported until cross-instance fanout exists.
