# Production hosted collaboration

This directory is the portable, single-replica deployment for AphelionDMM hosted collaboration. End users connect over HTTPS/WSS and authenticate through the selected provider. Aphelion's current target is the existing native Windows service at `https://mapcollab.a13.info`; see the [current operator handoff](../../docs/hosting/game-server-deployment-agent-handoff.md). The Compose setup below retains the OIDC self-hosting path.

The base Compose project is provider-neutral. Other operators can select the loopback overlay and place Caddy, nginx, Traefik, or another WebSocket-capable HTTPS proxy in front of it.

## Requirements

- Docker Engine 27 or newer with Docker Compose v2.
- A DNS hostname and trusted HTTPS edge.
- A standards-compatible OIDC confidential client using Authorization Code and PKCE, or a Discord confidential client configured as described in the operator handoff.
- Persistent local storage plus a separate encrypted backup destination.
- Exactly one `hosted` replica. Multi-replica deployment is unsupported.

## Initialize

Run from the repository root in PowerShell:

```powershell
& .\deploy\production\operations.ps1 -Action Initialize
```

Edit `deploy/production/config.yaml`. Its `public_origin` and `oidc.redirect_url` must use the public hostname, with the callback ending in `/v1/auth/complete`. The complete schema is [hosted-config.schema.json](../../docs/hosting/hosted-config.schema.json).

Create these regular files under `deploy/production/secrets`:

- `postgres_password.txt`: a randomly generated PostgreSQL password.
- `database_dsn.txt`: `postgres://apheliondmm:<URL-encoded-password>@postgres:5432/apheliondmm?sslmode=disable`.
- `oidc_client_secret.txt`: the OIDC confidential-client secret.
- `cloudflare_tunnel_token.txt`: required only for the Cloudflare overlay.

The PostgreSQL password and the password embedded in the DSN must match. Each file contains only its value and an optional final newline. Do not commit `config.yaml`, `.env`, `secrets`, or `backups`.

On Linux, set the secrets directory to `0700` and files to `0600`, then set `APHELIONDMM_RUNTIME_UID` and `APHELIONDMM_RUNTIME_GID` in `.env` to the numeric owner returned by `id -u` and `id -g`. The hosted and tunnel containers run with that identity so Compose's read-only bind secrets remain readable without weakening host permissions. On Windows Docker Desktop, keep the default UID/GID and remove inherited host access, granting Full Control only to the deployment operator and the account running Docker:

```powershell
icacls .\deploy\production\secrets /inheritance:r
icacls .\deploy\production\secrets /grant:r "$env:USERNAME:(OI)(CI)F"
```

## Public Cloudflare deployment

Create a dedicated remotely managed Tunnel. Configure its public hostname to route the selected hostname to `http://hosted:8080`. Do not create a Cloudflare Access application for the hostname. Copy only the tunnel token into `cloudflare_tunnel_token.txt`.

Validate and start:

```powershell
& .\deploy\production\operations.ps1 -Action Validate -Edge Cloudflare
& .\deploy\production\operations.ps1 -Action StartCloudflare
& .\deploy\production\operations.ps1 -Action Status -Edge Cloudflare
```

The reference deployment uses `mapping.a13.info`, a dedicated tunnel, and `trusted_proxy_cidrs: ["172.30.90.0/24"]`. If that Docker subnet conflicts with the host, change both the `edge` subnet in `compose.yaml` and the trusted CIDR in `config.yaml` before first start.

## Other reverse proxies

The loopback overlay publishes only `127.0.0.1:8080`. Terminate TLS at the host proxy, forward WebSocket upgrades, preserve the original `Host`, and append the client address to `X-Forwarded-For`. Configure only the proxy's actual source CIDR as trusted.

```powershell
& .\deploy\production\operations.ps1 -Action Validate -Edge Loopback
& .\deploy\production\operations.ps1 -Action StartLoopback
```

Never publish PostgreSQL. Do not bind port 8080 to a public interface.

## OIDC policy

For `mapping.a13.info`, register `https://mapping.a13.info/v1/auth/complete`. Any identity accepted by the OIDC provider may create a private, unlisted session. Another participant can join only by redeeming a one-use invitation.

Google OIDC is the recommended initial managed provider. Put its client ID in `config.yaml` and its client secret only in the secret file. Self-hosters may select another standards-compatible provider without rebuilding StrongDMM.

## Routine operations

```powershell
& .\deploy\production\operations.ps1 -Action Logs -Edge Cloudflare
& .\deploy\production\operations.ps1 -Action Backup -Edge Cloudflare
& .\deploy\production\operations.ps1 -Action Upgrade -Edge Cloudflare
& .\deploy\production\operations.ps1 -Action Stop -Edge Cloudflare
```

`Upgrade` builds the replacement first, stops the single hosted replica, then starts and waits for the replacement. It does not roll two service instances concurrently.

Backups are PostgreSQL custom-format dumps. Move each completed dump to encrypted storage outside this repository and Compose volume. Test restoration regularly:

```powershell
& .\deploy\production\operations.ps1 -Action Restore -Edge Cloudflare -BackupFile D:\Backups\apheliondmm.dump -ConfirmRestore
```

Restore is destructive and refuses to run without `-ConfirmRestore`. Rollback requires a previously retained local image tag:

```powershell
& .\deploy\production\operations.ps1 -Action Rollback -Edge Cloudflare -ImageTag previous-known-good
```

## Health and incident evidence

The public checks are `/v1/health/live`, `/v1/health/ready`, and `/v1/version`. Keep Cloudflare at normal or warning log level; debug logs may expose request headers. Reports must exclude bearer credentials, invitations, OIDC codes, database URLs, tunnel tokens, and map contents.

See `deploy/runbooks` for migration, backup, restore, incident-response, rollout, and security-review procedures.

## Hosted load certification

Hosted mode intentionally rejects the embedded `/join-tokens` API. For the recorded load profile, place one short-lived OIDC session credential per simulated editor in an ACL-restricted JSON file:

```json
{"tokens":["editor-1-credential","editor-2-credential"]}
```

Set `APHELIONDMM_LOAD_OWNER_TOKEN` to the hosted owner's in-memory session credential and `APHELIONDMM_LOAD_EDITOR_TOKENS_FILE` to the absolute credential-file path. The runner creates ordinary editor invitations and redeems each with the corresponding identity before connecting. It does not print credentials. Delete the file after the controlled test and revoke the test identities at the OIDC provider.
