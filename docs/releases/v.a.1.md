# AphelionDMM v.a.1

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. Please visit the original project and consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha introduces the AphelionDMM name and icon across the application and release packages. The icon was created by **Vinylspiders** for [Meridian](https://meridian.a13.info) and its [wiki](https://meridian-wiki.a13.info/wiki/Main_Page).

## Changes since pre-production

- AphelionDMM window title, About dialog, workspace logo, Windows icon and executable metadata, screenshot names, and release filenames.
- Updated README with upstream support, downloads, collaboration, build instructions, and artwork attribution.
- Selection stamps, repeat transforms, guarded edit capture, and workspace lifetime fixes.
- SQLite recovery improvements, verified reconnect replay and snapshot fallback, and explicit recovery of uncertain edits.
- Hosted collaboration defaults to `https://mapcollab.a13.info`; server access depends on the configured sign-in and membership policy.
- Removed support for Aphelion Content Tools, Rift build tooling, and MCP integration.
- Dependency and CI repairs across Windows, Linux, Intel macOS, hosted security checks, and collaboration recovery tests.

## Downloads

Use the archive for your platform: `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and SHA-256 sidecars are also produced by the tag workflow.

## Alpha notes

- Keep backups of your maps. This is an alpha release.
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS.
- Download updates manually. Production signing for the in-app updater is not configured; no unsigned update manifest is supplied.
- The macOS package targets Intel x86-64; this release does not include a native Apple Silicon build.

**[Changes since pre-production](https://github.com/Aphelion-Moon/AphelionDMM/compare/pre-production...v.a.1)**
