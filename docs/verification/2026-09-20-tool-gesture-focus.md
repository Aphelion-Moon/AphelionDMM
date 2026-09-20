# Tool gesture ownership across map focus changes

Baseline: `d4a6f595`. A native regression reproduced an object Move gesture
remaining attached to its source instance after tool bindings changed to another
map. Mouse release then committed through the destination editor, leaving the
source map's captured edit unfinished. Its `SaveSnapshot` failed with
`map has an uncommitted edit`.

`PaneMap.OnDeactivate` now ends the gesture through an owner-checked tool adapter
while the source editor is still bound. Grab retains its cancellation behavior;
the other tools finish their existing edit through their ordinary stop method.
Changing the editor binding also enforces this boundary if no deactivation was
delivered. Routing state is cleared, and the destination must observe a mouse
release before another gesture can start. Current ImGui input supplies that
release boundary because a newly bound canvas can still have previous-frame
control state. Commands targeting the last pane retain their existing bindings.

The native regressions use actual map editors, command histories, canvas control,
registered frame jobs and screen-coordinate callbacks:

- Shift-drag an object's pixel offset, change focus between two maps with the
  button held, continue moving and release. The source commits exactly its
  pre-transfer state to its own history. The destination stays unchanged and
  receives no undo entry. Source undo restores its initial canonical hash.
- A fresh destination press works after release and creates only destination
  history; its undo restores the destination's initial hash.
- A rectangular Grab drag cancels on transfer without creating history, and the
  held press cannot start another selection. A later deactivation from the old
  pane cannot clear the new owner's selection.

The test-only native frame helper is shared with the existing delayed-network
selection checks. It disables scrollbars as the actual map workspace does.

Validation: the object-move regression failed before the repair and passed
afterward. Focus-transfer and delayed-network mouse checks passed together under
`go test -race ./internal/app/window -run
'^Test(ObjectMoveFocusTransfer|GrabFocusTransfer|MouseDragWithDelayed)' -count=3
-timeout 90s`. `task verify` passed with `APHELIONDMM_GL_TEST=1`: lint/contracts,
Go tests, pinned Rust checks, parser and Windows editor build. Rust contains zero
unit tests; the inherited ImGui compiler warning remains. Logs are retained in
ignored `.artifacts/tool-focus-2026-09-20/`.

Toolchains were Go 1.25.13, GCC 15.2 UCRT, golangci-lint 2.12.2, Task 3.53.1 and
Rust `1.82.0-x86_64-pc-windows-gnu`. No dependencies or protected infrastructure
changed. This verifies injected native input and pane focus entry points, not
OS mouse delivery, complete workspace docking interactions or human acceptance.
The wider temporary-tool, hidden-selection and lifecycle plans remain open.
