# Architecture

## Current application

AphelionDMM retains StrongDMM's desktop and rendering subsystems. The current
editor routes committed map edits through Aphelion's operation engine:

```text
Dear ImGui UI
    -> editor tools
    -> mutable dmmap.Dmm gesture/display state
    -> explicit tile operation -> local or network executor
    -> acknowledged snapshot -> staged, validated DMM/TGM save

Accepted history -> actor-scoped inverse operation for undo
Display refresh -> dmmsnap compatibility copy and render invalidation

Go application
    -> cgo/static library boundary
    -> vendored Rust sdmmparser
```

Important existing seams include:

- `main.go` and `internal/app` for application lifecycle.
- `internal/app/ui/cpwsarea/wsmap/pmap/editor` for map editing and commits.
- `internal/dmapi/dmmap` for the mutable map model.
- `internal/dmapi/dmmsnap` for snapshot-derived undo/redo.
- `internal/dmapi/dmmap/dmmdata` for DMM/TGM parse and write behavior.
- `third_party/sdmmparser` for the Rust parser boundary.

`Editor.CommitOperation` captures explicit before/after tile changes. Network
completion callbacks are scheduled on the UI thread and fenced by attachment
generation. An older acknowledgement does not clear a newer open gesture.
Selection preflight owns only the before-states it acquires. Failed batches
release those entries before any display mutation; cancelled moves restore and
release their backgrounds without committing unrelated edits. A new destination
with a separate pending edit is refused. Invalid capture remains a Save fault
until validated replacement; cleanup does not reset the attachment or its pending
acknowledgements. See the
[capture/cancellation verification](../verification/2026-09-06-selection-capture-and-cancellation.md).
`WsMap.Save` refuses unfinished gestures/submissions, captures the executor's
acknowledged state, and reports staging or replacement failures. Close dialogs
honor that result. The mutable display map is not the save authority.

`Editor.ResizeMap` stages local maintenance before changing authority/display.
Each size has a distinct local document identity and retained local executor, so
undo/redo can resume its edit history and stable IDs. Current/target boundary
hashes must match before installation. A separate history-generation key permits
that local resumption; asynchronous attachment generations never rewind. External
attach/detach/close replaces history ownership. Network resize remains unavailable
until the approved owner-only maintenance protocol is implemented. See the
[local resize verification](../verification/2026-09-06-local-resize-and-history.md)
for exact evidence and remaining performance/acceptance limits.

Each editor binds its command history to the map's existing stack ID and stack
lifetime at construction. A tab switch cannot redirect a delayed acknowledgement
or local resize into another map's history. Disposal invalidates the binding even
if the same path is reopened. Popped/discarded commands release captured state;
live history and in-flight callbacks still retain the state they need. See the
[history audit](../verification/2026-09-06-map-history-ownership-and-retention.md).
During pending undo/redo, new accepted edits queue their local history insertion.
Completion finishes the original transition once, then inserts the new branch
in order. Saved-state identity follows the latest applied command; pending history
is modified. Durable edits continue through the executor and Save/close retains
its independent authority checks. See the
[ordering/Save verification](../verification/2026-09-06-history-ordering-and-save.md).

## Target boundaries

```text
Desktop UI ---- local executor -----+
                                     |
Remote client -- WSS transport ------+--> authoritative operation engine
                                     |        -> validation and ordering
HTTP control/snapshot ---------------+        -> revisioned operation log
                                              -> snapshot store
                                              -> atomic DMM/TGM export

Map repositories ---- user-selected DME/DMM/TGM fixtures for native fidelity checks
```

New Aphelion-owned packages:

- `internal/aphelion/collab/model`: deterministic domain values and operations.
- `internal/aphelion/collab/engine`: validation, ordering, conflict decisions, and inverse operations.
- `internal/aphelion/collab/protocol`: HTTP/WebSocket wire envelopes and version negotiation.
- `internal/aphelion/collab/server`: sessions, presence, authorization, and transports.
- `internal/aphelion/collab/client`: desktop transport and reconciliation.
- `internal/aphelion/collab/store`: snapshots and operation-log persistence.
- `cmd/apheliondmm-collab`: the collaboration service executable.

