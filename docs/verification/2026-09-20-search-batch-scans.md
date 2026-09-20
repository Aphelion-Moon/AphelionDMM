# Search batch membership and replacement scans

Baseline: `6f3a7dbf`. The dense-tile Search replacement path scanned the same
tile once per selected instance to validate membership, then again per instance
inside `InstanceReplace`. The retained CPU profile for 4,096 matching objects
attributes 48.17% flat sampled time to `InstanceReplace`, 17.63% to
`CommitInstanceBatch`, and 3.87% to numeric instance-ID reads. This is a sampled
component profile, not desktop input-to-visible attribution.

## Change and behavioral evidence

`internal/aphelion/search.CurrentTiles` validates exact current instance pointers
at their coordinates, visiting each affected tile once for batch membership.
It returns unique tiles in first-target order, with a direct scan for a single
row action. It is not a persistent cache: the map must remain on its owning
thread between validation, capture and mutation.

`CommitInstanceBatch` captures every affected tile before any display mutation,
validates the replacement as before, and directly replaces validated targets
using the existing same-base-type rule. Duplicate targets retain the same outcome;
instance ordering and stable IDs are untouched. The existing operation engine,
submission, rejection, draft retention and actor-scoped history remain in use.
Deletion still uses the inherited per-instance deletion/regeneration path; this
pass makes no deletion-speed claim or change to base-instance regeneration.

New membership tests cover duplicate/interleaved targets, copied pointers with
matching identities, wrong-coordinate membership, nil and out-of-bounds targets,
without mutating the map. A mixed two-tile replacement regression passed before
and after the optimization and verifies compatible replacements, incompatible
base preservation, stable ordering/IDs and exact undo/redo. Existing stale-result,
capture failure, invalid replacement, failed-submission retention, network
acceptance/rejection and native Search entry-point tests also pass.

## Matched component measurements

Five alternating control/candidate pairs, reversing first variant on even pairs,
used separately compiled binaries and the unchanged
`BenchmarkInstanceBatchUnchangedDenseTile`. Each benchmark has its own one-call
calibration/warmup followed by 20 measured calls on a fresh fixture. The test
asserts unchanged display, authority hash, revision zero, no undo entry, no fault,
and zero executor snapshot reads during measurement. Forty measured rows passed;
zero workload failures/timeouts. All 80 warmup/measured fixture hashes matched
between variants and trials at each size.

Fixture: one tile containing base area/turf and N matching objects, fixed
collaboration IDs. The benchmark covers unchanged replacement and the real
no-op commit path, excluding changed-operation submission, frame queue, rendering,
GPU work, transport and durability. Tracing is off. These are warm component
results, not general Search interaction latency or an end-to-end speedup.

Windows Server 2022 build 20348; Ryzen 7 9800X3D, Go 1.25.13, GCC 15.2.0 UCRT.
Medians and full observed ranges across five trials:

| Objects | Control ms/op (range) | Candidate ms/op (range) | Control B/op | Candidate B/op | Allocs/op control → candidate |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 32 | 0.014335 (0.012765–0.014655) | 0.013565 (0.012265–0.014275) | 25,560 | 26,760 | 144 → 148 |
| 256 | 0.175375 (0.174920–0.177550) | 0.097035 (0.093340–0.098575) | 195,284 | 204,804 | 1,040 → 1,044 |
| 2,048 | 4.345640 (4.341585–4.416135) | 0.605515 (0.594135–0.637170) | 1,558,242 | 1,632,127 | 8,208 → 8,218 |
| 4,096 | 15.502940 (15.178270–15.548840) | 1.111625 (1.073810–1.144270) | 3,097,840 | 3,245,629 | 16,400 → 16,418 |

At 4,096 objects this is about 92.8% less component time, at the cost of 147,789
additional temporary Go bytes per call (4.8%). The smallest case overlaps in time
and establishes no reliable speedup. Sparse maps, single-row timing and native
memory were not measured; the transient target index is a deliberate tradeoff.
Do not compare these absolute timings to the September 6 host's measurements.

Canonical fixture SHA256 values (N matching objects):

- 32: `1e80e7e8716dbe48c29b7ad2f2af1e58f0131bd4d02fb5d0b508245c4d7e3dd9`
- 256: `91300c4317059bb5e626f185702494f07989cdf9bc492764d2de7c8a71bae288`
- 2,048: `e1fe1290ec86cbc3b85d87106dcad483828367b7b8c55bb06e42a67dfadf7022`
- 4,096: `fd30f6438002b7557a4d604b87fdbfecbaf48c121e491778dce0d3f00da08a06`

Binary SHA256:

- Control: `701341EDE69EA3AEA543415C9B956B794444006FAA64A737D94BA2B4F06E70D3`
- Candidate: `319093E5216101B4FDB3CE2D67A93899AF3FC3F012524B20A572F28E88196F24`

## Verification and remaining work

Focused membership, editor batch and real native Search tests pass, including
race runs of the affected packages. `task verify` passes with
`APHELIONDMM_GL_TEST=1`: lint/contracts, Go tests, Rust checks, parser and Windows
editor builds. Rust contains zero unit tests; the inherited ImGui C++ warning
remains. Unconfigured external-service gates may skip. No interactive smoke or
human acceptance was performed in this pass.

Ignored `.artifacts/search-batch-2026-09-20/` retains both binaries, CPU profile,
raw trial/test/gate logs, sample/summary CSVs, binary/source/fixture hashes and the
summary script. Broader changed-operation, sparse/multilevel, frame latency and
lifecycle campaigns remain open. No dependencies or protected infrastructure
changed; the user's staged server inventory remains untouched.