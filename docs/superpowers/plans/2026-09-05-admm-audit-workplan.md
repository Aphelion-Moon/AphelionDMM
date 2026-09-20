# ADMM Audit Remediation and Performance Workplan

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task by task. Follow the user's authorization for implementation, subagents, Git operations, and infrastructure changes; this audit authorizes none of those follow-on actions by itself. Steps use checkboxes for tracking.

**Goal:** Restore snapshot-safe durability, ordered collaboration, and safe desktop editing/saving for the nine defects independently identified in the 2026-09-05 audit. Audit performance across the entire repository, establish reproducible measurements of shipped behavior, and produce a prioritized optimization backlog supported by those measurements.

**Architecture:** Keep one authoritative document owner and the existing operation engine. Repair the private recovery and publication boundaries, preserve immutable submitted intent independently from speculative display, and make desktop callbacks and saving use explicit ownership of acknowledged state. Keep transport version 1 unchanged unless a separate compatibility decision is approved.

**Tech stack:** Go 1.25.13; current pinned SQLite/PostgreSQL, coder/websocket, Dear ImGui adapter; Rust 1.82.0 GNU toolchain on Windows; Task 3.53.1 and golangci-lint 2.12.2 as currently checked.

**Spec:** `docs/superpowers/specs/2026-08-24-multiplayer-design.md`, `docs/agent/multiplayer-invariants.md`, and the source-backed [audit](../../verification/2026-09-05-code-audit.md). Audit baseline: `18429fc79bf69bb4720cfc92c13d028313438024`.

## Execution status — 2026-09-06

The user's subsequent "Proceed" authorized execution. Primary repairs are
implemented and uncommitted. The [qualification record](../../verification/2026-09-06-audit-remediation.md)
is the current evidence index; the initial audit remains historical evidence.
Do not treat all detailed checkboxes below as complete: several combine broader
edge cases or human/external gates beyond the named tests actually run.

| Tasks | Current result | Remaining boundary |
| --- | --- | --- |
| 1–2, recovery/integrity | Implemented; regressions and live memory/SQLite/PostgreSQL conformance passed | Larger fault/corruption combinations and recovery performance remain in follow-up coverage. |
| 3–4, ordering/conflicting drafts | Implemented; owner/hub, forced WebSocket rejection ordering and submitted-draft tests passed | Dependent drafts rejected before network submission need explicit coverage/handling. |
| 5–6, gesture/attachment/save | Implemented; gesture and attachment regressions, real hidden-context Save and close-save helper tests passed | Supplemental rejection/history/render-callback interleavings and human Save All/close acceptance remain. |
| 7–8, duplicates | Implemented; normalized retry and older exact duplicate regressions passed | Expanded altered-duplicate and multi-revision combinations remain. |
| 9, unknown prefabs | Implemented; setup, explicit values, filtering/grouping/read-only behavior passed | Instance/pinned/reload entry points and human inspection remain. |
| 10, performance audit | [Repo-wide source coverage and repeated component/pilot measurements delivered](../../verification/2026-09-05-repo-performance-audit.md) | Runtime coverage is partial. See the [ranked follow-up workplan](2026-09-06-admm-performance-followup.md) for concurrency, representative maps, desktop/native/GPU and lifecycle work. |
| 11, qualification | Final `task verify`, Aphelion race, live PostgreSQL/restore, real workspace Save, smoke/doctor, local image build and hosted lifecycle passed | Real Meridian/Rift/Content Tools, hosted CI/production, full performance qualification and named-human acceptance remain unrun. |

No performance optimization, protected infrastructure edit, commit, push, or
deployment was performed. Resume from the remaining boundaries above instead of
blindly repeating the initial implementation sequence.

## Global constraints

- Re-read `AGENTS.md` and the relevant routed guidance before editing. Recheck source and working-tree state; this is a workplan, not proof of future correctness.
- Preserve unrelated dirty work. No reset, checkout, merge, commit, push, worktree creation, or delegation without the user's applicable authorization.
- Use PowerShell for Windows commands. Check `$LASTEXITCODE` after each native command and stop the current gate on failure.
- Write the failing behavior test before each fix; run its narrow gate before broader tests. Leave red/green evidence tied to the actual source revision and diff.
- Keep new implementation under `internal/aphelion/` where possible; mark narrow inherited-editor changes with the required `APHELION EDIT` markers.
- Acknowledgement follows durable append. Validation is deterministic and all-or-nothing. Environment/base hashes, actor ownership, duplicate identity, inverse safety, and unknown-map-content preservation remain mandatory.
- Pure local mode and network mode use the same operation semantics. Presence stays ephemeral and cannot block durable acknowledgements.
- Do not change public schema/protocol semantics to conceal these failures. If the chosen solution requires such a change, prepare the exact contract diff and compatibility fixtures for the required decision first.
- `.github/workflows/ci.yml`, `Taskfile.yml`, `Taskfile_windows.yml`, release/signing/deployment/build entry points remain protected. This plan does not require editing them. Any later proposed change needs exact-file/effect explanation and explicit confirmation under root `AGENTS.md`.
- No art, branding, or external asset changes. Keep repository artifacts free of personal names, account directories, secrets, and machine-specific absolute paths.

## Sequence and exit criteria

| Order | Deliverable | Finding | Depends on |
| --- | --- | --- | --- |
| 1 | Recover complete validation context across snapshots | F1 | Fresh baseline |
| 2 | Verify persisted identity, continuity, and hashes | F2 | 1 |
| 3 | Publish revisions and dependent responses in order | F3 | 1–2 |
| 4 | Preserve conflicting submitted intent | F4 | 3 |
| 5 | Preserve active gestures and fence old callbacks | F6 | 4 |
| 6 | Save only acknowledged map state | F5 | 5 |
| 7 | Normalize server duplicate comparisons | F7 | 1–2 |
| 8 | Accept verified older event duplicates | F8 | 4, 7 |
| 9 | Make preserved unknown prefabs safe to inspect | F9 | Fresh baseline; coordinate with 5–6 |
| 10 | Audit repo-wide performance and produce a measured optimization backlog | Performance scope below | Inventory and baseline capture can precede repairs; final qualification follows 1–9 |
| 11 | Qualify the repaired composition and record remaining gates | All | 1–10; disclose blocked performance environments |

