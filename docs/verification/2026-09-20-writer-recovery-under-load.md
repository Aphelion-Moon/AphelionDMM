# Uncertain writer recovery while another editor continues

September 21 update: Windows CI exposed that the fixed 32-offer fixture could
finish during fault inspection or recovery. The current test keeps the same
absolute 40/second schedule until recovery is observed, sends eight further
offers, and requires at least 32 in total. The original ten-second context still
bounds the entire case; a finite 400-intent budget also fails on exhaustion.
Expected hashes and exact durable identities are derived from the actual offered
prefix. Reconnect counts, draft checks, and explicit recovery/undo checks remain.
A regression deliberately holds reconnect until 40 offers have been sent.
The fixed-count measurements below describe the original run, not a latency SLO.

At production revision `18cac4f3`, all 40 race-instrumented cases passed for
queued-but-unsent and committed-but-unacknowledged edits, each recovered through
replay or authenticated snapshot fallback, with memory and SQLite storage. A
healthy editor continues fixed-schedule work throughout each interruption and
reconnect. This adds test coverage; no production code or dependency changed.

## Scope and injection boundaries

`TestSessionWriterRecoversUncertainEditDuringLoad` creates two real session clients
on a disposable loopback HTTP/WebSocket service. A seed-20260920 concurrent
scenario supplies 32 distinct one-tile operations in a 33-by-1-by-1 map. The
healthy editor sends their authenticated revision-zero envelopes on absolute
deadlines at 40/second, without waiting for acknowledgements. It also publishes
presence every second offered edit, for 16 publication calls. Real session-client
projections consume the accepted stream, and the full durable log independently
accounts for the operation IDs. Transport send success means local enqueue;
store/client checks establish eventual acceptance.

The interrupted writer has one operation on the remaining coordinate. Its test
transport injects either of these boundaries:

- **Queued but unsent:** `Send` returns success, modelling admission to the local
  queue, but the wrapper never forwards the operation to the underlying socket.
  This simulates a queued frame lost before write, not a real socket-buffer fill.
- **Committed but unacknowledged:** the operation crosses the real transport and
  service. The wrapper observes and withholds its accepted envelope and every
  later accepted envelope, preserving the client's contiguous prefix. A store
  lookup verifies that the original ID is already durable.

After at least eight healthy offers, the test closes the original connection.
The session client's own monitor suspends the executor and starts reconnect.
The first reconnect attempt is held before connecting, allowing inspection of
the released caller and exactly retained `delivery_unconfirmed` draft while
healthy offers continue. Snapshot cases then publish a valid stored snapshot
strictly newer than the interrupted client's acknowledgement before reconnect
is released. The normal `snapshot_required`/HTTP snapshot/rejoin path executes.

Each case requires reconnect before all 32 healthy offers are sent, the same
executor, and a rotated credential. Replay uses two total transports including
the initial connection; snapshot fallback uses three. Across the five repetitions
per backend, observed recovery was at revision 8 (unsent/replay), 9
(committed/replay), 22–23 (unsent/snapshot) or 19 (committed/snapshot), with only
8–23 healthy offers sent. These observations establish overlap with ongoing
offers, not a recovery latency SLO.

## Outcome and draft accounting

| Original outcome | Recovery | Required draft behavior |
| --- | --- | --- |
| Not durable | Replay | Preserve exact uncertain draft; explicit rebuild uses a new ID and current authority. |
| Not durable | Snapshot | Preserve exact uncertain draft across baseline replacement; allow the same explicit rebuild. |
| Durable once | Replay | Validated accepted replay resolves the uncertain draft. |
| Durable once | Snapshot | Retain the uncertain draft because snapshot content is not proof of operation identity; reject a rebuild whose intent is already present. |

A counter wraps every writer transport. It must remain at one operation offer
through recovery, so idempotent automatic retries cannot hide behind a single
stored record. Only explicit test recovery actions increase it: a rebuilt or
new forward edit and its actor-scoped inverse. Retained conflicts are explicitly
discarded after inspection/recovery; reconnect does not silently discard them.

Before those actions both clients converge to all 32 healthy edits plus the
original writer edit only when it was committed. Afterward every case verifies
full stored recovery, unique known operation IDs, contiguous final history and
both clients' exact expected map hash. Unsent cases end at revision 34 without
the original draft ID; committed cases end at revision 35 with that ID exactly
once. There are no unaccounted healthy edits, automatic writer resends or
unexpected durable operations.

Protocol v1's inverse swaps tile values, not coordinate membership. Rebuilding
and undoing an edit at a previously omitted coordinate leaves an explicit empty
tile. The v1 hash includes that coordinate, so the expected final sparse
representation differs from the pre-rebuild representation even though its tile
values are restored. The test checks that exact representation and all unrelated
content; it does not claim structural omission is restored. No canonical hash
or protocol semantics were changed.

## Validation and evidence

The initial non-race cases passed after correcting the sparse inverse expectation
(6.514 s). An initial five-repeat race run exposed a fault-wrapper mismatch:
returning a pre-queue send error legitimately retained `submission_failed` when
it raced ahead of suspension. The final wrapper models successful local enqueue
followed by loss before write, preserving the stricter uncertain-draft assertions.
Initial failed logs remain available; neither observation was a production repair.

Final `go test -race ./internal/aphelion/collab/ui -run
'^TestSessionWriterRecoversUncertainEditDuringLoad$' -count=5 -v -timeout 120s`
passed all eight combinations five times, package duration 34.660 s. Focused
`golangci-lint run ./internal/aphelion/collab/ui` passed with zero issues. The pinned
Go 1.25.13 and golangci-lint 2.12.2 tools were used; the inherited ImGui compiler
warning remains. The suite, external services and desktop builds were not rerun
for this isolated test addition. These are correctness repetitions, not a
warmup-controlled capacity or memory campaign.

Raw logs, per-case recovery observations, fixture configuration and source hashes
are retained in ignored `.artifacts/writer-recovery-2026-09-20/`. Service context
is bounded to ten seconds per case; tests own all temporary databases and sessions.
SQLite exercises its maintained WAL/FULL path and stored reconstruction, not
close/reopen or crash recovery in this case.

## Remaining scope

Together with [observer recovery](2026-09-20-independent-observer-recovery.md), this
establishes bounded recovery during independent offers and correct handling of
one uncertain writer intent. The writer interruption here is injected explicitly;
it is not simultaneous slow-consumer overflow with several pending writer edits.
The [lost rejection follow-up](2026-09-20-lost-rejection-recovery.md) now covers one
withheld authoritative rejection during continued offers. Multiple writers
failing together, larger histories, long-duration resources, PostgreSQL and
hosted-network qualification remain open.
