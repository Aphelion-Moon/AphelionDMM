# Retained draft inspection and export

Continuation from `8d1c4521`. This pass closes the terminal-error retention and
explicit draft inspection/export gaps identified by the previous delivery report.
It does not close the wider editor, performance or hosting workplans.

## Behavior

- Terminal executor failure now retains queued operations as delivery-unconfirmed
  drafts, using the same rollback boundary as recoverable suspension. The last
  acknowledged snapshot stays available. Late replies cannot mutate a terminal
  executor or remove its recovery data.
- The Collaboration panel shows each draft's before and intended values as well
  as any rejection-provided authority. Prefab stack order is preserved. Preview
  tile/prefab/variable counts remain bounded, and individual strings are limited
  to 1,024 Unicode code points. Existing credential redaction covers both new
  preview sections.
- Export Draft uses the native save dialog and the existing staged atomic-file
  writer. Version 1 JSON contains the complete operation, recorded revision and
  map hash. It omits connection credentials, addresses and free-form error text.
  Oversized operations can be exported without the wire limit truncating them.
  This is a recovery reference; no automatic import or submission is provided.
- Export leaves the draft retained. Leaving, closing or replacing the project
  through desktop actions requires rebuilding or explicitly discarding retained
  drafts. The close permit checks again after any save confirmation. A
  disconnected session can be left after its drafts are resolved.

The existing 100-entry in-memory retention policy remains. Explicit export is
not automatic crash recovery. Low-level forced shutdown still releases resources;
the desktop user actions use the guarded close path.

## Evidence

Logs are in ignored `.artifacts/draft-recovery-2026-09-20/`.

- Red/green executor regressions reproduce lost drafts after explicit termination
  and malformed inbound messages. Both preserve exact intent after repair and
  reject a subsequently valid acceptance without changing acknowledged state.
- Session/controller tests export an operation containing more than 3 MiB of
  Unicode values through the real WebSocket transport's local size rejection.
  JSON round-trip preserves the operation, unknown prefab path, stable IDs and
  authority metadata; export preserves the unresolved conflict. Missing drafts
  and invalid destinations fail without losing retained intent or a prior export.
- Immediate and delayed project-close checks refuse the retained draft. Explicit
  discard unblocks closure. Preview tests cover stack order, credential redaction
  and leaving a disconnected session after resolution.
- Affected client/UI race tests passed. `task verify` passed lint, contracts, the
  Go suite, pinned Rust test/fmt/Clippy, release parser and Windows editor build.
  Rust currently has zero unit tests; default external-service and GL skips are
  not acceptance evidence.

The inherited ImGui C++ `memset` warning persists. Native panel interaction and
the save dialog were compiled but not exercised: the current host's previously
observed WGL/OpenGL initialization failure remains unresolved.

## Remaining work

Automatic durable draft recovery/import, recovery of desktop command history,
legacy oversized persisted replay, local-executor oversized-action UX and
deliberate batching/history rules remain open. Broader performance/QoL, actual
Meridian/PostgreSQL, hosted CI and human desktop/production acceptance gates also
remain open. No dependency manifests or protected infrastructure changed in this
pass; the previously installed toolchains were reused. The pre-existing staged
server-inventory document is untouched.
