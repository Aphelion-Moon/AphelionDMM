# Workspace area close lifetime

Baseline: `85fc01bf`. Inspection following the native map-content lifetime probe
found that the enclosing workspace area retained a removed workspace in unused
list storage. Active/focused references and a pending frame focus candidate
could also point at disposed content until another frame processed them.

The close path now clears the removed list slot and matching active/focused/frame
references. It retains content disposal and command-stack cleanup before focus
notifications: deactivating a still-live tool could commit an unfinished gesture
after a user chose to discard it. Existing switch helpers notify the application
of the cleared active workspace; the next frame does not repeat that notification.
Closing an inactive workspace preserves another workspace's focus.

## Evidence

All three new regressions failed before the change and pass afterward:

- Public `CloseAllGuarded` releases three workspace payloads without opening a
  replacement tab, while the workspace area itself remains live.
- A refused public `CloseGuarded` preserves ownership and emits no disposal or
  switch notification. Subsequent public `Close` clears active, focused and frame
  references immediately, with disposal before focus cleanup and one switch
  notification. End-of-frame cleanup cannot reactivate the disposed content.
- Closing an inactive workspace releases its payload while retaining the other
  workspace and its focus references.

Payloads are 8 MiB and checked using weak pointers after GC. These tests use the
actual workspace container and close methods with instrumented content; they do
not synthesize a native tab-close mouse event or measure process/GPU memory.

`go test -race ./internal/app/ui/cpwsarea -count=1` and `task verify` pass with
`APHELIONDMM_GL_TEST=1`. Rust contains zero unit tests; the inherited ImGui compiler
warning remains. Unconfigured external gates may skip. Failure and validation
logs remain in ignored `.artifacts/workspace-close-2026-09-20/`.

The separate 30-minute map-content endurance process was started from the
unchanged binary built at `85fc01bf`. It does not exercise this outer workspace
area fix. Neither result substitutes for full application/reconnect endurance or
human desktop acceptance. No protected infrastructure or dependencies changed.
