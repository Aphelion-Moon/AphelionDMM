# Held-tool selection ownership

Baseline: `1cb04826`. ImGui entry-point regressions reproduced three faults in
the inherited temporary-tool bookkeeping:

- After holding and releasing D, explicitly selecting Pick and tapping S could
  unexpectedly select Delete on release. A previous temporary tool leaked into
  the next hold session.
- Holding S, pressing D, then releasing S returned to Grab while D remained down.
  Short-circuit processing had skipped the lower-priority key's press.
- Choosing Move while temporarily holding D was overwritten by the original
  Grab selection when D was released.

The owned `hotkeys.HeldTools` state now records admitted presses, their original
selection and the current temporary selection. The narrow pane adapter samples
all S/D/R keys every frame, preserving their existing priority order. Releasing
one held key exposes the next admitted key; releasing the last restores the
original tool. A different explicit selection ends temporary ownership and is
preserved. A blocked press requires a fresh press after the guard clears.

Existing popup, text-input, active-item and Ctrl/Cmd guards remain. The native
mouse regression confirms that S/D presses during an active Grab drag do not
steal it or activate retroactively after mouse release. The drag commits
normally; subsequent fresh overlapping holds change only tool/selection state.
Undo still restores the exact initial display and authoritative hashes with no
extra history entry. This fixture uses real canvas control, pane callbacks,
registered frame jobs and editors in a hidden OpenGL context.

Validation:

- The three new transition regressions failed before the repair and pass after
  it. Additional entry-point cases cover three simultaneous holds, releasing a
  hold during a shortcut and releasing Ctrl while S is still down.
- Focused hotkey/pane tests passed. Native held-tool, focus-transfer and delayed
  network-drag checks passed together with race detection for three repetitions.
- `task verify` passed with `APHELIONDMM_GL_TEST=1`: lint/contracts, Go tests,
  pinned Rust checks, parser and Windows editor build. Rust contains zero unit
  tests; the inherited ImGui compiler warning remains.

Logs are retained in ignored `.artifacts/held-tools-2026-09-20/`. Toolchains were
Go 1.25.13, GCC 15.2 UCRT, golangci-lint 2.12.2, Task 3.53.1 and Rust
`1.82.0-x86_64-pc-windows-gnu`. No dependencies or protected infrastructure
changed. These injected-input cases do not establish OS keyboard-layout,
physical interaction, wider cross-pane sequences or human acceptance. The
broader editor plan remains open.
