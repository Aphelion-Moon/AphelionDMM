# Configurable selection movement step

Baseline: `3f7ae99c`. This implements the configurable grid-step item in the
editor QoL workplan. It does not complete the broader transform/interaction matrix.

Subsequent verification: after the [graphics driver installation](2026-09-20-graphics-driver.md),
the native grid-step workspace test and repository gate with OpenGL enabled pass.
The original skipped-test evidence below describes the implementation pass.

Editor preferences now expose **Selection Move Step**. Alt+Arrow and the Grab
move buttons read the same current value, and toolbar/help text explains the
setting. The default remains one tile. Valid values are 1 through 4,095, one less
than the maximum supported map dimension. Missing, zero, negative or excessive
saved values restore the default without changing existing nudge-variable mode
or save-format preferences. The established preference serializer persists it.

Multi-tile nudges use the existing selection move, capture, outcome and operation
path. Each accepted nudge remains one operation. Selection-level, active-gesture,
focus and map-boundary guards remain; the destination must fit completely and is
never clipped. Moving several tiles does not edit the intervening tiles. Pixel
offsets and directions are unchanged by translation.

## Validation

Logs: ignored `.artifacts/grid-step-2026-09-20/`.

- The new tool regression initially failed because `Nudge` only accepted unit
  shifts. Positive and negative horizontal/vertical multi-tile movement now
  passes, preserving moved IDs, passed-over contents and one commit per move.
  An out-of-map step leaves map contents, selection bounds and commit count intact.
- Tests load an older preference document, preserve its other settings, locate
  and use the actual Editor preference control, save, and reopen the custom step.
  They also cover invalid stored integers and live preference lookup by map actions.
- Focused race tests passed for the affected preference, tool, map-action and
  underlying move paths.
- `task verify` passed lint, contracts, the Go suite, pinned Rust test/fmt/Clippy,
  parser release build and Windows editor build. Rust has zero unit tests; the
  inherited ImGui C++ warning persists.

The new real-workspace test covers Alt+Arrow dispatch, one-operation revision
advance, map-boundary refusal, exact undo/redo hashes, stable IDs and changing
the preference while the map is open. It was compiled but **skipped**, as the
host still lacks a usable OpenGL context. This pass therefore does not claim
native workspace or human interaction acceptance for the configurable step.

No dependency manifests or protected infrastructure changed. The pre-existing
staged server-inventory document remains untouched.
