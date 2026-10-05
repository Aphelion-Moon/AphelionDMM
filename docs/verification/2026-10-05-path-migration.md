# Path migration for unknown types

Opening a map whose type paths the loaded environment does not define now
offers the **Path Migration** panel (also Edit > Resolve Unknown Types...) in
place of the read-only "Unknown Types [WIP]" list. The panel proposes a target
for every unknown path with a confidence tier and its reasons, and applies the
chosen migrations as one undoable edit.

## Sources and confidence

- **Certain:** the codebase's `tools/UpdatePaths/Scripts` (tgstation UpdatePaths
  syntax, evaluated line by line in pull request order as the reference tool
  does) and decisions a person chose to remember. A script history that moves a
  path past the first defined type offers both readings at High instead.
- **High/Medium:** directional mapping helpers flattened to `dir` (offsets come
  from an optional reference environment), unique trailing-segment matches, and
  appearance/name matches against a reference environment.
- **Low/Lossy:** loose name similarity within the same branch and the nearest
  defined parent. These are never selected automatically.

Script `@DELETE` results, root-type changes and every deletion require a person.
Map-edited variables the target does not declare lower the score; values a
candidate would overwrite are listed as conflicts and block automatic selection.

## Safety

- Only paths unknown at plan time are rewritten; stable IDs, unknown variables
  and tile order are retained. A resolver that cannot reach a defined type leaves
  the instance unchanged and reports it.
- A tile that would gain or lose an area or turf fails the whole edit, except a
  person's deletion of the only area/turf, which restores the map default.
- Unshared maps use the local work owner (one revision, one history entry).
  Sessions submit one tile-change operation; above 4,096 tiles without bulk
  edits the migration is refused before any display or capture change.
- Nothing is written to disk unless a person exports a script or enables
  Remember / codebase saving in preferences. Codebase saves require the
  `PRNUMBER_NAME.txt` form, stay inside the resolved
  `tools/UpdatePaths/Scripts` directory and confirm overwrites. No paths cross
  the collaboration protocol.
- The reference environment is parsed on a worker, can be cancelled, and never
  replaces the loaded environment, icon cache or map defaults.

## Evidence

Toolchain: Go 1.25.13 with w64devkit GCC 16.2 (cgo), Windows Server 2022.

- `go test ./internal/aphelion/repath/...`: parser, evaluator, formatter,
  inventory, generators, scoring, transform, export, memory and the panel
  controller (fake app/target) pass. The controller suite also runs under
  `-race`.
- `go test ./internal/app/ui/cpwsarea/wsmap/pmap/editor -run 'PathMigration|Oversized'`:
  unshared 1- and 256-tile maps (one revision, exact undo/redo, stable IDs and
  unknown variables kept, display relinked to the environment type), busy
  refusal, one session submission, and refusal of a 4,224-tile session edit
  without bulk edits before any state change.
- `go test ./internal/app -run PathMigration`: preference defaults and
  persistence, the separate remembered-decision config (no file until used),
  and the open-time offer following preferences.
- Real script corpus (`APHELION_UPDATEPATHS_CORPUS`, a tgstation-derived
  codebase): 309 scripts, 4,027 rules parsed with stable round-trip formatting;
  5 lines rejected, each malformed in the source (for example a doubled `:`,
  `Initialize(mapload)` in a path, `@OLD` used as a filter).
- Real map (`APHELION_REPATH_DME`/`APHELION_REPATH_DMM`): tgstation map_depot
  "MiniStation Redux January 2023" against the same codebase. 81 unknown types
  (257 instances): 69 Certain from codebase scripts and selected
  automatically, 4 Medium (3 are script deletions offered to a person), 5 Low,
  3 Lossy. The automatic plan rewrote 242 instances on 221 tiles with no tile
  composition conflicts and nothing unresolved.
- `go test ./...`: every package passes except `internal/aphelion/envcache` and
  `internal/dmapi/dmenv`, whose failures are the cache's Windows ACL check
  ("cache path grants writes outside the local user boundary") on this host's
  profile directory. Neither package is changed by this work.

## Not covered

- A manual desktop session (built binary, Meridian-Rift environment, MiniStation
  2023) opened the panel on map load, showed 69 Certain and 12 unresolved
  types, exercised filters, Keep, Custom path with autocomplete, a confirmed
  script deletion, Apply (244 instances on 221 tiles) and Undo (full
  restoration). It exposed two defects, both fixed with regression tests:
  choices on rows removed by Apply were lost after Undo, and directional helpers
  whose family moved to another branch had no suggestion. The fixes were not
  re-checked in a rendered session. No automated GL test drives the panel.
- Per-variant overrides exist in the engine and export but the panel offers
  path-level choices only.
- The reference-environment path was exercised with a test double; a real
  second `.dme` parse was not run alongside a loaded editor.
