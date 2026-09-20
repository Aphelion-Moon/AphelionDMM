# SQLite recovery after abrupt process termination

The SQLite persistence tests now terminate a live writer subprocess after a
successful durable append, then reopen the database and verify exact recovery.
The writer does not call `Store.Close`, run deferred cleanup or shut down
gracefully. Production code and dependencies do not change.

`TestSQLiteAbruptProcessRecovery` uses the existing deterministic two-tile history
fixture, including unknown prefab paths and variables. The default initial
history is 32 edits. Setting `APHELION_SQLITE_CRASH_HISTORY=10000` exercises
sequential ingestion beyond the 1,024-operation recovery cache limit. Every
operation goes through `Store.Append`; this setup does not bulk-insert history.
The store retains its normal WAL journal and FULL synchronous settings.

Each of five writer subprocesses saves a snapshot immediately before its final
append, reports the committed revision/hash and open WAL size, and waits. The
parent kills that specific live child and requires unsuccessful process exit.
It then checks SQLite integrity and reconstructs all retained history against
an independently generated reference, including exact operation IDs, revisions,
timestamps, inverse metadata and unknown map content. Duplicate append of the
last accepted operation must preserve identity. An inverse by another actor must
fail with `actor_mismatch`; the correct actor's inverse must succeed and remain
idempotent. Later crashes must preserve `already_inverted` status. The final
operation merges against the authentic revision-zero base on the untouched
second tile, proving the original historical base remains usable.

The initial subprocess writes the configured history. Each later subprocess
recovers the same database and appends one further edit after the parent's
inverse. Consequently the long-history case kills writers at revisions 10,000,
10,002, 10,004, 10,006 and 10,008 and ends at revision 10,010 after five inverses
and the historical-base edit. These are five successive crash/recovery cycles
on one history, not five independent ingestion or performance trials.

Validation on Windows with pinned Go 1.25.13 and SQLite 3.53.3:

- Initial default-history run: five abrupt terminations passed, package duration
  0.641 s.
- Race-instrumented default-history and existing cache-boundary checks passed
  together in 9.170 s.
- The 10,000-edit run passed all five termination cycles in 556.99 s. It ended at
  revision 10,010 with exact full-history reconstruction and no failure/timeout.
  This duration includes ingestion, checkpoints, process startup, recovery and
  assertions; it is not an isolated append or recovery benchmark.
- Final default-history race run passed in 2.372 s after tightening the child
  diagnostic check. The helper now halts on race reports and the parent refuses
  unexpected stderr, so intentional process termination cannot mask a child
  diagnostic. This final change does not alter the ingestion/recovery workload.
- Final focused SQLite lint: zero issues; diff whitespace check passed.

All five long-run recovered heads had canonical hash
`6ab44b75526434d1a5d755e2d596be247bc1567d5f4f48f193a87348518cd3f1`.
The first killed writer had a 4,177,712-byte WAL; later cycles had 28,872 to
41,232 bytes. These sizes establish that the live database had a WAL file;
they do not establish how many frames had already been checkpointed.

Raw logs, the long-run source and test executable are retained in ignored
`.artifacts/sqlite-process-crash-2026-09-20/`. The long-run executable SHA-256 is
`886e2263a285b018c6f3643df8e5fdde0ee71c01d4d0e83d96ebbf5530389d68`.
It predates only the final child diagnostic guard described above. The default
history remains short for ordinary regression runs. Full application and
external-service gates were not rerun for this test-only change.

This tests process termination after commit, with the operating system and
storage still running. It does not simulate power loss, termination during a
transaction/commit, a lost service acknowledgement, full application reconnect,
database loss or PostgreSQL. The two-tile fixture does not qualify representative
map performance or resource retention. Broader campaign gates remain open.
