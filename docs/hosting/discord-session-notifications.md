# Discord session notifications

The hosted service can post session-status summaries to a configured Discord text
channel. This is independent of its login provider. The bot needs View Channel
and Send Messages in that channel; no privileged gateway intents are required.

Add this optional section to the hosted YAML configuration:

```yaml
discord_notifications:
  guild_id: '1527801651346411590'
  channel_id: '1530200440032202802'
  bot_token:
    file: 'C:\Services\AphelionDMM\secrets\discord_bot_token.txt'
```

Keep the bot token in a restricted secret file readable by the service identity,
or use an `APHELIONDMM_` environment secret source. This is a bot token, not the
OAuth client secret. The service verifies that the channel belongs to the
configured guild before posting. Remove the section to disable notifications.

A background observer checks sessions every 30 seconds and posts a new summary
only when the displayed status changes. It reports Community titles and connected
participant counts, plus aggregate Private session and active-session counts.
Private titles, map/environment labels, member identities, invitations, tokens
and filesystem paths are never included. Mentions and link previews are disabled.
At most ten Community sessions appear in a summary; the remainder are counted.

This is best-effort status reporting, not an audit log: rapid changes may be
coalesced, an idle session remains persisted, and service restart produces a fresh
summary. Notification failures do not block authentication or map editing.
Requests have deadlines and bounded responses; rate-limit responses delay retry.
HTTP 401/403 pauses retries for five minutes. Logs contain status codes rather
than Discord response bodies or credentials. Changing a Community session to
Private does not erase information already posted while it was Community.

Discord API contracts: [messages](https://docs.discord.com/developers/resources/message#create-message),
[rate limits](https://docs.discord.com/developers/topics/rate-limits).

## Session creation returns HTTP 409

`transaction_upgrade_required` means the editor requested bulk edits against an
older transaction store. Discord guild ownership does not bypass this storage
requirement. Hosted metadata migration 5 does not imply transaction migration 4
was applied. Do not work around it by silently disabling bulk edit support.

Quiesce the service and take an encrypted backup. The existing
`apheliondmm-transactions-migrate` command validates and copies the original V3
schema to a distinct V4 schema, preserving documents, history and hosted metadata.
Use its `--postgres-source-schema` and `--postgres-destination-schema` flags with
the database credential supplied through `APHELION_POSTGRES_DSN`. Then select the
validated destination through the DSN's `search_path` parameter and restart the
service. Retain the original schema and previous DSN for coordinated rollback;
after new edits, switching back to the old schema would lose those newer edits.

`session_exists` means the map already has a hosted session. Use Browse Sessions
→ My sessions to rejoin it. These two conflicts have distinct desktop messages
in builds containing the September 27 follow-up fix.