Each repair is independently reviewable. Do not combine these with speculative performance refactors or a broad editor rewrite.

## Task 1: preserve validation context across snapshots

**Files:** modify `internal/aphelion/collab/engine/document.go`, `engine/inverse.go`, `server/recovery.go`, `store/store.go`, `store/memory.go`, `store/sqlite/store.go`, and `store/postgres/store.go` under the same collaboration root. Add private recovery helpers beside their owning package if needed. Tests belong in `server/recovery_test.go`, `server/snapshotter_test.go`, `store/conformance.go`, and the SQLite/PostgreSQL test files.

**Interfaces:** preserve `DocumentOwner.Submit`, `BuildInverse`, and the wire replay contract. Introduce an internal recovery-data boundary distinct from network `Load` if necessary: the recovery payload must carry a validated snapshot, contiguous suffix, retained base-revision hashes, accepted inverse targets, and already-inverted status. All store implementations must supply the same logical information. The existing operation rows and revision-hash tables already retain historical records; inspect and reuse those before proposing a schema migration.

- [ ] Install the three real SQLite probes from `docs/verification/audit-2026-09-05/server_test.go.txt` as regression tests beside the production code. Preserve their sequences; do not change stale bases to the current revision to make the tests pass.
- [ ] Run the red tests:

```powershell
go test ./internal/aphelion/collab/server -run '^TestAuditSQLite(UndoAcrossSnapshot|StaleBaseAcrossSnapshot|SnapshotCanMakeAcknowledgedLogUnrecoverable)$' -count=1 -timeout 60s
```

Expected at the audit baseline: safe undo fails with `operation_not_found`; nonoverlapping stale submission and acknowledged-tail recovery fail with `unknown_base_revision`.

- [ ] Restore the engine's required historical validation state before validating any suffix or newly appended operation. Use the same restoration path for server recovery and SQL-store append verification. Check that retained hashes and inverse metadata are bound to this document and persisted records; never inject client-provided history.
- [ ] Keep snapshot publication independent from a newer append. A snapshot captured at revision N must remain recoverable with every acknowledged suffix through the durable head, including an inverse referencing an operation at or before N and an operation based before N.
- [ ] Add the same cases to the shared store conformance contract and live PostgreSQL suite. Also test undo after restart, redo after that undo, and repeated snapshot/restart cycles. A safe inverse must still reject actor mismatch and changed after-values.
- [ ] Run the narrow gate green, then:

```powershell
go test ./internal/aphelion/collab/engine ./internal/aphelion/collab/server ./internal/aphelion/collab/store/... -count=1 -timeout 120s
```

**Acceptance:** no snapshot timing can strand an acknowledged suffix; the three probes pass without weakening validation. PostgreSQL is not accepted until its database-backed equivalents actually run rather than skip.

## Task 2: enforce storage integrity before serving recovered documents

**Files:** `internal/aphelion/collab/store/sqlite/store.go`, `store/postgres/store.go`, `server/recovery.go`; tests in SQLite `fault_test.go`, PostgreSQL `store_test.go`/`restore_test.go`, and server `recovery_test.go`.

**Interfaces:** consume Task 1's complete recovery state. A load/recovery failure returns an error before `startDocument` or hosted session registration. Public readiness remains unavailable for a failed document.

- [ ] Install `TestAuditSQLiteRecoveryChecksStoredHash`. Run:

```powershell
go test ./internal/aphelion/collab/server -run '^TestAuditSQLiteRecoveryChecksStoredHash$' -count=1 -timeout 60s
```

Expected red: a snapshot with altered dimensions is served despite its unchanged stored hash.

- [ ] Read snapshot bytes, row identity/revision/hash, retained events, and durable head in one consistent transaction. In PostgreSQL, do not discard `current_revision`/`current_hash`; use a consistent read isolation level. In SQLite, derive/check the head against the persisted revision-hash ledger and operation rows.
- [ ] Require serialized document ID and revision to match their row; compare the snapshot's canonical hash with stored snapshot metadata; verify contiguous revisions and the canonical resulting hash of each replayed event; compare the reconstructed final revision/hash with the durable head.
- [ ] Add corruption cases for a valid-JSON changed variable, wrong document ID, mismatched row/snapshot revision, changed operation payload/hash, missing middle event, and deleted final event with a retained head. Test both an empty suffix and a nonempty suffix. Every failure preserves the original database and produces failed readiness.
- [ ] Add a concurrent append/load test to ensure the integrity checks cannot read half of two valid database states and falsely reject a healthy document.
- [ ] Run the narrow test green, then the engine/server/store gate from Task 1 and configured PostgreSQL restore tests.

**Acceptance:** structurally valid corruption is rejected, and the last acknowledged revision is explicitly proven rather than inferred from the remaining rows.

## Task 3: put publication under document ordering

**Files:** `internal/aphelion/collab/server/document.go`, `hub.go`, `websocket.go`; tests in `hub_test.go`, `websocket_test.go`, `e2e_test.go`, and client conformance tests where public transport is exercised.

**Interfaces:** preserve `Hub.SubmitWithStatus` and `Hub.Inverse` result behavior. Move accepted-event publication into the same per-document sequence that owns append acceptance. Subscriber buffers remain bounded; slow consumers are disconnected without blocking the owner indefinitely.

- [ ] Install `TestAuditHubBroadcastsInRevisionOrder`; run:

```powershell
go test ./internal/aphelion/collab/server -run '^TestAuditHubBroadcastsInRevisionOrder$' -count=1 -timeout 60s
```

