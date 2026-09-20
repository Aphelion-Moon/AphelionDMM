# Brush capture refusal and batch ownership

Baseline: `049739b2`. Add, Fill, Delete and Replace inherited the same unchecked
before-state capture as single-instance Move. Invalid source contents could be
modified even though the operation could not commit; a later invalid Fill tile
left a partial fill in the display.

The existing checked capture adapter now accepts multiple coordinates. It
captures the whole batch before mutation and releases only newly acquired
entries on failure, preserving prior pending edits and the Save fault. Fill
collects its existing filled/outline target set before applying it. Selected-tile
deletion also preflights its whole target set. Add's common prefab helper and
the editor's individual Delete/Replace methods refuse failed capture.

## Evidence

- Before implementation, native mouse regressions reproduced display mutation
  after capture failure in Add, filled Fill, outline Fill, single-instance
  Delete, Alt tile Delete and Replace.
- `TestBrushCaptureAndHistory` now covers all six paths plus selected-tile
  deletion. Invalid cases leave exact display contents intact, retain the Save
  guard, report one error and create no undo entry. Valid cases commit one
  revision and preserve exact tiles through undo/redo. The Fill cases retain
  the distinction between a filled center and an untouched outline interior.
- `TestBrushBatchCaptureReleasesOnlyNewEntries` covers repeated coordinates,
  failure after earlier successful captures, and an existing independent edit.
  A clean failed batch permits validated attachment recovery to exact authority;
  an earlier edit retains its capture and blocks replacement.
- Window, Search, workspace, editor and tools packages pass with `-race` and
  native GL enabled, including the Move and renderer regressions. Existing
  selection tests preserve the contract that unrelated captured edits can
  coexist with a Grab preview and survive its cancellation. The helper checks
  capture readiness without refusing every edit during that preview.
- Three Search refresh fixtures now use the existing valid-authority fixture
  and real commits/resize. Their stale-row, XY/Z filtering and refresh assertions
  are retained; they no longer rely on mutating an editor whose environment
  could not initialize an executor.
- Final `task verify` passes with native GL enabled: lint, contracts, Go tests,
  Rust checks, parser release build and Windows editor build. The inherited
  ImGui compiler warning and zero Rust unit tests remain; unconfigured external
  service tests may skip.

Ignored evidence: `.artifacts/brush-capture-2026-09-20/` (`before.log`,
`after.log`, `race.log`, `compatibility-race.log`, `verify.log`,
`verify-final.log`). The first repository gate exposed the selection/fixture
compatibility issues described above. Toolchains: Go 1.25.13, GCC 15.2 UCRT,
Rust 1.82 Windows GNU, golangci-lint 2.12.2, Task 3.53.1. Native driver:
AMD Radeon 32.0.21002.27 on Windows Server 2022.

This closes these brush capture failures. It does not qualify other inherited
property/reset/reorder/global-replacement callers, malformed replacement data,
all attachment transitions, physical OS mouse delivery or human acceptance.
Earlier valid gesture intent remains guarded after a later capture failure;
the broader damaged-display recovery workflow is still separate work.
