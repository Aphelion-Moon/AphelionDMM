# Shortcut ownership across popups and dialogs

Baseline: `12e8c929`. A native workspace regression reproduced a custom nudge
changing the map on the first frame after a modal was queued, before the modal
was drawn. Global shortcut dispatch previously checked active input widgets but
not popup or pending-dialog ownership.

Global shortcuts, held tools and Grab's separate Escape handler now pause while
a popup or queued/open dialog owns input. The window frame boundary captures
popup ownership before ImGui can dismiss it, so the dismissal key does not also
edit or cancel a map gesture behind the popup.

The tile menu and main-menu sections dispatch their explicitly listed actions
inside the focused popup after its widgets. They use the effective custom
bindings and enabled/visible checks. Tile-menu key labels now match those
bindings; its rebound Close action closes both the native popup and tile state.
Unrelated background commands remain blocked.

## Evidence

Logs: ignored `.artifacts/shortcut-focus-2026-09-20/`.

- Before the guard was connected, the native modal test changed map contents
  on frame zero. A popup-routing regression also caught the same command being
  dispatched globally and inside the popup. Both now pass.
- The native workspace test verifies unchanged canonical map hash and selection
  while the modal is pending/open, and successful dispatch after it closes.
- Actual tile-menu rendering verifies a rebound Copy runs once, a background
  action sharing that key does not run, a rebound Close closes the popup, and
  ordinary dispatch resumes afterward.
- ImGui frame tests cover popup dismissal, custom-key repeat, text-field focus,
  held-tool isolation and Escape preserving an active Grab gesture behind a
  closing popup. Escape cancels the gesture after popup ownership ends.
- Focused checks passed before `task verify`, which also passed with
  `APHELIONDMM_GL_TEST=1`: lint, contracts, Go tests, pinned Rust checks, parser
  build and Windows editor build. Rust has zero unit tests; the inherited ImGui
  compiler warning remains. Unconfigured external-service gates may still skip.

This covers the listed popup/repeat regressions, not every OS keyboard layout,
focus transition or native menu interaction. Human acceptance remains open.
No protected infrastructure or dependency manifests changed; the unrelated
staged server-inventory document remains untouched.