The recorded red run received revision 4 first. This probe is scheduler-sensitive; add a deterministic scheduling fixture at the owner/publication seam to force a later caller to run first. Assert the original subscriber order without sorting.

- [ ] Publish each newly accepted event once, after its durable append and before the next event can overtake it. Remove independent caller-side publication from both forward and inverse paths. Do not use a single service-wide lock around slow database I/O.
- [ ] Route per-connection responses through an ordered writer. Before emitting a rejection at revision R, ensure that connection has received the contiguous accepted sequence through R. Keep duplicates separately correlated without advancing the revision.
- [ ] Add actual two-client WebSocket tests for concurrent disjoint edits, a competing edit rejection arriving behind its authoritative acceptance, inverse publication, replay-to-live handover, and slow consumer disconnect. Assert convergence without a repair reconnect for an ordinary conflict/disjoint edit.
- [ ] Run the narrow tests green, then:

```powershell
go test ./internal/aphelion/collab/server ./internal/aphelion/collab/client -count=1 -timeout 120s
go test -race ./internal/aphelion/collab/server ./internal/aphelion/collab/client -count=1 -timeout 120s
```

**Acceptance:** every live durable subscriber observes contiguous server order; valid ordered acknowledgements are not lost or overtaken by their dependent rejection responses.

## Task 4: preserve local drafts through competing authoritative edits

**Files:** `internal/aphelion/collab/client/reconcile.go`, `executor.go`, their test files, and `internal/aphelion/collab/ui/session_client.go`/`conflict_test.go` if event-state handling needs adjustment.

**Interfaces:** retain the `Execute`, `Receive`, `Conflicts`, refresh/discard/rebuild surface. Keep the submitted operation payload immutable until resolved; speculative visibility is separate state. Compatible speculation may remain visible, while incompatible speculation can be hidden without deleting the original draft or waiter.

- [ ] Install `TestAuditConcurrentSameTileRetainsConflictDraft`; run:

```powershell
go test ./internal/aphelion/collab/client -run '^TestAuditConcurrentSameTileRetainsConflictDraft$' -count=1 -timeout 60s
```

Expected red: acknowledgement stays at 0 and both pending/conflict collections become empty after valid revision 1.

- [ ] Commit valid authoritative progression independently of whether every speculative operation can be reapplied. Track incompatible pending intent until the ordered server response resolves its operation ID. Reserve synchronization failure for invalid authoritative identity, revision, or hash.
- [ ] When rejection arrives, remove only that operation's speculation and retain its exact draft for the existing conflict UI. Preserve other independent drafts. Handle dependent queued local edits explicitly instead of clearing every waiter or forcing a value overwrite.
- [ ] Test two same-tile clients through the public service, multiple independent pending operations, dependent local operations, normal rejection, disconnect, refresh, discard, and rebuild. Ensure rebuild uses a new operation ID and current authoritative before-values.
- [ ] Run the red test green, then:

```powershell
go test ./internal/aphelion/collab/client ./internal/aphelion/collab/ui ./internal/aphelion/collab/server -count=1 -timeout 120s
```

**Acceptance:** competing edits converge while retaining the rejected user's intent and exposing the documented conflict actions; valid remote edits do not trigger an unnecessary resynchronization failure.

## Task 5: preserve newer gestures and reject stale editor completions

**Files:** `internal/app/ui/cpwsarea/wsmap/pmap/editor/collaboration.go`, `editor.go` if lifecycle state is added, and `collaboration_test.go`. Mark inherited source spans as required.

**Interfaces:** maintain the editor's executor interface. Each queued completion records the attachment generation and the operation it completes. Updating acknowledged state is separate from clearing the current gesture capture.

- [ ] Install `TestAuditAcknowledgementPreservesActiveGesture` from `docs/verification/audit-2026-09-05/editor_test.go.txt` and run:

```powershell
go test ./internal/app/ui/cpwsarea/wsmap/pmap/editor -run '^TestAuditAcknowledgementPreservesActiveGesture$' -count=1 -timeout 60s
```

Expected red: the acknowledgement of the 2 → 4 edit erases the open 4 → 8 gesture.

- [ ] Split authoritative-baseline refresh from gesture reset. An earlier completion cannot clear `pendingChanges` captured after its submission. Reprojection must wait for, or explicitly preserve/rebase, the active gesture rather than overwriting its visible map.
- [ ] Increment an attachment generation on initialize/attach/detach/reset and fence queued callbacks against it. A callback from an old executor must never assign that executor back to the editor or modify the replacement session's history.
- [ ] Extend the regression to commit the second gesture and assert an acknowledged second operation, the intended final direction, and two correctly scoped undo entries. Add rejection during a new gesture, queued completion followed by detach/reattach, and map close with queued callbacks.
- [ ] Run the narrow gate green, then:

```powershell
go test ./internal/app/command ./internal/app/ui/cpwsarea/wsmap/pmap/editor ./internal/aphelion/collab/client ./internal/aphelion/collab/ui -count=1 -timeout 120s
```

**Acceptance:** an earlier result cannot erase newer local input, restore an obsolete attachment, or corrupt undo ownership.

## Task 6: make the shipped Save path use acknowledged state

**Files:** `internal/app/ui/cpwsarea/wsmap/save.go`, new `save_test.go` beside it, narrow editor/interface adapters as required, and `internal/dmapi/dmmsave` only if necessary to reuse existing format/key handling. Review Save All and close flows in `internal/app/action_user.go` and `internal/app/ui/cpwsarea/wsarea.go`.

**Interfaces:** keep `WsMap.Save() bool`. Add a narrow editor-facing way to capture an immutable saveable snapshot and identify active gestures, unregistered asynchronous submissions, and unacknowledged operations. Do not treat absence of a command-history entry as proof that nothing is pending.

- [ ] Create a fixture around the real `WsMap.Save` using a temporary original map, current environment, command stack, and delayed network executor. Define these tests before implementation:

