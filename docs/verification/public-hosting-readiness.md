# Public hosting readiness

September 27 update: the Windows reference service is deployed with Discord and
Community/Private browsing. See [current evidence and remaining gates](2026-09-27-hosted-and-ci-followup.md).
The August observations below are historical, including the discovery limitation.

Status date: 2026-08-31

Decision: not ready for public hosting or a general production claim.

The local container lifecycle and security scan pass, but no public DNS/tunnel change, reference-server deployment, external OIDC registration, production secret installation, production backup/restore rehearsal, telemetry/alert ownership, rollback exercise, or named-user pilot was performed in this remediation. Historical evidence does not substitute for a fresh production run.

Publication remains gated by:

- explicit operator-approved server inventory and deployment;
- ACL-restricted production secrets and a live PostgreSQL restore rehearsal;
- dedicated public tunnel and DNS without changing the existing game tunnel;
- external identity-provider acceptance and revocation testing;
- monitored load/fault evidence and a private named-user pilot;
- updater minisign key ownership and signed metadata/artifact publication;
- human desktop acceptance.

Deploy exactly one service replica. Multi-replica operation and public session discovery are not supported.
