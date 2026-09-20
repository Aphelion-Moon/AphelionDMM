# Editor tools, shortcuts, and performance follow-up

Baseline: commit `052e1acb02b790641d63466de40c91d56028383b`.

September 20: [popup and dialog shortcut ownership](../../verification/2026-09-20-shortcut-popup-focus.md)
now blocks background edits, including queued modals and popup-dismissal frames,
while keeping contextual commands available. Native and ImGui focus/repeat
regressions pass; OS layout and human interaction acceptance remain open.

September 20: [editable shortcut bindings](../../verification/2026-09-20-shortcut-rebinding.md)
now support saved alternatives, conflict acknowledgment and default resets.
The [manufacturer graphics driver](../../verification/2026-09-20-graphics-driver.md)
is installed; native workspace checks and `task verify` with OpenGL gates enabled
pass. Human acceptance and broader layout/focus qualification remain open.

Earlier September 20: the [shortcut action catalog and shared-binding diagnostics](../../verification/2026-09-20-shortcut-catalog.md)
established the live-registry foundation used by the rebinding continuation above.

September 20: [configurable selection grid steps](../../verification/2026-09-20-selection-grid-step.md)
now share a saved preference across Alt+Arrow and toolbar movement. Tool,
preference and real-workspace regressions pass; human acceptance remains open.

Latest continuation: [draft inspection and export](../../verification/2026-09-20-draft-inspection-and-export.md)
records terminal-error retention, complete local draft export and guarded desktop
close, following the [delivery/recovery work](../../verification/2026-09-20-delivery-limits-and-interrupted-drafts.md).
Desktop acceptance and wider unchecked work remain open.

September 20 continuation from committed `f243e913`: local network submission
failures now retain drafts, with actual Search and WebSocket regressions. See
[current evidence and blockers](../../verification/2026-09-20-unsent-drafts-and-toolchain.md).
OpenGL initialization was initially blocked on this host and is now resolved by
the driver installation above. Broader unchecked work remains open. The
stopping-point text below describes September 6.

Execution stopped at the user's request after the
[final replacement-validation qualification](../../verification/2026-09-06-search-after-values-and-stopping-point.md).
The current build includes selection transforms and the shortcut reference;
human acceptance and unchecked work below remain open. Changes are uncommitted.

Inspect the current source before each stage; earlier plans and passing tests are
not evidence that an implementation is correct. This extends the
[repo-wide performance workplan](2026-09-06-admm-performance-followup.md).

## Current bounded implementation

The [paste placement pass](../../verification/2026-09-06-paste-placement.md)
adds movable, cancellable Ctrl/Cmd+V placement, click/Enter confirmation, toolbar
controls and reference guidance. Hidden IDs, sparse holes and full map bounds
are preserved. Full hidden-workspace race and repository gates pass. The
[application routing follow-up](../../verification/2026-09-06-canvas-resize-and-application-routing.md)
now qualifies the actual Ctrl+V dispatcher and rendered workspace focus changes.
Human interaction and broader fault cases remain in the
[placement plan](2026-09-06-paste-preview.md).

- [x] Add clockwise/counterclockwise selection rotation through a narrow Grab
  adapter and the shared operation engine. Use current map contents, retain
  stable IDs, preserve hidden instances and opaque unrelated variables, capture
  every before-state before mutation, and commit one actor-scoped operation.
- [x] Keep the bottom-left selection anchor fixed when width/height exchange.
  Replace visible destination contents, regenerate vacated base turf/area, and
  reject an out-of-map rotation without clipping. Toolbar actions and `[` / `]`
  share the same path. Refuse rotation during an unfinished gesture.
- [x] Refuse rotation of a selection on a non-visible Z level, and refresh
  grabbed contents when a new drag starts after undo or a remote update.
- [x] Add horizontal/vertical mirrors on `H` / `V` and matching toolbar actions.
  Keep the selection rectangle fixed, retain hidden objects at their coordinates,
  preserve relative order within visible/hidden groups and stable IDs, reflect
  direction bits and finite offsets on the affected axis. Use the same operation,
  outcome and undo adapters as rotation. Exact modifiers protect Ctrl/Cmd+V;
  active text input and non-visible selections do not invoke these bindings.
