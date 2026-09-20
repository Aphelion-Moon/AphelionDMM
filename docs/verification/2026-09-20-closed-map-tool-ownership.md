# Release closed-map tool ownership

Baseline: `fb3fbad3`. `PaneMap.Dispose` closed the editor and queued canvas
resource deletion but left the global tools bound to that editor, canvas control
and canvas state. Grab could retain copied selection contents, and Move could
retain an instance/tile. A native workspace regression reproduced selectable
contents remaining after the real `WsMap.Dispose` entry point returned.

The pane now releases tool ownership before closing its editor. Release checks
that the global editor is the pane being disposed: closing an inactive pane
cannot clear another pane's tools. It cancels Grab/placement while the owner is
still available, clears retained tool state in place, and removes editor/canvas
bindings and active mouse bookkeeping. It preserves the selected tool name and
tool object identities. It never calls tool `onStop` during disposal, so cleanup
cannot submit an additional edit. Idle tool processing and selected-tile queries
now tolerate the absence of an editor/canvas binding.

## Verification

- The actual hidden native workspace disposal test fails before the change with
  retained selectable tool contents and passes after it. A later mouse event
  safely observes no closed canvas binding.
- The ownership regression verifies an unrelated editor release leaves the
  active preview unchanged; releasing its actual owner restores the exact
  before-state without a commit, clears editor/control/state references, drops
  inactive Move instance/tile references, and tolerates repeated release.
- Focused disposal/Grab tests and their race run pass.
- `task verify` passes with `APHELIONDMM_GL_TEST=1`: lint/contracts, Go and native
  workspace tests, pinned Rust checks, parser build and Windows editor build.
  Rust contains zero unit tests; the inherited ImGui C++ warning remains.
  Unconfigured external-service gates may skip.

Logs are retained in ignored `.artifacts/dispose-tools-2026-09-20/`. This is
ownership and functional cleanup evidence, not a measured reduction in heap,
private bytes or GPU memory. Deferred canvas deletion, queued renderer work and
the planned 30-minute lifecycle/resource campaign remain unqualified here.
No dependency, protocol or protected infrastructure changes were made.