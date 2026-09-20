# ADMM performance follow-up workplan

September 20: [unsent-draft retention and toolchain evidence](../../verification/2026-09-20-unsent-drafts-and-toolchain.md)
closes reproduced pre-submission intent loss. Large-action, desktop, hosting and
performance qualification remain open; the stopping-point text below is historical.

Execution stopped at the user's request after the
[replacement-validation repair and final qualification](../../verification/2026-09-06-search-after-values-and-stopping-point.md).
Resume from that handoff; unchecked items below remain open. Changes remain
uncommitted.

The [September 5 measurements](../../verification/2026-09-05-repo-performance-audit.md)
rank the component costs below. This plan is the output of the performance audit;
the remaining work is tracked below. Bounded streaming hashing, indexed visible
projection and the first editor-tool improvements are tracked in the
[September 6 QoL follow-up](2026-09-06-editor-qol-workplan.md). Remaining unchecked
items are open. Establish representative workload
and desktop/service evidence before accepting a production performance claim.
Preserve the correctness repairs, pinned toolchains, source ownership markers,
unrelated dirty work, and the protected-infrastructure approval boundary.

## 1. Close the measurement gaps

Re-inspection confirmed that the original pilot offers operations serially,
publishes presence only afterward, and measures all-client observation under
acknowledgement labels. It remains an explicit compatibility scenario. The new
`apheliondmm-loadtest -concurrent` mode uses independent intent schedules and
applies every accepted event to every client. The pilot reader cancellation
defect is also repaired. See the
[concurrent-load report](../../verification/2026-09-06-concurrent-load-measurement.md)
for the precise measurement boundaries and remaining campaign gates.

- [x] Extend the owned load harness in `internal/aphelion/collab/load` with tests
  that fail for serial offering, sequential presence, lost reader cancellation,
  missing client application, and incorrect latency attribution. Retain the
  existing pilot explicitly as a compatibility scenario.
- [x] Generate deterministic independent concurrent intents, rather than sending
  the current revision-dependent operation sequence concurrently. Include
  deliberate same-tile conflicts with exact expected accepted/rejected counts.
  Apply every received accepted event to each client's projection and verify
  revision/hash equality. A stalled or corrupt client currently fails the whole
  run with partial counts; it cannot produce a capacity pass.
- [x] Track scheduled, sent, accepted, rejected and applied counts, achieved
  rate and backlog. Report schedule-to-ack, send-to-ack and all-client-application
  separately; acknowledgement durability depends on the configured store. Send
  presence concurrently and count local coalescing and unique unobserved
  deliveries at a bounded cutoff. Unobserved presence is not a network-loss metric.
- [ ] Qualify disconnect/recovery under a slow consumer: every client must
  converge or be explicitly disconnected and recovered, with every offered
  operation accounted for. The current controlled stall test establishes a
  failed result; it does not establish successful reconnect or recovery.
- [ ] Inventory approved local DME/DMM/TGM fixtures with hashes, cell/level/type,
  prefab and variable counts. Add typical and large maps to the current synthetic
  100/1,000/10,000-cell matrix. Keep fixtures outside published artifacts when
  they contain private content; record repository-relative identifiers.
- [ ] Add UI-thread stage timings around gesture capture, operation dispatch,
  projection application, `refreshCollaborationView`, bucket rebuild and next
  displayed frame. Instrument native parser transfer, icon decoding and upload
  separately. Use a maintained owned diagnostic wrapper; any necessary protected
  entry-point change must first receive exact-file approval.
- [ ] Run a fresh-process/warm-cache matrix for startup, DME reload, first/cached
  icons, DMM/TGM open/save, drag/fill/paste, undo/redo, search, pan/zoom, level
  switch, resize, multiple tabs, and idle/minimized states. Capture CPU, Go heap,
  native/private bytes, GPU memory/timers and input-to-visible percentiles.
- [ ] Run a 30-minute repeated open/edit/save/reconnect/close lifecycle workload
  and compare post-GC Go heap, native process memory, goroutines, handles and GPU
  resource counts at stable checkpoints. Capture long-history restart, crash,
  database loss and slow-consumer scenarios without disabling durability.

