# Public Hosting and Self-Hosting Design

**Date:** 2026-08-26
**Status:** Approved

**September 20 deployment direction:** The user selected `https://mapcollab.a13.info`
as the main hosted endpoint on the current server, superseding `mapping.a13.info`
throughout the historical deployment description below. The desktop's editable
default now uses the selected hostname. The user subsequently selected native
Windows services, Cloudflare OIDC and backups under `D:\Backups`. The
[Windows deployment report](../../verification/2026-09-20-windows-hosted-deployment.md)
records running services and public readiness, with the OIDC allow policy still
awaiting the user's choice. The Docker topology below remains historical for
this installation and available to other self-hosters.

## Objective

Expose the single-replica hosted collaboration service at `https://mapping.a13.info` without requiring end users to hold Cloudflare accounts or install Cloudflare software, while preserving a documented, vendor-neutral path for other operators to host compatible servers.

## Public service policy

- StrongDMM defaults its editable hosted-service field to `https://mapping.a13.info`.
- A user may replace that origin with any compatible HTTPS service. Hosted credentials remain in memory and are never copied between origins.
- The Aphelion-operated service is publicly reachable over HTTPS and WSS. Cloudflare Access is not placed in front of it.
- Any successfully authenticated OIDC identity may create a session.
- Sessions are not publicly listed. Joining a session requires an explicit, one-use, expiring invitation.
- The hosted service remains exactly one replica until cross-instance durable-event and presence fanout is implemented.

## Aphelion deployment

The Meridian server runs one isolated Docker Compose project containing:

1. the hardened `apheliondmm-hosted` image;
2. PostgreSQL with persistent storage and health checks;
3. a dedicated `cloudflared` connector on the same private Compose network; and
4. explicit backup, restore, upgrade, rollback, and log-inspection procedures.

The dedicated Cloudflare Tunnel publishes `mapping.a13.info` directly to the hosted container over the private Compose network. The tunnel is a public transport path, not an identity gate. It opens no inbound origin port and does not share lifecycle or configuration with the existing `bark` tunnel.

Cloudflare terminates public TLS. The disposable pilot's local Caddy and OIDC fixture are not deployed publicly. Application OIDC, authorization, invitation, origin validation, rate limits, and payload limits remain authoritative.

## Portable self-hosting contract

The production bundle has a vendor-neutral base Compose file. Optional overlays support:

- a Cloudflare Tunnel sidecar with a remotely managed tunnel token; or
- a loopback-only host port for an operator-managed Caddy, nginx, Traefik, or equivalent HTTPS/WSS reverse proxy.

The repository publishes a strict JSON Schema for hosted YAML configuration and an example using file-backed secrets. Documentation defines the public origin, callback path, proxy CIDRs, database, OIDC, limits, telemetry, secret permissions, health checks, backup format, upgrade order, and single-replica restriction.

Self-hosters supply their own DNS name, TLS edge, PostgreSQL storage, and standards-compatible OIDC provider. They do not patch or rebuild the desktop: users enter the alternate origin in the normal sign-in dialog.

## Load and rollout authentication

Hosted mode continues to forbid embedded join-token minting. The load runner supports a separate hosted credential mode:

- the owner credential comes from the existing secret environment variable;
- editor OIDC session credentials come from a bounded, permission-checked JSON secret file;
- the runner creates one hosted invitation per editor and redeems it with that editor's credential;
- tokens are held only in memory and never emitted in JSON results or error text;
- embedded load tests retain their existing join-token path.

The credential file is operational test input, not a server API or production authentication bypass. External OIDC certification remains a separate human gate.

## Security and operations

- No Internet-facing origin port is opened for the Aphelion deployment.
- Reverse-proxy deployments bind only to loopback unless the operator explicitly supplies an isolated private network.
- Secret files must be regular, non-symlink files and inaccessible to other users on Unix; Windows operators apply an equivalent ACL.
- PostgreSQL is never publicly published.
- Upgrades are stop-then-start replacements with readiness verification and a documented rollback image.
- Backups are encrypted outside the Compose project and restore rehearsal is required before the pilot is accepted.
- Cloudflare and application logs must exclude OIDC secrets, bearer credentials, invitation secrets, map contents, and database URLs.

## Acceptance

- The desktop defaults to `mapping.a13.info` and still accepts a different valid HTTPS origin.
- The same production Compose base works with either supported edge overlay.
- Hosted configuration examples parse with the shipped binary and match the published schema.
- Public health, OIDC, session creation, invitation, WSS reconnect, and restart recovery work through the selected hostname.
- The hosted 25-editor load and fault profile completes without embedded join-token access.
- Backup restore and rollback use the exact documented artifacts.

