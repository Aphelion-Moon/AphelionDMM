# AphelionDMM v.a.11

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. SpaiR and the StrongDMM contributors made this editor possible; consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha repairs collaboration defects found by an adversarial audit of the service and editor, and ships the rotation and environment-loading fixes made since v.a.10. The collaboration protocol stays at v2: v.a.10 and v.a.11 editors and services can share sessions.

## Changes since v.a.10

### Fixed

- **Rejoining a session kicked you for your own cursor:** a fresh connection for the same user restarts its presence counter. If the connection it replaced had not yet timed out (up to ten seconds, or a second editor open with the same hosted login), the server closed the new connection on its first cursor message, and on a hosted service kept doing so for up to a minute. Presence ordering is now scoped to each connection, a stale cursor message is dropped instead of closing the connection, and an old connection closing late no longer erases the new one's cursor or colour.
- **Hosted display names reverted:** a display name chosen in a hosted session went back to the login name within 30 seconds or at the next edit. The chosen name now persists for the session.
- **Rotation and mirroring respect map lint rules:** an object whose repository bans editing a variable at all (such as cables) now moves unturned, and a turn that would write a banned direction (such as a windoor) is refused with the rule's own explanation instead of producing a lint violation. Disposal pipes, which declare no direction, turn as facing south.
- **Environment loading:** the error dialog now includes the parser's message. When free memory is low, the loading dialog says so and that loading may take several minutes. If memory admission refuses the cache reservation, the environment still loads and only the cache write is skipped.
- **Lint:** the source lint job failed on the previous `main`; it passes again.

### Changed

- **Join rate limiting:** when the limiter's table is full, the least recently seen address is evicted instead of refusing every new address, so a flood of fresh source addresses cannot lock other mappers out of joining or signing in. IPv6 peers are counted per /64.
- **Snapshot download:** fetching a session snapshot no longer copies and rehashes the whole map while every other participant's edits wait. The owner pins the current revision and reports the hash it already holds; the copy happens off the editing path.

See [the audit report](../verification/2026-10-16-adversarial-audit.md) for the findings, the tests that reproduce them, and what was reviewed without change.

## Hosted service

No protocol or schema change. Update the service to pick up the reconnect, display-name and rate-limit repairs; v.a.10 editors can keep joining.

## Downloads

Choose `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and their SHA-256 checksum files are also included.

All packages target x86-64. The macOS package targets Intel Macs; there is no native Apple Silicon package.

## Alpha notes

- Keep maps in version control and retain backups.
- Automated tests only. These changes pass the automated gates (repository tests, race gate, fault and fuzz gates, lint, Rust tests and the Windows build). They have not been exercised in a full desktop or multi-user session.
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS.
- Download updates manually. Production signing for the in-app updater is not configured.

**[Full changes since v.a.10](https://github.com/Aphelion-Moon/AphelionDMM/compare/v.a.10...v.a.11)**