## Dependency rules

- `model` depends only on the Go standard library.
- `engine` depends on `model`, not UI, networking, storage, or ImGui.
- `protocol` maps wire data to `model`; wire compatibility does not leak UI types.
- `server` owns operation ordering and publishes accepted revisions from the document loop after durable append. Stores revalidate retained history and incoming accepted records for consistency.
- `client` never mutates the map outside the same executor abstraction used by local mode.
- inherited UI code depends on narrow Aphelion interfaces; Aphelion packages do not depend on concrete ImGui widgets.
- integrations consume versioned contracts and immutable configuration, not shared database tables.

## Concurrency model

Unshared edits have a separate internal request contract behind the same engine.
`LocalRequest` carries an opaque owner/revision token and explicit tile deltas;
the shared changed-union validator stages identity moves and before-state checks
before publication. Engine coordinate/identity indexes are reused, with copy-on-write
ownership across branches. Ordinary local commits and local undo/redo update only
accepted display and compatibility tiles. They do not construct network envelopes,
retain network replay records, or eagerly hash unused whole-map state. Canonical
hashing remains exact at actual consumers. Session ownership is explicit, including
while disconnected; the availability of an in-process capability is not permission
to use it for a session. Legacy wire-shaped local producers remain valid during
migration and retain their base-hash and inverse checks at that boundary.

Each open document has one authoritative mutation loop. It serializes durable operations and owns the current revision. Network readers, presence updates, persistence, rendering, and telemetry may run concurrently, but they cannot mutate authoritative document state directly.

Before durable append, the server reserves wire headroom for acceptance, replay,
duplicate delivery and actor-scoped inverse metadata. Oversized optional rejection
details are omitted while retaining operation identity and authority. Recoverable
client suspension retains uncertain drafts without automatic resubmission; snapshot
fallback preserves them and matching accepted replay resolves them. See the
[delivery/recovery evidence](../verification/2026-09-20-delivery-limits-and-interrupted-drafts.md)
for exact size, persistence, replay and remaining desktop boundaries.

Terminal executor failures also retain uncertain drafts and freeze acknowledged
state. The desktop exposes bounded before/intended-value previews and complete
JSON draft export through staged atomic replacement. User-initiated leave and
project/application close require explicit draft resolution or discard. Export
does not resubmit or resolve an operation; see the
[draft recovery evidence](../verification/2026-09-20-draft-inspection-and-export.md).

Local capture faults and unsubmitted journals have a separate recovery view in
the map tools panel. Its immutable raw JSON reference preserves damaged display
entries without assigning identities. Inspection/export retains the Save guard;
explicit discard revalidates editor lifetime and contents before installing the
executor snapshot. It preserves accepted history and pending callback ownership.
Ordinary projection/refresh does not overwrite a faulted display. See the
[local recovery evidence](../verification/2026-09-20-local-edit-recovery.md).

Engine branches share private immutable snapshot and accepted-operation payloads.
Each branch owns its mutable history maps; validation copies the tile table before
replacing or appending states. Public inputs, outputs and changed tile states are
deep copies. Recovery preserves the same ownership boundary. This reduces
transaction-copy cost without making a single Document safe for concurrent
mutation or changing the server's append-before-publication requirement. See the
[engine-copy verification](../verification/2026-09-06-engine-copy-performance.md).

Private `SessionStore.LoadRecovery` returns the snapshot, retained operations,
revision hashes, and durable head. Recovery verifies them and restores inverse
targets and historical bases, including operations compacted out of public
reconnect replay. Public `Load` retains its snapshot-plus-suffix shape. This
retains full history and has a measurement-backed scaling investigation in the
2026-09-05 performance audit; no bounded-history optimization is implemented.

