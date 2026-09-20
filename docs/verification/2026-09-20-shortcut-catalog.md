# Shortcut action catalog and shared bindings

Baseline: `250b540a`. This implements the catalog and conflict-detection
prerequisites in the editor QoL workplan. User rebinding is not implemented.

The catalog groups registered chords by action name, retaining alternative
bindings while collapsing duplicate registrations from multiple map panes.
Its ordered results own their key slices. Closing a pane removes only that
pane's registrations through the existing registry lifecycle.

Conflict detection checks whether two actions can match the same pressed key
using the dispatcher's exact modifier rules. It handles left/right modifiers,
keypad alternatives and different modifier-slot order. An action's own
alternatives do not conflict with each other. These are potential overlaps:
visibility, focus and enabled predicates remain runtime UI decisions.

Help > Keyboard Shortcuts (F1) uses this catalog and includes a collapsible,
filterable **Shared bindings** section. The existing defaults and dispatcher
behavior are unchanged. No preference schema or dependency changes are needed.

## Validation

Logs: ignored `.artifacts/shortcut-catalog-2026-09-20/`.

- The new tests first failed because the catalog APIs did not exist.
- Focused hotkey and shortcut tests pass. They cover action grouping,
  deterministic ordering, independent key storage, exact modifier differences,
  keypad aliases, modifier order, and the real registry's add/dispose lifecycle.
  Existing reference-format tests remain passing. The menu package compiles.
- `task verify` passed lint, contracts, the Go suite, pinned Rust test/fmt/Clippy,
  parser release build and Windows editor build. Rust has zero unit tests.
  The inherited ImGui C++ warning persists.

Native OpenGL tests remain skipped on this host. The new rendered section has
not received native visual or human acceptance. Keyboard layouts, text fields,
popup focus, inactive maps and repeat interactions still need qualification;
editable bindings, preference migration and reset-to-default remain open.

No protected infrastructure changed. The pre-existing staged server-inventory
document remains untouched.
