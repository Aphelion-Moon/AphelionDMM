# AphelionDMM v.a.10

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. SpaiR and the StrongDMM contributors made this editor possible; consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha focuses on collaboration performance. It moves to collaboration protocol v2, which changes how map hashes are computed. **v.a.10 editors and services cannot join sessions with v.a.9 editors or services.** Read [Hosted service](#hosted-service) before updating a shared service.

## Changes since v.a.9

### Changed

- **Collaboration protocol v2:** the map hash is built from per-tile and per-row digests. An edit rehashes only the tiles it touches, their rows and the root, instead of the whole map.
- **Faster edits in sessions:** the server checks each new operation against the document state it last committed. It no longer reloads and replays the session's history on every edit. A mismatch, a failed write or a restart falls back to full recovery, which still validates every stored revision.
- **Smaller network traffic:**
  - WebSocket messages are compressed per message when both sides support it. Each message is compressed on its own, so a credential never shares a compression window with other data. Size limits apply to the decompressed message.
  - The server encodes each accepted edit once for all connections in a session.
  - The editor no longer resends an unchanged cursor every frame. It sends a periodic keepalive instead.
- **Clearer version errors:** an editor on a different protocol from the service now says which versions each side uses. It reports this when connecting and before creating or joining a session, instead of failing with a bare connection error.
- **Rotation:** an object whose sprite has more than one direction now turns even when only `/atom` declares `dir`, such as iron `smooth_half` floors. Turning it back to its default direction removes the `dir` edit instead of keeping a redundant one.

### Fixed

- **Map Lint in a session:** after applying fixes, Map Lint now waits for the server to acknowledge the edit before rescanning. Before, it could rescan the unacknowledged map.
- **Crash reports:** the Windows build has no console, so crashes left the session log cut off without a reason. The crash stack trace is now written to the session log, including panics on background tasks and fatal runtime errors.

## Hosted service

This release changes the collaboration protocol from v1 to v2.

- Update the hosted service and every editor together. A v.a.9 editor cannot join a v.a.10 service, and a v.a.10 editor cannot join a v.a.9 service.
- Sessions hosted with v.a.9 are **retired, not converted**. A v.a.10 service skips them at startup, leaves their data untouched and refuses joins with an explanation. It does not spend an invitation on them.
- **Before updating a service**, open each session you want to keep in v.a.9 and save the map. Then host it again on v.a.10.

## Downloads

Choose `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and their SHA-256 checksum files are also included.

All packages target x86-64. The macOS package targets Intel Macs; there is no native Apple Silicon package.

## Alpha notes

- Keep maps in version control and retain backups.
- Automated tests only. These changes pass the automated gates, including the collaboration resilience and hosted gates. They have not been exercised in a full desktop or multi-user session.
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS.
- Download updates manually. Production signing for the in-app updater is not configured.

**[Full changes since v.a.9](https://github.com/Aphelion-Moon/AphelionDMM/compare/v.a.9...v.a.10)**
