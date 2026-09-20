# Repeat last transform prototype

The selection tools panel now offers **Repeat**, with **F4** as its customizable
default shortcut. The button names the remembered rotation, mirror or grid move.
The shortcut also appears in the existing reference and rebinding UI.

The remembered value is an action: rotation direction, mirror axis, or resolved
tile displacement. It holds no map contents, selection coordinates, clipboard,
instance pointers or stable IDs. Repeating uses the current selection and
visibility filter through the existing transform/preflight/operation path. A
grid move retains its original tile distance after preference changes. Each
accepted repeat is one ordinary undoable operation; out-of-bounds moves are
disabled before dispatch.

For durable selection edits, only initial executor acceptance remembers the
action. Rejections, failed capture/preflight and no-op commits leave the previous
recipe intact. Undo and redo do not replace it. Completion order cannot allow an
older request to supersede a newer successful action. Attachment changes and
close clear the recipe and fence late completions. This state belongs to the
editor rather than the global Grab tool.

Successful rotation/mirror of a floating paste remembers its action immediately.
Repeat transforms that same floating template without submitting an operation,
changing clipboard contents, or assigning another set of copied IDs. The normal
place/cancel controls retain their semantics. Cancelling a preview does not erase
the recipe: it contains an action only. Grid moves are unavailable as repeats
inside a paste preview, where cursor placement owns translation.

This prototype records the rotation, mirror and grid-nudge commands. Freehand
mouse drags, instance pixel offsets and property edits are not recorded as
repeatable transforms. It adds no persistent macro/history format.

## Validation

Before implementation, native workspace regressions failed because F4 did not
repeat rotation or paste transforms, and rebinding did not repeat the original
move distance. After implementation:

- Native workspace shortcuts repeat rotations against new selections and current
  contents, preserving instance identity and exact undo/redo hashes.
- Horizontal and vertical repeats restore exact contents after two reflections.
- A new custom F8 binding replaces F4. Repeated moves retain the original
  two-tile step, and an out-of-bounds repeat leaves authority unchanged.
- A no-op mirror retains the last accepted move. Modified chords, pending modal
  ownership and actual focused ImGui text input block the repeat action.
- Floating paste rotation/repeat preserves copied IDs and clipboard contents,
  creates no intermediate history, and confirms as one revision.
- Controlled network transport verifies that an unacknowledged first action is
  not yet repeatable and that a rejected mirror does not replace the prior
  accepted rotation. Repeating then submits and accepts the correct rotation.
- A new attachment clears repeat state. The owned recipe test additionally
  checks reordered completions and completion after clear.

Focused native checks and affected race checks passed with
`APHELIONDMM_GL_TEST=1`. Race scope: owned editing, editor, pmap, tools and wsmap.
Logs are in `.artifacts/repeat-transform-2026-09-20/`.

`task verify` also passed lint, contract gates, all Go tests, Rust test/fmt/Clippy,
parser release build and desktop build. Toolchains were Go 1.25.13, GCC 15.2
UCRT, Task 3.53.1, golangci-lint 2.12.2 and Rust 1.82.0 Windows GNU. Rust currently
contains zero unit tests; the inherited ImGui C++ `memset` warning remains.

Human desktop/keyboard-layout acceptance and representative large-selection
performance remain open. Named selection stamps remain a separate planned
prototype. These checks do not establish hosted or cross-repository acceptance.
