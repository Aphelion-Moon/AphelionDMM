# Closed dialog retention

Baseline: `5af7c6b3`. Closing a dialog removed its visible registry entry but
left a strong interface reference in the global slice's unused backing array.
That reference could retain dialog records and callbacks, including their pane
and editor owners, after dismissal.

The registry now uses `slices.Delete` to clear the removed reference while
preserving name-based removal, the order of other dialogs and modal input state.

## Evidence

Both new regression tests failed before the repair and pass afterward:

- Public `Close` releases an 8 MiB callback payload after garbage collection,
  without opening a replacement dialog. Closing the last, first and remaining
  entries preserves payloads still owned by other open dialogs.
- Dismissal through actual ImGui `Process` releases its callback payload and
  removes the modal input block after the frame returns.

`go test -race ./internal/app/ui/dialog -count=1` and `task verify` pass with
`APHELIONDMM_GL_TEST=1`. The gate includes Go/native tests, lint/contracts,
pinned Rust checks, and parser/editor builds. Rust has zero unit tests; the
inherited ImGui compiler warning remains. Unconfigured external gates may skip.
Raw failure, focused and gate logs remain in ignored
`.artifacts/dialog-retention-2026-09-20/`.

Weak-pointer collection establishes release of Go object reachability. It does
not measure process memory returned to Windows, GPU memory, or the planned
30-minute full-editor lifecycle campaign. No dependency or protected
infrastructure changes were made.
