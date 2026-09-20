# Deferred canvas disposal lifetime

Baseline: `fba184f5`. Repeated `Canvas.Dispose` calls queued duplicate deletion
jobs. The jobs read mutable canvas handles at execution time, while `Process`
could still replace the texture after disposal was requested. Completed disposal
also left deleted object names exposed through the canvas.

Disposal is now idempotent and records the framebuffer/texture names to delete.
It blocks later `Process` calls, releases the native objects on the existing
next-frame queue, and clears canvas handles/dimensions after the job runs.
Readback on an unallocated or completed-disposal canvas returns no pixels.

The screenshot caller deliberately schedules disposal before `ReadPixels`.
That same-frame contract is preserved: the pending texture and pixels remain
available until the queue drains. Already-built draw commands can likewise use
the texture for the rest of the current frame. All lifecycle calls retain the
existing UI-thread ownership requirement.

## Evidence

The new native integration test runs the actual window deferred-job queue using
test-only exports, with no new production queue API. The original code failed
because two Dispose calls queued two jobs. The fixed code passes 32 cycles,
creating and deleting two canvas textures per cycle, checking:

- one cleanup job despite repeated disposal;
- unchanged same-frame readback after attempted late resize;
- deletion of the texture object (`gl.IsTexture` is false), zero exposed handle,
  and empty readback after the actual queue drains;
- no recreation by a late render and no deletion of a different live canvas by
  another call to the old canvas's Dispose;
- no remaining jobs or OpenGL errors at each cycle boundary.

Existing native canvas resize/pixel checks and affected race tests pass.
`task verify` passes with `APHELIONDMM_GL_TEST=1`: lint/contracts, Go/native tests,
pinned Rust checks, parser build and Windows editor build. Rust contains zero
unit tests; the inherited ImGui C++ warning remains. Unconfigured external gates
may skip. Raw failure, focused, race and gate logs remain in ignored
`.artifacts/canvas-lifetime-2026-09-20/`.

This establishes texture object lifetime and deferred-queue behavior. It does
not measure video RAM reclaimed by the driver, independently count framebuffer
objects, exercise screenshot file/clipboard output, or replace the planned
30-minute full-editor lifecycle campaign. No dependency or protected
infrastructure changes were made.