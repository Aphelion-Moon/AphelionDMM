# Single-instance Move identity

Baseline: `8604f5ee`. The inherited Move tool deleted the selected instance and
created a replacement on each tile hop. That discarded its stable identity;
looking up the replacement by prefab ID could also select an identical resident
and move that different object on the next hop.

Subsequent verification: the renderer pointer-check failure below is now
[repaired and covered by native pixel/race checks](2026-09-20-renderer-buffer-offsets.md).

The tool now transfers the original instance between tiles, preserving its
local ID, stable ID and editor references. A narrow coordinate mutator supports
the transfer. Both vacated and destination coordinates request a render refresh.
Existing turf regeneration and commit behavior remain in place.

## Evidence

`TestInstanceMoveKeepsIdentityAcrossMatchingPrefabs` drives the native workspace
mouse path through four hops, including a return through the origin. Every
destination contains an identical prefab. It checks all instance identities and
coordinates, the original selected pointer, exact return-to-origin state, one
committed revision, and exact undo/redo hashes.

- Before the repair, the regression failed on the first hop with an empty
  stable ID at the destination. After the repair it passed.
- The regression and existing Move/Grab focus transfer, held-tool ownership,
  and delayed network selection tests pass with `-race` and native GL enabled.
  The tools package also passes with `-race`; the instance package has no tests.
- A broader window-package race run fails in the existing
  `TestQueuedNativeUIStageTrace`: `checkptr` rejects the buffer-offset conversion
  at `internal/platform/gl.go:295`. This is a remaining native-boundary validation
  gap. No checks or assertions were disabled.
- `task verify` passes with `APHELIONDMM_GL_TEST=1`: lint, contracts, Go tests,
  Rust checks, parser release build and Windows editor build. Rust has zero
  unit tests; the inherited ImGui C++ compiler warning remains. External-service
  tests without configured services may skip. This gate does not use `-race`.

Ignored logs: `.artifacts/instance-move-2026-09-20/` (`before.log`, `after.log`,
`focused-race.log`, `move-race.log`, `verify.log`). Toolchains are Go 1.25.13,
GCC 15.2, Rust 1.82.0 Windows GNU, golangci-lint 2.12.2 and Task 3.53.1.
The installed AMD driver is 32.0.21002.27; native tests use a hidden GL context.

This synthetic object fixture does not qualify all turf/area moves, capture
failures, cross-chunk rendered pixels, real OS mouse input or human acceptance.
