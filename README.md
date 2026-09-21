## Support the original StrongDMM project

AphelionDMM builds on **[StrongDMM](https://github.com/SpaiR/StrongDMM)**, created by SpaiR and its contributors. Please visit the original project, give it a star, and consider **[supporting SpaiR on Ko-fi](https://ko-fi.com/spair)**. Their work made this editor possible. The support links in AphelionDMM also go to StrongDMM's creator.

# AphelionDMM

<p align="center">
  <img src="internal/rsc/png/editor_icon.png" width="220" alt="AphelionDMM: a dog astronaut surrounded by a rainbow orbit">
</p>

[![CI](https://github.com/Aphelion-Moon/AphelionDMM/actions/workflows/ci.yml/badge.svg)](https://github.com/Aphelion-Moon/AphelionDMM/actions/workflows/ci.yml)
[![Releases](https://img.shields.io/github/v/release/Aphelion-Moon/AphelionDMM?include_prereleases)](https://github.com/Aphelion-Moon/AphelionDMM/releases)

A BYOND map editor with local editing and authoritative multiplayer collaboration, built on StrongDMM's desktop editor and the SpacemanDMM parser.

## Download and run

Get the Windows, Linux, or Intel macOS package from **[GitHub Releases](https://github.com/Aphelion-Moon/AphelionDMM/releases)**. Extract the archive and launch `AphelionDMM.exe` on Windows or `AphelionDMM` on Linux/macOS. Use the checksums attached to the release to verify your download. An installer is not required.

The `v.a.1` release is an alpha. Keep backups of maps you edit and read its release notes for current limitations. The in-app signed updater requires release-signing configuration; download this release manually.

## Editing

- Open DME environments and DMM/TGM maps, with native parser and round-trip validation.
- Browse types, search maps, edit variables, filter layers, and capture screenshots.
- Rotate and mirror selections, repeat transforms, and save reusable selection stamps.
- Use the hotkey reference and configurable shortcuts.
- Save through a staged, validated replacement of the target map.

Pan with the middle mouse button, space-drag, or arrow keys. Zoom with the scroll wheel or `+`/`-`. Choose the map save format under **File → Preferences**.

You can also open environments and maps from the command line:

```text
AphelionDMM.exe path/to/environment.dme path/to/map.dmm
AphelionDMM.exe path/to/map.dmm
```

On Linux/macOS, use `./AphelionDMM`. When only maps are supplied, the editor attempts to find a matching environment.

## Collaboration

AphelionDMM supports an embedded local session and hosted collaboration. The server orders durable edits; clients reconcile accepted changes and keep uncertain edits available for explicit recovery. Presence is separate from map history. Reconnect uses verified replay or an authenticated snapshot, and undo submits an actor-scoped inverse operation.

The default hosted address is **[mapcollab.a13.info](https://mapcollab.a13.info)**. Access depends on the host's sign-in and membership configuration. Self-hosting uses the separate collaboration server; configuration and deployment guidance is in [the hosting handoff](docs/hosting/game-server-deployment-agent-handoff.md).

Aphelion Content Tools, Rift build tooling, and MCP integration are no longer supported. Game repositories are used directly as DME and map inputs.

## Settings and troubleshooting

This release retains the existing settings directories so upgrading from the previous package preserves preferences:

- Windows: `%USERPROFILE%\AppData\Roaming\StrongDMM`
- Linux/macOS: `~/.strongdmm`

Open logs from **Help → Open Logs Folder**. Report problems in [AphelionDMM Issues](https://github.com/Aphelion-Moon/AphelionDMM/issues), including the version, operating system, reproduction steps, and relevant logs with credentials removed.

## Build from source

Use the versions selected by the repository: **Go 1.25.13**, **Rust 1.82.0**, and **Task 3.x**. CGO requires a C/C++ toolchain. Windows builds use MinGW-w64 with the GNU Rust target. Linux builds require X11/OpenGL and GTK development libraries; on Ubuntu, install `xorg-dev libgtk-3-dev`.

The supported build compiles the pinned Rust parser and links it into the Go editor:

```text
task build
```

For Windows PowerShell, set the GNU Rust toolchain and generate executable resources first:

```powershell
$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
task task_win:gen_syso
task build
```

The executable is written to `dst/`. See [CI](.github/workflows/ci.yml) for the platform build commands and [verification guidance](docs/agent/verification.md) for the checks and their limits.

## Credits and license

- [StrongDMM](https://github.com/SpaiR/StrongDMM), by SpaiR and contributors, supplies the inherited editor foundation.
- [SpacemanDMM](https://github.com/SpaceManiac/SpacemanDMM), by SpaceManiac and contributors, supplies the underlying DM parser.
- The AphelionDMM icon was created by **Vinylspiders** for [Meridian](https://meridian.a13.info) and its [wiki](https://meridian-wiki.a13.info/wiki/Main_Page). See [artwork provenance and permitted use](docs/branding/README.md).
- The historical StrongDMM application icon was designed by [Clément “Topy”](https://github.com/clement-or). It is retained in project history; the current icon is the user-approved AphelionDMM artwork.

See [LICENSE](LICENSE) for GPL-3.0 source-code terms and [the artwork notes](docs/branding/README.md) for the icon. Existing upstream attribution and source history are retained.