**Gate:** fixed manifests, correct counts/hashes, independent warmup, at least five
trials, retained raw results, explicit failure/timeout counts and no hidden
closed-loop reduction of offered demand. Desktop/GPU, service, database and
developer-tool results remain separate. These experiments remain open; this
audit's component results do not substitute for them.

## 2. Reduce engine copy and canonical-hash allocation

**Current result:** the
[engine-copy comparison](../../verification/2026-09-06-engine-copy-performance.md)
uses separately preserved binaries after the streaming-hash repair. At 10,000
synthetic cells and one changed tile, five alternating trials reduce median
clone/apply from 18.169 to 8.276 ms, 11,324,840 to 3,161,512 B/op, and 60,197 to
196 allocations/op. All eight fixture hashes/revisions match; all 24 measured
Clone/Validate/CloneApply cases improve in median time and allocation. These are
component measurements, with full observed ranges retained in the report.

**Files:** `collab/engine/document.go`, `model/clone.go`, `model/hash.go`, and
`server/document.go`, all under `internal/aphelion/`.

The engine now shares immutable private snapshot and accepted-operation payloads
between branches. Each branch owns its mutable history maps, validation owns its
tile table, and public ingress/egress and changed states remain deep copies.
Durable publication is unchanged. A no-history clone allocates 496 B at every
tested map size, but 1,000 retained operations still require 285,504 B per clone.
The initial pre-streaming-hash baseline is retained in the September 5 audit;
do not compare its timing directly with this candidate.

- [ ] Complete granular indexing, canonical encoding and long-history map-copy
  attribution. Separate current Clone/Validate/CloneApply CPU/allocation profiles
  at 10,000 cells are retained. In candidate CloneApply, Snapshot.Validate is
  47.21% of flat sampled allocation and SHA-256's AVX2 block routine is 31.55% of
  sampled CPU. Profile totals include setup and are not per-operation costs.
- [x] First prototype sharing immutable unchanged tile states or avoiding a
  redundant transaction copy. Keep accepted and caller-owned snapshots isolated,
  and do not publish any state before append succeeds. Do not replace the wire
  hash algorithm or change canonical bytes to obtain a benchmark gain.
- [x] Prototype streaming the existing canonical bytes into SHA-256 with bounded,
  invocation-local scratch space; test the golden hash, buffered compatibility,
  long values and buffer boundaries. This reduces encoding allocation without
  shared buffer ownership. Broader latency and representative-map gates below
  remain independent.
- [ ] If further encoding buffer reuse is justified, prove lifetime isolation, bounded
  capacity and concurrent safety. Streaming canonical bytes into the same hash
  is a candidate only if it reduces measured allocation without regressions.
- [x] Add mutation-isolation and failed-append tests before behavioral changes.
  Re-run engine/hash conformance, inverse/recovery/corruption tests, race checks,
  and actual server acceptance with the same revision/hash outcomes.
- [x] Compare interleaved control/candidate trials at 100/1,000/10,000 cells,
  multiple Z levels, 1/large tile batches and 0/100/1,000 retained operations.
  Accept only a repeatable allocation/time improvement with unchanged work and
  no material regression in another fixture; set a user-facing budget only after
  the desktop/service timings from step 1 exist.
- [ ] Extend beyond the current eight synthetic combinations: heterogeneous
  prefab/variable density, batches crossing Z levels, 4,096 changed cells,
  10,000 retained operations, real maps and retained-branch memory. Establish
  service/database and input-to-visible costs before setting a product budget.

## 3. Bound repeated speculative projection work

**Current implementation:** `Projection.Visible` now owns one deep map copy and
reuses per-call coordinate and stable-ID indexes across pending operations.
Whole-operation checks happen before mutation; submitted requests remain intact.
The old clone/hash application path remains the compatibility fallback for
malformed baselines and the differential-test oracle. Indexed projection passes
640 fixed-seed prefix comparisons, relocation/collision/failure-isolation tests,
and a pending-edit allocation-growth gate. Submit/Accept, render refresh and
end-to-end projection scheduling still need separate profiling.
The [selection/projection report](../../verification/2026-09-06-selection-lifecycle-and-projection.md)
records five paired trials: eight-pending projection on 1,000 synthetic cells
fell from 9.763 to 0.797 ms/op and from 6,520,192 to 899,728 B/op. This control
already contains streaming hashing; the initial measurements below predate both
changes. Desktop latency and 32-pending/conflicting workload measurements remain
open.

