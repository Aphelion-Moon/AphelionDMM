# Reuse of fully checked SQLite recovery state

SQLite append now reuses one previously validated document when a complete
transactional reread matches its snapshot, accepted operations, revision hashes
and head metadata. In five interleaved trials of the previously failing
160-offered-operation/second workload, candidate schedule-to-ack p95 was
4.3498–4.5354 ms, compared with 603.3492–623.5781 ms for the frozen control.
All five candidates passed the unchanged 250 ms gate and exact SQLite reopen;
all five controls failed the latency gate after convergence.

This establishes an improvement for this short synthetic SQLite workload. It
does not establish general service capacity or close independent-load recovery,
long-history, PostgreSQL or desktop acceptance.

## Change and correctness boundaries

Previously every append loaded all retained records and called
`RecoveryState.Restore`, replaying the entire history before validating the new
accepted operation. The [rate-sweep profile](2026-09-20-concurrent-rate-sweep.md)
identified this reconstruction as a major allocation cost.

`sqlite.readRecovery` still reads and checks every persisted snapshot, operation
row and revision hash inside the append transaction. `recoveryCache.restore`
compares the entire parsed recovery state with `reflect.DeepEqual`; a matching
head alone is insufficient. A mismatch takes the existing full restore path,
including when another store changes the snapshot, history or authoritative
head. Public load, snapshot validation and cold reopen always retain their
original full validation paths. No schema, hash format, history retention,
transaction, acknowledgement or durability setting changed.

Taking a cached entry removes it from shared ownership before the candidate
document is mutated. Only a successful commit publishes the new checked state
and document. Failed validation, writes or commit therefore cannot publish an
uncommitted candidate. A small separate mutex protects entry transfer/publication;
it is not held while restoring, comparing state or accessing SQLite. Overlapping
publication may replace a newer entry with an older one, but every reuse still
requires full equality with the current transaction and safely falls back.

The store retains at most one eligible document. Eligibility limits are 1,024
operations and 4 MiB of encoded recovery row data, including snapshot, operation
and hash fields. These are source-enforced eligibility limits, not a measured
4 MiB Go-heap ceiling: the document, decoded state and metadata add overhead.
Oversized histories follow the original uncached path. Close clears the entry.
The cache owns its records and document; caller-facing load/lookup data remains
separate. Memory at the eligibility boundaries and long-history performance have
not been measured in this pass.

## Behavioral verification

Before implementation, added checks passed against the uncached implementation
(0.443 s), establishing behavior to preserve. They warm the store with a valid
append, then verify rejection of a changed retained operation, changed revision
hash, changed snapshot hash or removed operation without persisting the next
edit. Another check uses a second store to publish a snapshot and append, then
uses the first store for an actor-scoped inverse and verifies exact cold reopen.

The existing rollback check now starts with a successful append and injects an
operation-insert failure on the next edit. After rollback it submits the edit
with a different operation ID: this detects a mutable cached candidate left at
an uncommitted revision, which an identical-ID retry could conceal.

| Check | Result |
| --- | --- |
| Final complete SQLite package with `-race` | Passed, 1.939 s; includes store conformance, duplicate concurrency, cancellation, migration, rollback and the new warm-state cases. |
| Focused server recovery/audit tests with `-race` | Passed, 2.211 s; includes retained inverse/stale-base and corrupt recovery records. |
| Real session slow-consumer recovery with `-race` | Passed, memory and SQLite; package 1.174 s. This remains the bounded queue-overflow case, not an independent load campaign. |
| Maintained `task verify` | Passed lint/contracts, Go tests, pinned Rust checks, parser and Windows editor builds. Run before the final test-only different-ID refinement; the complete SQLite race package was rerun afterward. |
| Produced load CLI against candidate service | Five measured runs plus independent warmup passed with 320 sent, 256 accepted, 64 rejected, 1,024 client applications, no unresolved/unsent operations and exact durable reopen. |