```text
TestSaveRefusesUnacknowledgedOperation
  Begin edit; submit without acknowledgement; call WsMap.Save.
  Require false, original file bytes unchanged, and pending/dirty state retained.
TestSaveUsesAcknowledgedSnapshot
  Acknowledge edit; call WsMap.Save; reparse output.
  Require true and acknowledged semantic contents in the file.
TestSaveAfterRejectedEditPreservesAuthoritativeContents
  Reject speculative edit; save after reconciliation.
  Require the rejected contents absent and authoritative contents preserved.
TestSaveFailureDoesNotBalanceCommands
  Force staging/validation/replace failure; call WsMap.Save.
  Require false, original file intact, and dirty state unchanged.
```

- [ ] Run `go test ./internal/app/ui/cpwsarea/wsmap -run '^TestSave' -count=1 -timeout 60s` and record the failing behavior.
- [ ] Refuse Save while a gesture or submission is unresolved. When saving is allowed, capture acknowledged state once and serialize an isolated representation with the existing format and fidelity policies. Never pass the mutable speculative display DMM as save authority. Balance only the successfully persisted state.
- [ ] Add Save All and close confirmation tests to prove a blocked save does not silently close a workspace or report all maps saved. Cover remote projection changes whose authority has advanced independently from local command history.
- [ ] Run the workspace gate green, then:

```powershell
go test ./internal/app/ui/cpwsarea/... ./internal/dmapi/dmmap/dmmdata ./internal/dmapi/dmmsave ./internal/aphelion/collab/mapadapter -count=1 -timeout 120s
```

**Acceptance:** no speculative operation reaches the target file, blocked/failed saves preserve both target and dirty state, and accepted unknown map data still round-trips.

## Task 7: normalize duplicate submission identity consistently

**Files:** `internal/aphelion/collab/engine/document.go`, `internal/aphelion/collab/server/document.go`, model/engine normalization helper if needed; tests in `engine/document_test.go`, `server/document_test.go`, store conformance, and protocol transport tests.

**Interfaces:** define one pure operation normalization path used for accepted records and duplicate comparison. Preserve original operation ID, actor, base revision/hash, preconditions, and inverse target. Normalization may order coordinates and representation-equivalent collections; it must not rewrite intent.

- [ ] Install `TestAuditDuplicateUnsortedOperation`; run:

```powershell
go test ./internal/aphelion/collab/server -run '^TestAuditDuplicateUnsortedOperation$' -count=1 -timeout 60s
```

- [ ] Compare normalized incoming and persisted operations before returning a duplicate. The exact same retry returns the original revision and acceptance timestamp without appending, applying, or broadcasting again.
- [ ] Test ascending/descending coordinate order, equivalent empty collection representations allowed by the contract, lost acknowledgement, retry after restart/snapshot, and ID reuse with a different actor/before/after/base/inverse. The latter cases must still fail.
- [ ] Run the narrow test green, then the core/store gate from Task 1 plus the actual WebSocket retry test.

**Acceptance:** an accepted request remains idempotent regardless of its original valid coordinate order, without weakening identity or content checks.

## Task 8: recognize verified older accepted duplicates

**Files:** `internal/aphelion/collab/client/executor.go`, `executor_test.go`, `reconcile_test.go`, and reconnect conformance tests.

**Interfaces:** retain enough accepted-event identity and per-revision hash information to validate an exact duplicate at an older revision. Keep the result independent from the current acknowledged hash; a later map hash cannot authenticate an earlier event.

- [ ] Install `TestAuditOlderExactAcceptedDuplicate`; run:

```powershell
go test ./internal/aphelion/collab/client -run '^TestAuditOlderExactAcceptedDuplicate$' -count=1 -timeout 60s
```

- [ ] Return success without changing acknowledged state or resolving unrelated waiters for an authenticated, retained exact duplicate. Keep fail-closed handling for altered payload, timestamp/revision association, or map hash.
- [ ] Test duplicates of revisions 1 and 2 after advancing further, changed duplicate hashes and contents, duplicates with compatible pending edits, and reconnect/snapshot fallback. Define what happens if verification history has been deliberately evicted; never blindly ignore unknown old events.
- [ ] Run the narrow test green, then the client/UI/server gates from Tasks 3–4.

**Acceptance:** exact older duplicates do not suspend the executor; altered or unauthenticated old events are still rejected.

## Task 9: make unknown prefabs safe in the variable editor

**Files:** `internal/app/ui/cpvareditor/vareditor.go`, `collect.go`, `process.go`, new tests beside the component, and narrow adapters under `internal/aphelion/` if needed. Preserve inherited source markers. Recheck callers in `internal/app/action_user.go`.

**Interfaces:** keep `EditPrefab` and `EditInstance`. Unknown type metadata is an explicitly absent value, not an object to dereference. Preserve the path and exact explicit variable values regardless of metadata availability.

- [ ] Install `TestAuditVariableEditorAcceptsUnknownPrefab` from `docs/verification/audit-2026-09-05/unknown_test.go.txt`; run:

```powershell
go test ./internal/app/ui/cpvareditor -run '^TestAuditVariableEditorAcceptsUnknownPrefab$' -count=1 -timeout 60s
```

Expected red: `EditPrefab` panics with a nil pointer dereference while collecting type paths.

- [ ] Represent missing environment metadata explicitly during setup and path collection. Render the available explicit variables without traversing a nonexistent type hierarchy. Give default-value, flags, filtering, pinning, and grouped-view paths the same handling.
- [ ] Keep unknown values inspectable. If metadata is required for an editing action, disable that action with an existing neutral UI explanation while preserving the data; do not invent default values or erase explicit overrides. Any supported edit must use the ordinary operation capture/commit path.
- [ ] Test both prefab and instance entry points, empty and nonempty opaque variables, grouped/default/pinned views, environment reload, and subsequent snapshot/save round trips. Add a shipped desktop acceptance step for opening an unknown type from the map and prefab list.
- [ ] Run the narrow test green, then:

