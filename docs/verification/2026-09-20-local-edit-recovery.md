# Local edit recovery

The map tools panel now offers **Inspect retained edit** while an unsubmitted
capture or capture fault blocks Save. Inspection opens a modal with a bounded
preview of changed/captured tiles, their before-values, and recorded authority.
Closing it keeps the edit. **Export JSON** saves the inspected record without
committing, resolving, discarding, or clearing the Save guard. **Discard local
edit...** requires a second explicit confirmation inside the modal.

## Ownership and data

`internal/aphelion/editing/recovery` captures an immutable, versioned JSON
reference containing the complete display, dimensions, captured before-states,
and recorded authoritative snapshot. It reads raw display entries, rather than
using `mapadapter.CaptureTile`, which can assign missing stable IDs. Invalid or
empty IDs, nil tile/instance/prefab/variable-storage entries, unknown types,
variable values and instance ordering are retained. Source paths, connection
configuration and free-form errors are excluded. Values that cannot round-trip
losslessly through JSON (invalid UTF-8) refuse inspection/export; they are never
silently replaced. This format is a recovery reference, not an import format,
wire operation, saved DMM/TGM, or automatic crash-recovery file.

Preview shows at most 20 changed/captured display entries and 4,096 bytes per
before/current/authority section, marking truncation. Export has no preview or
wire-message truncation. Export requires a `.json` target and uses the existing
staged, flushed, byte-validated atomic replacement. The inspected record remains
unchanged if the editor later changes; exporting it does not approve discard.

Discard verifies the originating editor, attachment, display generation and
complete raw contents again. It refuses an active selection/paste preview,
unresolved submission, pending acknowledgement, closed/disposed history, changed
contents, or unavailable/incompatible authority. A failed check retains the
journal and display. Success validates and installs the executor's current
snapshot, then clears only the local journal/fault and refreshes the display.
Attachment/callback and accepted-history ownership remain unchanged; no new
operation or undo command is created. The pane clears stale tool pointers after
success without invoking legacy tool completion. Network conflict drafts remain
owned by the existing collaboration recovery UI.

Incoming projection, ordinary refresh and completion synchronization previously
could replace a capture-faulted display when no journal entry had been acquired.
They now preserve that display until explicit recovery or validated attachment.
Incoming projections remain queued/coalesced by the existing executor mechanism.

## Evidence

- Added recovery/API and ImGui control regressions before implementation; initial
  focused runs failed on the absent entry points. A separate projection
  regression reproduced replacement of damaged display contents before the fix.
- Editor checks exercise export without mutation, export failure, exact discard
  to committed authority, preserved accepted undo/redo, stale raw contents and
  journal, changed attachment, another editor, closed editor, invalid authority,
  active preview, unresolved submission and pending acknowledgement.
- Archive checks retain malformed entries and unknown values without identity
  repair, omit local paths, retain immutable exported contents, reject map-file
  extensions and refuse lossy JSON conversion.
- Actual ImGui button presses require two discard steps and display refusal
  errors. The export action retains the record and does not authorize discard.
- `TestLocalRecoveryWorkspaceSave` uses the hidden native GL workspace and real
  mouse Move tool: a valid hop precedes a damaged destination, release preserves
  the preview, pane inspection opens the application modal, export retains it,
  explicit recovery restores authority, and actual `WsMap.Save` writes a map
  that reparses and clears the unsaved-state guard.
- `go test -race` passed for recovery, editor, pmap and native window packages
  with `APHELIONDMM_GL_TEST=1`. After responsive modal sizing, focused UI/native
  race checks passed again. `task verify` passed lint, contract gates, all Go
  tests, Rust test/fmt/Clippy, parser release build and desktop build.

Evidence logs are under `.artifacts/local-recovery-2026-09-20/`. Toolchain:
Go 1.25.13, GCC 15.2 UCRT, Task 3.53.1, golangci-lint 2.12.2, and
Rust 1.82.0 Windows GNU. Rust currently contains zero unit tests. The inherited
ImGui C++ `memset` compiler warning remains.

## Remaining qualification

Native file-picker interaction and human desktop acceptance are not automated
here. This is explicit in-session recovery; there is no automatic persistence or
resubmission. Full-map inspection/export allocates on demand and has not been
qualified for representative large-map latency or memory usage. It does not
complete hosted/database or cross-repository acceptance gates.
