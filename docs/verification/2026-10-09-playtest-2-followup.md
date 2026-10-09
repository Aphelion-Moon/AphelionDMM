# October 9 play-test 2 follow-up: implementation evidence

Baseline `c6a6f662`. Rounds 1–6 are in `4a9a5738`, `8b3d10a0` and
`b281ed07`. The Meridian UI refresh that followed is uncommitted and recorded
in `docs/design/2026-10-09-ui-review.md`. Automated evidence only;
**nothing here has been exercised in an interactive desktop session.**

| Report | Cause and change | Evidence | Open human checks |
| --- | --- | --- | --- |
| Lighting overlay stuck bottom-left | `Translate2D(...).Mat4()` stores the camera shift in the z column. The brush shader emits z = 1; the lighting shader emitted z = 0, which dropped the shift. It now emits z = 1. | `render/lighting_test.go` `TestLightingPassFollowsCameraShift` failed before the fix (GL). | Pan and zoom with lighting on. |
| Map Lint opens as a narrow floating window | Saved `imgui.ini` layouts predate the panel. A one-time `MapLintDocked` migration docks it with Environment. Layout reset also docks it there. | `layout/maplint_dock_test.go`. | First launch with an existing `imgui.ini`. |
| Changelog header kerning | `configureFonts` shared one `FontConfig`, so each header font inherited the icon merge's `GlyphMaxAdvanceX` from the previous, smaller size (H1 was clamped to 16 px per glyph). Each font now gets its own config. U+2010–2027 and arrows were added to the glyph ranges (release headers use an em dash). | `window/fonts_test.go` failed before the fix (128 px instead of at least 187 px). | Visual check of the Changelog tab. |
| Fake pointer inside the remote cursor tile | Presence is tile-granular, so the arrow suggested a precision the protocol does not carry. Removed; the tile highlight and name badge remain, with the badge anchored to the tile corner. | `pmap/collaboration_presence_badge_test.go`. | Visual check. Exact sub-tile cursors would need a presence protocol extension. |
| Map Lint auto-resolve | `maplint.FixTile` plans fixes per tile. A candidate is kept only if the tile then has strictly fewer violations and no new ones. Kinds: duplicate removal, superseded-ancestor removal, subtype promotion (including sibling directional helpers), name capitalization, variable stripping, banned-object removal, and opt-in placement stripping. Turfs and areas are never removed. Names, descriptions, pixel offsets, dir and piping/cable layer are never stripped by default (pixel offsets and dir are stripped from areas). `Editor.ApplyLintFixes` replans from current authoritative state. It applies as one undoable local revision or one session operation and keeps stable IDs. Panel: per-kind checkboxes, per-rule counts, Apply, automatic rescan. | `maplint/fix_test.go`, `editor/lint_fix_test.go` (local undo, disabled kinds, session operation). Real data: Meridian-Rift rules, `tgstation.dme` and `MiniStation.dmm` as saved at 16:46. Defaults resolve 221 of 227 violations (97%). The remaining 5 telescreens with non-helper offsets and 1 barricade on an airlock are left for review. | Apply on a real map, then review the diff and save. |

## Round 2 (same day)

| Report | Change | Evidence |
| --- | --- | --- |
| Lighting line overlaps the status strip | The strip is positioned from `ContentRegionAvail`, but the line used the pane size. The line now anchors to the strip's recorded top edge. | `pmap/lighting_markers_test.go` |
| Replacing with a directional variant loses edits | Tile menu "Replace, Keep Edits". The selected prefab's own edits win. Old edits are kept unless they are undefined on the new type or equal its default (the redundant `dir`). | `editing/replace_keep_test.go`, `editor/instance_replace_keep_test.go` |
| No Save As; cannot save over a map | File > Save As... When the destination exists, a confirmation follows; the replace is fenced by the destination's disk state at confirmation and refused if the file changed. Choosing the map's own file is a normal save. A programmatic Save As still never overwrites. | `wsmap/save_test.go` `TestSaveAcknowledgementBoundaries` (GL) |
| No tab context menu | Right-click a tab: Close, Close Others, Close to the Right, Close All, Save, Save As, Copy Path, Open Containing Folder (reuses the existing `open.Run`). Actions are queued until after the workspace loop. | `cpwsarea/tab_menu_test.go` |