The UI thread remains the only owner of OpenGL/ImGui work. Applied operations produce immutable render invalidations that are scheduled onto the UI thread.

Clipboard and stamp placement retain ToolGrab's lifecycle but use an isolated
payload, pose and renderer presentation. Translation changes the anchor without
capturing tiles, building operations, interning prefabs, or rebuilding base chunks.
Copied IDs are assigned once per placement; orientation is a normalized rectangle
symmetry applied to the original source. The last complete presentation remains
visible while replacement appearances are prepared on the graphics owner thread.
The shared composer keeps sparse footprints and explicit channel intent. The
default preserves absent channels; Apply Over appends collections; explicit
Replace Including Blanks permits represented clears. Hidden singleton conflicts
are rejected rather than creating another turf/area or deleting hidden content.
Only confirmation composes and submits changes. A click received during source
preparation keeps its exact target. Cancellation discards presentation without
restoring tiles; submitted outcomes remain owned until resolution. Pure previews
allow committed saves, queries and incoming projections. Real captured gestures
and unresolved submissions retain their guards. Accepted network paste metadata
comes from coherent executor authority, including intervening revisions.
The [paste-transform report](../verification/2026-09-06-paste-transforms.md)
records actual shortcut, cancellation, fault and local/network history evidence,
plus selected-size model costs without claiming desktop latency improvements.
Ordinary Grab movement still uses captured backgrounds until its own migration;
cancellation and return-to-origin restore them. The following reports describe
that earlier mutating lifecycle, not the isolated clipboard/stamp presentation.
The [preview-copy audit](../verification/2026-09-06-preview-copy-performance.md)
records profile attribution, matched display hashes, allocation savings and mixed
timing results for ordinary Grab movement and floating rotation.
The [application routing and canvas audit](../verification/2026-09-06-canvas-resize-and-application-routing.md)
qualifies shortcut dispatch and workspace focus with a real hidden UI context.
It also removes the redundant CPU canvas-resize upload: texture allocation uses
nil data followed by the existing full framebuffer clear before drawing. Its
pixel-equivalence and synchronized blank-resize measurements do not replace
representative map, GPU-memory or human-interaction qualification.

Search queries use the owned `internal/aphelion/search` helper for a single map
traversal, preserving prefab grouping and tile/instance order. Results reference
current display instances and are not a persistent index. The inherited search
panel clips rows, computes focused-result scrolling before clipping, and stops
iteration when its result generation changes. Free releases result references;
filter changes reset navigation. Search's result generation remains local to the
panel. A separate `Editor.MapViewVersion` fences its cached instance pointers
across local edits, snapshot replacement, resize/history and attachment changes.
Queries rebuild once when invalidated and ready; unfinished gestures defer them.
Automatic same-map refresh retains filter bounds. Stale row/bulk actions refresh
without retargeting their old indices. `CanStartMapEdit` fences mutations, and
`CommitInstanceBatch` verifies membership and captures all targets before display
changes. It also validates the shared replacement through a temporary capture
before installation, rejecting an empty path or missing variable data. Failed
preflight releases only its captures; target-capture faults retain the map guard,
while an invalid selected replacement leaves the unchanged map usable. Later
snapshot-read failures retain valid entered intent for explicit retry. See the
[after-value qualification](../verification/2026-09-06-search-after-values-and-stopping-point.md).
Accepted operations and actor-scoped history keep their existing owners. See the
[ownership/capture qualification](../verification/2026-09-06-search-ownership.md)
and the earlier
[search verification](../verification/2026-09-06-search-performance.md) for exact
query/draw equivalence, allocation tradeoffs and remaining action-lifecycle gates.

## Deployment modes

The shared service package supports these entry points:

- embedded loopback mode in the desktop;
- `cmd/apheliondmm-collab` for local and explicitly enabled LAN service;
- `cmd/apheliondmm-hosted` behind TLS, OIDC, PostgreSQL, backups, and operational monitoring.

Loopback is the default. A deployment mode may strengthen authentication and persistence, but it may not change operation semantics.
