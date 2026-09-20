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

## Property-action continuation

Baseline: `9292322d`. Before-state capture is now also checked by instance reset,
stack reordering, tile replacement, delete-by-prefab, global prefab replacement,
the instance variable editor and Quick Edit. Bulk operations preflight every
matching tile before mutating any instance. The variable editor captures before
changing its session prefab cache. Quick Edit checks again before release-time
sanitization, preserving entered values if capture has failed in the meantime.

- `TestPropertyActionsCaptureAndHistory` reproduced partial display mutation in
  all six editor actions before repair. Its valid cases still commit once and
  restore exact identities, ordering and variables through undo/redo. Invalid
  cases preserve display/authority, report failure, create no history, retain
  the Save guard and release unused captures for validated recovery. These are
  public editor-method checks with the real local executor, not menu clicks.
- `TestVariableCaptureFailurePreservesPrefabSession` reproduced instance/session
  mutation after a real editor capture fault. It now preserves both the instance
  and cached session prefab without selecting a replacement or creating undo.
- `TestQuickNudgeCaptureAndRelease` uses actual ImGui wheel input with controlled
  capture outcomes. Failed change and failed release originally mutated the
  prefab. Both now retain exact references; valid release still removes an
  inherited default override. Direction controls use the same checked pattern
  but are source-inspected/compiled, not exercised with a DMI direction fixture.
- Variable editor, editor, Quick Edit, workspace and window packages pass with
  `-race`, with native GL enabled for the workspace/window fixtures. Quick Edit's
  new input test renders ImGui data without a GPU backend or OS event delivery.
- Final `task verify` passes with native GL enabled: lint, contracts, Go tests,
  Rust checks, parser release build and Windows editor build. The inherited
  ImGui warning and zero Rust unit tests remain; external services were not
  newly qualified by this pass.

Logs: ignored `.artifacts/property-capture-2026-09-20/` (`before-editor.log`,
`before-variable.log`, `before-quick.log`, `after.log`, `race.log`, `verify.log`,
`verify-final.log`). The initial full gate found an unused test-fixture field;
it was removed. Toolchains and driver match the brush pass above.

The remaining direct `BeginTileChange` implementation callers are the checked
adapter and existing selection/paste/Search adapters that inspect capture errors
and manage journal ownership. This does not prove all new-value validation,
stale instance references, attachment transitions or human interaction complete.
