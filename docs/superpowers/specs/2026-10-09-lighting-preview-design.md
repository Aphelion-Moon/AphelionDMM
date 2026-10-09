# Lighting preview design

Date: 2026-10-09. Status: proposed, awaiting the human decisions in section 12.
Workplan: section N of `docs/superpowers/plans/2026-10-08-playtest-followup-workplan.md`.
Spike evidence: `docs/verification/2026-10-08-lighting-preview-spike.md`.
Pure model: `internal/aphelion/lighting` (implemented, no UI or render integration yet).

## 1. Goal and non-goals

An approximate, editor-only preview of how a map will look under the game's
lighting, so mappers can judge light placement without launching a server.

- The preview is never map data, never part of a collaboration operation or
  presence message, and never an input to any environment or map hash.
- It is derived state: recomputed from the displayed map and the parsed
  environment, discarded on close, and never saved.
- It does not need to match the game pixel for pixel. It must be deterministic,
  cheap enough to leave on while editing, and clearly labelled "approximate".
- Out of scope: mob/ghost night vision, lighting cutoffs from glasses, emissives,
  bloom, multi-z light bleed, power/area state, day/night, animations.

## 2. What the real model does (Meridian-Rift, tg-derived)

All paths below are under `code/` of `Meridian-Rift`.

### 2.1 Pipeline

1. Each emitting atom owns a `/datum/light_source` (`modules/lighting/lighting_source.dm:7`).
   It exists only if `light_power`, `light_range` and `light_on` are all truthy
   and `light_system` is `COMPLEX_LIGHT` (`lighting_atom.dm:44-62`).
2. The source finds the corners it touches: every non-opaque turf in the square
   BYOND `view(ceil(light_range + visual_offset))` contributes its four corners
   (`lighting_source.dm:464-513`). Opaque turfs are skipped
   (`IS_OPAQUE_TURF`, `__DEFINES/turfs.dm:12`), so a wall face is lit through
   its transparent neighbours.
3. Each corner gets `falloff * light_power` per channel, multiplied by the
   colour's `lum_r/g/b` (`APPLY_CORNER`, `lighting_source.dm:198-213`;
   `PARSE_LIGHT_COLOR`, `__DEFINES/lighting.dm:134-146`).
4. A corner sums all sources into `lum_r/g/b` (`lighting_corner.dm:100-102`),
   normalises by its largest channel only when that exceeds 1
   (`lighting_corner.dm:113-116`), and rounds to `1/64`
   (`LIGHTING_ROUND_VALUE`, `__DEFINES/lighting.dm:35`; `lighting_corner.dm:130-132`).
5. A turf's `lighting_object` colours its icon with the four corner values
   (SW, SE, NW, NE, `lighting_object.dm:89-116`); the icon interpolates across
   the tile. The object is multiplied over the game plane
   (`render_plate.dm:266-279`, `BLEND_MULTIPLY`).
6. Area base lighting is added on the same plane (see 2.4).
7. The plane has a floor: `set_light_cutoff(10)` (`render_plate.dm:296`) installs
   subtract-then-add colour matrices of `ratio = 0.10` (`render_plate.dm:352-353`),
   which clamps every channel to at least 0.10.

### 2.2 Falloff and cone

`falloff_at_coord` (`lighting_source.dm:270-294`), with `x, y` the offset of a
corner from the emitter in tiles and `z = 0`:

```
divisor    = max(1, range)                                   :271
multiplier = 1 - clamp01( sqrt(x^2 + y^2 + z^2 + height) / divisor )   :276
```

`height` is added inside the root, unsquared (default `LIGHTING_HEIGHT` 1,
`__DEFINES/lighting.dm:33`; starlight uses `LIGHTING_HEIGHT_SPACE` -0.5, `:29`).
The sheet evaluates it at half-integer corner offsets (`lighting_source.dm:248-253`),
so a source tile's own corners sit at `(+-0.5, +-0.5)`.

Cone, when `0 < angle < 360` (`lighting_source.dm:277-294`):

