# Mouse gestures with delayed collaboration outcomes

Baseline: `c5a96f15`. `TestMouseDragWithDelayedSelectionOutcome` extends the
editor-preview checks to the registered tool frame handler, actual canvas
control and the mouse callback registered by a native `PaneMap`. A test-only
window export invokes the existing repeat-job queue; production APIs and behavior
are unchanged.

The hidden OpenGL fixture supplies ImGui button/position input and screen
coordinates to the pane callback. It follows frame ordering: queued completions,
registered tool handlers, canvas control/rendering, then mouse callbacks. It
retains the real one-frame control latency. Drag start, motion and release do not
call editor preview methods or private Grab handlers directly.

A 3x2 selection rotates to 2x3 before a diagonal drag. Three cases pass:

- Rotation accepted while dragging, then drag accepted after release. Undo both
  operations and redo both, checking exact display/authority hashes and bounds.
- Rotation rejected while dragging. The preview and Save guard survive the old
  rejection. Releasing the drag fails its stale preconditions locally and retains
  an unsent draft; no second operation is sent. Both drafts remain available,
  map and selection return to their original state, and neither enters undo.
  Reconstructing the retained drag from its rotated before-state reproduces the
  complete preview hash, including stable IDs and instance ordering.
- Rotation accepted during dragging, then the released drag rejected. The map
  and geometry retain only the rotation; undo/redo traverses that accepted
  operation and never inserts the rejected drag.

Validation with `APHELIONDMM_GL_TEST=1`:

- Focused native check passed before broad validation.
- `go test -race ./internal/app/window -run
  '^TestMouseDragWithDelayedSelectionOutcome$' -count=3 -timeout 90s` passed all
  three scenarios on each repetition.
- `task verify` passed: lint, contracts, Go tests, pinned Rust checks, parser and
  Windows editor build. Rust has zero unit tests; the existing ImGui compiler
  warning remains. Logs are in ignored `.artifacts/mouse-network-2026-09-20/`.

Toolchains: Go 1.25.13, GCC 15.2 UCRT, golangci-lint 2.12.2, Task 3.53.1 and
Rust `1.82.0-x86_64-pc-windows-gnu`.

This is deterministic native integration evidence, not physical mouse or socket
timing acceptance. Input is injected below the OS/GLFW event-delivery layer;
network envelopes use a controlled in-memory transport and the real executor
and authoritative engine. It does not exercise the complete workspace UI,
multi-tab/temporary-tool combinations, larger hidden-type selections or human
acceptance. Those broader plan items remain open. No dependencies, protocol,
protected infrastructure or deployment state changed.