- [x] Report no-op transforms to selection history so an unchanged mirror cannot
  keep stale bounds when an earlier nudge is undone. Reproduced through the real
  workspace; the regression checks both canonical map hash and selection bounds.
- [x] Remove the tile-edit fallback to legacy snapshot history after authority
  initialization/capture failure or a missing executor. Report the fault, retain
  the Save guard, and clear it only on a validated replacement attachment.
  The subsequent local resize repair replaces its distinct legacy path below.
- [x] Rotate numeric/named cardinal and diagonal `dir`, plus finite numeric
  `pixel_x/y` and `step_x/y`. Resolve inherited variables from the loaded
  environment. Reject unsupported orientation expressions as one whole action;
  preserve unknown types and other variables. Custom sprite transforms, type
  swaps, cable/pipe connectivity variables, and environment-specific rotations
  need explicit domain adapters and are not inferred from their names.
- [x] Add permanent Pick/Delete/Replace selection on `5` / `6` / `7` (including
  keypad equivalents), `Esc` deselection, and retain held S/D/R behaviors.
- [x] Fix temporary-tool handling of Ctrl/Cmd shortcuts and active text inputs.
  A reproduced Ctrl+S previously changed Grab to Pick and discarded selection.
  Match ordinary shortcut modifiers exactly so a longer/unknown modified chord
  cannot fall through to an unrelated bare-key action. Keep Shift+= zoom-in.
- [x] Add Help > Keyboard Shortcuts and F1, searchable by action/key/context.
  Derive registered bindings and alternatives from the actual registry, dedupe
  multiple map panes, and explain held/mouse behaviors. Map-specific entries
  appear when a map is open; visibility and enabled state still govern execution.
- [x] Prototype bounded streaming canonical hashing without changing the v1
  bytes, validation, sorting, or SHA-256 algorithm. Retain a buffered compatibility
  oracle and test buffer-boundary, long-string, Unicode, and opaque-value cases.
- [x] Complete and record repository, hidden-workspace, race, and paired
  component qualification in the
  [verification report](../../verification/2026-09-06-editor-qol-and-hash-performance.md).
  Representative desktop/service latency and human acceptance remain open.

## Next tool work, in dependency order

The subsequent selection/projection pass replaces the inherited drag mutation
path with an owned, cancellable per-gesture preview. It preserves moved and
passed-over IDs, prevents restoration of a previous gesture's background, blocks
resize during an edit, and closes drag handles on attachment reset. Escape works
through the tool frame handler while the canvas is active. Real hidden-workspace
tests cover cancellation, exact undo/redo hashes, IDs and level-switch cancellation.
This closes those reproduced cases, not all interaction combinations below.
See the [selection/projection verification report](../../verification/2026-09-06-selection-lifecycle-and-projection.md)
for the final gates, matched measurements and remaining limits.

The [nudge/network follow-up](../../verification/2026-09-06-selection-nudges-and-network-transitions.md)
releases restored before-states from the editor journal and tests incoming state
while a newer gesture is open. One-tile nudges use Alt+Arrow and toolbar buttons.
Bare arrows pan; Shift+Arrow explicitly restores the inherited five-times-faster
pan that exact modifier matching had made unreachable. Real workspace tests cover
undo/redo, bounds, active text, rejection, passed-over remote edits and old queued
attachment completions. Larger/hidden-type input sweeps remain open.

The [selection-outcome pass](../../verification/2026-09-06-selection-outcomes-and-noop-performance.md)
connects selection geometry to operation acceptance, rejection, undo and redo.
Hidden workspace regressions now cover rectangular rotation and completed nudge
rejection, consecutive rejected transforms, immediate failure, new selection and
inactive-tab contexts. A tool-entry test preserves a newer open mouse preview
until release. Unchanged gestures also avoid full executor snapshots, with
measured one-tile commit allocation independent of total synthetic map size.

