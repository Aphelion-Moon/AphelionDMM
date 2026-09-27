# Game-server deployment agent handoff

## Current native Windows direction (September 26)

Aphelion uses `https://mapcollab.a13.info` and the existing native Windows service,
dedicated tunnel, PostgreSQL cluster, service accounts and backup process. The
August Docker instructions below are historical for Aphelion; portable OIDC
self-hosting remains supported. Do not recreate that topology or change unrelated
services or tunnels as part of provider cutover.

The new binary supports one selected provider. Omit `auth_provider` to preserve
OIDC, or add the following to the existing operator-owned YAML configuration:

```yaml
auth_provider: discord
discord:
  client_id: "<application ID>"
  guild_id: "<required Discord server ID>"
  redirect_url: "https://mapcollab.a13.info/v1/auth/complete"
  client_secret:
    file: "<existing restricted secrets directory>/discord_client_secret.txt"
  session_ttl: "12h"
```

Register that exact callback. Keep the secret in an ACL-restricted regular file;
do not supply it through chat, repository files or logs. Only the selected provider
is initialized. An inactive OIDC block can remain for coordinated rollback but
cannot authenticate anyone while Discord is selected. A Discord outage blocks new
logins; it does not invalidate an existing application login.

Before authorizing production cutover:

1. Record the installed revision, binary hash, schema version and counts of sessions,
   identities, enabled/disabled memberships and invitations without dumping data.
2. If historical OIDC ownership exists, stop provider cutover until an operator-
   verified principal-to-Discord mapping preserves canonical actor IDs and immutable
   history. This release does not invent account links or migrate actors by name.
3. Back up using the existing process and stage binary/configuration/database
   together. Schema 5 adds Private-default metadata; it does not publish old sessions.
4. Qualify real Discord member success and nonmember denial using the configured
   application, and complete a disposable two-user Community/Private/restart pilot.
   Confirm the documented confidential-client/browser-state protection; this release
   does not claim Discord-side PKCE enforcement.
5. Switch only the approved AphelionDMM service after explicit operator authorization.

Application credentials are memory-only, expire after 12 hours by default (maximum
24 hours), and are lost on restart. Membership persists. Reauthentication uses the
same service and actor; uncertain drafts remain recoverable and are not replayed
automatically. My sessions provides access to enabled Private/idle memberships
until the session ends. Empty sessions expire after a one-minute reconnect grace
period, or five minutes after creation/recovery without a connection. Cleanup
retains the stored document and edit history, but removes session membership and
invitations. Snapshot transfers use bounded three-minute HTTP deadlines and
negotiated gzip; ordinary control-request deadlines remain short.

Rollback requires a coordinated decision: the previous binary rejects schema 5
even though its changes are additive, and cannot authenticate Discord identities.
Prefer forward repair once writes have advanced. Restoring a pre-cutover database
requires explicit acceptance of losing post-backup edits; do not silently restore
over newer work. Retain the previous configuration/binary/provider resources until
acceptance. Source tests and rebuilt binaries do not certify the live deployment.

## Historical August handoff

**Prepared:** 2026-08-26  
**Target:** AphelionDMM hosted collaboration at `https://mapping.a13.info`  
**Host:** The dedicated server that already runs Meridian-Rift  
**Deployment model:** One isolated Docker Compose project with PostgreSQL, one hosted-service replica, and one dedicated Cloudflare Tunnel sidecar

## Objective

Install and certify the AphelionDMM hosted collaboration service on the Meridian game server without disrupting Meridian-Rift or the existing Cloudflare `bark` tunnel.

The public hostname must be reachable without Cloudflare Access. Users authenticate through the collaboration service's OIDC provider. Sessions remain private and unlisted; joining requires a one-use invitation.

## Read first

Work from the AphelionDMM repository root and read:

