# AphelionDMM v.a.2

AphelionDMM is a BYOND map editor with authoritative multiplayer collaboration, built on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. SpaiR and the StrongDMM contributors made this editor possible; consider [supporting SpaiR](https://ko-fi.com/spair).

This alpha brings Discord sign-in and a hosted session browser, expanded mapper and source-composition tools, and improvements to background loading, editing, saving, and recovery.

## Changes since v.a.1

- **Hosted collaboration:** Discord sign-in, Community and Private session browsing, owner controls for session title and visibility, and reopening admitted sessions after everyone leaves. OIDC remains available to self-hosters. The browser login handoff keeps application credentials out of callback responses.
- **Mapper tools:** persistent selections, mapping profiles and construction tools, improved composition navigation, and more consistent ownership of move/paste previews. Selection masks and protected channels remain respected through editing and history.
- **Source workflows:** source composition and comparison inspection, bounded reference browsing, source templates, and guarded recipe writes against accepted revisions.
- **Loading and rendering:** background map preparation, validated environment snapshot caching, incremental level construction, bounded renderer work, and improvements to icon recovery and interface responsiveness during large local operations.
- **Saving and recovery:** asynchronous saves from immutable revisions, validated atomic replacement, detection of external file changes, and recoverable local edit history. Save completion no longer marks later edits as saved.
- **Platform fixes:** Windows cache short-path handling and trusted-owner checks, macOS memory admission, hosted WebSocket reauthorization race repair, and native lifecycle fixture corrections.
- **Upstream integration:** VSCodium go-to-definition support with fallback coverage.

## Downloads

Choose `apheliondmm-windows.zip`, `apheliondmm-linux.zip`, or `apheliondmm-macos.zip`. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Versioned standalone binaries and their SHA-256 checksum files are also included.

All packages target x86-64. The macOS package targets Intel Macs; there is no native Apple Silicon package.

## Collaboration and alpha notes

- Keep maps in version control and retain backups. This remains an alpha release; performance varies with the map, environment, operation and hardware.
- The default hosted service is [mapcollab.a13.info](https://mapcollab.a13.info). Use **Collaboration → Sign In to Hosted Service**, then **Browse Sessions...**. Access requires membership in the configured Discord server and a compatible local game environment.
- New hosted sessions default to Private. Community sessions admit new members as editors. Switching a session back to Private retains previously admitted members.
- Discord membership is checked at login. Application sessions default to 12 hours and are cleared on service restart; leaving the guild does not immediately revoke an existing login. Local recovery draft export remains available while signed out.
- Real Discord member/nonmember and two-user hosted acceptance, human desktop acceptance, and extended representative-map qualification remain outstanding. Automated checks are not a production certification.
- Existing preferences remain in `%USERPROFILE%\AppData\Roaming\StrongDMM` on Windows or `~/.strongdmm` on Linux/macOS.
- Download updates manually. Production signing for the in-app updater is not configured; no unsigned update manifest is supplied. GitHub build provenance is separate from application/update signing.
- Self-hosters should review the [deployment guide](../hosting/game-server-deployment-agent-handoff.md) before upgrading. Hosted metadata migration 5 requires a coordinated database restore when rolling back to older binaries; replacing only the executable is insufficient.

The application icon is by **Vinylspiders**, created for [Meridian](https://meridian.a13.info) and its [wiki](https://meridian-wiki.a13.info/wiki/Main_Page).

**[Full changes since v.a.1](https://github.com/Aphelion-Moon/AphelionDMM/compare/v.a.1...v.a.2)**