**Priority:** second measured target. At 1,000 cells, projecting eight compatible
pending edits allocated 8.58 MB and took a median 18.393 ms, versus 0.691 ms for no
pending edits. This is component work, not a measured full frame duration.

**Files:** `collab/client/reconcile.go`, `executor.go`; narrow owned editor adapter
spans in `internal/app/ui/cpwsarea/wsmap/pmap/editor/collaboration.go`.

- [ ] Attribute cloning, indexing, validation and UI reprojection separately.
- [x] Replace per-pending full-map copies in `Visible` with one owned snapshot
  and per-call indexes. Check each whole batch before applying it; retain the
  legacy malformed-baseline fallback and independent differential oracle.
- [ ] Prototype cached immutable visible state or change-coordinate projection
  only after proving invalidation on acceptance, rejection, attachment changes,
  reconnect and conflict rebuilding. Keep submitted operation headers immutable.
- [ ] Test two pending edits where one conflicts, dependent pending edits, a
  rejection during a newer open gesture, queued completions after detach/close,
  delayed acknowledgements and undo/redo. Preserve inspectable failed intent.
- [ ] Benchmark 0/1/8/32 pending edits on compatible and conflicting workloads,
  then measure network-to-visible and input-to-visible distributions in the
  actual desktop. A faster projection that hides lost work fails acceptance.

## 4. Avoid replaying all retained history for each durable append

**Priority:** allocation evidence and a strong source hypothesis; elapsed-time
benefit is not established. At 100 cells, compacted history of 100 operations
raised measured append allocation from about 0.40 MB / 2,049 allocations to
1.56 MB / 14,400 allocations. Short SQLite timing ranges overlap substantially.

**Files:** `collab/store/sqlite/store.go`, `recovery.go`; PostgreSQL equivalents;
`collab/engine/recovery.go`; shared conformance fixtures.

- [ ] Profile SQL read/decode, prefix validation, suffix replay, transaction wait,
  hash construction, write and fsync separately for both databases.
- [ ] Investigate a versioned in-memory validated recovery cache with strict
  invalidation, or a private persisted recovery checkpoint retaining required
  hashes/inverse targets. No cache may assume exclusive ownership across store
  instances or trust a partially committed append.
- [ ] Preserve historical valid bases, actor-scoped inverses, already-inverted
  status, duplicate identity and unknown content. Do not truncate history or
  weaken recovery verification as a performance change without a retention and
  compatibility design. A schema/migration proposal needs independent review.
- [ ] Re-run concurrent store, cancellation, connection termination, committed-
  then-error, snapshot race, corruption, restart and logical backup/restore gates.
- [ ] Measure at fixed 0/100/1,000/10,000 history lengths using repeated longer
  trials on recorded storage, including full durability. Compare recovery and
  append costs; moving unbounded work from one to the other is not sufficient.

## 5. Investigate the remaining desktop and integration hypotheses

Keep measured component results distinct from remaining user-visible hypotheses:

