# October 7 play-test follow-up: implementation evidence

Plan: [2026-10-08 play-test follow-up workplan](../superpowers/plans/2026-10-08-playtest-followup-workplan.md).
Baseline `f7dce778`. All changes are uncommitted. This report records automated
evidence only. **No change has been exercised in an interactive desktop session
or a real hosted deployment.**

## Outcomes by workstream

| WS | Result | Key evidence | Open human checks |
| --- | --- | --- | --- |
| A | The server's per-connection presence gate drops excess updates. 4429 is sent only after 5 consecutive abusive windows. The client classifies 4429 as `ErrRateLimited` and reconnects with bounded backoff. Lifecycle logging covers create/join/leave/state/close/reconnect. Root cause: the limiter ran at processing time, so a drained backlog tripped it. | `server/presence_rate_test.go` failed before the fix. `client/rate_limited_test.go`, `ui/session_client_rate_limited_test.go`. | The component that stalls the read loop in production is still unidentified (workplan A.4). A forced 4429 has not been recovered in the real UI. |
| B | Undo and redo wait for unacknowledged operations, and are refused while a Grab drag or move preview is open. Mask moves process pending updates before capture. Precondition text is mapped to a user-facing message. | `wsmap/history_grab_network_test.go`: 5 variants that reproduced the play-test error before the fix. | No multi-client run with a remote actor. |
| C | Rotate and mirror touch `dir` and offsets only when they are explicit, from a declared helper, or owned by the type. Areas never rotate. A no-op returns the identical prefab. Mirror switches helper paths. Palette rotation publishes the rotated helper as the global selection. | `editing/orientation_scope_test.go` (real-parser macro fixture). `tools/held_rotation_test.go` `TestPaletteRotationPublishesRotatedSelection`. | Rotating an APC, firealarm or diagonal helper in the live editor. |
| D | Regression `0cda9bbb`: modifiers were blanked while ImGui had an active item, which covers the whole canvas drag. Fixed at the root in `tools/action_context.go` (canvas drag no longer suppresses modifiers). `move.go` keeps a fallback. The variable editor refreshes the edited tile. | `window/offset_preview_test.go`, `render/offset_preview_test.go`, `tools/modifiers_suppressed_test.go`. | Live Shift-drag across a chunk boundary. The variable-editor intermittency did not reproduce, so its cause is unconfirmed. |
| E | Empty local preparations finish as no-ops without contacting the engine (`local_work.go`). | `editor/bulk_work_noop_test.go`. | — |
| F | Join opens an untitled, session-owned tab when no map is open. The environment hash is checked first. Save of an untitled tab routes to Save As through the staged save. The repository descriptor (`dme_name`, `environment_hash`, `git_branch`, `git_commit`) is negotiated via the `X-Aphelion-Repository-Descriptor` header on the HTTP hosted API. The WebSocket protocol is unchanged. The alignment assistant shows copyable git commands only. | `app/collaboration_join_document_test.go` (loopback join → edit → converge → Save As reparse hash). `wsmap/untitled_test.go` (GL). `repoinfo` tests against a real temporary repository. | Full desktop install path, clipboard button, and a hosted run against an older service. |
| G | Bulk Export, Discard (export first by default), Refresh and Keep across all drafts. "Unsent collaboration changes" prompt shown once per disconnect episode and on leave or close. Rate-limit and gave-up banners. | `ui/draft_bulk_test.go`. | Modal, folder picker and banner rendering. |
| H | Optional `cursor_color` palette index (12 entries) in `profile_update` and presence. The server forwards it only to connections that sent one (per-connection gate) and re-sends it after reconnect. Pointer and initials badge drawn with ImGui primitives; no image assets. | `server/cursor_color_test.go`, `ui/presence_color_test.go`, protocol fixtures. | Visual check. Residual risk: a client with an explicitly chosen colour is closed by an older server. |
| I | The visibility checkbox is always shown beside the icon. Alt+click on the icon toggles visibility. Hidden types are dimmed. | `cpenvironment/process_visibility_test.go`. | Layout and dimming. The `F` types filter no longer changes the tree. |
| J | Area units drawn at a preference alpha (default 35%). The base area is hidden by default. Both are Editor preferences that repaint immediately. | `render/area_policy_test.go`, `prefs/area_test.go`. | Visual check on an SS13 map. |
| K | Drag-and-drop of `.dme`/`.dmm`/`.tgm`. Maps dropped with an environment open after it loads. The nearest ancestor `.dme` is resolved, with a prompt to switch or open in the current environment. `.tgm` is accepted in program arguments. | `envresolve` tests. | A real Explorer drop. |
| L | Repository `tools/maplint/lints` rules are evaluated in Go (50 of 52 Meridian-Rift rules supported; the 2 using regex lookahead are listed as unsupported). Identical duplicates are skipped on Add and Fill. Other violations warn with a one-click "Replace existing". Paste is warn-only. Map Lint panel. | `maplint` tests, including real-rules loading. Editor and tools placement tests. | Move-tool checks are not implemented. Random Fill is unchecked. |
| M | View > Docking Ports overlay using `return_coords` geometry (`shuttle.dm:81-108`). Warns on edge, overlap (not docked pairs) and non-space turfs under stationary ports. | `docking` tests. | Alignment on real shuttle maps, and cost on a large map. |
| N | Spike only: SpacemanDMM has no lighting pass. See the [spike note](2026-10-08-lighting-preview-spike.md). | — | Decision N1. |
| X | Bucket and chunk debug logs are aggregated to one summary per second per map. Collaboration lifecycle logs are added. Rate-limited edit refusals are reported once per episode. | `chunk/summary_test.go`, `editor/user_facing_error_test.go`. | Key-length growth warning (X.3) is not implemented. |