The [mirror/authority pass](../../verification/2026-09-06-selection-mirrors-and-authority.md)
adds fixed-bounds reflection and verifies rejection/undo through the real
workspace. It also reproduces and repairs failed-capture legacy commits and
no-op transform geometry masking an earlier undo. The report records current
verification separately from prior performance measurements.

The [local resize pass](../../verification/2026-09-06-local-resize-and-history.md)
stages dimensions/content before installation, preserves local edit history and
new tile IDs across undo/redo, rejects modified checkpoints, and leaves same-size
requests unchanged without allocation. Network resize is still deferred. Current
and inactive checkpoint hashes, local history ownership and monotonic callback
fences are separate checks.

1. **Selection lifecycle and drag integrity.** Extend the established rejection,
   bounds/history and visible-level regressions to longer rotate/undo/drag chains,
   delayed rectangular transforms during actual network-backed mouse gestures,
   temporary tool switches, multiple-tab focus transitions, map resize and larger
   hidden-type selections through the real entry points. Audit
   `ToolGrab.initTiles` and legacy paste `prevTiles` bookkeeping: the old motion
   path is retained only as provenance, and live moves use `editing.Move` through
   editor capture/commit adapters. Remove unused legacy paste background capture
   when designing the deferred paste-preview path. Extend stable-ID conservation
   and current-state precondition coverage before adding more transforms.
   Separate background restoration during one gesture from committed history;
   never restore stale copied tiles over a newer remote edit.
   The tile-commit fallback and local resize failure/undo paths are repaired.
   The [capture/cancellation pass](../../verification/2026-09-06-selection-capture-and-cancellation.md)
   now releases failed preflight captures, preserves the prior preview after a
   destination failure, and cancels faulted moves without leaving orphan entries.
   Cancellation leaves unrelated edits pending; a preview refuses destinations
   already owned by another pending edit. Controlled network acceptance and
   validated recovery with actual Save pass through the hidden workspace.
   A damaged-display recovery UI still needs an explicit retain/export/discard
   design for real unsent intent. Capture cleanup alone does not clear a fault
   or authorize replacing pending work. Use these ownership boundaries when
   designing the placement preview.
   The [ordering/Save pass](../../verification/2026-09-06-history-ordering-and-save.md)
   now repairs accepted-edit insertion during pending undo/redo and saved-depth
   replacement detection. Four real-workspace acceptance/rejection cases restore
   the original map hash; the independent close guard remains in force.
   The [history ownership/retention pass](../../verification/2026-09-06-map-history-ownership-and-retention.md)
   reproduces and repairs delayed-acknowledgement and inactive-resize insertion
   into the wrong tab. Editors now hold an explicit stack-lifetime target.
   Discarded undo/redo slots release captured payloads; real map checkpoint and
   process-memory scaling still need the lifecycle measurements below.
2. **Selection transform parity.** One-tile nudges and H/V mirrors are implemented.
   Floating paste rotation/mirroring is implemented and qualified through actual
   workspace shortcuts, including level cancellation, capture-fault recovery and
   local/network outcomes. See the [transform report](../../verification/2026-09-06-paste-transforms.md).
   The [preview-copy pass](../../verification/2026-09-06-preview-copy-performance.md)
   reduces allocation in both floating transforms and ordinary Grab movement,
   with matched display hashes and explicit mixed timing results.
   Extend attachment/later-acknowledgement interaction qualification. Configurable
   grid steps are implemented; native workspace qualification remains open.
   Test rectangular and sparse selections, overlapping
   destinations, hidden types, map edges, all Z levels, instance ordering,
   stable IDs, failed submission, and exact undo/redo. A custom direction/type
   mapping registry needs project-specific fixtures and reviewable rules.
3. **Shortcut usability.** A per-action catalog, potential conflict detection,
   saved rebinding and per-action/all-default resets now feed the dispatcher and
   shortcut reference. Left/right modifiers and keypad aliases are covered.
   Popup ownership, pending modals, dismissal, custom repeat, text input and
   inactive/disabled registrations now have focused regressions. Complete OS
   keyboard-layout and wider native focus-transition qualification. The
   reference consumes the same bindings, and missing/invalid preferences retain
   defaults. Human acceptance of the editing controls remains open.