| Investigation | Concrete targets | Required decision evidence |
| --- | --- | --- |
| Full refresh for small edits | Editor `refreshCollaborationView`, `dmmsnap.Sync`, zones, prefab persistence and bucket updates | Stage timings and input-to-visible impact; incremental invalidations must preserve active gestures and level changes. |
| Cancel/no-op release | Editor `commitOperation` now compares captured tiles before reading authority; one-tile unchanged commits make zero executor snapshot calls | Five alternating synthetic trials establish the avoided full-copy cost at 100/1,000/10,000 map cells; measure larger selections, cancellation/restoration/render work and total frame latency separately. |
| Export canonicalization | `collab/mapadapter/export.go`, DMM/TGM writer | 10,000-cell export measured 23.760 ms median. Measure actual staged Save, dictionary diversity and unknown content before changing the writer. |
| Idle frames and resize upload | `window/process.go`, canvas texture creation, brush stream upload | Redundant CPU resize upload removed and blank-canvas resize measured in the follow-up below. Continue idle/minimized CPU, real-map frame pacing, draw counts and GPU timing. Respect animation and ImGui/input requirements. |
| Parser/FFI and icon lifetime | Go/Rust parser boundary, `dmenv.New`, icon cache/free and texture queue | Fresh/warm large environments, native copy/JSON attribution and repeated reload resource counts. No new FFI contract without measurements. |
| Search/tree/large selection | `cpsearch`, `cpenvironment`, `cpprefabs`, map tools and chunk layer rebuilds | Search's repeated variant scans and unbounded row construction are repaired and measured below. Continue sparse queries, real fonts/scales, current-map result ownership, tree/prefab panels and selection size sweeps; account for existing clipping, caching and culling. |
| Auth/telemetry/update/integration | OIDC registry, OTLP, bounded MCP adapter, staged artifacts, `internal/req` and updater | Latency and peak-memory distributions under local fixtures and approved external endpoints; no real executable replacement or production traffic for benchmarking. |

The [nudge/network pass](../../verification/2026-09-06-selection-nudges-and-network-transitions.md)
also removes retained before-states for restored passed-over drag tiles. Its
regression proves journal entries are bounded by source/current destination,
including revisits; it supplies no new desktop or component timing claim.

The [selection-outcome/no-op report](../../verification/2026-09-06-selection-outcomes-and-noop-performance.md)
records the next component measurement. For one unchanged captured tile on a
10,000-cell synthetic map, median commit-path allocation falls from 6,085,044 to
1,712 B/op and median time from 4.140 ms to 0.001359 ms across five alternating
trials. The control executes the removed snapshot read before the same no-op
comparison; it is not a separately built historical checkout. Every sample ends
at the same revision/hash with no history entry. Whole-gesture, desktop-frame and
representative-map gains remain unmeasured. A transient snapshot failure during
a real change still retains its capture and Save guard for a successful retry.

The [mirror/authority follow-up](../../verification/2026-09-06-selection-mirrors-and-authority.md)
closes two correctness gaps found while extending selection tools: failed tile
capture cannot fall back to legacy history, and a no-op transform cannot mask an
earlier selection undo. Mirror planning visits only selected cells/instances and
reuses transformed shared prefabs within the call. These are source and regression
results; this pass adds no timing, allocation, frame-rate or capacity claim.
Keep selected-area scaling and complete input-to-visible timings in the desktop
matrix above. Resize/reinitialization failure remains a separate audit target.

The [local resize repair](../../verification/2026-09-06-local-resize-and-history.md)
now closes that failure target and preserves pre/post-resize local history.
Same-size requests return before snapshot work; a fresh regression reports zero
allocations at 100/1,000/10,000 cells and no state/history change. This is not a
changed-size latency measurement. Changed-size maintenance retains local executor
checkpoints for correct undo and validates current/target hashes before switching.
Profile snapshot/hash duplication, candidate construction and retained checkpoint
memory across 0/10/100 resizes. The
[history ownership/retention audit](../../verification/2026-09-06-map-history-ownership-and-retention.md)
reproduced discarded closures retained by `redo[:0]`, popped entries and Balance's
append into unused undo capacity. The repair clears those references and releases
disposed stacks; synchronous/asynchronous regressions collect four discarded
8 MiB payloads while live undo/redo remains usable. Measure real checkpoint and
process-memory release after branching and close separately. This reachability
result is not a measured desktop memory or latency improvement.

The [history ordering/Save follow-up](../../verification/2026-09-06-history-ordering-and-save.md)
queues only local history insertion while undo/redo is pending, preserving normal
edit submission and accepted state. It repairs stale/lost inverse entries and
false clean saved-depth replacement; queued references are cleared after flush
or disposal. Include delayed/disconnected acknowledgements with continuing edits
in the long-history memory matrix. No frame-time, service-capacity or process-
memory improvement is claimed from these correctness regressions.

## Search follow-up and remaining acceptance

