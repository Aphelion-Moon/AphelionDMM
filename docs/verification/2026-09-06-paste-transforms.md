# Floating paste transforms and selection cost

Current controls (2026-10-04): Q/E rotate left/right for selections, active Grab moves, clipboard/stamp previews, and held prefabs in Add, Fill, Replace, and Move. Declared directional object variants follow the turn. The original bracket bindings below describe the earlier implementation.

Baseline commit: `052e1acb02b790641d63466de40c91d56028383b`, with prior
uncommitted work preserved. Current source was inspected directly. Workspace
red tests reproduced both absent floating rotation and inability to rotate an
initially invalid edge target into a valid position.

## Result and boundaries

While placing a clipboard selection, `[` / `]` rotate left/right and `H` / `V`
mirror it. Existing toolbar buttons expose the same actions; placement guidance
and the F1 reference describe them. Click/Enter commits one Paste Tiles operation,
and Escape cancels all intermediate movement/orientation changes. Completed Grab
selection transforms and nudge bindings retain their separate behavior.

Transforms keep the current bottom-left target, exchange rectangular dimensions
for quarter turns, and retain sparse holes. They reuse the existing direction
and finite pixel/step offset conversion rules, preserving unknown unrelated
variables. Clipboard instances are never modified and copied stable IDs persist
through previews, confirmation, and undo/redo. Hidden destination instances stay
in place with their identities intact.

The owned editing model builds a sparse candidate template, validates orientation
and bounds, then reuses Move's capture-before-restore path. Failed preflight drops
only newly acquired captures and keeps the old template/display. A failed
transform displays its error and blocks confirmation until a valid move or
transform succeeds. A transform may make an initially invalid target fit.
Capture faults remain latched until validated replacement, including after
cancellation. Level and attachment ownership are checked by the editor adapter.

The constructor still requires the clipboard's original extents to fit the map
somewhere. Oversized templates, arbitrary-angle/sprite matrix transforms,
project-specific type/connectivity remapping, and cross-level templates remain
outside this feature. No protocol, schema, dependencies, protected entry points,
or assets changed. Changes remain uncommitted.

## Verification

Artifacts: `.artifacts/paste-transform-audit-2026-09-06/`.

| Evidence | Result |
| --- | --- |
| Red workspace tests | Both new cases failed for missing floating transform behavior. |
| Owned model | Sparse rectangular geometry, full-turn/inverse identity and variable preservation, source isolation, passed-over restoration, partial acquisition rollback, malformed orientation refusal, bounds/level rejection and closed-handle guards pass. |
| Actual workspace shortcuts | Rotation and both mirrors update an uncommitted preview; hidden IDs and clipboard contents survive; Enter commits one revision; real Save, local undo/redo restore exact hashes. An invalid edge target can be rotated to fit; a later invalid rotation leaves the display intact and cannot confirm. |
| Input/lifecycle | Left/right modifier combinations and a real active ImGui text field suppress transforms. Level switching cancels transformed placement; subsequent shortcuts do not mutate stale state. |
| Network adapter | No operation is sent during transforms. Confirmation sends only the two final destination changes. Acceptance/rejection converge with the authoritative engine; rejection retains a conflict, and accepted undo/redo restore exact hashes/IDs. This uses a controlled in-memory transport. |
| Capture fault | Invalid destination identity leaves the old preview untouched. Further transforms cannot bypass the fault; Escape retains the Save guard and submits nothing. Reattaching the validated executor restores exact original state and permits real Save. |
| Affected race suites | Editing: 19 top-level tests, 1.270 s. Workspace: all 41 top-level tests, 28.224 s. App: 4.144 s; pane: 1.270 s; editor: 4.209 s; canvas: 5.735 s; settings: 1.322 s; tools: 1.444 s. No existing test skipped in these packages. |
| Repository | `task verify` passes: zero lint issues, contracts, all Go tests, Rust test/fmt/clippy, parser release and desktop build. Rust currently has zero unit tests. Default Go runs skip gated GL tests; separate hidden race runs above supply that evidence. |
| Produced command | Parser/loopback smoke passes open/save/reparse, two authenticated clients at accepted revisions 1/2, equal map hashes, leave and shutdown. This does not exercise desktop UI. |

