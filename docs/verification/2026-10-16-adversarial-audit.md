# Adversarial audit: design, function and performance

Baseline: `c17449f9` (two commits after `v.a.10`). This pass treats every
client message, socket lifecycle and HTTP request as hostile, reads the
collaboration server, protocol, engine, hashing, store, authentication,
atomic-save and recent desktop editing code for behaviour that breaks under
adversarial or merely unlucky sequencing, and repairs what it confirms. It does
not replace human desktop or multi-user session testing.

## Method

- Static review of `internal/aphelion/collab/{server,protocol,engine,model,
  client,store,auth}`, `internal/dmapi/dmmap/dmmdata/save_atomic.go`,
  `internal/aphelion/mapsave` and the diff since `v.a.10`.
- Each suspected defect was turned into a test against the shipped entry
  point (WebSocket or HTTP handler) before the repair. The two functional
  repairs were confirmed red on the pre-repair sources by stashing only the
  repaired files and re-running the new tests.
- Baseline gates before any change: `go build ./...`, `go vet`, and
  `go test ./... -count=1` (106 packages passing). `golangci-lint run ./...`
  failed on the baseline (see finding 5).

## Confirmed and repaired

### 1. A replacement socket for the same actor was disconnected by its own presence

`PresenceManager` ordered presence by a per-actor sequence and returned an
error for any update whose sequence was not greater than the stored one. The
WebSocket loop treated every presence error as fatal and closed the socket.
A fresh socket for an actor starts its sequence at 1 (`SessionClient` resets
`presenceSequence` on join), so whenever the socket it replaced was still
registered (a read deadline of up to ten seconds, or a second editor instance
with the same hosted identity), the new socket was closed on its first cursor
message. Hosted sessions kept the stale entry until its one-minute expiry, so
the replacement was closed repeatedly. The existing
`TestPresenceBudgetIsPerConnection` avoided the defect by starting the second
socket at sequence 100.

Repair (`server/presence.go`, `server/hub.go`, `server/websocket.go`,
`server/hosted_activity.go`): every served socket receives a connection
number. Presence ordering is scoped to that connection: a newer connection for
the same actor supersedes the older one regardless of sequence, and a late
update from the older connection is stale. Stale presence is `ErrStalePresence`,
which the handler drops without closing anything, matching the lossy-presence
invariant. Presence and cursor colour removal on disconnect is scoped to the
owning connection, so an older socket closing late cannot erase its
replacement's state.

Tests: `TestPresenceSequenceRestartOnNewSocketIsAppliedWithoutClosing`
(socket-level, red before the repair with the socket closed on the first
presence message), `TestPresenceNewerConnectionSupersedesOlderSequence`,
updated `TestPresenceRejectsOldSequenceAndRemovesOnDisconnect`.

### 2. A hosted member's chosen display name reverted on the next reauthorization

`profile_update` persisted the new name through the hosted registry and
renamed the hub member. Hosted connections reauthorize every 30 seconds and
before every durable or profile message; `reauthorizeHosted` built a principal
from the login identity's display name and rejoined the hub, which overwrote
the member. The next presence publication showed the login name again. The
session directory returns only role and disabled state, so nothing read the
persisted name back until service restart.

Repair (`server/hub.go`): `Hub.Join` keeps the existing member's display name
when the same principal rejoins; the role still follows the fresh
authorization.

Test: `TestHostedDisplayNameSurvivesReauthorization` (red before the repair:
presence carried `Login Name` after a reauthorizing profile message).

### 3. The join rate limiter could lock every new peer out

`rateLimiter.Allow` refused any new key once its table held 4096 live keys,
and only pruned keys idle for a full window. A peer with many source addresses
(trivial with IPv6) could keep the table full and refuse every join and
desktop sign-in start from any other address.

Repair (`server/security.go`): a full table evicts its least recently seen
key instead of refusing; surviving keys keep their budget. The join and
desktop-auth limiters key IPv6 peers by /64 so one host cannot fill the table
from its own prefix. Updated `TestRateLimiterBoundsEntriesAndExpiresIdleKeys`,
new `TestRateLimitKeyBucketsIPv6ByPrefix`.