The [search audit](../../verification/2026-09-06-search-performance.md) preserves
query order and actual ImGui draw geometry across five alternating pairs. At
10,000 matching instances and 128 variants, median query time falls from 7.363
to 0.609 ms. At 5,000 results, table preparation falls from 31.373 to 0.129 ms and
median Go allocation from 6,283,706 to 20,312 B/frame. These are component results
on synthetic fixtures, excluding GPU rasterization and full desktop frames.
Multi-variant query allocation increases about 83-93 KB/query, and one-variant
timing has no reliable improvement. The existing F3/Shift+F3 route passes through
a real hidden workspace without changing map authority. Filter-index resets and
result-reference release have red/green regressions.

- [x] In `internal/aphelion/search/instances.go`, collect matching prefab groups
  in one traversal while preserving exact path/group/tile/instance order. Compare
  against independent scans at seed 20260906 and ordered stable-ID hashes.
- [x] Clip actual `cpsearch` table rows and preserve distant-result scrolling;
  compare draw buffers/commands and final-row scroll extents in matched binaries.
- [x] Reset filtered navigation and release result references on Free; run owned,
  UI and actual-workspace tests, affected race, `task verify` and produced smoke.
- [x] Reproduce stale search ownership after snapshot replacement, shrink/undo,
  remote edits and switch/close. `Editor.MapViewVersion` now invalidates cached
  pointers; Search rebuilds once when ready and refuses stale row indices.
  Automatic same-map refresh preserves filter bounds; unfinished gestures defer
  queries/actions. An unchanged 10,000-cell view reuses results with zero
  freshness-check allocation. The
  [ownership report](../../verification/2026-09-06-search-ownership.md) records
  red/green failures, 47 passing hidden workspace tests, affected race and full
  repository/build/smoke gates. Matched query/draw output and median allocation
  counts remain unchanged; overlapping timing ranges establish no new speedup.
- [x] Route search mutations through `Editor.CommitInstanceBatch`: check current
  instance membership and capture all targets before mutation. Existing and new
  capture faults leave the display unchanged, release unused before-states and
  retain the fault guard. Row/bulk local undo/redo and controlled network
  acceptance/rejection/undo preserve exact hashes. This does not close arbitrary
  batch-size, after-value or persistence-failure qualification below.
- [ ] Qualify `doDeleteAll`/`doReplaceAll` with absent/changed results, hidden
  instances, pending authority, rejected operations, map edges and protocol-size
  limits. Validate accepted count/hash and actor-scoped undo; do not silently
  truncate or split a logical action without defined history/UI behavior.
  Extend the current small-fixture/capture evidence to invalid replacement
  after-values, failures after successful capture and oversized wire payloads.
  Profile membership preflight, capture and real input-to-visible latency with
  many instances per tile and rapid accepted edits while Search is visible.
- [x] Reject uncapturable selected replacements and empty paths before display
  mutation. Six row/bulk red/green cases preserve a usable map and corrected
  retry; unknown content remains accepted. A later snapshot-read failure still
  retains valid intent for explicit retry. The stopping-point report above
  records affected race, 48 hidden workspace tests, full build and fresh smoke.
- [x] Establish a dense-tile no-op batch baseline at 32/256/2,048/4,096 matching
  instances with five alternating pairs and exact display/authority checks.
  The 4,096-instance candidate median is 54.970 ms before rendering or changed-
  operation submission. Replacement validation adds five allocations; overlapping
  timing ranges establish no speedup. Profile/group repeated tile scans next.
- [ ] Qualify the actual 4,096-tile engine bound and 1 MiB protocol message bound
  (including lower configured hosted limits). Preserve one-action history and
  inspectable failed intent; batching semantics are not changed by this repair.
- [ ] Extend `BenchmarkSearchPath` to no-match/sparse results, variable instance
  density and real multi-level map manifests. Attribute logging, group storage
  and result lifetime separately before reducing the new allocation overhead.
  Preserve weak-reference release and exact ordered results in every candidate.
