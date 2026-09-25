# AphelionDMM

<p align="center">
  <img src="internal/rsc/png/editor_icon.png" width="220" alt="AphelionDMM: a dog astronaut surrounded by a rainbow orbit">
</p>

[![CI](https://github.com/Aphelion-Moon/AphelionDMM/actions/workflows/ci.yml/badge.svg)](https://github.com/Aphelion-Moon/AphelionDMM/actions/workflows/ci.yml)
[![Releases](https://img.shields.io/github/v/release/Aphelion-Moon/AphelionDMM?include_prereleases)](https://github.com/Aphelion-Moon/AphelionDMM/releases)

**A BYOND map editor for individual users and teams.**

AphelionDMM adds functions to **[StrongDMM](https://github.com/SpaiR/StrongDMM)**. These functions include shared map sessions, selection stamps, selection controls, paste controls, and map recovery. The editor keeps the StrongDMM desktop interface. It reads DME environments and DMM/TGM maps with the [SpacemanDMM](https://github.com/SpaceManiac/SpacemanDMM) parser.

> **StrongDMM is the original project.** SpaiR and other contributors made StrongDMM. Their work made AphelionDMM possible. To show your support, select **Star** on the [StrongDMM repository](https://github.com/SpaiR/StrongDMM). For donations, use [SpaiR's Ko-fi page](https://ko-fi.com/spair). Donation links in AphelionDMM also go to the creator of StrongDMM.

**[Download](https://github.com/Aphelion-Moon/AphelionDMM/releases)** · **[Report a problem](https://github.com/Aphelion-Moon/AphelionDMM/issues)** · **[Build from source](#build-from-source)** · **[Host a collaboration service](docs/hosting/game-server-deployment-agent-handoff.md)**

> [!IMPORTANT]
> AphelionDMM is an **alpha** release. Keep your maps in version control. Keep backup copies.
>
> This README describes the current `main` branch. A release can have fewer functions than this branch. Before you install a release, read its release notes.

## Differences from StrongDMM

StrongDMM supplies the basic editor functions. These include environment browsing, map search, variable editing, visibility filters, multiple map panes, multi-Z rendering, and DMM/TGM support.

AphelionDMM keeps these functions. The table shows the added functions and changes.

<!-- Comparison reviewed on 2026-09-25 against AphelionDMM main at 1b8acb0e6370bf6ca137c5b6138fc9c3a6095736 and the then-current StrongDMM main source. Recheck both branches before revising availability claims. -->

| Function | StrongDMM | AphelionDMM additions |
| --- | --- | --- |
| **Collaboration** | Local map editing. | Shared sessions, participant presence, server-ordered edits, and connection recovery. You can use a hosted service or your own server. |
| **Undo and redo** | Local edit history. | Operation-based history. In a shared session, undo applies only to your own accepted edits. The server validates each reverse operation. |
| **Selections** | Rectangle selections, selection movement, and clipboard operations. | Area-based selections with exact tile masks. Controls for 90-degree rotation, horizontal and vertical mirrors, movement by whole tiles, and repeat transforms. |
| **Reusable content** | Copy and paste between maps. | Named stamps in `.admmstamp` files. Environment checks and a preview before placement. |
| **Paste controls** | Clipboard placement with the Grab tool. | Three paste modes and separate controls for areas, turfs, objects, and mobs. Placement previews remain separate from committed map contents. |
| **Keyboard controls** | Tool shortcuts and quick-edit controls. | A hotkey reference and configurable shortcuts. Number keys select all seven tools. Controls rotate an object before placement or during movement. |
| **Saving and recovery** | DMM/TGM serialization and map saving. | Background save tasks, round-trip validation, atomic file replacement, and checks for external file changes. Recovery views permit inspection and export of affected edits. |
| **Large maps** | Desktop renderer, map search, and editor tools. | Background tasks prepare large local operations. Loading and search use small steps. Renderer updates use affected chunks. Move and paste operations have separate previews. |

These changes do not give better performance than StrongDMM in all conditions. Map size, graphics hardware, operation type, and session mode affect performance.

Refer to the [architecture guide](docs/agent/architecture.md) and [collaboration model](docs/agent/multiplayer-invariants.md) for more information.

## Additional editor functions

### Select and change tiles

The **Grab** tool can select a rectangle or tiles that belong to an area. **Area selection** can include **all matching areas on the current level**. A selection can contain gaps. The editor changes only the selected tiles, not all tiles inside the outer rectangle.

The selection tools can rotate content in 90-degree steps. They can also make horizontal or vertical mirrors of the content. You can move selected content by whole tiles. You can also repeat the previous transform.

The **Fill selection** and **Replace selected channel** commands apply only to the selected tiles. They do not change gaps in the selection. Visibility filters protect excluded content. The editor rejects a change if it conflicts with this protection.

During a selection move, the editor shows a separate preview. The preview does not change the committed map. The editor submits the change when you complete the move.

With **Add** or **Move**, **Q / E** rotate the object that you hold. These controls change its direction before placement or during a move.

### Save and use selection stamps

A selection stamp contains an arrangement of map content. You can save the stamp in an `.admmstamp` file. You can use stamps for rooms, equipment assemblies, or decorative layouts. A stamp contains only visible content from the selection.

To save a stamp:

1. Select tiles with **Grab**.
2. Open **Stamps…**.
3. Enter a name.
4. Select **Capture selection**.
5. Select **Save stamp…**.
6. Save the file in the necessary location.

A tile selection or clipboard copy is not a saved stamp. The named capture step is necessary. A stamp in memory does not remain available after you close the map.

To open a saved stamp:

1. Open **Stamps…**.
2. Select **Load stamp…**.
3. Open the stamp file.
4. If the environments differ, give approval to use the stamp with the current environment.
5. Select **Preview stamp**.

A stamp from a different environment can contain different types or default values. The editor requires your approval before it uses that stamp. You can move the preview to the necessary position before placement.

### Control the changes from a paste operation

Clipboard content and selection stamps use these paste modes:

| Paste mode | Function |
| --- | --- |
| **Only Overwrite With Data** | The editor replaces only content that the copied data contains. It keeps destination content for channels that the copied data does not contain. |
| **Apply Over** | The editor adds copied objects and mobs to the destination contents. Each tile permits only one turf and one area. |
| **Replace, Including Blanks** | The editor can clear a destination channel when the copied data contains an explicit empty value for that channel. |

The **Areas**, **Turfs**, **Objects**, and **Mobs** controls apply separately. Gaps in a selection remain unchanged. The editor protects excluded or hidden content. It can reject a placement that conflicts with these protections.

Before you place the content, you can move, rotate, or mirror the preview. The preview is not a committed map edit. Saved maps do not include the preview.

To apply the placement, press **Enter** or select **Place**. To cancel a placement before submission, press **Esc** or select **Cancel**.

## Collaboration

With collaboration, users can change the same map in a shared session. An embedded local session or a separate collaboration service can manage the shared map.

Participant presence is information about users in the session. The system keeps this information separate from the map history. The server validates map edits and puts them in order for all session users.

The server accepts or rejects each operation as a complete unit. It does not apply only part of an operation. Edits can continue if they do not conflict. The server rejects an edit if its expected values do not match the current values.

**Undo applies only to your own accepted operation.** The current map state must permit the reverse operation. Undo does not restore an earlier state of the complete session. It does not remove another user's history.

After a connection failure, the editor uses verified history replay or an authenticated snapshot to restore the shared map state. If a submission has an unknown result, the editor keeps the submission for recovery. It does not automatically submit the edit again.

The recovery view shows the values before the edit and the intended values. You can export this information as a JSON file. An exported recovery draft does not change the map. The export does not complete recovery of the edit.

The default hosted service address is **[mapcollab.a13.info](https://mapcollab.a13.info)**. Access depends on the sign-in and membership rules of the host. Refer to the [hosting guide](docs/hosting/game-server-deployment-agent-handoff.md) to install your own service.

Local map editing does not require a hosted account or a collaboration server. Shared maps still require compatible DME environments, code, and assets. Collaboration does not replace the game environment.

> [!NOTE]
> You can change map dimensions in a local document. Network sessions do not permit map resize. After a disconnection, a shared document does not become an independent local document.

## Saving and recovery

A save operation uses one committed map revision. A background task processes that revision. The editor first writes the output to a temporary file. It parses the file again and validates its contents. It then replaces the target file with one atomic operation. An earlier save operation does not change the saved status of newer edits.

The editor also compares the file on disk with the file state at load time. It shows a prompt if another application changes or deletes the file. At this prompt, you can overwrite the changed file or create the deleted file again. You can also select **Save As…** or cancel the save operation. This protection also applies to file changes from version-control operations.

You can inspect and export affected edits through the recovery views. These views include local edit faults and collaboration submissions with unknown results. An unresolved edit can prevent a save or close operation. These protections do not replace backups or version control.

## Large maps and response times

Current development includes changes to reduce delays on large maps. Map load progress does not use a modal dialog. Background tasks parse map data and prepare assets. The editor builds levels and search results in small steps.

Background tasks also prepare large local edit operations. Renderer updates use the affected map chunks. Move previews and paste previews remain separate from committed map contents.

Some operations can still temporarily prevent interface input. Local edits and network edits have different performance limits. These changes do not remove all delays.

Refer to the [architecture guide](docs/agent/architecture.md) for implementation details. Refer to the [verification guidance](docs/agent/verification.md) for the scope and limits of verification.

## Functions from StrongDMM

AphelionDMM keeps the **Add**, **Fill**, **Grab**, **Move**, **Pick**, **Delete**, and **Replace** tools from StrongDMM. It also keeps the environment tree, type browser, variable editor, and map search. Map search includes replace and delete operations. Quick-edit controls change direction and pixel offsets.

The editor also has multiple map panes, multi-Z rendering, visibility filters, and screenshots. You can dock map panes in different positions. Screenshots can show the map or a selection. Visibility filters can hide types and object categories. The editor can save maps in DMM or TGM format. These functions come from StrongDMM.

## Download and start the editor

**[GitHub Releases](https://github.com/Aphelion-Moon/AphelionDMM/releases)** contains the download packages. The package targets are **Windows x86-64**, **Linux x86-64**, and **Intel macOS x86-64**. The Intel macOS package is not a native Apple Silicon build. An installer is not necessary.

To start the editor:

1. Download the package for your platform.
2. Extract the archive.
3. On Windows, start `AphelionDMM.exe`. On Linux or macOS, start `AphelionDMM`.
4. Open the DME environment for your game.
5. Open the map.
6. If necessary, select a save format under **File → Preferences**.

If a versioned executable has a related SHA-256 file, use that file to verify the executable.

You can also open a map before you open its DME environment. The editor then tries to find the environment.

### Open a map from the command line

On Windows, use PowerShell:

```powershell
# Open an environment and a map.
.\AphelionDMM.exe path/to/environment.dme path/to/map.dmm

# Open a map without a DME argument.
.\AphelionDMM.exe path/to/map.dmm
```

On Linux or macOS, use a terminal:

```sh
./AphelionDMM path/to/environment.dme path/to/map.dmm
```

### Install updates

Download updates from Releases. Read the release notes for information about the in-app updater.

The [v.a.1 release](docs/releases/v.a.1.md) requires manual updates. Its release configuration does not include production signing for the in-app updater.

### Default controls

| Action | Control |
| --- | --- |
| Move the map view | Drag with the middle mouse button, hold Space and drag, or use the arrow keys. |
| Change the zoom | Use the mouse wheel or `+` / `-`. |
| Select Add / Fill / Grab / Move / Pick / Delete / Replace | Press `1` / `2` / `3` / `4` / `5` / `6` / `7`. |
| Temporarily select Pick / Delete / Replace | Hold `S` / `D` / `R`. |
| Rotate the object held with Add or Move | Press `Q` / `E`. |
| Rotate a Grab selection or placement preview | Press `[` / `]`. |
| Make a horizontal or vertical mirror of a Grab selection or placement preview | Press `H` / `V`. |
| Move a Grab selection by one tile | Press `Alt` + an arrow key. |
| Apply or cancel a paste preview or stamp preview | Press `Enter` / `Esc`. |
| Open the hotkey reference | Press `F1`. |

Shortcuts depend on the active map, tool, and editor state. The hotkey reference and tooltips give more information about the controls.

## Build from source

Use **Go 1.25.13**, as specified in [go.mod](go.mod). Use **Rust 1.82.0** and **Task 3.x**. Git and a C/C++ toolchain are also necessary. The desktop uses CGO to link the Rust parser from this repository.

Windows builds require **MinGW-w64** and the **GNU Rust toolchain**. Linux builds require X11/OpenGL and GTK development dependencies. The Ubuntu CI configuration installs `xorg-dev libgtk-3-dev`.

Refer to [CI](.github/workflows/ci.yml) for the configuration of each platform.

### Get the source code

Run these commands:

```sh
git clone https://github.com/Aphelion-Moon/AphelionDMM.git
cd AphelionDMM
```

### Build on Linux or macOS

Run this command from the repository directory:

```sh
task build
```

### Build on Windows

In PowerShell, run these commands from the repository directory:

```powershell
$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
task task_win:gen_syso
task build
```

The Windows commands select the GNU Rust toolchain and generate executable resources before the build. The `task build` command compiles the pinned parser before it compiles the Go editor. It writes the executable to `dst/`.

Refer to [Taskfile.yml](Taskfile.yml) for other build tasks. Before you change the parser, file storage, or collaboration code, read the [verification guidance](docs/agent/verification.md).

## Settings and problem reports

AphelionDMM keeps the StrongDMM directory names for compatibility with existing preferences.

| Platform | Settings directory |
| --- | --- |
| Windows | `%USERPROFILE%\AppData\Roaming\StrongDMM` |
| Linux / macOS | `~/.strongdmm` |

To open the logs, select **Help → Open Logs Folder**. For a problem report, use **[AphelionDMM Issues](https://github.com/Aphelion-Moon/AphelionDMM/issues)**.

Include the editor version or commit, operating system, and steps that cause the problem. State whether you used a local map or a shared map. Remove credentials from the logs before you attach them. For display or performance problems, include the map dimensions and graphics hardware, if available.

## Contributions and unsupported integrations

Read [Contributing](docs/CONTRIBUTING.md) and the [development guidance](docs/agent/README.md) before you submit changes.

A plan can describe a function that the editor does not yet have. A plan is not evidence that a release contains that function.

**AphelionDMM does not include support for Aphelion Content Tools, Rift build tooling, or MCP integration.** The editor opens game repositories directly as environments and maps. These operations do not require the removed integrations.

## Credits and license

**[StrongDMM](https://github.com/SpaiR/StrongDMM)**, by SpaiR and other contributors, supplies the basic map editor. **[SpacemanDMM](https://github.com/SpaceManiac/SpacemanDMM)**, by SpaceManiac and other contributors, supplies the DM parser. The repository keeps upstream credits and source history.

**Vinylspiders** made the AphelionDMM dog-astronaut artwork for [Meridian](https://meridian.a13.info) and its [wiki](https://meridian-wiki.a13.info/wiki/Main_Page). [Clément “Topy”](https://github.com/clement-or) made the historical StrongDMM application icon.

[LICENSE](LICENSE) contains the GPL-3.0 terms for the source code. The [artwork notes](docs/branding/README.md) contain information about the origin of the icon and its permitted uses.