```
coord_angle = delta_to_angle(x, y)             north-zero clockwise (__HELPERS/maths.dm:10-17)
center      = dir2angle(light_dir)             (__HELPERS/type2type.dm:81-99; unknown dir -> null = 0)
delta       = abs(center - coord_angle); if delta > 180: delta = 180 - (delta - 180)
result      = max( multiplier * (1 - max(delta - angle/2, 0) / 30), 0 )      :294
```

So the cone has a 30 degree soft edge beyond `angle / 2`.

### 2.3 Emitter position

`calculate_light_offset` (`lighting_atom.dm:218-230`) shifts the emitter by the
atom's pixel offset (`/ ICON_SIZE`) unless `LIGHT_IGNORE_OFFSET`
(`__DEFINES/lighting.dm:24`), plus `get_light_offset()`. Wall light fixtures add
half a tile along their own `dir` (`modules/power/lighting/light.dm:166-171`) and
point `light_dir` at `REVERSE_DIR(dir)` (`light.dm:114`) with `light_angle = 170`
(`light.dm:14`). The sheet is evaluated at `corner + offset` (`lighting_source.dm:250-253`),
which equals "emitter moved by `-offset`"; the Go `Source.ShiftX/Y` stores that
moved position directly.

### 2.4 Ambient light

- `area.base_lighting_color` and `base_lighting_alpha` (`lighting_area.dm:9-11`)
  produce a `BLEND_ADD` overlay at `alpha` on the lighting plane
  (`lighting_area.dm:62-76`), so ambient = `color * alpha / 255`, added after the
  per-corner light.
- `area.static_lighting` (default TRUE, `static_lighting_area.dm:39`) controls
  whether turfs get a `lighting_object` at all (`static_lighting_area.dm:46-51`,
  `lighting_turf.dm:13-17`). Without one, only base lighting shows.