- [ ] Exercise row buttons that refresh the list during clipping, scaled fonts,
  filters at the scrolled end and manual focus/keyboard interaction. Measure
  native allocations, process memory and input-to-visible time independently of
  Go B/op. Retain control/candidate binary hashes and raw alternating trials.

## Final comparison and qualification

The [canvas resize pass](../../verification/2026-09-06-canvas-resize-and-application-routing.md)
removes the CPU pixel buffer uploaded immediately before framebuffer Clear.
Five alternating trials per size preserve final pixel hashes. Synchronized blank
resize medians improve from 5.624 to 2.005 ms at 640x480 and 22.413 to 4.699 ms at
1920x1080; candidate trials report zero B/op versus 1.23/8.30 MB control medians.
These are forced-resize component results on one GPU, not general desktop FPS or
GPU-memory gains. Continue realistic sprite/map sizes, UI resizing, invalid or
minimized dimensions and resource lifetime measurements.

The [paste placement audit](../../verification/2026-09-06-paste-placement.md)
adds a shared preview lifecycle with destination-only capture ownership. Repeated
unchanged-position model previews allocate zero times in the focused check and
do not recapture/regenerate. A sparse two-cell traversal retains exactly two
backgrounds after each successful preview. Measure clipboard/template creation,
destination capture, render invalidation and input-to-visible latency separately
at 1/100/4096 cells, including sparse bounding rectangles, invalid targets and
cancel/retry cycles. These checks do not establish a desktop speedup.

The [floating-transform pass](../../verification/2026-09-06-paste-transforms.md)
now measures model-only rotation at 1/100/4096 selected cells. Five 40-iteration
samples give medians of 0.007/0.137/11.242 ms and 2,002/35,650/1,509,251 B/op.
The 4096-cell range is 7.960-14.426 ms before real capture conversion, render
bucket updates or GPU work. Profile candidate instances, background copies,
deterministic sorting, capture conversion and invalidation separately before
choosing the next optimization. Preserve exact cancellation and stable IDs.
Two-cell sparse templates at extents 16 and 256 both allocate 2,626 B/op; source
visits sparse cells and sorts them, without scanning their bounding rectangle.
Timing has a large outlier in the wider fixture, so equal allocation is the
stronger scaling evidence. These are new-feature baselines, not speedup claims.

The [preview-copy repair](../../verification/2026-09-06-preview-copy-performance.md)
profiles the current code and removes deep copies of still-owned destination
contents that were immediately discarded. Cancellation/return-to-origin and
passed-over restoration remain exact. Eight matched workloads, five alternating
pairs each, preserve canonical display hashes. At 4096 cells, rotation median
allocation falls from 1,509,301 to 1,214,337 B/op; ordinary movement with hidden
objects falls from 1,350,476 to 746,827 B/op. Current matched timing medians are
7.198 -> 6.455 ms and 5.131 -> 3.834 ms, with overlapping ranges. Small cases have
mixed timing outcomes. Do not compare these timings to the earlier feature
baseline or call them general desktop speedups. Next attribute sorting,
candidate instances, capture conversion and actual bucket/GPU refresh cost.

The [capture/cancellation audit](../../verification/2026-09-06-selection-capture-and-cancellation.md)
releases unused captures/backgrounds after failed selection preflight and on
cancellation. It also prevents cancellation from committing an unrelated edit
and prevents a new destination from acquiring another edit's journal entry.
Regression evidence proves ownership and exact recovery, not a memory or latency
gain. Include repeated failed destinations, retries, cancellation and concurrent
pending input in the selected-area and lifecycle measurements. Acquisition
bookkeeping scales with new captured cells and should be included in stage costs.


- [ ] Keep control/candidate binary and fixture hashes, settings, raw samples and
  cold/warm labels. Randomize/interleave comparison order and investigate the
  large laptop variation observed in the initial baseline.
- [ ] Run the maintained `task verify`, affected race and real database gates,
  plus shipped-entry-point smoke and full desktop/service scenarios.
- [ ] Publish both positive and negative results, measured limits, and remaining
  unrun domains. Obtain named-human desktop acceptance and separate hosted CI /
  deployment evidence. Leave Git operations and protected infrastructure under
  their existing explicit-authorization rules.
