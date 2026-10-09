# AphelionDMM v.a.9

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. SpaiR and the StrongDMM contributors made this editor possible; consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha responds to the second October 9 play-test and refreshes the interface. It adds an in-game look for connected atoms, a utility Brush, Map Lint auto-fixes, a Mapping Helpers tab, a Playtest panel, light switching and a colour picker. The new Meridian theme follows the Meridian Rift website.

## Changes since v.a.8

### New

- **In-game look** (**View → In-Game Look**, **Ctrl+I**): draws connected atoms as they appear in game.
  - Covers wall and window smoothing, power cables and smart pipes. Window spawners show the grille and window they create.
  - Atoms whose in-game icon the editor cannot read keep their mapper icon.
  - The preview never changes map data.
- **Brush tool** (key **8**): lays the configured utility bundle (by default supply and scrubber pipes and a cable) along a dragged route, and optionally a disposal pipe. Dragging back retracts the route. The run applies as one edit.
- **Map Lint auto-fix:** the Map Lint panel can fix violations by kind, as one undoable edit. Kinds include duplicates, superseded atoms, directional subtypes, name capitalisation, banned variables and banned objects.
  - A whole-map scan also reports edits equal to the type default, `dir` on single-direction sprites, broken disposal networks, and `light_*` edits on wall light fixtures. Fixtures ignore those edits in game.
- **Mapping Helpers tab**, beside Prefabs: lists the mapping helpers that act on the selected object, read from the helpers' own code.
  - Search box, category chips and a grouped tree. A public airlock has 236 helpers.
  - Ticking a helper adds it to the tile; unticking removes it. Helpers already on the tile appear as chips.
  - The tab is tinted while the selected object has helpers. The right-click menu entry opens it.
- **Playtest panel** (**Window → Playtest**): writes `data/next_map.json`, compiles only when sources are newer than the `.dmb`, then starts DreamDaemon and connects DreamSeeker on `127.0.0.1`. Set your BYOND folder in the panel.
- **Light switch:** right-click a light → **Turn Light Off / On** as one undoable edit. Ordinary lights use `light_on`. Wall fixtures use the bulb status, as the `/empty` subtypes do.
- **Colour picker:** colour variables (`color`, `*_color`, `*_colour`) show a swatch that opens a Paint-style dialog: basic colours, recent colours and a spectrum. Nothing is written until **OK**.
- **Save As** (**File → Save As...**), with confirmation before replacing an existing file.
- **Map tab menu:** right-click a map tab to close tabs, save, copy the path or open the folder.
- **Replace, Keep Edits:** replaces an instance with the selected prefab and carries over its meaningful variable edits.

### Changed

- **Meridian theme:** a new default look taken from the Meridian Rift website.
  - Warm dark surfaces, a cyan accent and status colours.
  - Text contrast is tested against accessibility (WCAG) levels.
  - **IBM Plex Mono** sets type paths, variable values and coordinates.
  - **Preferences → Interface → Theme** offers **Classic** for the original look; **Compact Layout** tightens spacing.
- **Map status bar:** the status line is split into coordinates, tool state, action, target, size and warnings.
- **Lists and menus:** prefab rows take one line. Long type paths in the right-click menu are shortened in the middle, with the full path on hover. Object-specific actions are grouped.
- **Lighting preview:** honours a fixture's `color` override and its bulb status, so broken, burned or empty fixtures are dark.
- **Rotation:** an object's declared `dir` now turns only when its sprite has more than one direction. Machinery and mobs are exempt.
- **Remote cursors:** the pointer inside a collaborator's tile is removed. Presence is per tile, so the highlight and name badge remain.
- **Environment panel:** the visibility controls take one row.

### Fixed

- **Lighting preview:** the overlay no longer stays at the bottom-left corner when the view moves, and the lighting line no longer overlaps the status bar.
- **Window spawners:** spawned windows could show error icons after their icon file finished loading.
- **Map Lint panel:** it opened as a narrow floating window with saved layouts; it now docks beside Environment once.
- **Changelog headings:** large headings were squeezed together.
- **Mapping Helpers:** group arrows flickered without opening.

## Hosted service

No hosted service, protocol or schema changes. Editors v.a.8 and v.a.9 work with the same service.

## Downloads

Choose `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and their SHA-256 checksum files are also included.

All packages target x86-64. The macOS package targets Intel Macs; there is no native Apple Silicon package.

## Alpha notes

- Keep maps in version control and retain backups.
- Automated tests only. These changes pass the automated gates, and the Windows build was tested with GL. Most changes have not been exercised in a full desktop session. The Playtest panel has not launched a real BYOND install. See [the verification record](../verification/2026-10-09-playtest-2-followup.md) and [the UI review](../design/2026-10-09-ui-review.md).
- The in-game look and lighting preview are approximations. Mineral walls, tanks, and border-object and proc-filtered smoothing (such as railings) keep their mapper icons.
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS. Your layout gains the new tabs once.
- Download updates manually. Production signing for the in-app updater is not configured.
- IBM Plex Mono is © IBM Corp. and licensed under the SIL Open Font License 1.1. The licence is in `internal/rsc/font/plex/`.

**[Full changes since v.a.8](https://github.com/Aphelion-Moon/AphelionDMM/compare/v.a.8...v.a.9)**