1. `AGENTS.md`
2. `docs/agent/security-and-networking.md`
3. `docs/agent/verification.md`
4. `docs/superpowers/specs/2026-08-26-public-hosting-and-self-hosting-design.md`
5. `deploy/production/README.md`
6. `deploy/production/operations.ps1`
7. `docs/verification/phase-7-public-hosting-2026-08-26.md`

Use PowerShell for host commands. Preserve unrelated services and files. Do not reset, checkout, merge, commit, push, delete volumes, change firewall policy, or modify protected deployment files without the authority required by `AGENTS.md`.

## Known state and decisions

- The portable production bundle has passed local Compose validation and a real build, health, backup, restore, rollback, and stop lifecycle.
- `mapping.a13.info` was unoccupied when last inventoried. Recheck immediately before provisioning.
- The existing remotely managed `bark` tunnel was healthy when last inventoried. Do not edit, restart, retoken, or add this application to it.
- Use a new remotely managed tunnel dedicated to AphelionDMM. Recommended name: `apheliondmm-mapping`.
- The published application route is `mapping.a13.info` to `http://hosted:8080`.
- Do not create a Cloudflare Access application or Access policy for `mapping.a13.info`.
- Do not expose PostgreSQL or bind hosted port `8080` to a public interface.
- Deploy exactly one `hosted` replica. Multi-replica operation is unsupported.
- Google OIDC is the recommended initial provider. The exact callback is `https://mapping.a13.info/v1/auth/complete`.
- Content Tools, Meridian-Rift code changes, public session discovery, and updater publication are outside this handoff.

## Hard prerequisites

Stop before changing the server unless all of these are satisfied:

- The server operator has completed the interactive authentication needed to administer the server.
- The repository owner has authorized an immutable AphelionDMM revision or an approved source bundle containing the production changes. The current development changes were still uncommitted and unpushed when this handoff was prepared; do not reproduce them manually on the server.
- The OIDC provider has issued a production client ID and secret for the exact callback above.
- A separate encrypted backup destination and retention owner have been selected.
- The Cloudflare connector can create a dedicated tunnel and DNS route in the `a13.info` zone.
- The server has enough capacity and an unused Docker subnet for `172.30.90.0/24`, or a replacement subnet has been selected before first start.

Never ask the operator to paste a tunnel token, OIDC secret, database password, bearer credential, invitation, or access URL containing a token into chat. Install secrets directly into ACL-restricted files on the server.

## Phase 1: Read-only server inventory

Record command output with timestamps, but redact usernames or paths if they reveal unrelated infrastructure. Do not use `docker inspect` on existing containers because environment blocks may contain secrets.

```powershell
$PSVersionTable.PSVersion
Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber,OSArchitecture,LastBootUpTime
Get-CimInstance Win32_ComputerSystem | Select-Object Manufacturer,Model,TotalPhysicalMemory
Get-Volume | Select-Object DriveLetter,FileSystem,HealthStatus,Size,SizeRemaining
docker version
docker compose version
docker info --format 'ServerVersion={{.ServerVersion}} OSType={{.OSType}} Architecture={{.Architecture}} CPUs={{.NCPU}} MemoryBytes={{.MemTotal}} StorageDriver={{.Driver}}'
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker network ls
docker volume ls
Get-NetTCPConnection -State Listen | Sort-Object LocalPort | Select-Object LocalAddress,LocalPort,OwningProcess
w32tm /query /status
Get-Service | Where-Object { $_.Name -match 'docker|cloudflared' } | Select-Object Name,Status,StartType
```

Then establish, without guessing:

- the service account that runs Docker;
- the approved installation volume and its free space;
- the encrypted off-host backup destination and retention period;
- whether Docker is configured for Linux containers and starts after reboot;
- whether `172.30.90.0/24` conflicts with any host, VPN, Docker, or game-service network;
- whether outbound Cloudflare Tunnel connectivity is allowed;
- which existing monitoring system should receive service alerts.

Save the inventory in a new dated file under `docs/verification/`. Do not record credentials, private map content, database URLs, or full container environment data.

### Inventory stop conditions

