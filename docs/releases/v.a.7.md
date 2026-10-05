# AphelionDMM v.a.7

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. SpaiR and the StrongDMM contributors made this editor possible; consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha adds the **Path Migration** resolver for maps that use types the loaded environment no longer defines, plus rendering and authoring fixes and two editing performance improvements.

## Changes since v.a.6

### New

- **Path Migration:** opening a map with type paths the loaded environment does not define now offers the **Path Migration** panel (also **Edit → Resolve Unknown Types...**), replacing the read-only unknown-types list.
  - Proposes a target for every unknown path with a confidence tier (Certain, High, Medium, Low, Lossy) and the reasons for it.
  - Certain results come from the codebase's `tools/UpdatePaths/Scripts` (UpdatePaths syntax, evaluated in order) and from decisions you chose to remember. Name, appearance, trailing-segment, and directional-helper matches are suggested at lower tiers. Low and Lossy suggestions are never selected automatically.
  - Script deletions, root-type changes, and all deletions always require a person. Variable conflicts block automatic selection.
  - Applies as one undoable edit. In a collaboration session it is submitted as a single tile-change operation; session edits above 4,096 tiles without bulk edits are refused before anything changes.
  - A tile that would gain or lose an area or turf fails the whole edit.
  - Nothing is written to disk unless you export a script or enable Remember or codebase saving in preferences. A reference environment can optionally be parsed on a worker to improve suggestions; it never replaces the loaded environment.

### Fixed

- **Rendering:** removed chunk layers are now released, fixing retained render resources.
- **Mapping authoring:** authoring recovery is retained after its source is closed.

### Performance

- Validating the sources of move operations no longer copies them.
- Canonical hashing reuses variable scratch space, reducing allocations.

## Downloads

Choose `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and their SHA-256 checksum files are also included.

All packages target x86-64. The macOS package targets Intel Macs; there is no native Apple Silicon package.

## Alpha notes

- Keep maps in version control and retain backups. Review Path Migration proposals before applying, especially anything below Certain.
- Path Migration was exercised in a manual desktop session, but the fixes for two defects found there were not re-checked in a rendered session, no automated GL test drives the panel, and the panel offers path-level choices only (no per-variant overrides). See [the verification record](../verification/2026-10-05-path-migration.md).
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS.
- Download updates manually. Production signing for the in-app updater is not configured. GitHub build provenance is separate from application/update signing.

**[Full changes since v.a.6](https://github.com/Aphelion-Moon/AphelionDMM/compare/v.a.6...v.a.7)**
