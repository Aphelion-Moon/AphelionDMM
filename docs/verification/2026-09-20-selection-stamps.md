# Named selection stamp prototype

The map tools panel now provides **Stamps...**. A user can name and capture the
visible contents of a finished selection, review its tiles/types/variables, save
an `.admmstamp` file, load a previously saved file, and explicitly start a preview.
Capture and load retain one current stamp per map pane. They do not place data
or change the clipboard. The dialog clearly distinguishes an in-memory capture
from a saved file; files are required for reuse after closing the map.

## Format and ownership

The versioned JSON file contains the user-entered name, environment hash,
normalized relative tile coordinates, explicit prefab paths/variables, and the
paths hidden at capture. It contains no source project/map path, process-local
instance ID, stable collaboration ID, image or generated content. Unknown types
and explicit variable values are preserved. Capturing does not assign identities
or change the source map. Capture waits for open gestures and unacknowledged
operations to finish.

File loading is bounded to 8 MiB and regular files. Validation rejects unsupported
format/version, unknown fields, trailing data, duplicate/out-of-bounds cells,
inconsistent dimensions, invalid names/hashes/paths/variables and invalid UTF-8.
Stamps contain 1 through 4,096 selected tiles on one source level, with dimensions
no larger than the map model limit. Saving requires the stamp extension and uses
staged, flushed, byte-validated atomic replacement. Failed load keeps the current
stamp; failed save preserves the previous file. File limits do not waive the
operation engine or network message limits when placing a stamp.

The owned stamp value is immutable to callers. Its bounded inspection preview is
built once on capture/load, not regenerated during each frame. Each use rebuilds
an independent paste template with the current environment's inherited defaults.
Capture-time and currently hidden paths are combined, preserving destination
instances on either set of hidden paths. The normal placement engine assigns
fresh stable IDs for every use and preserves those IDs across preview transforms.

An environment mismatch requires explicit acknowledgement in the review dialog
before preview. It never changes the executor's environment contract. Pane and
attachment checks are repeated before capture/placement; closed, switched or
busy editors cannot be replaced by an old dialog action.

Clipboard paste and stamps share the same preview-start adapter. Both retain
normal move/rotate/mirror/repeat/place/cancel behavior. Save stays blocked during
preview. Only confirmation submits an ordinary operation; acceptance creates one
undo command, and rejection retains the existing network conflict draft. Stamps
do not introduce a second mutation or history mechanism.

## Evidence

- Native workspace regressions were added before the stamp API existed. Later
  malformed-file tests exposed inconsistent declared dimensions, which are now
  rejected instead of being silently renormalized during placement.
- File tests cover malformed/oversized input, independent template/filter data,
  unknown values and preservation of the previous file on a failed save.
- Native workspace tests capture, save, load, rotate and cancel a stamp; reuse it
  with fresh IDs; confirm exactly one revision; preserve hidden destination
  contents; undo/redo exact map hashes; retain the clipboard; and run actual Save.
- Additional native cases require acknowledgement for a foreign environment,
  preserve currently hidden objects, and refuse replacement of a preview or
  pending edit. The shared test app now retains its visibility filter like the
  real app; its former getter created a fresh empty filter on every call. Hidden
  contents are checked by identity, respecting the existing preview's ordering
  of retained hidden instances before pasted instances.
- Controlled network tests cover confirmation, delayed acceptance, rejection,
  retained drafts, capture refusal during acknowledgement, and exact undo/redo.
- The review dialog's load/save/preview actions are tested, including a real ImGui
  Preview-button click and a refused placement that keeps the dialog available.
- Affected race checks passed with `APHELIONDMM_GL_TEST=1`: stamps, editor, pmap,
  wsmap and the native window package. Focused file/UI checks passed after caching
  the immutable preview. Logs: `.artifacts/stamps-2026-09-20/`.
- `task verify` passed lint, contract gates, all Go tests, Rust test/fmt/Clippy,
  parser release build and desktop build. Toolchain: Go 1.25.13, GCC 15.2 UCRT,
  Task 3.53.1, golangci-lint 2.12.2 and Rust 1.82.0 Windows GNU. Rust currently
  has zero unit tests; the inherited ImGui C++ `memset` warning remains.

## Remaining qualification

Human desktop and native file-picker interaction remain unverified. This is a
file-based prototype with one current stamp per pane, not an indexed stamp
library or a slot manager. Representative large-stamp latency/memory and broad
environment compatibility remain unmeasured. Hosted/database and cross-repository
acceptance gates remain separate.