```powershell
go test ./internal/app/ui/cpvareditor ./internal/app/ui/cpwsarea/wsmap/pmap/editor ./internal/aphelion/collab/mapadapter ./internal/dmapi/dmmap/dmmdata -count=1 -timeout 120s
```

**Acceptance:** a preserved unknown prefab can be opened and inspected without crashing; its path/variables survive operation and save round trips.

## Task 10: perform a repo-wide performance audit

**Scope:** inspect every executable subsystem, including inherited StrongDMM code, the Rust/cgo parser boundary, the desktop, collaboration services, integrations, and developer tooling. This task produces audit evidence and a work backlog. It does not authorize speculative optimizations or edits to protected infrastructure. The performance audit is planned here; no runtime performance result is established by this document.

**Timing:** begin the source inventory, harness review, and reproducible baseline capture before correctness repairs where useful. Record the original revision and dirty diff with each run. Label workloads affected by F1–F9 as correctness failures; they cannot establish a valid throughput or latency baseline. Repeat affected workloads after repairs, retaining both records. Use the repaired, correct implementation as the control for subsequent optimization comparisons.

**Deliverables:** create `docs/verification/<date>-repo-performance-audit.md` with the coverage matrix, workload manifest, measurement method, results, and limitations; keep raw profiles, traces, benchmark output, and fixture copies under an ignored `.artifacts/performance-<date>/` directory. Add a dated optimization plan under `docs/superpowers/plans/` only after measurements identify worthwhile changes. Use repository-relative paths in published records and keep local configuration and credentials out of them.

### 10.1 Inventory source and verify the measuring tools

- [ ] Start with `rg --files` and inspect callers from `main.go`, every `cmd/` executable, and the UI/service lifecycle. Map every runtime directory to a row below. Account for `internal/env`, `internal/util`, `internal/rsc`, contracts, scripts, and vendored dependencies through their callers; explicitly identify generated/data-only files and unsupported platform paths. Prior plans, test names, and profiling reports are leads to verify, never proof of coverage.
- [ ] Record, per subsystem: source paths and symbols, synchronous work on the UI/document-owner threads, loops and sorting, copying/serialization, allocation ownership, cache lifetime, queues/locks, file/database/network calls, existing instrumentation, and the experiment needed to confirm each suspected cost. Distinguish a source hypothesis from a sampled hotspot and from a demonstrated user-visible bottleneck.
- [ ] Recheck the existing benchmarks. At this planning revision, the Go benchmark search found only `BenchmarkDisabledTelemetry` and `BenchmarkNoExporterTelemetry` in `internal/aphelion/collab/telemetry/telemetry_test.go`; neither measures the complete editor or collaboration path. Add focused benchmarks beside measured code, with new Aphelion-owned harness support under `internal/aphelion/` as needed. Audit setup costs, timer boundaries, fixture resets, and correctness assertions before accepting output.
- [ ] Validate `cmd/apheliondmm-loadtest/main.go`, `internal/aphelion/collab/load/runner.go`, `scenario.go`, and `testdata/collaboration/load/pilot.json` independently. The current runner sends one operation, waits for all clients to receive it, then sends the next; it sends presence only after all operations. Its acknowledgement histogram therefore measures send-to-all-client-delivery under serial demand. The checked-in pilot is 25 clients, 250 operations on a 10-by-10 single-level map. Preserve this as a named compatibility scenario, but do not treat it as concurrent sustained load, simultaneous presence pressure, or a representative map-size test.
- [ ] Before extending that harness, add failing tests proving intended send cadence, overlapping editors, concurrent presence, slow-reader behavior, cancellation, and correct latency attribution. The current generated operations depend on prior revisions: introduce valid deterministic concurrent intents and explicit conflict expectations instead of merely parallelizing that loop. Verify every client's applied revision/hash, not only the final server snapshot.
- [ ] For sustained-load measurements, record scheduled/offered, actually sent, accepted, rejected, and observed operations, backlog, and achieved rates. Separate schedule-to-durable-ack, send-to-durable-ack, all-client-delivery, and desktop-visible latency. A slow service must not silently reduce offered demand and hide queueing delay. Count errors, timeouts, disconnects, and dropped presence alongside latency.

### 10.2 Cover every performance domain

Paths in this table are inspection targets, not claims that each contains a defect. Record a measured result or an explicit unrun/blocked reason for every row.