Stop and report before installation if:

- Docker or Compose does not meet the requirements in `deploy/production/README.md`;
- the selected storage is unhealthy or lacks room for the image, PostgreSQL growth, and local backup staging;
- the Docker subnet conflicts;
- the only available action would expose a new inbound origin port;
- the game server or an existing container would need to be stopped;
- the source revision is dirty, unidentified, or not approved.

## Phase 2: Prepare an immutable deployment checkout

Keep this service separate from the Meridian-Rift checkout and its containers. Prefer an operator-owned path such as `C:\Services\AphelionDMM` if the inventory confirms that volume is appropriate.

1. Obtain the approved source through its authorized Git remote or approved source bundle.
2. Record `git rev-parse HEAD`, `git status --short`, and `git remote -v` with credentials removed.
3. Require a clean deployment checkout. Do not deploy from a development working tree with unreviewed changes.
4. Set these values in `deploy/production/.env`:

```dotenv
APHELIONDMM_IMAGE_TAG=<approved-release-or-revision-tag>
APHELIONDMM_BUILD=<approved-build-identifier>
APHELIONDMM_REVISION=<full-approved-git-revision>
APHELIONDMM_RUNTIME_UID=65532
APHELIONDMM_RUNTIME_GID=65532
```

The identifiers are evidence, not secrets. Use exact approved values rather than the angle-bracket examples.

Initialize the local-only files:

```powershell
& .\deploy\production\operations.ps1 -Action Initialize
```

Do not commit `deploy/production/.env`, `config.yaml`, `secrets`, or `backups`.

## Phase 3: Configure OIDC and application secrets

Register a confidential Web OIDC client with:

- issuer: `https://accounts.google.com` for the recommended Google setup;
- redirect URI: `https://mapping.a13.info/v1/auth/complete`;
- Authorization Code flow with PKCE;
- no wildcard redirect URI;
- only the identity scopes required by the application.

Use this application configuration:

```yaml
bind_address: "0.0.0.0:8080"
public_origin: "https://mapping.a13.info"
trusted_proxy_cidrs:
  - "172.30.90.0/24"
database:
  dsn:
    file: "/run/secrets/database_dsn"
oidc:
  issuer: "https://accounts.google.com"
  client_id: "<production-client-id>"
  redirect_url: "https://mapping.a13.info/v1/auth/complete"
  client_secret:
    file: "/run/secrets/oidc_client_secret"
limits:
  max_connections: 64
  max_operation_changes: 512
  max_websocket_message_bytes: 262144
  max_http_body_bytes: 524288
  max_snapshot_body_bytes: 268435456
telemetry:
  endpoint: ""
```

Replace only the OIDC client ID and any operator-approved telemetry endpoint. If the Docker subnet changes, update both `deploy/production/compose.yaml` and `trusted_proxy_cidrs` through the protected-file approval process.

Create these files under `deploy/production/secrets`:

- `postgres_password.txt`
- `database_dsn.txt`
- `oidc_client_secret.txt`
- `cloudflare_tunnel_token.txt` after the dedicated tunnel is created

Generate the PostgreSQL password with a cryptographically secure generator. URL-encode it in the DSN:

```text
postgres://apheliondmm:<URL-encoded-password>@postgres:5432/apheliondmm?sslmode=disable
```

The password file and DSN password must match. Each secret file contains only the value and an optional final newline.

On Windows, remove inherited access and grant only the deployment operator and Docker-running account the access they require. Start with the repository-documented operator ACL command and verify the effective ACL with `icacls`. Do not weaken ACLs merely to make a container start.

## Phase 4: Prove local readiness before public routing

Validate and build first:

```powershell
& .\deploy\production\operations.ps1 -Action Validate -Edge Loopback
& .\deploy\production\operations.ps1 -Action Build -Edge Loopback
& .\deploy\production\operations.ps1 -Action StartLoopback
& .\deploy\production\operations.ps1 -Action Status -Edge Loopback
Invoke-RestMethod http://127.0.0.1:8080/v1/health/live
Invoke-RestMethod http://127.0.0.1:8080/v1/health/ready
Invoke-RestMethod http://127.0.0.1:8080/v1/version
```

