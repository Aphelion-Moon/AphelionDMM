# Canvas replacement during map resize

Baseline: `759f3f7b`. `PaneMap.reloadCanvas` replaced the canvas without disposing
the old one. Successful resize and resize undo/redo therefore abandoned its
framebuffer and texture. The native regression reproduced a live old texture
after the real deferred queue drained.

Replacement now calls the old canvas's existing `Dispose` method before creating
the new canvas. Deletion remains deferred to preserve already-built draw commands
and same-frame screenshot readback. The shared camera is retained as before.

## Evidence

`TestResizeHistoryReleasesReplacedCanvas` exercises `Editor.ResizeMap` and actual
command-storage undo/redo on a native map pane. Eight resize/undo/redo/undo cycles
perform 32 replacements. Each verifies:

- old and new textures coexist until the queue drains, with unchanged old pixels;
- the old texture is deleted and its exposed handle cleared after queue cleanup,
  while the new texture remains live and no deferred jobs remain;
- a stale old canvas cannot recreate a texture;
- the same camera object, position and scale survive replacement;
- undo restores the exact initial authority hash and redo preserves the expanded
  map's contents and stable identities, with no OpenGL errors.

No-op and invalid resize calls preserve the canvas and queue. The test failed on
the original replacement path and passes with the fix. The focused native race
check and `task verify` pass with `APHELIONDMM_GL_TEST=1`. Rust contains zero unit
tests; the inherited ImGui compiler warning remains. Unconfigured external gates
may skip. Logs remain in ignored `.artifacts/resize-lifetime-2026-09-20/`.

The existing disposal method deletes both framebuffer and texture; this test
directly queries texture lifetime, not framebuffer counts or reclaimed driver
memory. The separate map-content endurance binary built at `85fc01bf` does not
resize and does not measure this fix. Full application/reconnect endurance,
representative maps and human desktop acceptance remain open. No dependencies
or protected infrastructure changed.
