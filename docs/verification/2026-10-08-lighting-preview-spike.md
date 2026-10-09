# Lighting preview spike (research only)

Date: 2026-10-08. No repository code was edited. Only this file was created.

## 1. SpacemanDMM / dmm-tools: no lighting pass

- Upstream repo: https://github.com/SpaceManiac/SpacemanDMM (LICENSE: GNU GPL v3, fetched from https://raw.githubusercontent.com/SpaceManiac/SpacemanDMM/master/LICENSE).
- `crates/dmm-tools/src/` contains `lib.rs`, `dmi.rs`, `dmm.rs`, `icon_cache.rs`, `minimap.rs`, and `render_passes/`. Modules declared in `lib.rs`: `dmi`, `dmm`, `icon_cache`, `minimap`, `render_passes`. No lighting module.
- `crates/dmm-tools/src/render_passes/` (GitHub contents listing, fetched via the raw/tree pages): `icon_smoothing.rs`, `icon_smoothing_2020.rs`, `icon_smoothing_2025.rs`, `mod.rs`, `random.rs`, `smart_cables.rs`, `structures.rs`, `transit_tube.rs`.
- `render_passes/mod.rs` declares a `RenderPass` trait and passes `HideSpace`, `HideAreas`, `HideInvisible`, `Overlays`, `Pretty`, `Wires`, `Pipes`, `WiresAndPipes`, `FancyLayers`, plus re-exports for icon smoothing, random, smart cables, gravity gen, spawners and transit tube. None are lighting, shading or light-source passes. The word "light" does not appear in the fetched `mod.rs` or `lib.rs` content.
- Limit: the GitHub code-search API (`api.github.com/search/code`) returned HTTP 401 without auth, so a repo-wide `light` grep was not possible. Only `lib.rs` and `mod.rs` were read in full. The other pass files were not individually fetched. The conclusion "no lighting pass" is therefore based on the module list and pass registry, not a full-text grep.
- Conclusion: the minimap renderer is a tile-icon compositor (smoothing, hiding, wires/pipes overlays). It has no lighting simulation that an editor could reuse.

## 2. Local vendored copy (read-only check)

- Path: `third_party/sdmmparser/src/vendor/spacemandmm-0290db5`, pinned to suite-1.11, revision `0290db5c752fa292f9824521515b603a2afd11dc` (per `UPSTREAM.md`).
- Vendored crates: `crates/builtins-proc-macro`, `crates/dreammaker`, `crates/interval-tree`. `dmm-tools` is NOT vendored, so the minimap renderer is not available locally.
- The vendored `dreammaker` has Aphelion-owned source-tracing edits marked `APHELION`. Upstream GPL-3.0 authorship is retained.

## 3. Static light variables on atoms (Meridian-Rift, read-only)

Declarations in `code/game/atom/_atom.dm`:
- `light_system`: line 69, default `COMPLEX_LIGHT`
- `light_range`: line 71, default 0
- `light_power`: line 73, default 1
- `light_color`: line 75, default `COLOR_WHITE`
- `light_on`: line 77, default TRUE
- `light_flags`: line 79
- `light_render_source`: line 83 (OVERLAY_LIGHT only)
- `light_angle`: line 88, default 360 (COMPLEX_LIGHT only)
- `light_dir`: line 90, default NORTH
- `light_height`: line 92, default `LIGHTING_HEIGHT`
- Runtime light datum: `light` at line 94, `light_sources` at line 96.

`code/__DEFINES/lighting.dm` defines the light systems:
- `NO_LIGHT_SUPPORT` 0 (line 2), `COMPLEX_LIGHT` 1 (line 4), `OVERLAY_LIGHT` 2 (line 6), `OVERLAY_LIGHT_DIRECTIONAL` 3 (line 8), `OVERLAY_LIGHT_BEAM` 4 (line 10).
- `LIGHTING_HEIGHT` 1 (line 33), `MINIMUM_USEFUL_LIGHT_RANGE` 1.5 (line 26), `LIGHT_RANGE_FIRE` 3 (line 45).

Setters and runtime engine:
- `code/modules/lighting/lighting_atom.dm`: `set_light` line 3 (the public entry point), `set_light_power` 110, `set_light_range` 121, `set_light_color` 132, `set_light_on` 143, `set_light_flags` 154, `set_light_render_source` 167, `set_light_angle` 180, `set_light_dir` 191, `set_light_height` 203.
- `code/modules/lighting/lighting_source.dm`: the light source datum holds `light_power` (line 24), `light_range` (26), `light_color` (28), `light_dir` (38, default NONE), `light_angle` (40, default 360), `light_height`, and the luminosity fields.
- Signals: `code/__DEFINES/dcs/signals/signals_atom/signals_atom_lighting.dm` (`COMSIG_...` for light set events, lines 6-44).

Examples of authored values (for a preview's sample data):
- `code/modules/art/statues.dm:101-103`: range 3, power 0.7, colour `LIGHT_COLOR_NUCLEAR`
- `code/game/turfs/open/lava.dm:15-18`: range 2, power 0.75, colour `LIGHT_COLOR_LAVA`, `light_on = FALSE`
- `code/game/turfs/open/floor/light_floor.dm:9`: range 5
- `code/game/turfs/open/space/space.dm:64-68`: starlight defaults (power 1, range 2)

Colours are typically `LIGHT_COLOR_*` macros or hex strings. Use `code/__DEFINES` to resolve them.

## 4. Opacity declaration

- `opacity` is a built-in BYOND atom variable. It is changed through the Aphelion-readable proc `/atom/proc/set_opacity(new_opacity)` at `code/modules/lighting/lighting_atom.dm:70`. It is also edited through VV at `code/game/atom/atom_vv.dm:284-285`.
- Turf occlusion: `code/game/turfs/turf.dm:75-78` defines `directional_opacity` (`var/directional_opacity = NONE`) and `opacity_sources` (a lazy list of movable atoms). Line 184-185 sets `directional_opacity = ALL_CARDINALS` when `opacity` is TRUE.
- Opacity is a boolean on the atom. There is no per-atom alpha. Opacity sources on movables also block vision.

## 5. Design implications for an approximate overlay

- Use the static fields above (`light_range`, `light_power`, `light_color`, `light_on`, `light_angle`, `light_dir`) as the per-atom input. Treat `light_system` as a filter: only `COMPLEX_LIGHT` atoms and `OVERLAY_LIGHT*` atoms emit light in-game.
- Use `opacity` (boolean, plus `directional_opacity` on turfs) as the occluder input for a 2D shadow pass. A simple raycast to the opaque tiles is a reasonable approximation.
- Keep this in the editor only. It is a preview, not a durable map state, so it does not cross the collaboration protocol (AGENTS.md: presence and preview are separate from operations).
- Do not reuse SpacemanDMM code for lighting; there is none. Any implementation is new Aphelion-owned code under `internal/aphelion/`.

## Key URLs

- https://github.com/SpaceManiac/SpacemanDMM
- https://raw.githubusercontent.com/SpaceManiac/SpacemanDMM/master/LICENSE
- https://raw.githubusercontent.com/SpaceManiac/SpacemanDMM/master/crates/dmm-tools/src/render_passes/mod.rs
- https://raw.githubusercontent.com/SpaceManiac/SpacemanDMM/master/crates/dmm-tools/src/lib.rs
- https://api.github.com/repos/SpaceManiac/SpacemanDMM/contents/crates/dmm-tools/src/render_passes