| Domain | Source targets | Required measurements and experiments |
| --- | --- | --- |
| Startup and environment loading | `main.go`, `internal/app/app.go`, `project.go`, `internal/dmapi/dm/`, `dmenv/`, `third_party/sdmmparser/sdmmparser.go`, `third_party/sdmmparser/src/environment.rs` | Process start to interactive window and usable map; discovery, Rust parse, native-to-Go string/JSON transfer, tree construction, and UI initialization separately. Compare fresh-process and warm reload behavior with fixed environment hashes and recorded file/type counts. |
| Icons and resources | `internal/dmapi/dmicon/`, `internal/platform/texture.go`, `internal/rsc/`, `internal/app/window/fonts.go`, `third_party/sdmmparser/src/icon.rs` | First-use and cached icon lookup, metadata parsing, image decode/conversion, texture upload, cache growth and disposal, font initialization. Separate Go allocations, native memory, process private bytes, and GPU resources; include missing-icon and repeated environment-switch paths. |
| Map parsing and saving | `internal/dmapi/dmmap/`, `dmmap/dmmdata/`, `internal/dmapi/dmmsave/`, `dmmclip/`, `internal/app/ui/cpwsarea/wsmap/save.go`, `internal/aphelion/collab/mapadapter/` | DMM/TGM parse, map construction, import/export, dictionary/key reuse, variable sorting, save validation, backup and atomic replacement, clipboard serialization, and screenshot readback. Measure elapsed/CPU time, peak memory, allocations, bytes and I/O. Keep unknown content, deterministic hashes, and atomicity checks enabled. |
| Frame rendering and input | `internal/app/window/process.go`, `internal/platform/gl.go`, `glfw.go`, `internal/app/render/`, `render/bucket/`, `internal/app/ui/cpwsarea/wsmap/pmap/canvas/`, `canvas_camera.go`, `internal/imguiext/` | Frame-time distributions, missed frame budget, input-to-visible latency, CPU/GPU time, draw calls, submitted geometry, texture changes, culling, bucket rebuilds, and canvas resize. Exercise stationary, pan/zoom, level switch, animation, overlays, minimized/unfocused states, and multiple map tabs. Record the frame cap, vsync, resolution and display scale; capped FPS alone cannot show spare capacity. |
| Editing, undo and navigation | `internal/app/ui/cpwsarea/wsmap/pmap/editor/`, `pquickedit/`, `tools.go`, `internal/app/command/`, `internal/dmapi/dmmsnap/`, `internal/app/ui/cpsearch/`, `cpprefabs/`, `cpenvironment/`, `cpvareditor/` | Single tile, sustained drag, fill, large selection, copy/paste, variable changes, undo/redo, search/filter, and tree expansion. Attribute gesture capture, diffing, operation construction, speculative projection, acknowledgement callbacks and render refresh. Measure stalls and retained history growth as map size and changed-tile count vary independently. |
| Authoritative engine and local executor | `internal/aphelion/collab/model/hash.go`, `model/clone.go`, `engine/document.go`, `engine/inverse.go`, `executor/local.go`, `server/document.go` | Validate/apply/hash/clone/inverse time and allocation cost against map cells, prefab/variable density, changed cells and retained operation history. Compare direct engine, pure-local executor, and durable owner paths without equating them. Preserve rejection, idempotency, inverse and base-hash semantics. |
| Client projection and reconciliation | `internal/aphelion/collab/client/`, `internal/aphelion/collab/mapadapter/export.go`, `internal/app/ui/cpwsarea/wsmap/pmap/editor/collaboration.go` | Accepted-event application, pending rebase, conflict retention, replay and snapshot fallback, backlog drain, and time to visible convergence. Vary pending depth and reconnect suffix length; measure allocations, full-map reconstruction and UI invalidations per accepted edit. Include delayed acknowledgements while a new gesture is active. |
| Transport, fan-out and presence | `internal/aphelion/collab/server/hub.go`, `websocket.go`, `http.go`, `presence.go`, `limits.go`, `internal/aphelion/collab/protocol/`, `client/websocket.go`, `cmd/apheliondmm-collab/`, `cmd/apheliondmm-hosted/` | Per-stage latency, serialization bytes/allocations, owner/subscriber queues, lock/block time, fan-out cost, goroutines, connections and throughput. Exercise simultaneous editors/presence, slow readers, bounded reconnect storms, large supported payloads and multiple independent documents. Report service and load-generator resources separately. |
| Persistence and recovery | `internal/aphelion/collab/store/memory.go`, `store/sqlite/store.go`, `store/postgres/store.go`, `server/recovery.go`, `server/document.go`, `store/conformance.go` | Append transactions, query counts/plans, lock/pool waits, history decode/replay, snapshot capture/write/publication, startup recovery, backup/restore and checkpoint export. Vary history and snapshot age; measure database/WAL size, I/O, commit and recovery tails. Run actual SQLite and PostgreSQL modes with documented durability settings; memory-store timing is a component baseline. |
| Authentication, telemetry, integrations and update lifecycle | `internal/aphelion/collab/auth/`, `telemetry/`, `server/hosted.go`, `internal/aphelion/integration/`, `internal/app/update.go`, `selfupdate/`, `internal/req/` | Join/auth latency and cache behavior; instrumentation disabled, enabled and slow/unavailable exporter cases; bounded download/hash/stage/verification calls; cancellation, repeated failures and shutdown. Separate ADMM work from OIDC, OTLP, MCP and other external-service time. Exercise update preparation in test fixtures without publishing or installing a release. |
| Build, tests and support executables | `Taskfile.yml`, `Taskfile_windows.yml`, `.github/workflows/ci.yml`, `third_party/sdmmparser/src/Cargo.toml`, `internal/aphelion/buildcheck/`, `smoke/`, all remaining `cmd/` and maintained scripts | Record clean-cache versus warm-cache build/test duration, Go/Rust/linking contributions, peak resource use, binary size, and smoke/doctor/compat/healthcheck startup. Inspect repeated or serial work. These are developer-workflow measurements, separate from product performance. Use dedicated output/cache locations and maintained entry points; do not invoke broad cleanup or edit protected files. |

### 10.3 Define workloads and acceptance budgets before measuring candidates

- [ ] Build a manifest from existing approved fixtures and read-only copies of representative working maps/environments. Record content hashes, format, dimensions/levels, occupied cells, prefab and variable counts, icon count/pixel volume, file sizes, and history depth. Tiny protocol fixtures alone do not cover production-shaped maps. Mechanical synthetic fixtures may scale existing test content; do not introduce art, names, lore or branding.
- [ ] Use a bounded matrix rather than an uncontrolled full cross-product. Start with small, representative and large supported maps; sparse and dense prefab/variable distributions; one and multiple levels/tabs; and isolated scaling sweeps where only one dimension changes. Document the selected numeric sizes from the fixture inventory and current limits before execution.
- [ ] Cover session histories of 0, 1,000 and 10,000 accepted operations where valid; 1, 5 and 25 editors; and edits touching 1, 100 and 1,000 cells where supported. Keep map size, changed-cell count, clients, pending depth and history as separate axes. Stop increasing a dimension once a reproducible limit is established; report the limit and failed work instead of omitting those samples.
- [ ] Define distinct scenarios for startup/open/save, idle/render interaction, edit/undo/search, steady concurrent collaboration, snapshot/recovery, reconnect/fault recovery and resource lifecycle. Include at least 20 repeated open/edit/save/close or reconnect cycles and a bounded 30-minute representative steady-state run to look for retained growth; confirm suspected growth against object/resource ownership rather than inferring a leak from one high-water mark.
- [ ] Record CPU, RAM, storage class, GPU/driver, OS, power mode, display settings, Go/Rust/C compiler versions, build flags/binary hashes, database configuration, network conditions, instrumentation state and fixture seed. Keep hardware descriptions reproducible without account names or machine-specific paths. Do not compare different modes or workloads as if only the candidate changed.
- [ ] Set provisional user-visible budgets for open/save time, input/frame latency, durable acknowledgements, convergence/recovery, and memory growth from representative measurements and product requirements. At the current 60 Hz UI target, report frames exceeding 16.7 ms, but do not invent a universal startup or map-size SLA. The pilot's 250 ms p95 is a pilot threshold, not a repo-wide acceptance guarantee. Mark any budget needing a product decision explicitly.