## Second pass (user decisions applied, 2026-10-09)

- **Cursor colour:** older servers are unsupported, so the per-connection gate is removed and `cursor_color` is always delivered (`server/cursor_color_test.go`).
- **Repository descriptor:** persisted in the hosted PostgreSQL store. Postgres schema 5 → 6 (`006_hosted_session_repository.sql`) adds four nullable, constrained columns. It is restored on recovery, and invalid stored values read as unknown. Tested against a temporary local PostgreSQL 18, including migration from an existing v5 schema. SQLite has no hosted tables and is unchanged.
- **`F` key:** the types-filter mode is removed. `F` now toggles visibility of the selected type. The binding ID is kept so saved custom bindings survive.
- **Production stall (A.4):** hosted connections reauthorized every incoming message, presence and ping included, with a registry SELECT on the connection goroutine. With 80 ms or more registry latency the read loop saturated (pre-fix: 3 to 8 of 19 operations unacknowledged). Presence, ping and acknowledged-revision no longer reauthorize per message. Durable submissions still do, and the 30 s periodic reauthorization remains. Measurements are in `.artifacts/read-loop-{baseline,after}.json`. Still open: durable submission work remains serial on the connection goroutine; the recommended design is a per-connection ordered submission worker.
- **Offset refresh with network executors:** not reproduced (`window/offset_preview_network_test.go`).
- **Lint:** Move-tool hops onto identical duplicates are refused; other violations warn. Random Fill skips identical duplicates in append mode. A Python-compatible negative-lookahead matcher means all 54 Meridian-Rift rules now load with none unsupported.
- **X.3 key-length warning:** a confirmation appears inside the save worker after key assignment and before writing. Cancel leaves the file byte-identical and the tab dirty, and an approved length is not re-prompted. Observed but not changed: later saves are re-keyed relative to the load-time backup.

## Lighting preview (N)

The [design spec](../superpowers/specs/2026-10-09-lighting-preview-design.md)
cites the Meridian-Rift lighting code for every formula. It uses the recommended
defaults N1–N14.

- **Pure model:** `internal/aphelion/lighting`. A corner-based model: falloff, cone, clamp and rounding, ambient, starlight, and wall-fixture profiles. An incremental update is bit-identical to a full recompute over 150 seeded random edits.
- **Adapter and controller:** `lighting/maplight`. A UI-thread capture with a 2 ms budget feeds an immutable snapshot to a worker. At most one job runs with one pending. Results are fenced by epoch. Dirty tiles come from the render repaint path.
- **Rendering:** a GL multiply pass after map units. Area overlays, selection and ImGui overlays stay unlit.
- **Controls:** View > Lighting Preview, and Editor preferences for darkness (default 70), starlight, overlay lights and source markers. A status line shows a skipped-atom popup.
- **Timing (synthetic 255×255 map, 832 sources):**
  - first frame: about 43 ms on the worker, plus about 8 ms of UI work spread over several frames;
  - one-tile edit to new frame: about 7.4 ms;
  - steady-state draw: about 0.55 ms.
- **Not done:**
  - The hovered-unit highlight is dimmed, because it is drawn inside the map unit pass.
  - There are no golden captures from real stations or in-game screenshots.
  - The source-marker visuals still need a human check.

## Gates run (2026-10-09, Windows, Go 1.25.13)

- `go build ./...`: clean.
- `go vet ./internal/aphelion/...`: clean.
- `go test ./internal/aphelion/... -count=1`: pass.
- `APHELIONDMM_GL_TEST=1 go test ./internal/app/... -count=1 -timeout 900s`: pass.
- `go test -race ./internal/aphelion/... -count=1`: passes in isolation and per package. In the full parallel run, `TestSessionClientRecoversDuringIndependentLoad/sqlite` intermittently fails with 4408 "durable consumer fell behind". The same failure occurs on the unmodified baseline `f7dce778`, so it is a pre-existing load-timing flake. `TestMultipleWritersRecoverMixedPendingEditsDuringLoad` failed once in a parallel run, then passed 8 of 8 times under `-race` and in two later full runs.
- Final pass (after lighting): `go build ./...` and `go test ./internal/aphelion/...` clean, `APHELIONDMM_GL_TEST=1 go test ./internal/app/...` clean, `task verify` exit 0 (one earlier run hit the multi-writer flake above).
- `task verify` (`RUST_TARGET=1.82.0-x86_64-pc-windows-gnu`): pass (exit 0) after one lint fix (an unchecked `Close` in `maplint/load.go`). One intermediate run failed in `test-go` with output that was truncated. The two following full runs passed, and the failing package was not identified.

A load-timing race test (`TestSessionClientRecoversDuringIndependentLoad/sqlite`)
failed once under concurrent agent load and passed on rerun. Treat it as flaky,
not as qualified.