## Round 3: rotation and in-game look

**Rotation.** `aphelion/spritedirs` reads DMI direction counts from metadata only (`sdmmparser.ParseIconMetadata`). It is thread-safe and cached per icon, and active per environment.

- A type-declared `dir` now turns only when the sprite has more than one direction (the Dir slider's signal).
- Explicit `dir` edits, directional helpers and unreadable sprites behave as before.
- `/obj/machinery` and `/mob` are exempt: the real-map audit showed thermomachines and blast doors whose functional `dir` sits on a one-direction map sprite.
- Evidence: `editing/orientation_scope_test.go`, `spritedirs` (with `-race`).

**Map Lint editor audit.** Attached only to whole-map Scan and Apply, never to placement checks. It reports:

- edits equal to the type default;
- `dir` on a single-direction sprite (machinery and mobs excluded).

Fix kinds: "Strip edits equal to the default" and "Strip dir on single-direction sprites", both on by default.

- Real MiniStation: 289 findings (for example `dir` on plating, wood and carpet; `pixel_y = 0`), all resolved by the defaults.
- Evidence: `maplint/audit_test.go` and the editor lint tests.

**In-game look** (View → In-Game Look, Ctrl+I; Lighting Preview gained Ctrl+L). `aphelion/ingame` ports Meridian-Rift's rules for:

- bitmask smoothing, including object smoothing, map-edge borders, area limits and diagonal walls;
- cable links, including layers, banned links, the SMES/terminal exclusion and nodes;
- smart-pipe connections, including each atmos family's init directions, layers, colours, all-layer and all-colour flags, distro/waste layers, heat-exchange junctions and stub sprites.

How it renders:

- Chunk rebuilds resolve connected atoms against the live map. Any edit also dirties its 8 neighbours.
- A toggle rebuilds the pane's geometry.
- The choice persists in Editor preferences.
- A predicted state missing from its DMI keeps the mapper icon.

Real MiniStation check: every resolved atom was checked against its DMI's state list. Smart pipes (2,125), walls (1,457), cables (1,245), lattices, catwalks, tables, floors and windows: 0 missing states.

Known gaps:

- Mineral walls and stationary tanks keep their mapper icons. Their in-game icons come from `MAP_SWITCH` or greyscale generation, which the parsed environment cannot show.
- Diagonal wall underlays and pipe-component stub underlays are not drawn.
- Border-object and proc-filtered smoothing (railings) keep mapper icons.

Evidence: `ingame` tests and `chunk/ingame_test.go` (the shipped `inGameAppearance` path, including the missing-state gate).

## Round 4: spawners, brush, helpers, playtest

**Window spawners (in-game look).** Static structure spawners draw what `spawn_list` creates, each part with its own layer and colour, and picking a part selects the spawner. Spawned windows join neighbour smoothing. Dir-dependent hollow window ends, middles and directionals, and `flipped_table`, keep the mapper icon.

- Fixed: `PARSE_CAN_SMOOTH_WITH` turns on `SMOOTH_OBJ` when `canSmoothWith` holds an object group. Walls therefore smooth with windows, which the earlier port missed.
- Real MiniStation: 317 spawners give 634 parts, all present in their DMIs; 1,457 walls, 0 missing states.
- Evidence: `ingame` tests and the GL test `chunk/ingame_test.go`.

**Brush tool (key 8).** One tool replaces the separate utilities and disposals tools, at the user's request.

- It lays the configured bundle (default: supply layer 4, scrubbers layer 2, cable) and, optionally, a disposal pipe along a 4-connected route. Dragging back retracts the route.
- On release the run applies as one edit. If the disposal plan fails, nothing changes.
- Disposal connections port `pipe.dm` Initialize, so sorting junctions, y-junctions and multi-z trunks need no special cases. Unedited pipes (`dir = NONE`) face SOUTH.
- Evidence: `disposals` tests and `tools/brush_test.go`.

**Map Lint disposal network check.** Reports pipes open to nothing and trunks without a bin, chute or outlet.

- Real MiniStation: 87 pipe tiles, 13 findings. All inspected findings are genuine stray or disconnected segments.

**Mapping helper finder.** Right-click an object → Mapping Helpers.

- An index reads the helpers' own DM procs for `locate(<type>) in loc` and `for(var/<type>/x in loc)`.
- Real environment: 340 helper types. APC: 10 helpers; airlock: 236; air alarm: 13; mail sorting pipe: 37.
- Windoors list the airlock helpers too, because those helpers can be set to apply to windoors.
- Evidence: `helpers` tests and `tilemenu/helpers_test.go`.

**Playtest panel** (Window → Playtest).

- Writes `data/next_map.json`, reusing the station's `_maps/*.json` when one names the map.
- Compiles only when `.dm`, `.dme`, `.dmf` or `.dmi` files are newer than the `.dmb`, or when "Always recompile" is on.
- Starts DreamDaemon on a local port, waits for it to accept connections, then opens DreamSeeker on `127.0.0.1`.
- Uses fixed executables from one configured BYOND folder, with no shell.
- Evidence: `playtest` tests with `-race`, using a helper process in place of BYOND. **No real BYOND launch has been run.**

## Round 5: spawner placeholders, lighting, colour picker

**Window spawners drew error icons.** The retained draw cache recorded each unit's dependency as the instance's own `icon`. A spawned window's sprite comes from the window DMI, not the spawner's. When that DMI finished loading in the background, the batch was reused with its placeholder baked in. Units now carry the icon they draw (`unit.Icon()`), and the cache keys dependencies on it.

- Not a rules problem: on MiniStation, KiloStation, Tram, Snowglobe, Meta, Serenity, Ouroboros, Ocean Pubby, Delta, IceBox and Blueshift, every spawner part resolves to an existing state. The only missing predicted states are the known `MAP_SWITCH` cases (mineral walls, grass, mushroom turfs, tanks), which the gate keeps as mapper icons.
- Evidence: `chunk/ingame_test.go` (GL) asserts the parts' drawn icons. The async-load timing itself is not reproduced by a test.

**Lighting.** As in game (`light.dm` `update()`), fixtures take range, power and colour from `brightness`, `bulb_power` and `bulb_colour`.

- The preview now also honours `color` (it overrides the bulb colour) and `status`: broken, burned or empty fixtures are dark and listed under skipped lights.
- Right-click → Turn Light Off / Turn Light On is one undoable edit. Ordinary lights switch with `light_on`. Fixtures switch with `status = LIGHT_EMPTY` and the `-empty` icon state, as the `/empty` subtypes do. A value equal to the type default removes the edit.
- Offered only for lights the type has on. Real MiniStation: 539 switchable lights (331 fixtures, 208 others), each in agreement with the preview's extraction. Without that restriction, 58,156 space tiles and some guns and welders would have offered "Turn Light On".
- Map Lint editor audit: `light_range`, `light_power`, `light_color` and `light_on` on a fixture are reported as overwritten in game. Opt-in fix "Move light edits on fixtures to bulb variables" (off by default, because it changes the in-game look): an existing bulb edit wins, and a value equal to the bulb default is dropped.
- Evidence: `lighting/switch_test.go`, `editor/instance_light_test.go`, `tilemenu/lightswitch_test.go`, `maplint/audit_test.go`.

**Colour picker.** A variable named `color`, `*_color` or `*_colour` shows a swatch beside its input, which opens a Paint-style dialog:

- the 48 basic colours and 16 custom colours (recent picks, saved in the variable editor's config);
- a hue/saturation picker with RGB, HSV and hex fields.

Nothing is written until OK. A pick keeps the original value's letter case and alpha digits. Matrices and null start from white and are replaced by a pick.

- Evidence: `colorvar` tests and `cpvareditor/colorpicker_test.go` (a headless imgui frame). The OK path writes through the same `setCurrentVariable` as the text input but is not driven by a test.

**Play-test screenshot requests**, checked against code: utility Brush (`tools/brush.go`), disposal routing in the Brush plus the Map Lint disposal check (`editor/lint_scan.go`), Mapping Helpers menu (`tilemenu/helpers.go`), spawner rendering (above), and the Playtest panel (`cpplaytest`). The Playtest panel has still never launched a real BYOND (`ByondBin` is empty in the current preferences).

## Round 6: Mapping Helpers tab

Play-test result: lighting and spawner visuals confirmed fine. The tile menu's Mapping Helpers submenu did not load for a public airlock (236 helpers).

- **Mapping Helpers tab** beside Prefabs and Search (also Window → Mapping Helpers). It docks with Prefabs once in existing layouts. It shows the helpers for the instance selected in Variables:
  - helpers already on its tile, as removable chips;
  - a search box where every word must match path, name or description;
  - category chips with counts. A branch with at least 8 helpers gets its own chip, the rest fold into General. A public airlock shows General 14 and `airlock/access` 222.
  - a tree with single toggles before groups. Each helper is a checkbox ticked when it is on the tile; ticking adds it and unticking removes it, as one operation. Searching opens every matching group.
  - Family roots with members (the plain `airlock` helper) are not offered.
- **Tab highlight:** while the selected object has helpers, the tab is tinted toward the theme accent (inactive 45%, active 22%). Colours are pushed around that window's `Begin`, which docked tabs read.
- **Tile menu:** "Mapping Helpers (N)..." selects the object and opens the tab, replacing the nested submenu.
- Layout follows common large-catalogue patterns: a live filter that keeps category context and opens matches, collapsible groups, chips with counts, and applied items surfaced first.
- Evidence: `helpers/browse_test.go`, `cphelpers/panel_test.go` (a headless imgui frame, and removal through the editor), `layout/tab_highlight_test.go`. Adding through the panel reuses the earlier tile-menu code and is not driven by a test.
- **Fix (play-test: group arrows flickered and did not open):** group IDs came from the node's address, and the tree is rebuilt every frame, so a clicked group's open state was lost on the next frame. IDs are now the group's path (`Node.Key`, with `###` so the count can change). A search or category change opens matching groups once, instead of forcing them open every frame. Evidence: `cphelpers/tree_state_test.go` simulates the click and fails with the old ID; `helpers/browse_test.go` checks the keys stay stable.

## Gates run (Windows)

- `go build ./...`: clean.
- `go vet` on the changed packages: clean.
- `golangci-lint run` (2.12.2) on the changed packages: 0 issues.
- `APHELIONDMM_GL_TEST=1 go test` on the changed packages: pass.
- `APHELIONDMM_GL_TEST=1 go test ./... -count=1`: every package passed except `cmd/apheliondmm-loadtest`. Its `TestConcurrentCommandAppliesRealServiceStream` hit its 10 s load deadline during the parallel run. It passed when rerun alone and touches no changed code. Treat it as a load-timing flake, not as qualified.
- Not run: `task verify`, the Rust gates, `task build`, and a desktop smoke run of `dst/AphelionDMM.exe`.
- Round 3:
  - `golangci-lint` on `./internal/aphelion/... ./internal/app/...`: 0 issues.
  - `APHELIONDMM_GL_TEST=1 go test ./...`: all packages passed except the known `TestSessionClientRecoversDuringIndependentLoad/sqlite` load-timing flake, which passed alone.
  - `task build` (`RUST_TARGET=1.82.0-x86_64-pc-windows-gnu`) produced `dst/AphelionDMM.exe`. It has not been launched.
- Round 4, after commit `4a9a5738`:
  - `golangci-lint` on `./internal/aphelion/... ./internal/app/...`: 0 issues.
  - `APHELIONDMM_GL_TEST=1 go test ./... -count=1`: every package passed, after fixing a Brush teardown panic, a missing Brush case in a test helper, and a timing-dependent hang in the save test (it pumped a native error dialog).
- Round 5:
  - `go build ./...` and `go vet ./internal/aphelion/... ./internal/app/...`: clean.
  - `golangci-lint` on `./internal/aphelion/... ./internal/app/...`: 0 issues.
  - `APHELIONDMM_GL_TEST=1 go test ./... -count=1`: every package passed.
  - None of the Round 5 changes have been exercised in the running desktop app.
- Round 6: build, vet and `golangci-lint` (0 issues) are clean, and `APHELIONDMM_GL_TEST=1 go test ./... -count=1` passed for every package. The tab has not been used in the desktop app.
