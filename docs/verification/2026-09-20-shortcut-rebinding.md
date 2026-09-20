# Editable shortcut bindings

Baseline: `625e1681`. The F1 / Help > Keyboard Shortcuts window now includes
**Customize bindings** with per-action edits, alternative chords, conflict
preview, disabling an action, resetting one action and resetting all defaults.
Shared keys require explicit acknowledgment in the editor before applying.

Examples: `Ctrl+K; Alt+F8`, `Ctrl+1/Numpad 1`, `RightCtrl+K`. Generic modifier
names accept both sides. Up to four alternatives and all four modifier families
are supported. Held-tool S/D/R require Ctrl or Cmd in custom bindings; Space
and unmodified Esc remain reserved for camera dragging and cancellation.
Reset restores registered defaults, including reserved default chords.

Overrides live in the existing preferences document and follow its established
background/exit save lifecycle. Missing or invalid entries retain defaults;
unknown action names survive so unopened panes can use their settings later.
The settings object owns key slices and locks serialization against edits.
The normal dispatch path reads it without copying custom chords each frame.

Dispatch, the reference, main-menu key labels, selection-move hints and mirror
button labels consume effective bindings. Pane callbacks and visibility remain
UI-owned. Dispatch skips disabled candidates instead of allowing a disabled
pane to swallow a matching enabled action. Equal-priority candidates retain
registration order. Fast camera movement now belongs to its action rather than
depending on whether its custom chord happens to include Shift.

## Validation

Logs: ignored `.artifacts/shortcut-rebinding-2026-09-20/`.

- New settings/dispatcher tests initially failed on missing APIs. They now cover
  parsing, aliases, exact modifier matching, independent storage, invalid saved
  entries, disabling/resetting, and concurrent preference serialization.
- Actual preference loading, saving and reopening preserves other settings,
  custom alternatives and reset behavior. Missing/null/invalid shortcut settings
  keep existing defaults.
- Actual ImGui dispatch exercises invisible/disabled panes, alternative chords,
  pane disposal/reopening, disabling and resetting. The editor's apply handler
  rejects unacknowledged conflicts and invalid edits; its render path produces
  draw data in an ImGui context.
- A native workspace regression first reproduced rebound fast pan moving only
  32 units instead of 160. Both fast pan without Shift and normal pan with Shift
  now preserve their action's intended speed. Existing native selection/grid-step
  checks also passed after the [graphics driver installation](2026-09-20-graphics-driver.md).
- Focused race checks passed for settings, registry, editor and preference paths.
- `task verify` passed with `APHELIONDMM_GL_TEST=1`: lint, contracts, the Go suite,
  pinned Rust test/fmt/Clippy, parser release build and Windows editor build.
  Rust has zero unit tests and the inherited ImGui compiler warning persists.

Keyboard-layout/OS key interception, the full popup/focus/repeat interaction
matrix, and human visual/interaction acceptance remain open. Conflict previews
cover currently registered actions; unopened panes can introduce new overlaps.
No protected infrastructure or dependency manifest changed. The unrelated
staged server-inventory document remains untouched.
