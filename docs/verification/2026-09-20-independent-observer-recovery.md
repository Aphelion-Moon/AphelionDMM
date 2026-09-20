# Observer recovery during independent collaboration load

At production revision `e696923b`, the real desktop session client recovers from
server queue overflow while four other clients continue independently scheduled
edits and presence. Five race-instrumented repetitions passed with each of the
memory and SQLite stores. This extends the earlier
[serial slow-consumer case](2026-09-20-slow-consumer-recovery.md); production code
and dependencies are unchanged.

## Workload and failure injection

`TestSessionClientRecoversDuringIndependentLoad` uses the existing
`load.RunConcurrent` runner and slow socket wrapper. The service has six real
HTTP/WebSocket participants: an owner observer, the blocked session client and
four load editors. The deterministic scenario uses seed 20260920, 64 operations,
eight conflict pairs and an initially empty 8-by-7-by-1 map. Operations are
offered on absolute schedules at 40/second. The first 16 intents form the eight
conflict pairs; the remaining edits use distinct cells. Expected durable state
is revision 56 and canonical hash
`300d2e71fcfdd7edc058014a9cbde81e9c1e5652c5ba5fbf2af20f38a9532c06`.

Each load editor independently offers 32 presence updates at 20/second during
the approximately 1.6-second edit window. The configured service presence
interval is 16 ms. The test has a ten-second context and an eight-entry durable
subscription queue. It does not set a latency acceptance threshold; race-run
latency samples are retained diagnostics, not performance measurements.

The wrapper blocks only the slow participant's server socket writes after its
authenticated join. It waits for the healthy owner to observe 12 accepted
events, exceeding the queue depth even if the blocked writer already removed
one event, then releases the socket. That wait controls failure injection only;
the load editors never wait for it or for acknowledgements to offer their next
intent. The server itself emits the actual `4408` slow-consumer close.

The test requires the slow client's normal session monitor to reconnect exactly
once with the same actor/executor and a rotated resume credential. It must be
observed caught up at revision 12 or later, strictly before revision 56. In all
ten race cases, that observation occurred at revision 12. This prevents a pass
based solely on reconnecting after all workload edits have finished.

## Accounting and follow-up edits

Every race case scheduled, started and sent all 64 operations, observed exactly
56 acceptances and eight expected rejections, and validated 224 applications
across the four load clients. There were no unsent or unresolved operations and
no timeouts. The four load clients and final HTTP snapshot matched the manifest.
The two session clients are additional observers, not part of the runner's
224-delivery counter; their final authority checks are separate.

The test reconstructs the complete durable recovery state, verifies revision 56
and the manifest hash, and checks that every stored operation has a unique ID
from the offered scenario. The recovered session client then submits a new
edit and its actor-scoped inverse. Both session clients reach revision 58 with
the workload's original final map hash, and the store contains exactly the 58
accepted operations in contiguous revision order. SQLite uses its enforced
WAL/FULL durability. This test exercises `LoadRecovery` and reconstruction, not
a database close/reopen, service restart or crash.

All 128 planned presence updates were sent in every race case, with zero local
coalescing and peak send backlog one. Unobserved deliveries among the four load
clients were 101–147/512 for memory and 115–127/512 for SQLite. These remain
bounded-cutoff observation counts, not identified network packet loss.

## Validation and fixture corrections

- Focused old and new recovery cases passed: 3.471 s.
- Complete collaboration UI package passed with `-race`: 5.067 s. This also
  exercises the existing session reconnect/in-flight-operation checks.
- Four additional repetitions of the new case passed with `-race`: 14.731 s,
  giving five repetitions per backend including the package run.
- `golangci-lint run ./internal/aphelion/collab/ui` passed with zero issues.

These use pinned Go 1.25.13 and golangci-lint 2.12.2. The inherited ImGui compiler
warning remains. No full repository build or external gate was rerun for these
test-only changes; the current production revision's maintained gate is recorded
in the preceding SQLite report.

Two initial fixture failures are retained. The first inherited the service's
default 100 ms presence interval, causing expected local coalescing and failing
the test's all-presence-sent assertion. The independent case now explicitly
configures 16 ms so its 20/second offered presence can all be sent; the original
serial case keeps its defaults. The second exposed a single-tile assumption in
the shared replacement-operation test helper: it took before-values from the
first tile but always addressed `(1,1,1)`. The helper now uses that tile's actual
coordinate, permitting the randomized multi-tile fixture. Neither was a
production recovery failure, and no durable accounting assertion was removed.

Raw logs, parsed race results and source hashes are retained in ignored
`.artifacts/independent-recovery-2026-09-20/`. Reproduce with the complete UI race
package or `go test -race ./internal/aphelion/collab/ui -run
'^TestSessionClientRecoversDuringIndependentLoad$' -count=5 -v -timeout 90s`.

## Remaining scope

The disconnected participant is an observer during the independently scheduled
phase. The load writers remain connected; the recovered participant edits only
after that phase. This does not qualify uncertain or rejected drafts originating
from a disconnected writer, replay/snapshot fallback under that writer's offered
load, multiple simultaneous failures, long histories, larger maps, PostgreSQL,
long-duration resource use, hosted networking or production capacity. The
broader independent-load recovery acceptance item therefore remains open.