### 10.4 Collect profiles and reproducible baselines

- [ ] Use at least one untimed warm-up and five measured trials per normal workload; retain individual results and report median, spread, sample counts, error rates and relevant percentiles. Distinguish process-cold from OS-cache-cold runs and document cache preparation. Do not claim a stable p99 from a 250-operation pilot; gather enough events and repeated windows to show tail variability. Keep profiler-on diagnostic runs separate from uninstrumented acceptance timing.
- [ ] Collect Go CPU/heap/allocation profiles and short execution traces; use mutex/block profiles where waiting is material. Attribute Rust/cgo time with a symbolized native capture and GPU stalls with graphics/frame measurements. A Go heap profile omits native and GPU allocations, and flat/self versus cumulative/inclusive costs must not be added together. Compare matched process private bytes, working set, handles, goroutines, file descriptors where available, database process resources and GPU memory separately.
- [ ] Run the existing telemetry microbenchmarks as a narrow harness check, not a repo performance gate. Example commands below run from the repository root; check each native exit code. New benchmarks and profile files mentioned afterward are planned additions, not existing evidence.

```powershell
$perfDir = Join-Path $PWD '.artifacts/performance-2026-09-05'
New-Item -ItemType Directory -Force -Path $perfDir | Out-Null
go test ./internal/aphelion/collab/telemetry -run '^$' -bench '^Benchmark(DisabledTelemetry|NoExporterTelemetry)$' -benchmem -benchtime=2s -count=5 | Tee-Object -FilePath (Join-Path $perfDir 'telemetry-bench.txt')
if ($LASTEXITCODE -ne 0) { throw 'Telemetry benchmark failed' }
```

- [ ] Add `performance_test.go` beside the engine, model, mapadapter, store, and selected map/parser/render helpers only when their profile hypothesis warrants it. Define `BenchmarkAuditDocumentApply` in `internal/aphelion/collab/engine/performance_test.go` with named map-size/edit-size/history cases, deterministic reset between iterations, and assertions on accepted output. Confirm benchmark discovery and meaningful work before using a zero-test benchmark command. Capture one representative case per profile so unrelated workloads do not obscure attribution.

```powershell
# After the planned benchmark exists; select a documented subcase for attribution.
go test ./internal/aphelion/collab/engine -run '^$' -bench '^BenchmarkAuditDocumentApply$' -benchmem -benchtime=2s -count=5 | Tee-Object -FilePath (Join-Path $perfDir 'engine-bench.txt')
if ($LASTEXITCODE -ne 0) { throw 'Engine benchmark failed' }
# For a separate diagnostic run, add -cpuprofile and -memprofile with paths under
# $perfDir, plus -o for the test binary there; inspect with go tool pprof.
```

- [ ] Exercise the actual load executable against a disposable, correctly seeded test session. Configure endpoint, origin, session and credentials through trusted local configuration; recreate the same initial scenario state before every trial. Preserve the existing pilot separately from the revised concurrent scenarios. This example builds the existing CLI without changing infrastructure; `$loadEndpoint`, `$loadOrigin` and `$loadSession` must refer to that prepared test session, and the CLI's documented credential environment must be configured first.

```powershell
go build -o (Join-Path $perfDir 'apheliondmm-loadtest.exe') ./cmd/apheliondmm-loadtest
if ($LASTEXITCODE -ne 0) { throw 'Load executable build failed' }
& (Join-Path $perfDir 'apheliondmm-loadtest.exe') -endpoint $loadEndpoint -origin $loadOrigin -session $loadSession -scenario testdata/collaboration/load/pilot.json -timeout 10m | Tee-Object -FilePath (Join-Path $perfDir 'pilot-result.json')
if ($LASTEXITCODE -ne 0) { throw 'Pilot scenario failed; retain the failure evidence' }
```

- [ ] Measure the shipped desktop built with `task build` and the pinned Rust target. Drive the recorded UI interaction sequence through `dst/StrongDMM.exe`; helper benchmarks cannot establish interactive performance. If native symbolization needs an unstripped diagnostic build, document the exact flag difference and keep it distinct from release timing. Use narrow Aphelion-owned instrumentation when needed; no public profiling listener, protected build edit, or substituted binary is implied by this plan.
- [ ] Retain reproducible commands and machine-readable outputs alongside sanitized summaries. Give each sample a source/binary/fixture identity and success or failure status. When an environment cannot run locally, name the missing prerequisite and exact next gate; do not substitute mocks for PostgreSQL, native/GPU, real desktop, or hosted measurements.

### 10.5 Investigate source-backed candidates and prioritize measured work

These starting hypotheses were traced in current source while extending the plan. Reinspect them after correctness repairs; none is a measured performance finding yet.

