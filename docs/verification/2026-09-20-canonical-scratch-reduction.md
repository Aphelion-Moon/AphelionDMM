# Smaller canonical scratch during history recovery

Canonical encoding now uses a fixed 2 KiB scratch buffer instead of 4 KiB. In
five paired trials at 10,000 retained operations, this reduces SQLite following-
append allocation by 16.3% without compaction and 24.5% with a compacted prefix.
Median append time improves by 5.2% and 6.4% respectively. Direct hashing of the
10,000-cell fixture has a small measured tradeoff: its median rises by 1.6%, or
0.033 ms. This is a bounded component improvement, not completion of long-history
or service-capacity qualification.

## Evidence and change

The [history-boundary baseline](2026-09-20-sqlite-history-boundaries.md) exposed
large allocation after the exact-state cache becomes ineligible. A separate
profile of its compacted 10,000-operation case attributes 181.83 MB of flat
sampled allocation to `Snapshot.Hash`, out of 379.58 MB cumulatively under
SQLite Append. This filtered profile includes the fixture's preceding append
and the measured append; these are not per-operation allocation numbers. CPU
sampling was brief and partly attributed to Windows CGO calls, so it is not a
precise CPU or fsync breakdown.

The production change only reduces the encoder's fixed array size. The existing
write/flush path, byte order, canonical domain, sorting and SHA-256 algorithm
remain intact. Each invocation still owns its buffer and digest. There is no
buffer pool, shared cache, retained-history truncation or durability change.
The independent buffered-hash oracle now also exercises value lengths around
2,048 bytes; existing long-string and bounded-allocation coverage remains.

A first adaptive 512-byte/4-KiB prototype reduced allocation but slowed the
10,000-cell hashing fixture by about 6%. It was discarded. Its source diff,
binaries and paired measurements remain under ignored
`.artifacts/adaptive-canonical-buffer-2026-09-20/`; it is not shipped.

## Paired measurements

The final fixed-buffer comparison uses production baseline `e4a4cbbf` and
separately preserved control/candidate binaries on the same host as the linked
history baseline. Each version has an independent warmup. Five subsequent
fresh-process rounds alternate which version runs first.

SQLite repeats all 34 fixed-history combinations, with one timed operation per
case and complete close/reopen verification afterward. All 340 measured cases
and 68 warmup cases passed. Across all 408 fixture observations, operation count,
snapshot revision, encoded size, cache eligibility and head hash match between
versions and rounds. The bulk fixture setup, snapshot placement and timing
boundaries are unchanged from the linked baseline.

At 10,000 operations, following-append time is median [minimum–maximum] across
five process samples. Allocation is median decimal MB per operation.

| Snapshot position | Control ms | Candidate ms | Control MB | Candidate MB |
| --- | ---: | ---: | ---: | ---: |
| Revision zero | 113.302 [110.259–116.425] | 107.432 [104.830–108.994] | 156.778 | 131.170 |
| Revision 9,999 | 120.745 [117.680–122.766] | 112.993 [111.658–114.430] | 209.333 | 158.127 |

Full recovery at 10,000 operations also improves: median time is 108.834 to
104.537 ms without compaction and 115.487 to 110.894 ms at a head snapshot.
Allocation falls from 156.750 to 131.147 MB and from 209.310 to 158.107 MB.
Eligible-cache following appends change little, since their full-history hashing
was already avoided. Small SQLite timing ranges overlap and some medians are
slower; no universal per-case timing improvement is claimed.

The existing direct snapshot-hash benchmark checks the broader map-size cost.
Each process sample averages 100 iterations; the table shows the median of five
such samples, not a latency percentile from 500 independent observations.

| Cells | Control ms/hash | Candidate ms/hash | Control B/hash | Candidate B/hash |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 0.026021 | 0.024032 | 21,896 | 19,336 |
| 1,000 | 0.214213 | 0.215343 | 245,549 | 243,040 |
| 10,000 | 2.118635 | 2.151969 | 2,018,092 | 2,015,479 |

The fixed buffer retains the simple write path while reducing the dominant
scratch allocation in repeated small historical hashes. The small large-map
timing cost above is explicit; representative native input-to-frame latency
remains a separate gate.

## Validation and retained artifacts

Final model, engine and SQLite race tests passed in 2.468 s, 32.964 s and 8.736 s.
`task verify` passed with pinned Go 1.25.13, Rust
`1.82.0-x86_64-pc-windows-gnu` and native GL tests enabled. This includes lint,
contract checks, Go tests, Rust test/fmt/Clippy and the Windows editor build.
The inherited ImGui compiler warning remains; Rust reports zero unit tests.
Unconfigured external-service gates do not become live-service evidence.

Raw final logs, paired measurements, fixture observations, binary/source hashes
and validation output are retained in ignored
`.artifacts/canonical-scratch-2026-09-20/`. The diagnostic profile is retained in
`.artifacts/sqlite-history-profile-2026-09-20/`. An initial native benchmark launch
failed before running because its MinGW DLL directory was absent from PATH; the
corrected runs above all completed. No benchmark failures were hidden or treated
as passes.

Allocation totals are cumulative Go allocations, not retained heap or native
memory. Long histories still incur complete SQL reads, decoding and validation;
larger/representative maps, resource qualification and PostgreSQL measurements
remain open. No game build or Content Tools gate is required under the current
approved integration scope.