### 4. Snapshot download copied and rehashed the whole map on the owner loop

`GET /v1/sessions/{id}/snapshot` cloned the complete document inside the
document owner's turn (`document.Snapshot()`), so every edit of every
participant waited for a whole-station copy, then rehashed the copy on the
handler goroutine to produce the `ETag`.

Repair (`server/document.go`, `server/http.go`): the owner turn pins the
immutable tile payloads (`CaptureSnapshot`, the mechanism periodic snapshots
already use) and returns the hash it already holds; the handler materializes
the copy off the loop and serves that hash as the `ETag`.
`TestSnapshotEndpointServesAuthoritativeHashWithoutRehash` checks the `ETag`
equals the accepted hash and that the materialized snapshot hashes to it.
No latency claim is made for a representative map.

### 5. Lint failed on the baseline

`internal/aphelion/editing/rotation.go` (commit `c17449f9`) iterated the edit
violations with a loop that returned unconditionally (`staticcheck SA4004`),
so the `Lint Source Code` job fails for the current `main`. Rewritten to take
the first violation explicitly; behaviour unchanged.

## Reviewed and not changed

- Protocol decoding applies byte limits before JSON decoding, rejects unknown
  fields and trailing values, and bounds collection sizes after decoding.
  Engine validation checks bounds, exact before-values and stable-ID
  uniqueness across the whole batch before any mutation. Incremental v2
  hashing falls back to a full rehash whenever its indexes disagree with the
  snapshot, and was read against the full hasher for tile insertion, row
  reshaping and dimension changes.
- Inverse validation requires exact reversal of the target's changes, the
  requesting actor, a forward target and no prior inverse.
- Recovery verifies the stored snapshot hash, contiguous revisions, retained
  hash count and every replayed revision's hash before serving a document.
- Hosted authentication binds browser start to a `__Host-` cookie, compares
  handoff secrets in constant time, never returns tokens on callbacks, and
  bounds provider responses and timeouts.
- Atomic save stages in the target directory, syncs, reparses and
  hash-compares before replacement, and verifies the replaced target when a
  disk state was expected.

## Residual observations (not repaired)

- A connection waiting on its own submission does not drain its durable
  subscription; with the default queue depth of 64 a busy session could close
  it as a slow consumer. Load tests have not shown this; raising the depth is
  a configuration decision.
- Session creation hashes the supplied snapshot more than once (handler,
  engine, store). It is a one-off cost per session.
- Presence cursor coordinates are only checked to be positive; selection
  bounds are checked against the map dimensions. A cursor beyond the map is
  harmless ephemeral data.
- `TestPresenceBudgetIsPerConnection` still starts its second socket at
  sequence 100; the new socket-level test covers the restart case.

## Evidence

Levels follow `docs/agent/verification.md`.

- Static review: the packages above.
- Focused tests: the tests named above pass; the two functional ones were
  confirmed red on the pre-repair sources.
- Repository tests: `go test ./... -count=1` passed (Go 1.26.9, 106 packages,
  0 failures). `go test -race ./internal/aphelion/... -count=1` passed. The
  SQLite restart/fault gates, collaboration fault gates and both protocol fuzz
  smokes (`-fuzztime=5s`) passed. `golangci-lint run ./...` (2.12.2) reports
  no issues. `task test-rust` with `RUST_TARGET=1.82.0-x86_64-pc-windows-gnu`
  passed (cargo test, fmt check, clippy).
- Cross-stack build: `task task_win:gen_syso` and `task build` with the same
  `RUST_TARGET` produced `dst/AphelionDMM.exe` (Task 3.53.1).
- Not reached: entry-point exercise of the produced desktop executable, the
  OpenGL Save gate, PostgreSQL tests (no `APHELION_POSTGRES_TEST_DSN`), or a
  live multi-user session.
