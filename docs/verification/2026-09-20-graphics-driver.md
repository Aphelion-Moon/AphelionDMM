# Windows graphics driver and native workspace verification

The user explicitly requested downloading and installing the necessary graphics
driver. The host is an ASRock Rack EPYC4000D4U with an AMD Ryzen 7 9800X3D,
running Windows Server 2022 Standard, build 20348, x64.

Before installation, the AMD device used Microsoft Basic Display Adapter with
problem code 31. Its hardware ID is `PCI\VEN_1002&DEV_13C0&REV_CB`. A separate
ASPEED basic display adapter and the Microsoft Remote Display Adapter were healthy.

## Package and installation

The [ASRock Rack download center](https://www.asrockrack.com/support/download-center.asp)
lists AMD Onboard VGA Driver `32.0.21002.27_2022` for this motherboard and OS.
Downloaded the [manufacturer package](https://download.asrock.com/Drivers/AMD/VGA/AMD_VGA(v32.0.21002.27_2022).zip)
into the task's ignored external working directory, not the repository.

- Downloaded ZIP SHA-256: `9BFF6DB2DCB717ABE1E0613E7D246EE5F20B800100B97E0338600142319B1828`.
  This is a recorded local checksum, not a comparison with a published checksum.
- The display INF `u2414929.inf` explicitly matches device revision CB and the
  Server 2022 build. Driver date/version: April 2, 2025 / `32.0.21002.27`.
- Its catalog signature validated as Microsoft Windows Hardware Compatibility
  Publisher. The package's setup executables also had valid AMD signatures.
- Used elevated Windows PnPUtil `/add-driver <display INF> /install`. Exit code 0.
  No forced reboot, unsigned-driver setting, security exclusion, or policy change.
- Windows subsequently reports **AMD Radeon(TM) Graphics**, version
  `32.0.21002.27`, status OK, configuration-manager error code 0.

## Evidence

Logs: ignored `.artifacts/shortcut-rebinding-2026-09-20/`.

- With `APHELIONDMM_GL_TEST=1`, `TestSaveAcknowledgementBoundaries` passed.
  The previous WGL/OpenGL initialization failure no longer reproduces.
- The native selection nudge, fast camera pan, and configured grid-step
  workspace tests passed explicitly without skips. The grid-step test checks
  operation revision, boundaries, undo/redo hashes, stable IDs and live preferences.
- `task verify` subsequently passed with `APHELIONDMM_GL_TEST=1`, enabling the
  existing hidden-context Go gates, followed by the Rust checks, parser build
  and Windows editor build. The inherited ImGui compiler warning remains;
  Rust still has zero unit tests. Unconfigured external-service gates may skip.

This resolves this host's graphics dependency blocker. Automated hidden-window
tests do not constitute human desktop acceptance, hosted-service acceptance,
or measured graphics performance.