- Space: `/turf/open/space` has `light_range 2`, `light_power 1`,
  `light_color COLOR_STARLIGHT` (#8589fa), `light_height -0.5`, `light_on FALSE`,
  `space_lit TRUE` (`game/turfs/open/space/space.dm:64-69`). The light is switched
  on at runtime only when one of the 8 neighbours is not space or cordon
  (`space.dm:104-122`). `/area/space` has `static_lighting FALSE`,
  `base_lighting_alpha 255`, `base_lighting_color COLOR_STARLIGHT`
  (`game/area/areas/misc.dm:3-10`); `/area/space/nearstation` has static lighting
  on and alpha 0 (`misc.dm:22-25`). A `space_lit` turf in an area without base
  lighting adds the starlight overlay itself (`game/turfs/turf.dm:175-176`).
  Defaults are overridable at runtime (`GLOB.starlight_range/power/color`,
  `space.dm:1-41`).

### 2.5 Which atoms emit

`light_system` declarations (`game/atom/_atom.dm:69`, default `COMPLEX_LIGHT`;
values `__DEFINES/lighting.dm:1-10`):

| value | in game | preview |
| --- | --- | --- |
| `NO_LIGHT_SUPPORT` 0 | none | never emits |
| `COMPLEX_LIGHT` 1 | corner model above | modelled faithfully |
| `OVERLAY_LIGHT` 2 | prebaked circular mask added on a separate plane, range clamped 1..6, alpha `min(230, abs(power)*120+30)` (`datums/components/overlay_lighting.dm:394-436`) | approximated as a corner light with that range and strength |
| `OVERLAY_LIGHT_DIRECTIONAL` 3 | as 2 plus a cone image; the mask is cast `clamp(round(range/2),1,3)` tiles forward (`overlay_lighting.dm:416-420,511-525`) | circular light shifted forward by the cast distance |
| `OVERLAY_LIGHT_BEAM` 4 | as 3, narrow | 45 degree cone approximation |

Overlay lights are installed only for movables (`game/atoms_movable.dm:210-217`)
and are mostly toggled by gameplay code, so few appear on mapped atoms.

Anything that sets its light only in code is invisible to a plain variable read.
The important case is `/obj/machinery/light` (the dominant station light):
static `light_range` is 0 and the real light comes from `brightness` (default 8),
`bulb_power` (1) and `bulb_colour` (`LIGHT_COLOR_DEFAULT` #f3fffa) when
`update()` runs (`light.dm:24-28,247-306`). The preview therefore supports
**profiles**: a path prefix plus the variables to read.

### 2.6 Opacity

`opacity` is a built-in atom variable. Turfs derive `directional_opacity` from
their own opacity and from movable `opacity_sources`
(`lighting_turf.dm:140-160`); the lighting code only treats a turf as opaque
when all four cardinals are blocked (`IS_OPAQUE_TURF`). BYOND `view()` itself
blocks on any opaque atom. The preview uses one boolean per tile.

## 3. Faithfulness targets and known deviations

Targets, in priority order:

1. Same falloff, cone, colour mixing, normalisation and 1/64 rounding per corner
   as sections 2.1-2.2 (unit-tested against hand-computed values).
2. Same light-vs-dark layout around walls and doorways: corners reached only
   through transparent neighbours (the "wall face is lit" behaviour).
3. Same ambient and starlight composition and the 0.10 floor.

Known deviations (all intentional, all reported in the UI help text):

| area | game | preview |
| --- | --- | --- |
| visibility | BYOND `view()` | recursive shadowcasting over opaque tiles, square radius; edge cases differ by a corner |
| opacity | per-atom, directional border opacity | one bool per tile (turf or any opaque object); `ON_BORDER` objects ignored |
| overlay lights | separate plane, additive mask | approximated as corner lights (2.5) |
| light height/multi-z | z-levels bleed through transparent turfs (`lighting_source.dm:483-510`) | single level, `z = 0` |
| range | unbounded | clamped to `MaxRange` (default 16) for cost |
| set_light clamp | `set_light` raises range in (0,1.5) to 1.5 (`lighting_atom.dm:13-14`) | mapped static values used as-is |
| runtime state | power, bulbs, nightshift, alerts | all lights assumed on and powered |
| movables with a non-turf `loc` | held/contained lights re-parent | only atoms placed on a tile |
| negative output | colour matrices clamp | clamped to 0 |
| mob vision | cutoffs, night vision | none, fixed 0.10 floor |
| pixel offsets | `get_visual_offset` includes more terms | `pixel_x`/`pixel_y` only |

## 4. Inputs read from the environment and map

Everything comes through the existing variable lookup, which returns DM source
text with inheritance (`internal/dmapi/dmvars` `Variables.Value`). The Rust
parser expands macros and evaluates constants before values reach Go, so
`COLOR_WHITE` arrives as `"#FFFFFF"` and `NORTH` as `1`. Symbolic leftovers
(for example an unresolved `LIGHT_COLOR_X`) are reported as **unparsable** and the
atom is skipped with a reason, never guessed.

Per atom (defaults from `_atom.dm:69-92`):

| var | default | use |
| --- | --- | --- |
| `light_system` | 1 | filter; 0 skips |
| `light_range` | 0 | <= 0 skips |
| `light_power` | 1 | 0 skips; negative subtracts |
| `light_color` | `#ffffff` | rgb multipliers; alpha ignored |
| `light_on` | TRUE | false skips |
| `light_angle` | 360 | cone width |
| `light_dir` | NORTH | cone centre (overlay cones use `dir`) |
| `light_height` | 1 | falloff term |
| `light_flags` | 0 | bit 4 ignores pixel offsets |
| `pixel_x`, `pixel_y` | 0 | emitter shift, `/ 32` |
| `opacity` | FALSE | occluder |

Per area: `static_lighting` (TRUE), `base_lighting_alpha` (0),
`base_lighting_color` (white). Per turf: whether the type descends from
`/turf/open/space`.

Extraction reports one of: emits, not a light (with reason), or unparsable (with
reason). The UI should aggregate unparsable reasons into a single dismissible
notice rather than one log line per atom.

## 5. Ambient sources

- Area base lighting: `color * alpha / 255`.
- `static_lighting` off: tile drawn from ambient only.
- Starlight: toggle plus defaults range 2, power 1, colour #8589fa, height -0.5.
  Starlight lights come from space tiles that touch non-space, as in game.
  A `space_lit` tile in an area without base lighting shows starlight colour.
- "Ambient override": optional uniform minimum added to every tile for readability
  (preview-only, off by default).

## 6. Occlusion model

Corner-based like tg, not a per-tile ray test:

1. Run shadowcasting from the source tile over a square of radius
   `ceil(range + visual_offset)`; opaque tiles are visible but block beyond.
2. For each visible **non-opaque** tile, collect its four corners (deduplicated).
3. Evaluate falloff at each corner independently; there is no per-corner ray.

This reproduces the in-game look (lit wall faces, light leaking around door
frames by one corner) at a cost of O(range^2) per source. A per-tile model was
rejected because tile-level values cannot express the diagonal gradients that the
four-corner interpolation produces.

## 7. Output

`lighting.Result` stores raw `RGB` sums for a `(W+1) x (H+1)` corner lattice.
`Result.Tile(level, x, y)` returns the four displayed values (SW, SE, NW, NE):
`max(clamp01(corner + ambient), cutoff)`, with corner light dropped on tiles
without a lighting object.

Overlay (outside this task, for the render integration):

- Multiply the map by the tile colour, interpolated across the tile, with an
  adjustable **darkness strength** `s` in 0..1: `final = lerp(1, light, s)`.
  `s = 0` shows the map unchanged; `s = 1` is the game.
- A **Show light sources** mode draws a marker at each emitter, a ring at its
  range, and the cone edges for directional lights, plus skipped-atom markers on
  hover. Colours and glyphs for this overlay are chosen by the human.

## 8. Performance

Measured on the pure model (i7-9750H, `go test -bench`, 255x255 grid, 1 in 6
tiles opaque, 500 sources with ranges 5-9, a third of them 170 degree cones):

| operation | time | allocations |
| --- | --- | --- |
| full `Compute` | about 10 ms | 1.9 MB, 13 allocs |
| `Update` after a one-tile opacity edit | about 1.5 ms | 0.3 MB |
| `Tile` for all 65,025 tiles | about 12 ms | 0 |

Design:

- Compute on a worker per level, never on the UI thread. The worker input is an
  immutable snapshot: dimensions, opacity bits, ambient, space and unlit flags,
  and the source list. The result is published by swapping a pointer.
- Cache by `(level, map view version, preferences version)`. Any change to the
  displayed map increments the view version, which invalidates the cache entry.
- Incremental recompute: the producer reports a dirty rectangle (tile edits, plus
  old and new positions of moved sources). `Result.Update` re-renders the corners
  within twice the maximum reach of that rectangle and is bit-identical to a
  full compute (randomised test with a fixed seed).
- Bounds: `MaxRange` (default 16) clamps source cost; the shift clamp is 3 tiles;
  the source list is capped by the UI (suggest 5,000) with a visible warning.
- Edit bursts (drag-paint) coalesce into one dirty rectangle per frame.
- Texture upload: one RGBA8 texture per level chunk of the render cache
  (workplan N says one texture per level chunk), updated by sub-rectangle.

Expected cost at the 255x255 limit is small enough that a single worker per
level is sufficient; no GPU path is proposed.

## 9. UI (not implemented here)

- `View > Lighting Preview` toggle, persisted as a per-user preference.
- Preferences: darkness strength (0..100%, default 70%), ambient override
  (off / 0..50%), starlight on/off and its range/power/colour, show light
  sources, include overlay lights, max range, include profiles
  (wall fixtures).
- Status line: "Lighting preview (approximate)" with source count and number of
  skipped atoms.
- The toggle must not enter undo history, the dirty flag, saved state, or any
  collaboration message.

## 10. Testing strategy

Implemented in `internal/aphelion/lighting`:

- Falloff and cone against hand-computed tg values, including the unsquared
  height term, `max(1, range)`, the soft 30 degree edge, and the 350 degree wrap.
- Single-source corner values after 1/64 rounding.
- Opacity: wall shadow, doorway pass-through, wall face lit.
- Colour mixing and channel normalisation, negative power, clamps, cutoff floor.
- Ambient, `static_lighting` off, starlight derivation and `ResolveAmbient`.
- Overlay-light approximation, `MaxRange`, emitter shift.
- Extraction: defaults, every skip reason, colour forms, systems, directions,
  pixel shift, the fixture profile and its path boundary.
- Incremental update equals full compute over 150 random edits, fixed seed.
- Benchmarks for the three operations above.

Still to do with integration: golden fixtures from real MiniStation and
DeltaStation2 environments, comparison screenshots against in-game captures
(a human review), and a shipped-entry-point test that toggling the preview does
not change the saved map bytes or operation history.

## 11. Rollout

1. Pure model (this task).
2. Adapter from `dmmap`/environment to `lighting.Level` and sources, with a
   skipped-atom report.
3. Worker, cache and dirty-rect plumbing.
4. Render overlay and View menu entry.
5. Preferences, then measurement on MiniStation and DeltaStation2 for workplan
   decision N1.

## 12. Open decisions for the human

| ID | Question | Recommended default |
| --- | --- | --- |
| N1 | Proceed past the spike into render integration? | Yes, after step 2 measurements on both stations |
| N2 | Default darkness strength | 70%, so the map stays readable |
| N3 | Include `/obj/machinery/light` and other profiled types? | Yes; without them station lighting is mostly empty. Extend profiles only with a documented source line |
| N4 | Treat fixtures as always on? | Yes, with a preference to hide fixtures with `status` not OK later |
| N5 | Include overlay lights (systems 2-4)? | Yes, labelled approximate; they rarely appear on mapped atoms |
| N6 | Starlight defaults | On, range 2, power 1, #8589fa; configurable |
| N7 | Max light range | 16 tiles; warn above |
| N8 | Show the 0.10 game floor? | Yes by default; allow 0 for debugging |
| N9 | Multi-z bleed | Out of scope for v1 |
| N10 | Source markers, ring and cone-edge visuals | Human-authored; engineering supplies hooks only (art rules in `generated-and-external-assets.md`) |
| N11 | Show skipped atoms | Aggregated count in the status line, details on demand |
| N12 | Reuse upstream code | None reused; this is new Aphelion-owned code, so no licence review is triggered. SpacemanDMM `dmm-tools` has no lighting pass |
| N13 | Where profiles live | Built-in list now; later a project config file |
| N14 | Grid fidelity for directional-opacity objects (windoors, border windows) | Ignore in v1 |

## 13. Integration status (2026-10-09)

Implemented with decisions N1-N14 as approved: adapter and scheduler in
`internal/aphelion/lighting/maplight`, GL multiply pass in
`internal/app/render/lighting.go`, preferences in `internal/app/prefs/lighting.go`,
pane glue in `pmap/lighting_markers.go`, View > Lighting Preview.

- Capture runs on the UI thread in budgeted steps (shared frame allowance) from an
  `atomInfo` cache keyed by prefab pointer; the worker only sees a cloned `Level`.
- Dirty regions come from `Render.SetTileObserver`, called from `UpdateBucketV`, so
  every display write that already repaints the map also feeds the preview without
  touching collaboration code. A `MapViewVersion` change with no observed region
  falls back to a full recompute. Results are fenced by epoch (map, level,
  environment pointer, size, compute options) and a monotonic job sequence.
- At most one job runs and one is pending; a result for the current key is shown
  even if newer edits are already queued (the queued job follows immediately).
- Known gaps: the hovered-unit highlight is drawn before the pass and is dimmed;
  `snapshot()` clones the whole level on the UI thread per job (about 0.7 ms at
  255x255) and could become copy-on-write; edits to a lower z-level do not light
  the active level (single-level model, N9).