The hidden workspace uses real editor/clipboard/shortcut/ImGui/GL components and
the actual Save path, with fixture content and adapted application services.
It does not inject physical OS events or establish human usability, real sprite
rendering, real WebSocket deployment or project-specific map acceptance. Menu,
shortcut, overlay, quick-edit and tile-menu packages currently have no tests;
their package-level skip records are not hidden-test skips.

Toolchains: Go 1.25.13 windows/amd64; Rust 1.82.0 GNU; Task 3.53.1;
golangci-lint 2.12.2; GCC 15.2.0. The inherited ImGui C++ `memset` warning remains.

Desktop SHA-256:
`8c919000168f94a16d82148182f699d8d72b3e3649527dcdc31b423a8bf4fe46`.
The artifact manifest records source, documents, binaries and raw evidence using
relative paths. The smoke report's revision field is `undefined`; binary/source
hashes are the build identity evidence.

## Performance baseline

`BenchmarkPlacementTransform` runs five samples of 40 rotations per case.
Each timed call transforms and redraws the model preview. Map/template setup,
stable-ID creation, final inverse completion, and content/identity checks are
outside timing. All samples check the complete template's IDs, coordinates and
variables after a full rotation cycle and bound backgrounds to selected cells.
No concurrent build/test workload was launched during these measurements.

| Selection | Median ms (range) | Median B/op | Allocs/op |
| --- | --- | --- | --- |
| 1 cell | 0.007048 (0.006555-0.010732) | 2,002 | 26 |
| 100 cells | 0.137142 (0.068905-0.149642) | 35,650 | 626 |
| 4,096 cells | 11.242278 (7.960235-14.425518) | 1,509,251 | 24,616 |
| 2 sparse cells, extent 16 | 0.008998 (0.006378-0.011098) | 2,626 | 39 |
| 2 sparse cells, extent 256 | 0.008055 (0.005902-0.180580) | 2,626 | 39 |

The implementation visits selected cells/instances and sorts coordinates rather
than scanning the sparse bounding rectangle. Equal sparse allocation is stronger
evidence than timing here: the larger fixture has a substantial outlier, and
desktop activity/power/GC scheduling were uncontrolled. These are baseline costs
for a new feature, with no historical control or speedup claim.

The 4,096-cell result warrants further work: profile candidate instance creation,
background copies, sorting, real capture conversion and render invalidation.
These measurements exclude production capture callbacks, base regeneration,
bucket rebuilds, GPU upload, network dispatch and complete input-to-visible time.
Keep those costs in the repo-wide performance matrix rather than interpreting
this model baseline as editor frame time.

## Reproduction and remaining work

From the repository root with the pinned toolchains; check each native exit code:

```powershell
go test ./internal/aphelion/editing -count=1
go test ./internal/aphelion/editing '-run=^$' '-bench=^BenchmarkPlacementTransform$' -benchtime=40x -benchmem -count=5
$env:APHELIONDMM_GL_TEST = '1'
go test -race ./internal/app ./internal/app/ui/cpwsarea/wsmap ./internal/app/ui/cpwsarea/wsmap/pmap/... ./internal/app/ui/cpwsarea/wsmap/tools -count=1 -timeout 120s
Remove-Item Env:APHELIONDMM_GL_TEST
$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
task verify
```

Restart with rebuilt `dst/StrongDMM.exe` for human acceptance. Continue expanded
attachment/reconnect/later-acknowledgement interactions, malformed content and
all-level composition, representative sprite/large-selection input latency,
configurable bindings/grid steps and the remaining repo-wide performance matrix.
Real PostgreSQL, hosted CI, Meridian integration and human acceptance are separate
unrun gates. The overall audit/repair/QoL goal remains active.
