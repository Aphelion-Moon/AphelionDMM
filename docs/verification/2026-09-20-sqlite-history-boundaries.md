# SQLite cache boundaries and fixed-history costs

The exact-state recovery cache preserves durable recovery across its operation
and byte limits. Five fresh-process measurements also show that the earlier
short-workload improvement does not solve long-history cost: with 10,000 retained
operations, the following append takes about 107–114 ms in median and allocates
157–209 MB on this two-tile fixture. No production behavior or cache limit changed.

## Correctness at the limits

`TestRecoveryCacheLimitTransitions` uses actual SQLite append, complete stored
reconstruction and close/reopen. One case starts with 1,023 engine-validated
operations. Append 1,024 retains the cache; appends 1,025 and 1,026 do not.
The states occupy 934,416, 935,330 and 936,244 encoded bytes respectively, so the
operation limit is isolated from the byte limit.

Three other cases place the complete encoded state immediately after the first
append at 4,194,303, 4,194,304 and 4,194,305 bytes. An ASCII variable on an unchanged
tile supplies the padding. The first two retain the entry; the third does not.
Further appends cross the limit and continue correctly without retention. A
complete independent durable read matches every retained state.

Each case then applies an actor-scoped inverse twice, closes and reopens the
database, and checks exact map content and every stored accepted record against
the independent engine reference. The repeated inverse remains idempotent and
recovery remembers that its target is already inverted. The default focused run
passed in 1.337 s; the focused race run passed in 8.379 s. SQLite package lint
reported zero issues.

## Measurement boundary

`BenchmarkSQLiteRecoveryHistory` uses fixed UUIDs, acceptance timestamps and a
two-tile, two-prefab map with an opaque unknown path. Each operation toggles one
tile's `dir` variable; the other tile is unchanged. The history lengths are
0, 100, 1,000, 1,024, 1,025 and 10,000. This isolates history growth from map size;
it is a different fixture from the earlier 100-cell audit and concurrent service
workloads.

The setup writes engine-validated prefix records in one SQLite transaction,
outside timing, then verifies the full stored history. This is fixture loading,
not a measurement of sequential ingestion through public Append. For nonempty
append cases, a real preceding Append brings the database to the named history
length. The two timed append modes then start with identical durable rows:

- `following_append` keeps whatever recovery entry the preceding append retained.
- `cache_cleared_append` discards only that optional entry before timing.
- `recovery` measures LoadRecovery plus Restore without using the append cache.

Uncompacted cases retain the initial snapshot at revision zero. Compacted append
cases snapshot history minus one, leaving one replay operation; compacted recovery
cases snapshot the complete history. Snapshot publication retains all validation
history. The zero-history fixture has no preceding append and therefore omits
`following_append`.

Every sample closes and reopens the store after timing, then checks exact map
content, history count and all operation records against the reference. Setup,
reference generation, result checks and reopen are outside timed allocation and
elapsed measurements. WAL and synchronous FULL remain enabled. Cache clearing
does not flush SQLite, filesystem or device caches; these are warm-data component
measurements. Reported allocation is cumulative Go allocation per operation,
not retained heap, peak process memory or native/database memory.

The preserved test binary ran one separate 34-case warmup and five fresh-process
34-case trials with `-test.benchtime=1x`. All 170 measured cases and 34 warmup cases
passed, with zero workload failures/timeouts. An initial PowerShell argument
formatting error exited before running any workload; that log is retained.

## Results

Following-append elapsed time is median [minimum–maximum] across five samples.
Allocation columns are median decimal MB/op. At history 1,024 the incoming
append can consume the previous cache entry but does not retain its 1,025-row
result. At history 1,025, the preceding append already left the cache empty.

| Prior operations | Snapshot at zero, ms | Compacted prefix, ms | Allocation, zero / compacted MB |
| ---: | ---: | ---: | ---: |
| 100 | 2.212 [1.930–2.453] | 2.284 [1.913–2.597] | 0.670 / 0.672 |
| 1,000 | 9.597 [9.367–9.971] | 9.171 [8.940–9.382] | 6.451 / 6.453 |
| 1,024 | 9.430 [9.101–10.214] | 9.575 [9.290–9.895] | 6.569 / 6.569 |
| 1,025 | 12.817 [12.443–13.551] | 14.227 [13.815–14.515] | 15.800 / 21.184 |
| 10,000 | 107.428 [106.915–109.328] | 114.187 [113.924–115.500] | 156.778 / 209.333 |

At 1,000 operations, explicitly clearing the cache gives median append costs of
12.767 ms / 15.440 MB without compaction and 13.406 ms / 20.689 MB with compaction.
At 10,000 operations, the corresponding cache-cleared medians are 107.968 and
115.044 ms: no cache entry exists in either mode. The full encoded history at
10,000 operations is about 9.14 MB, also above the byte limit.

| History | Recovery median, snapshot zero / head, ms | Recovery allocation, zero / head MB |
| ---: | ---: | ---: |
| 0 | 0.042 / 0.036 | 0.015 / 0.014 |
| 100 | 1.197 / 1.264 | 1.528 / 2.054 |
| 1,000 | 10.991 / 11.977 | 15.411 / 20.668 |
| 10,000 | 105.936 / 112.656 | 156.750 / 209.310 |

All 34 combinations, individual samples, allocation ranges and fixture hashes
are retained in `measurements.csv`, `summary.csv` and raw logs under ignored
`.artifacts/sqlite-history-boundaries-2026-09-20/`. Production revision was
`35f9e3ba`; the test binary SHA256 is
`36999460f6e215be71b03ec22b8d0a57b31ee37cdbcc80672d1817382fe2f578`.
The host was Windows Server 2022 (10.0.20348), Ryzen 7 9800X3D, 8 cores/16 logical
processors, Samsung SSD 990 PRO 1TB NVMe, using pinned Go 1.25.13 and SQLite 3.53.3.

## Remaining work

This supplies boundary correctness and a bounded history-cost baseline, not a
service capacity pass or long-duration resource qualification. Two tiles do not
represent typical map density; five single-operation samples do not establish
tail latency. Snapshotting preserves history and does not remove this cost.
Further work must separate SQL decoding, retained-prefix validation, replay,
hashing and durable write costs, and address long histories without dropping
historical bases, duplicate identities or inverse targets. Retained heap/native
resources, larger maps, extended trials and the PostgreSQL counterpart remain open.
