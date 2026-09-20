# Native Windows hosted deployment

The user selected the current Windows server, `https://mapcollab.a13.info`,
Cloudflare OIDC, and `D:\Backups`. This supersedes the older Linux Compose and
`mapping.a13.info` deployment selection for this installation. The portable
Compose bundle remains unchanged.

## Installed state

The native service at `C:\Services\AphelionDMM` serves build
`mapcollab-windows`, revision `81a89615c4ae13f7e704df566dde2db62ab79389`.
Its executable SHA-256 is
`057f519fa9ebcf66ca722a31174745ed8bf471619e43241b18672025bf58d8c4`.

Three automatic-start Windows services are running:

| Service | Account | Role |
| --- | --- | --- |
| AphelionDMM-Postgres | NetworkService | Separate PostgreSQL 17.11 cluster, loopback port 5487 |
| AphelionDMM-Hosted | LocalService | One hosted process, loopback port 18080 |
| AphelionDMM-Tunnel | LocalService | Dedicated cloudflared 2026.7.3 connector |

The PostgreSQL runtime was copied from the installed 17.11 distribution; the
existing SS14 database and service were not modified. Hosted/tunnel supervision
uses the existing NSSM 2.24 executable copied into this installation. The hosted
service depends on its PostgreSQL service; the tunnel depends on the hosted
service. Logs rotate at 10 MiB. No public origin port or firewall rule was added.
The existing Docker, Cloudflared service, bark tunnel and game services remain.

Cloudflare has a dedicated remotely managed `apheliondmm-mapcollab` tunnel and a
proxied CNAME for the selected hostname. Both loopback and public readiness
returned `{"status":"ok"}` after deployment; the public version endpoint reported
the exact revision above. These are operational startup observations, not a
load, fault, browser-login or compatibility campaign.

## Authentication

The dedicated Cloudflare Access SaaS OIDC application uses Authorization Code
with PKCE and a confidential-client secret. Its callback is exactly
`https://mapcollab.a13.info/v1/auth/complete`; the public API is not behind a
self-hosted Cloudflare Access application. The client secret and tunnel token
are stored in restricted local files, not source control or logs.

Initial service startup exposed a configuration defect: the hosted loader
allowed only pathless issuer origins, while Cloudflare issues a client-specific
issuer URL with a path. Commit `81a89615` accepts HTTPS issuer paths, still
forbidding credentials, queries and fragments, and updates the configuration
schema. Public-origin validation is unchanged. The native binary was rebuilt;
the service then loaded its actual issuer discovery and started successfully.
File-backed application secrets are owned by LocalService with read grants only
to that identity, SYSTEM and Administrators, as required by the Windows secret
permission contract.

**Pending:** the user must choose who may sign in (Cloudflare account members,
specified emails, or anyone who authenticates). There are currently zero allow
policies on this OIDC application. Sign-in is therefore not enabled, and no human
OIDC login, session or WebSocket operation has been exercised.

## Backups

`AphelionDMM-DailyBackup` runs at 03:30 as SYSTEM, with start-when-available and
overlap prevention. It makes a PostgreSQL custom-format dump, encrypts it using
age 1.3.2, and atomically publishes it under `D:\Backups\AphelionDMM` with a
SHA-256 sidecar. Plaintext staging is confined to the restricted service directory
and removed after the attempt. Automatic deletion is disabled until a retention
period is selected.

The first scheduled invocation completed with result 0 and produced
`20260920-225958-726.dump.age`, SHA-256
`191984d0ee296a020d36402c40ccc3d83726f538a0fd0e64cbc59c76bc58f17e`.
The private recovery key is in
`C:\Services\AphelionDMM\secrets\backup-age-key.txt`; it needs a separately kept
operator recovery copy. D: is the user-selected local backup destination and is
not an off-server backup. No restore rehearsal or reboot was performed.

## Artifacts and validation boundary

The desktop's editable default endpoint was changed in `e58d932d` and its Task
build succeeded. The Windows server was compiled with pinned Go 1.25.13.
The downloaded age archive matched the SHA-256 published in its official GitHub
release metadata. Deployment used reviewed local PowerShell installers through
Windows elevation, with scope limited to the named services and directories.

The task's `work/mapcollab-deployment/` retains the installer, completion script,
payload hashes, configuration, Cloudflare resource IDs, installation result and
restricted secret staging. Operational scripts are installed alongside the
service. No credentials are included in this report.

The user prohibited further tests after the final IceBox native-frame run.
No additional tests or lint were run; the issuer repair has source review,
successful native compilation and actual service-start evidence only.

References: [Cloudflare OIDC SaaS setup](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/saas-apps/generic-oidc-saas/),
[NSSM configuration](https://nssm.cc/usage),
[age](https://github.com/FiloSottile/age).
