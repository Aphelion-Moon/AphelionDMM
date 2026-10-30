# AphelionDMM v.a.12

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. SpaiR and the StrongDMM contributors made this editor possible; consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha fixes a map that could no longer be saved and an editor that went blank after selecting an unknown type, and adds mapper-requested tools: variable copy and paste, selection export, Go to Definition on the map, neon carpets in the in-game look and lava lighting on planet maps. The collaboration protocol stays at v2: v.a.10, v.a.11 and v.a.12 editors and services can share sessions.

## Changes since v.a.11

### Fixed

- **"Unable to save the map: staged map dimensions are (0,0,0)":** typing a lone `"` into a variable stored an unterminated string. The saved file then could not be read back, so every save was refused (the original map on disk was never touched).
  - The variable editor now refuses any value that would not survive a save unchanged, and says why in the panel. A lone `"` becomes the empty string `""`.
  - The map reader now reports an unterminated definition instead of reading the rest of the file as part of it.
  - If a save is still refused this way, the error names the type, variable, value and a tile that uses it, so it can be found and fixed or undone.
- **Editor went blank and the close prompt never appeared:** selecting an object whose type the environment no longer defines (for example with Pick on an old map) made the map pane's quick-edit controls fade every panel a little more each frame. Within a fraction of a second every panel and dialog was invisible. The "Save Workspace?" prompt still opened, but invisibly, and it blocked the editor.
  - The quick-edit controls no longer fade the interface.
  - Every frame now starts at full opacity, so a similar fault elsewhere cannot build up.
  - Pressing the window close button repeatedly no longer queues several identical save prompts.
- **Hiding areas left unknown areas visible:** hiding `/area` (or a branch such as `/area/engine`) now also hides instances of undefined types under it, which draw with the missing-icon checkerboard. Erase, delete, copy and screenshots follow the same visibility. This applies to `/turf`, `/obj` and `/mob` too.
- **Environment tree selection:** selecting an undefined type no longer forces its ancestor nodes open on every frame. `/area/hall` no longer counts as a parent of `/area/hallway`.

### Added

- **Copy and paste variables:**
  - Each variable row has a copy button. Select another object and a paste button appears on the same variable.
  - **Copy changed** copies every variable that differs from the type's default. **Paste** applies them to the selected instance or prefab as one undoable edit, such as moving a normal airlock's access and names onto a glass airlock.
  - Variables the target type does not declare, or cannot edit, are skipped and listed.
- **Export selection to a new map:** select an area and choose **Selection… > Export selection to new map...**. The new map is the size of the selection, with its lower-left tile at (1,1).
  - It contains every instance with its variables, including unknown types. Unselected cells of an irregular selection get the base area and turf.
  - It keeps the source map's format, line endings and key length, and uses the same validated save path as Save.
  - It refuses to overwrite the open map.
- **Go to Definition on the map:** right-click a tile, open an instance's submenu, and choose **Go to Definition** to open its type's source in your configured code editor. The option is disabled for undefined types. Source files outside the environment folder are refused.
- **Neon carpets in the in-game look:** with View > In-Game Look on, neon carpets draw their coloured pattern overlay (`neon_icon`, `neon_icon_state`, `neon_color`, alpha and smoothing junction) the way the game does. The emissive glow is not drawn.
- **Lava lighting on planet maps:** in the lighting preview, lava turfs bordering non-lava light their surroundings the way the game switches them on, and lava with a light mask is shown fully lit. On /tg/'s Lavaland this adds about 3,300 light sources where only 127 were used before.
  - Outdoor areas without base lighting stay dark, as they are in game.
  - Light between z-levels is not modelled.

## Hosted service

No protocol or schema change.

## Downloads

Choose `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and their SHA-256 checksum files are also included.

All packages target x86-64. The macOS package targets Intel Macs; there is no native Apple Silicon package.

## Alpha notes

- Keep maps in version control and retain backups.
- Automated tests only. The lighting changes were also run headlessly against an upstream /tg/ checkout's Lavaland, IceBox and MetaStation maps. None of these changes has been exercised in the desktop editor.
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS.
- Download updates manually. Production signing for the in-app updater is not configured.

**[Full changes since v.a.11](https://github.com/Aphelion-Moon/AphelionDMM/compare/v.a.11...v.a.12)**