- [ ] **Map/history copying:** `server/document.go` clones the document before apply; `engine/document.go` copies snapshot and retained acceptance history, and validation copies/hashes candidate map state. Measure bytes and time per edit as map size and history grow independently. Any proposed retention bound or copy reduction must preserve snapshot-safe undo, duplicate verification and stale-base validation from Tasks 1–2 and 7–8.
- [ ] **Append replay:** SQLite `Store.Append` loads the persisted snapshot/suffix and reconstructs a document before verifying an append. Measure query/decode/replay work versus suffix length and snapshot cadence; inspect PostgreSQL independently. Evaluate a narrower verified persistence boundary only if profiles justify it, retaining durable acknowledgement and corruption detection.
- [ ] **Projection-to-render amplification:** inspect `mapadapter/export.go`, editor `collaboration.go`, snapshot refresh and render bucket callers for how much state is rebuilt for a one-cell edit. Count visited/allocated cells and invalidated chunks, then measure input-to-visible impact. Consider incremental updates only with equivalence tests for conflicts, active gestures, undo and level changes.
- [ ] **Parser/icon transfer and cache lifetime:** the parser wrapper transfers native strings into Go and decodes JSON; icons additionally decode image pixels and create textures. Attribute each stage and verify cache hit/miss/retention behavior across reloads before proposing a new FFI contract or cache policy.
- [ ] **Idle and resize work:** trace the 60 Hz frame loop, canvas drawing and texture recreation in `window/process.go` and `pmap/canvas/canvas.go`. Measure idle/minimized CPU, frame pacing and resize allocation/upload behavior before proposing invalidation or scheduling changes.
- [ ] Expand this list from all coverage rows, including negative results. For each proposed optimization, record the observed workload, measured cost and user impact, source location, confidence, expected benefit, complexity/risk, correctness dependencies, narrow regression/benchmark gates and shipped-entry-point acceptance. Rank measured high-impact bottlenecks first; keep unmeasured hypotheses in a separate investigation queue.
- [ ] Define the comparison contract for future implementation: fixed valid workloads, matched builds/settings, interleaved or randomized control/candidate trials, retained raw results, and the same revision/hash, accepted/rejected counts and preserved content. Re-run durability/conflict/save tests for affected paths. A change that drops work, weakens checks, reduces durability, or regresses another covered workload does not qualify as an optimization.

**Acceptance:** deliver a source coverage matrix accounting for every runtime directory and shipped executable; a reproducible workload/hardware/build manifest; validated measuring tools; baseline tables and profiles for every runnable domain; explicit unrun/blocked domains; and a ranked optimization backlog with concrete files, dependencies and verification gates. Report product performance, developer-tool performance, component benchmarks, desktop/native/GPU, database and hosted evidence separately. Task 10 is not complete merely because a benchmark or the existing load pilot is green, and a partial environment audit must remain labeled partial.

## Task 11: qualify the repaired composition

**Files:** update the audit evidence with a new dated verification record. Fix relevant behavior documentation in `docs/agent/architecture.md` and `docs/agent/verification.md` if it is stale after inspecting final code. Do not rewrite historical reports to claim tests they never ran.

- [ ] Recheck `git status --short`, `git diff --check`, the final implementation diff, and all nine finding acceptance criteria. Confirm that imported overlay regressions now pass against real source tests and that F5 has shipped-entry-point tests.
- [ ] Record exact tool versions and run maintained gates with the checked-in toolchain:

```powershell
go version
task --version
gcc --version
golangci-lint --version
rustup run 1.82.0-x86_64-pc-windows-gnu rustc --version
go test ./... -count=1 -timeout 120s
go test -race ./internal/aphelion/... -count=1 -timeout 120s
$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
task verify
```

Check each exit code separately. `task verify` includes lint, contracts, Go, pinned Rust test/fmt/Clippy, and cross-stack build; it is a later release gate, not a substitute for the targeted regressions.

- [ ] Exercise built collaboration/desktop entry points and the existing smoke/doctor commands. Build CLI binaries to an ignored evidence directory when needed. Record entry-point results separately from package tests. Do not launch tests against an existing production session or overwrite a user's map.
- [ ] With trusted test infrastructure configured, run the live PostgreSQL suite including backup/restore, the container lifecycle gate, and public protocol load/fault scenarios. Capture skips explicitly. Credentials remain local configuration; do not put them in the plan, logs, URLs, or commands copied into repository artifacts.
- [ ] Stage representative DMM/TGM output and run real Meridian-MCP parsing, map inspection and diagnostic reporting using approved local configuration. Keep staging separate from applying output to a game repository. Content Tools and Rift build tooling were removed from scope by the September 20 user decision.
- [ ] Perform named-human desktop acceptance for overlapping drags, undo/redo, conflict rebuild, delayed acknowledgements, reconnect, Save/Save All, and close with pending work. Automated editor fixtures cannot replace this gate.
- [ ] Review the complete Task 10 performance coverage matrix and measured backlog. Repeat workloads affected by correctness repairs and record whether agreed budgets were met. Keep component results separate from shipped desktop/service results, and carry every unrun or blocked performance domain into the final qualification matrix.

**Acceptance:** produce a matrix distinguishing focused tests, full Go/Rust/lint, cross-stack build, executable smoke, real database/container, integration, hosted CI, load/fault, and human results. Missing gates stay explicitly unrun or blocked. No completion, performance, hosted readiness, or production acceptance claim may exceed its recorded evidence.

## Execution handoff

The initial execution began with Task 1; current resumption follows the execution-status table above. Keep changes uncommitted unless separately authorized. Review the durability pair (Tasks 1–2), ordered transport/conflict pair (3–4), desktop pair (5–6), duplicate handling (7–8) and unknown-prefab handling (9) against their current source and evidence. Prior agent reports are never a substitute for inspection.

Task 10 inventory and baseline design may proceed before the repairs; corrected runtime qualification follows the affected repair checkpoints. Review its repo-wide coverage and measurement validity before accepting any optimization proposal. Task 11 consolidates correctness and performance evidence, including all remaining external and human gates. Authorization to execute this workplan does not by itself authorize every optimization proposed by the resulting audit.