4. **Navigation and selection tools.** Audit existing Go to Coordinates and
   Search/Replace All before claiming they are missing. The
   [search audit](../../verification/2026-09-06-search-performance.md) verifies
   existing F3/Shift+F3 through the actual workspace, repairs repeated type scans,
   clips off-screen rows, resets obsolete filter indices and releases results
   on Free. The [ownership follow-up](../../verification/2026-09-06-search-ownership.md)
   now refreshes results after local/remote edits and resize/history, defers them
   during gestures, retains same-map filter bounds and refuses stale row actions.
   Search mutations capture all current targets before changing the display;
   failed preflight leaves no partial edit. Oversized bulk actions, replacement
   validation and broader interaction acceptance remain open. Design explicit
   Z-level filtering with multi-level fixtures. Evaluate
   select-same-type,
   connected selection, camera bookmarks, and selection isolation against
   actual mapping workflows. Preserve hidden state and per-document ownership.
5. **Reusable stamps and repeated actions.** Prototype named selection stamps
   and repeat-last-transform after copy/paste identity and preview semantics are
   verified. Persist mechanical map data only; do not generate content or assets.

The comparison sources are the official
[Tiled tile-editing manual](https://doc.mapeditor.org/en/stable/manual/editing-tile-layers/)
(stamp rotation/mirroring, selection tools, stamp slots) and
[TrenchBroom manual](https://trenchbroom.github.io/manual/latest/)
(selection transformations, navigation, visibility, shortcut configuration).
These suggest capabilities, not bindings to copy blindly into ADMM.

## Repo-wide performance audit remains in scope

Follow the existing workplan's complete matrix: engine copies/hashing, pending
projection, durable stores and recovery, service queues/fanout, concurrent load
measurement, parser/FFI, map import/export, sprite/icon caches, renderer/GPU,
startup/reload, editing/search, idle/minimized behavior, repeated lifecycle
resource retention, and development/build tooling. Instrument component and
input-to-visible stages separately. Use representative DME/DMM/TGM manifests,
multiple map sizes and histories, and fixed seeds.

Run alternate-order control/candidate trials after compilation/tests finish,
with identical work, CPU settings, validation, durability, counts and hashes.
Retain raw results under ignored `.artifacts/`. A component allocation saving
does not establish a desktop-frame, service-capacity, or production speedup.

The [engine-copy pass](../../verification/2026-09-06-engine-copy-performance.md)
now shares immutable internal payloads while isolating mutable branch metadata
and tile tables. Eight deterministic workload hashes/revisions match across
preserved control/candidate binaries, and all 24 component cases improve in
five alternating trials. Full-map validation/indexing, history-map growth and
real desktop/database latency remain targets in the performance workplan.

## Executable gates

Use Go 1.25.13 and Rust `1.82.0-x86_64-pc-windows-gnu` on Windows:

```powershell
go test ./internal/aphelion/editing ./internal/aphelion/hotkeys ./internal/aphelion/collab/model ./internal/aphelion/collab/client -count=1
go test ./internal/app/ui/cpwsarea/wsmap/tools ./internal/app/ui/cpwsarea/wsmap/pmap/... ./internal/app/ui/shortcut ./internal/app/ui/menu -count=1
$env:APHELIONDMM_GL_TEST = '1'
go test ./internal/app/ui/cpwsarea/wsmap -run '^Test(History|Resize|Selection|SaveAcknowledgementBoundaries)' -count=1 -timeout 120s
Remove-Item Env:APHELIONDMM_GL_TEST
go test -race ./internal/aphelion/...
$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
task verify
```

Keep actual desktop smoke, interactive acceptance, GPU profiling, PostgreSQL,
hosted CI and external integrations distinct. Do not modify protected build or
deployment entry points to bypass a failing gate. Changes remain uncommitted
unless the user authorizes a commit.
