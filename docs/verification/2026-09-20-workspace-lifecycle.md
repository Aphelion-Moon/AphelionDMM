# Native workspace lifetime probe and shortcut retention

Baseline: `ad41161c`. The shortcut registry removed visible entries without
clearing the unused backing-array slots. Action and enablement callbacks could
therefore keep a closed map's pane and editor reachable. Removal now clears the
pointer with `slices.Delete`; shortcut identity, ordering and dispatch are unchanged.

## Verification

The new shortcut regression fails against the original removal and passes with
the fix. Three holders own 8 MiB callback payloads. Disposing the tail, first and
remaining holder releases each payload without registering replacements, while
preserving still-live holders. Repeated disposal is also exercised.

`TestNativeWorkspaceLifecycle` repeatedly parses a saved map, constructs and
activates `WsMap`, nudges a selection through Alt+Right, performs undo/redo/undo,
saves, reparses, deactivates and disposes the map content and command stack.
The production native frame loop performs deferred rendering and deletion.
Each cycle checks exact restored authority, four accepted revisions, saved
contents under the inherited Save path-weight ordering, deletion of the owned
canvas texture, an empty deferred queue, released mouse callback/history, and
collection of the closed editor through a weak pointer after GC.

One application fixture, environment, ImGui/GL context and inactive primary pane
remain alive throughout. Four warmup cycles precede sixteen measured cycles in
the ordinary test. No per-cycle testing cleanup retains closed maps. The original
shortcut removal also fails this corrected native fixture; the repaired version
passes. Initial fixture failures due to Save ordering and omitted outer command
stack disposal are retained separately and are not product regressions.

`go test -race ./internal/app/ui/shortcut ./internal/app/window -count=1` and
`task verify` pass with `APHELIONDMM_GL_TEST=1`. Rust has zero unit tests; the
inherited ImGui compiler warning remains. Unconfigured external gates may skip.
Logs are retained in ignored `.artifacts/workspace-lifecycle-2026-09-20/`.

## Endurance mode

Use the same pinned toolchains as `task verify`, and run only this test:

```powershell
$env:APHELIONDMM_GL_TEST = '1'
$env:APHELION_LIFECYCLE_DURATION = '30m'
$env:APHELION_LIFECYCLE_OUTPUT = '<new absolute checkpoint JSONL path>'
go test ./internal/app/window -run '^TestNativeWorkspaceLifecycle$' -count=1 -v -timeout 35m
```

Duration must be positive and at most one hour. Output creation is exclusive.
After warmup, the test records post-GC heap allocation/object counts, heap arena
size and goroutine counts initially, approximately every minute, and on success.
Each record is flushed. Endurance suppresses debug/info logging and runs at most
ten complete lifetimes per second, without catch-up bursts. Every completed cycle
still runs the correctness, texture lifetime and editor collection checks.

This is a synthetic 4x4x1, 48-instance, hidden-window map-content probe, not full
application automation or a representative performance campaign. It manually
follows the outer workspace area's command-stack disposal order; it does not
exercise that area's workspace list, tab-close dialogs, environment reload,
network reconnect, crash recovery, or long retained history. Texture checks
cover the canvas texture, not all GL objects or driver memory. Go reachability
release does not prove native/private bytes or video RAM reclamation. Longer-run
results must be reported separately; adding this mode does not complete the
30-minute full-editor acceptance gate.