Record the built image ID and immutable source revision. Confirm PostgreSQL has no published port and hosted port `8080` is bound only to `127.0.0.1`:

```powershell
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
$imageTagLine = Get-Content -LiteralPath .\deploy\production\.env | Where-Object { $_ -match '^APHELIONDMM_IMAGE_TAG=' } | Select-Object -First 1
$imageTag = ($imageTagLine -split '=', 2)[1].Trim()
docker image inspect "apheliondmm-hosted:$imageTag" --format '{{.Id}}'
```

Do not proceed unless both health endpoints report success, `/v1/version` matches the approved build and revision, logs contain no secrets, and no public origin port was opened.

Stop the loopback deployment before switching overlays:

```powershell
& .\deploy\production\operations.ps1 -Action Stop -Edge Loopback
```

## Phase 5: Create the dedicated public tunnel

Use the Cloudflare control plane to create a new remotely managed tunnel named `apheliondmm-mapping`. Do not reuse or modify `bark`.

Cloudflare's current documentation is authoritative for dashboard locations and API fields:

- [Create a remotely managed tunnel](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel/)
- [Published application routing](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/routing-to-tunnel/)
- [Tunnel token handling](https://developers.cloudflare.com/tunnel/advanced/tunnel-tokens/)
- [`--token-file` run parameter](https://developers.cloudflare.com/tunnel/advanced/run-parameters/#token-file)

Configure exactly one published application:

| Setting | Value |
| --- | --- |
| Public hostname | `mapping.a13.info` |
| Service type | HTTP |
| Service URL | `http://hosted:8080` |
| Cloudflare Access | None |

Adding the published route through the dashboard normally creates the tunnel CNAME automatically. Confirm that the final proxied record resolves to the new tunnel's `<UUID>.cfargotunnel.com` target and that no competing `A`, `AAAA`, or `CNAME` record exists.

Retrieve the token without printing it into terminal history or agent output, and place it directly in `deploy/production/secrets/cloudflare_tunnel_token.txt`. Treat anyone holding this token as able to run a connector for the tunnel. Record only the tunnel name and UUID in evidence.

Validate and start:

```powershell
& .\deploy\production\operations.ps1 -Action Validate -Edge Cloudflare
& .\deploy\production\operations.ps1 -Action StartCloudflare
& .\deploy\production\operations.ps1 -Action Status -Edge Cloudflare
```

Confirm that the new tunnel reports healthy connections. Reconfirm that `bark` is unchanged and that no Cloudflare Access application exists for `mapping.a13.info`.

## Phase 6: Public certification

Run these from a different network if possible:

```powershell
Invoke-RestMethod https://mapping.a13.info/v1/health/live
Invoke-RestMethod https://mapping.a13.info/v1/health/ready
Invoke-RestMethod https://mapping.a13.info/v1/version
```

Then use the current `dst\StrongDMM.exe` test artifact and `docs/testing/multiplayer-online-pilot-guide.md` to verify:

1. opening the hosted sign-in uses `https://mapping.a13.info` by default;
2. no Cloudflare Access login or client software is required;
3. OIDC sign-in returns successfully to the desktop callback flow;
4. an authenticated owner can create a private session;
5. a second authenticated identity can redeem a one-use editor invitation;
6. two clients edit, reconnect, and converge;
7. owner and editor names propagate correctly;
8. a hosted-service restart recovers acknowledged state.

If the endpoint displays a Cloudflare Access interstitial, returns a tunnel `1016` error, redirects to a different origin, or presents a certificate/hostname error, stop the pilot and correct routing before inviting users.

## Phase 7: Backup, restore, and rollback rehearsal

Create a backup with the shipped operator entry point and move the completed dump to the approved encrypted off-host destination:

```powershell
& .\deploy\production\operations.ps1 -Action Backup -Edge Cloudflare
```

Verify its size, timestamp, encryption-at-rest status, retention location, and restore ownership. Do not infer recoverability from backup creation alone.

Before accepting the pilot, rehearse restore with explicit confirmation during a maintenance window:

```powershell
& .\deploy\production\operations.ps1 -Action Restore -Edge Cloudflare -BackupFile <absolute-approved-backup-path> -ConfirmRestore
```

Retain the first known-good hosted image under an operator-recorded tag. Rehearse rollback during a maintenance window:

```powershell
& .\deploy\production\operations.ps1 -Action Rollback -Edge Cloudflare -ImageTag <retained-known-good-tag>
```

Replace the angle-bracket examples with the verified local path and tag. After each operation, require container health plus public live, ready, version, OIDC, and reconnect checks.

## Phase 8: Load, fault, and observability gates

Follow `deploy/production/README.md` and `docs/verification/phase-7-load-tooling-2026-08-25.md` for the hosted 25-editor profile. Use disposable OIDC identities and an ACL-restricted editor-token file. Delete that file and revoke the identities after the test.

Record:

- acknowledgement p50, p95, and p99;
- convergence and final map hashes;
- reconnect and conflict counts;
- WebSocket connection churn;
- PostgreSQL health and storage growth;
- slow-consumer and sustained-presence behavior;
- database interruption and recovery;
- authentication failures without credential material.

Configure or document the selected telemetry destination and alert owner before the named-user pilot. At minimum, cover public readiness, hosted process health, connection churn, operation latency, PostgreSQL health/storage, tunnel status, and authentication failure rate.

## Evidence package required from the deployment agent

Create or update `docs/verification/phase-7-public-hosting-2026-08-26.md` with:

- server OS, CPU, memory, storage, time synchronization, Docker Engine, and Compose versions;
- approved source revision and clean-tree evidence;
- hosted image tag and image ID;
- SHA-256 of `config.yaml` with confirmation that it contains no secrets;
- tunnel name and UUID, public route, DNS result, and tunnel health;
- confirmation that `bark` was unchanged and no Access application protects `mapping.a13.info`;
- health/version output and timestamps;
- OIDC, session, invitation, two-client, reconnect, and restart outcomes;
- backup location category, dump hash, restore outcome, retained rollback tag, and rollback outcome;
- hosted load/fault metrics and exceptions;
- telemetry destination category and alert owner;
- final go/no-go recommendation for the named-user pilot.

Never record tunnel tokens, OIDC secrets or codes, bearer/session credentials, invitations, database passwords or DSNs, Cloudflare Access URLs containing tokens, private map content, or full unredacted logs.

## Rollback and incident rules

- If public routing fails but the application is healthy locally, remove or disable only the new `mapping.a13.info` published route. Do not alter `bark`.
- If the new service is unhealthy, stop only the `apheliondmm-mapping` Compose project. Do not delete its PostgreSQL volume during incident response.
- If an application release regresses, use the retained known-good image tag through `operations.ps1 -Action Rollback`.
- If a tunnel token is exposed, rotate that dedicated tunnel token and replace its secret file. Do not paste the replacement token into an issue or chat.
- If an OIDC secret or session credential is exposed, revoke it at the provider before continuing.
- If durable state may be inconsistent, stop new testing, preserve both clients' redacted logs and relevant database backup, and do not run destructive repair commands without an approved recovery plan.

## Final handback status

Report one of these exact states:

- **Ready for named-user pilot:** every public, recovery, load/fault, observability, and security gate above has evidence.
- **Public endpoint staged, certification incomplete:** routing is live but one or more non-destructive acceptance gates remain; list them.
- **Not deployed:** a prerequisite or stop condition blocked the deployment; state the condition and the smallest user action needed.

Do not report general production completion. Production updater signing ownership and multi-replica fanout remain separate future gates.