The inherited ImGui compiler warning remains. Rust reports zero unit tests;
unconfigured external gates may skip. These results do not imply new full-stack
Meridian/Content Tools acceptance or a new PostgreSQL run.

## Controlled comparison

Control service test binary is the retained binary from the preceding sweep
(production source `228d75a5`, scenario-aware test patch). Candidate production
source is `47536484` plus the SQLite changes in this commit. Both use the exact
same load CLI and `scenario-160.json`: seed 20260920, four clients, 320 one-tile
operations, 64 conflict pairs first, a 16-by-16 map, 160 presence updates total
at 20/second/editor, and 250 ms schedule-to-ack p95 limit. The original 10-second
CLI and 30-second test deadlines remain. The nominal offered window is two
seconds; acknowledged history grows from zero to 256.

Host, loopback topology, Go 1.25.13 and SQLite 3.53.3 match the preceding report.
Each run starts fresh processes and a new database with verified WAL/FULL
durability. One control and one candidate warmup preceded measured trials.
Measured order alternated control/candidate, candidate/control, control/candidate,
candidate/control, control/candidate. Builds and other tests finished before
measurement; uncontrolled background workstation activity remains a limitation.

| Trial | Control schedule-to-ack p95, ms | Candidate schedule-to-ack p95, ms |
| ---: | ---: | ---: |
| 1 | 612.0086 | 4.4870 |
| 2 | 616.8628 | 4.5354 |
| 3 | 603.3492 | 4.3937 |
| 4 | 623.5781 | 4.4444 |
| 5 | 607.1421 | 4.3498 |

Candidate send-to-ack p95 was 3.7156–3.7827 ms; send-to-all-applied p95 was
4.2714–4.5925 ms; schedule-to-ack p99 was 6.9732–9.7272 ms. Achieved accepted
rate was 128.1148–128.1826/s versus control 106.4551–106.9477/s. The target is an
offered rate, and 20% of intents are expected rejections. Ranges are extrema of
five trial summaries, not pooled percentiles or maximum capacity estimates.

All 12 warmup/measured runs converged to revision 256 and canonical hash
`ec8036cca93104c5475c87dacbcd5d64f8cd21eb2462ff59097d6b2f6b08bf4e`.
There were no timeouts or disconnects in this comparison. Control latency failures
exit before the harness's SQLite reopen; all six candidates completed reopen.
The earlier sweep's two consumer disconnects remain valid retained failures;
their absence here does not prove recovery under independent load.

All planned presence was sent, with no local coalescing and peak send backlog
one. Unobserved presence deliveries at cutoff were 111–140/640 for measured
candidates and 174–219/640 for controls. This is a bounded observation metric,
not identified packet loss.

## Allocation diagnostic and evidence

A separate candidate allocation-profile run also passed with exact reopen.
It estimated 196.60 MB total allocation, with 142.32 MB cumulative in SQLite
append and 108.94 MB in `readRecovery`. The previous separate control profile
estimated 1,195.41 MB total and 1,144.49 MB in append. These are sampled cumulative
allocations, not retained memory or a repeated allocation benchmark. The candidate
includes successful final reopen, which the failed control profile did not reach.
The remaining complete read/decode cost is intentional; this change does not
assume exclusive database ownership to skip durable-record comparison.

Raw evidence lives in ignored `.artifacts/sqlite-recovery-reuse-2026-09-20/`:
source patch and new-source copy, binary/input hashes, ordered run records, all
12 logs, full parsed JSON/CSV, verification logs and the separate allocation
profile. The original control, CLI and scenario remain in the preceding sweep
directory. Candidate service SHA-256 is
`11c01cbfb1cbe91987cc1876c04a889c3a1e752e09d10f61ab490127baab6329`;
the manifest records every input's full hash.

Larger maps, cache-ineligible histories, multiple documents, long-running
resource retention and PostgreSQL need separate qualification. Exact recovery
and ambiguous-send accounting during independently scheduled disconnects remain
open even though this candidate passes the previously failing short workload.
