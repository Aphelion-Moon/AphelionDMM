# AphelionDMM v.a.8

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. SpaiR and the StrongDMM contributors made this editor possible; consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha responds to the October 7 collaboration play-test. It fixes hosted disconnects and undo problems and makes rotate and mirror preserve map data correctly. It also adds join-without-a-map, map-lint placement guards, a shuttle docking overlay and an approximate lighting preview.

## Changes since v.a.7

### New

- **Lighting preview** (**View → Lighting Preview**): an approximate, editor-only light map following the codebase's lighting rules. It covers light falloff, directional cones, colour mixing, starlight, area base lighting and wall-mounted light fixtures.
  - Settings in **Preferences → Editor**: darkness strength, starlight, overlay lights, and light-source markers.
  - The status line counts lights that could not be read, with details.
  - The preview never changes map data.
- **Join without a local map:** with only the environment (`.dme`) loaded, joining a session opens the shared map in a new untitled tab. **Save** asks where to write it.
  - Joining is refused if your environment does not match the session's.
  - Hosted sessions advertise the host's `.dme`, environment hash, git branch and commit. The session browser compares them with your checkout and offers copyable `git` commands. The editor never runs them.
- **Map-lint placement guards:** the editor reads the codebase's own `tools/maplint/lints/*.yml` rules.
  - Add, Fill, Random Fill and Move skip an exact duplicate (for example a second identical window, cable or pipe on a tile).
  - Other rule hits still place, with a warning and a one-click **Replace existing**.
  - The new **Map Lint** panel (Window menu) scans the open map.
- **Docking-port overlay** (**View → Docking Ports**): draws each shuttle docking port's footprint. It warns when the footprint crosses the map edge, overlaps another port, or (for stationary docks) covers non-space turfs.
- **Collaboration cursors:** a pointer and initials badge, with a chosen colour from a 12-colour palette in the collaboration panel.
- **Unsent changes prompt:** after a disconnect, or on leave or close with retained drafts, one prompt offers **Export all**, **Discard all** (exports first by default), **Refresh all** or **Keep for later**.
- **Drag and drop:** drop `.dme`, `.dmm` or `.tgm` files onto the window. A map from a different repository offers to switch to its own environment.
- **Key-length warning:** a save that would lengthen tile keys, changing every key in the file, now asks first.

### Changed

- **Environment visibility:** the show/hide checkbox is always next to each type. **Alt+click** on the icon toggles it, and hidden types are dimmed.
  - **F** now toggles visibility of the selected type. The old Types Filter mode is removed.
- **Areas:** shown areas are drawn as a translucent overlay (35% by default). The base area (usually `/area/space`) is hidden by default. Both are set in **Preferences → Editor**.
- **Logs:** routine render logging is summarised, so session logs are far smaller. Collaboration joins, disconnects and reconnects are now logged.

### Fixed

- **Hosted disconnects:** the server could close an editor with "presence rate exceeded" after a busy edit burst. Two causes are fixed:
  - Every cursor update was reauthorized against the database on the connection's read loop. Presence and ping no longer reauthorize per message; edits still do.
  - Excess cursor updates are now dropped instead of closing the connection.
  - A rate-limited connection now reconnects automatically and keeps your unsent edits.
- **Undo during collaboration:** undo or redo while a move was still awaiting the server could fail with a precondition error. They now wait for outstanding edits, and are refused while a drag is open.
- **Rotate and mirror:** these no longer write a direction onto turfs, areas or objects that had none. Mirror now switches directional helper types (for example `…/directional/west` ↔ `east`). Rotating a held object updates the selected type.
- **Pixel offsets:** Shift-dragging an object with the Move tool shows the offset live again. The same fix restores modifier keys during every canvas drag.
- **Empty bulk actions:** a bulk action matching nothing is now a quiet no-op instead of an error.
- **Opening a map:** opening a map while another repository's environment is loaded now offers that map's own `.dme`.

## Hosted service

The hosted PostgreSQL schema moves from 5 to 6. Migration 6 adds the nullable session repository columns and runs automatically on service start. The protocol adds an optional `cursor_color`, and older editors are not supported against this service. Update editors and the service together.

## Downloads

Choose `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and their SHA-256 checksum files are also included.

All packages target x86-64. The macOS package targets Intel Macs; there is no native Apple Silicon package.

## Alpha notes

- Keep maps in version control and retain backups.
- Automated tests only: these changes pass the automated Windows, Linux, macOS and collaboration gates. They have not yet been exercised in a human desktop session or a two-person hosted play-test. See [the verification record](../verification/2026-10-09-playtest-followup.md).
- The lighting preview is approximate. It has not yet been compared with in-game screenshots. The hovered-object highlight is dimmed while it is on.
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS.
- Download updates manually. Production signing for the in-app updater is not configured. GitHub build provenance is separate from application/update signing.

**[Full changes since v.a.7](https://github.com/Aphelion-Moon/AphelionDMM/compare/v.a.7...v.a.8)**
