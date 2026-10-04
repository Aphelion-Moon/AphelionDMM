# Hosted storage upgrade and Discord status

On September 27, the reference service at https://mapcollab.a13.info was upgraded
to `4257b040c53ecb9e6d2e2067fd9693f81e810e64`, build
`mapcollab-session-fix-20260927`. Executable SHA-256:
`0993a562768846058a1da4296af0e280065ada6699de8947608d7b7a9317909f`.
This supersedes the running-revision information in the earlier same-day record.

## Session creation failure

The operator successfully signed in through Discord but session creation returned
HTTP 409. Source and live database inspection identified
`transaction_upgrade_required`: the desktop requests `bulk_edits: true`, whereas
the service's original schema contained migrations 1/2/3/5 but not transaction
migration 4. There were no registered sessions or members, so this was not an
existing-session or Discord-role conflict. One document snapshot had been stored
before transaction capability setup failed.

The desktop now maps known conflict codes to local actionable messages without
echoing untrusted response text. Its regression failed before the fix and passed
afterward. This message improvement is on main; the published v.a.2 desktop does
not need replacing to use the corrected hosted storage.

The existing migration command passed its PostgreSQL upgrade test in an isolated
database, then validated and copied `public` into `aphelion_v4_20260927` while the
service was quiesced. Exact snapshot/hash/revision comparison preserved the single
document. The new schema contains migrations 1/2/3/4/5. The application DSN now
selects it through `search_path`. The original `public` schema remains intact.

Before final cutover, the source was checked for changes and another encrypted
backup was taken: `20260927-171446-278.dump.age`, SHA-256
`929f4f178cd0158d783a7e1e4ce719a2ae96476af7b7d59f4726399320b81ce1`.
Post-cutover backup: `20260927-171544-618.dump.age`, SHA-256
`18e869b899405517ca56cffcffaaef7022e2d2ca1d1140652769c01f72064210`.
Both are in the operator-selected `D:\Backups\AphelionDMM` directory. Previous
binary/configuration/DSN are retained under the restricted service staging
directory `backup-staging\session-fix-20260927`. After new accepted edits,
switching back to the retained schema requires a coordinated recovery decision;
the old schema does not contain those newer edits.

## Discord status

The optional [Discord observer](../hosting/discord-session-notifications.md) is
configured for guild `1527801651346411590`, channel `1530200440032202802`
(`main-bots`). The separate bot token is in a restricted secret file with the
same service-account ownership/access policy as the OAuth secret.

Bot authentication and channel/guild identity were checked. The deployed service
successfully posted [its initial session summary](https://discord.com/channels/1527801651346411590/1530200440032202802/1553786995669598311).
It checks every 30 seconds and posts only changes; no mention pings are allowed.
Community titles/counts are public to that channel, while Private sessions expose
only aggregate counts. Queries return aggregate totals and at most ten Community
rows, rather than repeatedly loading the full registry. The observer stops before
the database is closed and cannot block durable editing on Discord availability.

## Evidence and limits

- Focused server, client, configuration and hosted-command checks passed with the
  race detector. New observer tests cover privacy/content bounds, channel identity,
  missing channel type, mention suppression, unchanged-state coalescing, rate-limit
  delay, and cancellation of an outstanding registry query.
- PostgreSQL migration and bounded-summary tests passed against isolated schemas.
  The summary test verifies aggregate Private activity and the ten-row result cap.
- Focused lint reports zero issues; the hosted executable builds successfully.
- All three automatic Windows services are running. Local/public readiness and
  the exact deployed revision were verified after cutover. Discord delivery was
  confirmed by reading the bot's posted message.
- Main CI for the executable source is [run 36328789777](https://github.com/Aphelion-Moon/AphelionDMM/actions/runs/36328789777);
  it was still running when this record was written. No completed-CI claim is made.
- Restart cleared in-memory application logins. The operator must sign in again
  and retry actual map-session creation. Nonmember denial, multi-user acceptance,
  and human desktop qualification remain separate checks.

No protected CI, build, signing or deployment-script entry point was edited.
