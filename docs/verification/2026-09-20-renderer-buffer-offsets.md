# Native renderer buffer offsets

Baseline: `9dd1d7fd`. The actual queued-frame test failed under `-race` because
Go's pointer checker rejected a vertex-buffer offset converted to `unsafe.Pointer`
in `platform.bind`. The indexed draw path used the same invalid Go conversion.

`platform.Render` now uses `DrawElementsWithOffset` and
`VertexAttribPointerWithOffset` from the already-pinned go-gl dependency. These
entry points pass `uintptr` through cgo as `uintptr_t` and convert it at the C
OpenGL boundary. Buffer layout, stride, normalization, index type and offsets
are unchanged. No dependency update or pointer-check suppression is needed;
the four obsolete `govet` exclusions were removed.

## Verification

- Added `TestImGuiRendererBufferOffsets` before changing production code. It
  reproduced the pointer-check failure at `internal/platform/gl.go:295`.
- The test creates two clipped ImGui commands in one index buffer and reads
  pixels from the real hidden window back buffer before swap. Red and green
  interior pixels plus untouched black background verify rendering, including
  the second command's nonzero offset. It also checks for OpenGL errors.
- That regression and `TestQueuedNativeUIStageTrace` pass with `-race` after
  the repair. The full window package also passes with `-race` (2.248 seconds).
  Pointer checks remain enabled throughout.
- `task verify` passes with native GL enabled: lint, contracts, Go tests,
  Rust test/format/clippy checks, parser release build and Windows editor build.
  Rust has zero unit tests; the inherited ImGui C++ warning remains. External
  service tests may skip when their services are not configured.

Logs are in ignored `.artifacts/gl-offsets-2026-09-20/`: `before.log`,
`after.log`, `window-race.log` and `verify.log`. Environment: Go 1.25.13,
GCC 15.2 UCRT, AMD Radeon driver 32.0.21002.27, Windows Server 2022,
`APHELIONDMM_GL_TEST=1`.

This closes the native pointer-check failure recorded during the instance Move
repair. It does not qualify physical display timing, other GPU drivers or human
desktop interaction.
