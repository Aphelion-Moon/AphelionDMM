# October 7 play-test follow-up workplan

> **Status (2026-10-09):** All workstreams A–N and X are implemented with
> automated evidence and are uncommitted. The lighting preview has gone past
> the spike into an integrated editor preview. Human acceptance is outstanding
> for every workstream. See the
> [implementation evidence](../../verification/2026-10-09-playtest-followup.md).

Baseline: commit `f7dce778` (`main`, clean). Build under test: `AphelionDMM, v.a.7`.

This plan covers the October 7 hosted-collaboration play-test notes plus what the
session log shows. Before each stage, check the current source. The hypotheses
below are where to start investigating. They are not confirmed root causes.
Nothing here authorizes a commit, push or protected-infrastructure change.

## Evidence

Session log: `%APPDATA%\StrongDMM\logs\2026-10-07 11.00.25.log` (≈100 MB,
11:00–17:09). Environment: `Meridian-Rift\tgstation.dme`. Main map:
`_maps\map_files\Ministation\MiniStation.dmm`. A hosted session started at
15:19:41 (*Sign In*, *Start Hosted Session*). The log records no joins,
reconnects or disconnect reasons. Only errors that reach the editor appear.

| ID | Time | Log record | Interpretation |
| --- | --- | --- | --- |
| L1 | 16:44:53–16:45:05, 16:50:05–16:50:10 | 26× `received close frame: StatusCode(4429) "presence rate exceeded"` while committing `Add Atoms` | The server closed the editor's socket for presence volume. Every edit after that failed. Session-breaking. |
| L2 | 15:55:33, 16:20:58, 16:39:33, 16:49:38 | `Unable to undo map change: apply speculative operation: precondition failed at (x,y,1)` | Undo cannot form a valid inverse against the speculative projection. Matches the "undo then grab/move" note. |
| L3 | 16:44:38, 16:44:46 | `unable to save map workspace: map changes are awaiting completion` | Acknowledgements were stalled immediately before L1. |
| L4 | 11:02:21–11:02:54 | 4× `Unable to apply bulk edit: operation rejected (invalid_operation) at revision 0: invalid tile change count 0` | A bulk prefab action matching nothing (selected `/area/space`) is submitted as an empty operation and reported as an error. Unshared document. |
| L5 | 15:36:00, 16:47:29–43 | `confirm or cancel paste before undo/redo`, `confirm or cancel paste placement first` | The guards work, but the user got no clear feedback and retried repeatedly. |
| L6 | 15:55:37 | Save: `unable to create a key, changing key length:3`, then 65 025 locations re-keyed | MiniStation went over the 2-letter key space, so every tile key in the saved file changed. Prefab variants may be inflated by the transform bug in C. |
| L7 | entire log | ~900 k of ~970 k lines come from `render/bucket` and `level/chunks.go` debug output | Log noise buries the useful records. The file is roughly 100× larger than needed. |

## Priority and order

| Order | Workstream | Play-test notes | Why this position |
| --- | --- | --- | --- |
| P0-1 | A. Presence disconnects and recovery | (log L1, L3) | Ends hosted sessions. |
| P0-2 | B. Undo → grab/move in collaboration | "Undoing then grab-selecting…" | Produces corruption-risk errors (L2). |
| P0-3 | C. Direction-aware transforms | Held rotation; rotate/mirror paths; turfs/areas rotated | Silently writes wrong map data. Possibly inflates L6. |
| P0-4 | D. Offset preview and refresh regression | Pixel-drag preview; var-edit pixel refresh | Regression. Likely one shared cause. |
| P0-5 | E. Empty bulk actions | (log L4, L5) | Small fix that removes misleading errors. |
| P1-1 | F. Joining without a local map; repository alignment | Host map provision; repo-branch sync | Main collaboration usability blocker. Has design decisions. |
| P1-2 | G. Draft queue resolution on disconnect | Queue clearing prompt and mass actions | Partially built already. Depends on A's disconnect classification. |
| P1-3 | H. Cursor presence presentation | Cursor avatars and colour | Visible polish. Has a protocol decision. |
| P2-1 | I. Always-available visibility toggles | Hide/Show buttons | Small UI change. |
| P2-2 | J. Area rendering order | Space area over turfs | Render policy. |
| P2-3 | K. File drop and environment resolution | Drag-to-open; parent `.dme` | Small. Shares the open path. |
| P2-4 | L. Placement guards from repository lint rules | Double-placing windows, cables, pipes | Medium. Reuses the repository's own `maplint` rules. |
| P3-1 | M. Docking-port footprint overlay | Shuttle size predictor | New read-only overlay. |
| P3-2 | N. Lighting preview | Lighting simulation | Needs a design spike. SpacemanDMM reuse does not look possible (see N). |
| Ongoing | X. Diagnostics hygiene | (log L6, L7) | Needed to read the next play-test log. Land early alongside A. |

---

## A. Presence disconnects and recovery (P0)

**Current state.** The client coalesces presence to the server-advertised
interval (100 ms by default), in
[session_client.go:729](../../../internal/aphelion/collab/ui/session_client.go).
The transport keeps a lossy one-slot presence queue
([client/websocket.go:183](../../../internal/aphelion/collab/client/websocket.go)).
The server checks a fixed-window limiter keyed by actor: 120 per second
([limits.go:47](../../../internal/aphelion/collab/server/limits.go)). Exceeding it
**closes the connection** with 4429
([server/websocket.go:453](../../../internal/aphelion/collab/server/websocket.go)).
The limiter runs when a message is processed, not when it arrives. At 10/s, a
client can only trip it if: the server's read loop stalls and then drains a
backlog (L3 points to stalled acknowledgements), several connections share one
actor ID, or something publishes outside `PublishPresence`.

**Plan.**

1. Reproduce first. Write a server test that holds the session reader (or the
   durable append) for over 12 s while a client publishes at the advertised
   interval, then releases it. Separately, test two connections sharing an
   actor. Confirm which one produces 4429 and record it.
2. Test first: excess presence from a client within the advertised interval
   must be **dropped or coalesced** server-side, not answered with a close. This
   is required by `security-and-networking.md` ("Coalesce lossy presence
   updates") and `multiplayer-invariants.md` (presence "may be dropped or
   coalesced"). Reserve 4429 for sustained abuse, for example several
   consecutive windows over the limit. Consider keying by connection rather
   than actor. Durable-operation rate limiting stays unchanged.
3. Client: classify a 4429 close as recoverable. Use bounded backoff, show
   *Reconnecting (rate limited)*, and stop dispatching new edits into a dead
   transport. L1 shows about 20 edits each failing separately. Keep uncertain
   drafts as the existing recovery path does.
4. If step 1 confirms a stalled reader, find the stall (durable append, hosted
   PostgreSQL latency, fan-out) using the existing load harness
   (`cmd/apheliondmm-loadtest`) with a large paste or Add burst.

**Acceptance.** A server test shows a stalled-then-drained presence backlog does
not close the socket. A real-workspace test shows a forced 4429 recovers to
*Caught up* without losing acknowledged edits. `go test -race
./internal/aphelion/collab/...` passes.

## B. Undo, then grab-select and move, in collaboration (P0)

**Current state.** L2 shows inverse construction failing preconditions against
the speculative projection. Earlier qualification covered delayed rectangular
chains and mouse/frame paths
([2026-09-20 selection network chains](../../verification/2026-09-20-selection-network-chains.md)).
It did not cover undo pending → new Grab capture over the undone tiles → move
→ late acknowledgement.

**Plan.**

1. Test first: add a real-workspace network test using the existing
   `APHELIONDMM_GL_TEST` harness in `internal/app/ui/cpwsarea/wsmap`.
   Sequence: edit A → undo A (hold acknowledgement) → Grab-select a region
   including A's tiles → move → release the undo acknowledgement → undo the
   move → redo. Vary acceptance and rejection of each step. Compare display and
   authority hashes, stable IDs and history depth.
2. Check that Grab source capture pins the *coherent speculative projection*
   including the pending inverse, as `architecture.md` requires. Also check that
   an undo issued while a Grab presentation is open waits behind it or is
   refused with clear feedback.
3. Turn the remaining errors into an explicit status message ("This change
   depends on an edit that was rejected — refresh or discard"). Do not just log
   them.

**Acceptance.** All permutations converge to identical authority and display
hashes. No `precondition failed` appears from an ordinary user sequence.

## C. Direction-aware rotate, mirror and held rotation (P0)

**Current state.** [rotation.go:101](../../../internal/aphelion/editing/rotation.go)
reads `dir` with `vars.Value`, which **includes inherited defaults**. Every
atom inherits `dir`, so rotating a selection writes an explicit `dir` onto
turfs, areas and non-directional objects. That matches the note about turfs and
areas being rotated. These explicit values also create new unique tile contents,
which may have contributed to the key-length growth in L6.
[mirror.go:91](../../../internal/aphelion/editing/mirror.go) never calls
`directionalVariant`. Mirroring `…/directional/west` therefore keeps the
`west` path and writes `dir = 4`. Held rotation
([held_prefab.go](../../../internal/aphelion/editing/held_prefab.go)) does pass
the environment lookup. The play-test still reports no path change, so the
helper detection in
[directional_rotation.go](../../../internal/aphelion/editing/directional_rotation.go)
is the suspect. It requires `abstract_type == family`, and the only fixture
writes that value as a raw string. Meridian-Rift declares it through
`MAPPING_DIRECTIONAL_HELPERS` (`code/__DEFINES/directional.dm`). How the
parser stores a token-pasted typepath is unverified.

**Plan.**

1. Test first: build a fixture from the real parser output (the sdmmparser
   environment) for `/obj/machinery/power/apc/auto_name/directional/*` and one
   `MAPPING_DIAGONAL_HELPERS` type. Assert the stored `abstract_type` value
   form. Fix the comparison (normalize the typepath) if it differs.
2. Rotate and mirror only an **explicit** `dir`, or a declared directional
   helper. Never write `dir` onto `/area`. Never write it onto a turf or
   object that lacks an explicit `dir`, unless that type is opted in. An opt-in
   example is a type whose own definition, not `/atom`'s, sets a non-default
   `dir`. Apply the same rule to inherited `pixel_*`/`step_*` defaults. Add
   unchanged-prefab no-op tests: rotating a plain floor must return the
   identical prefab pointer.
3. Give mirror the same variant switching as rotation. Swap
   `west↔east`/`north↔south`, and diagonals per axis. Drop explicit offsets the
   helper now supplies, as rotation already does.
4. Held rotation: once step 1 passes, check the palette display, the Add hover
   preview and the placed instance all use `held.Value()`. The panel should
   show the resulting path, not only the dir.
5. Run the same cases through the floating-paste and stamp paths
   (`TransformPlacementTemplate`, `Orientation.Prepare`, `MovePayload.Rotated`).
   Every call site must pass the lookup.

**Acceptance.** For the APC, firealarm and a diagonal helper: rotating ×4 and
mirroring ×2 each return the original prefab. Turfs, areas and plain objects
round-trip byte-identical through the DMM writer.

## D. Offset preview and variable-edit refresh (P0, regression)

**Current state.** Shift-drag in [move.go:100](../../../internal/app/ui/cpwsarea/wsmap/tools/move.go)
replaces the prefab and calls `UpdateCanvasByCoords` for the tile only.
`Render.UpdateBucketV` now queues frame-batched updates
([render.go:99](../../../internal/app/render/render.go)). The variable editor
shows the same symptom intermittently. Candidates: (a) the queued bucket update
is deferred while a gesture owns the editor; (b) a sprite shifted by `pixel_*`
overhangs into a neighbouring chunk that is never invalidated, which would also
explain "not always"; (c) the variable-editor instance path never schedules a
refresh. Recent render commits worth bisecting: `aca17d9f` (per-rebuild
appearance reuse; pointer-keyed, so lower suspicion), `728374a5`,
`c6f8f3bc`.

**Plan.**

1. Bisect against the last build where the human saw a live preview, using a
   hidden-workspace test. Shift-drag an object 40 px across a chunk boundary
   and assert the rendered unit's bounds change on the **same frame** before
   release.
2. Make offset changes invalidate both the old and new overhang footprints.
   Keep ordinary frame batching but flush it for the active gesture's tiles.
3. Add the variable-editor case to the same test. Edit `pixel_x` on a placed
   instance and on its prefab.

**Acceptance.** Live preview tracks the mouse with no ghost left at the old
position. Undo restores both footprints.

## E. Empty bulk actions and guard feedback (P0)

**Current state.** [bulk_work.go](../../../internal/app/ui/cpwsarea/wsmap/pmap/editor/bulk_work.go)
submits even when `changes` is empty. The engine rejects zero-change requests
([engine/local.go:130](../../../internal/aphelion/collab/engine/local.go)),
which L4 shows as an error.

**Plan.** Test first. When preparation produces no changes, finish as a no-op
with a status message ("Nothing to delete: /area/space only matches the base
area"). Do not submit. Route L5's paste-guard refusals to visible status text
instead of only logging them.

---

## F. Join without a local map; repository alignment (P1)

**Current state.** Joining requires an active map editor (`DoJoinCollaborationSession`
in [action_user.go:404](../../../internal/app/action_user.go)). The received
snapshot is installed *into* that editor. The pilot guide tells testers to open
byte-identical copies first. The hosted listing already carries `title`,
`map_label` and `environment_label`
([protocol/hosted.go](../../../internal/aphelion/collab/protocol/hosted.go)).
`mapadapter.Export` can already write a snapshot to DMM/TGM.

**Plan.**

1. **Join into a new document.** Allow joining with only the matching
   environment loaded. Build a workspace directly from the joined snapshot
   (`mapadapter.ApplyWithEnvironment` on a fresh `dmmap.Dmm`) as an untitled,
   session-owned document. *Save As* uses the existing staged export, with the
   target picked by the user. Refuse when the environment hash differs, and
   show both labels/hashes and the host's repository descriptor (step 2).
2. **Repository descriptor (additive, optional protocol field).** When hosting,
   the desktop records the environment's DME file name, environment hash, and
   (when the DME is inside a git worktree) branch name and commit SHA. It reads
   these with a fixed local `git` executable and fixed `rev-parse` subcommands,
   never a shell. Strict validation: SHA = 40 hex, branch = git ref grammar
   with a length cap. Treat the values as hostile on receipt. No paths, remote
   URLs or credentials cross the protocol unless decision F2 allows them.
   Update `api/collaboration/*.yaml` and the v1 compatibility fixtures. Absence
   means "unknown".
3. **Joiner alignment assistant.** Compare the descriptor with the joiner's
   local checkout of the selected environment. Show *matches / different commit
   / dirty worktree / unknown*. By default, offer copyable commands only.
   Running `git fetch` + `git switch --detach <sha>` from inside the app is a
   separate opt-in (decision F1). It requires a clean worktree, an explicit
   confirmation and the fixed-adapter rules in `security-and-networking.md`.
   Afterwards, reload the environment and re-check the hash.
4. Test the shipped entry point: two real clients, the joiner with no map open,
   join → edit → converge → Save As → reparse hash equality.

## G. Draft queue resolution on disconnect (P1)

**Current state.** The panel already resolves drafts one at a time (Refresh,
Discard, Rebuild, Export Draft), paginated
([panel.go:107](../../../internal/aphelion/collab/ui/panel.go)). Leave and
close require resolution. An involuntary disconnect (L1) gives no prompt, and
there are no bulk actions.

**Plan.** Test first, against the view model. Add **Export all**, **Discard
all** (with confirmation and a count), **Refresh all** and **Retry when
reconnected** (keeps everything). Add a modal shown on leave, close, and a
terminal or rate-limited disconnect when drafts remain. Bulk actions reuse the
per-draft resolvers in order and stop at the first failure, leaving the rest.
Export happens before discard unless the user unticks it.

## H. Cursor presence presentation (P1)

**Current state.** Presence draws a tile-sized rectangle plus a name, with one
of eight hash-derived colours
([collaboration_presence.go](../../../internal/app/ui/cpwsarea/wsmap/pmap/collaboration_presence.go),
[presence.go](../../../internal/aphelion/collab/ui/presence.go)).

**Plan.**

1. Draw a pointer-shaped marker with a name badge and initials using ImGui
   primitives. No image assets. Keep the tile highlight.
2. Colour selection: a local preference for your own colour. Decision H1: either
   local-only (each viewer recolours others) or broadcast as an optional,
   validated palette index in `profile_update`. A palette index is less risky
   than free RGB. Presence stays ephemeral and outside hashes either way.
3. Avatars from the identity provider are decision H2. They need image fetches
   from a fixed CDN template and avatar caching, and bring privacy
   considerations. The plan assumes **no** remote images unless approved.

## I. Always-available visibility toggles (P2)

**Current state.** The environment tree shows a visibility checkbox **instead
of** the icon, and only while the types filter (`F`) is active
([cpenvironment/process.go:244](../../../internal/app/ui/cpenvironment/process.go)).

**Plan.** Always render the icon. Clicking it with a modifier (Alt+click
proposed), or clicking a small eye toggle beside it, calls the same
`SetFilterVisibility`. Dim the icons of hidden subtrees and keep the
mixed-state dash. Decision I1: icon click vs. persistent eye column.

## J. Area rendering over turfs (P2)

**Current state.** Units sort by `plane*10000 + layer*1000`
([unit.go:114](../../../internal/app/render/bucket/level/chunk/unit/unit.go)).
Codebases put areas on a high plane, so a visible `/area/space` icon covers
turfs once areas are shown by the filter.

**Plan.** Add an area presentation policy: areas drawn as a tinted overlay at a
configurable alpha (default about 35%). An option hides the map's base area
(`world.area` / the environment's default area) unless it is selected. The
policy is render-only and never touches map data. Test sort order and alpha with
a two-unit fixture.

## K. File drop and environment resolution (P2)

**Current state.** There is no GLFW drop callback. `loadResourceV` finds the
nearest ancestor `.dme` **only if no environment is loaded**. Otherwise the map
opens in whatever environment is current
([project.go:51](../../../internal/app/project.go)). The first `.dme` in a
directory wins. Program arguments ignore `.tgm` ([args.go](../../../internal/app/args.go)).

**Plan.**

1. Register a drop callback (inherited window code, Aphelion-marked span).
   Queue paths to the UI thread and dispatch each through `loadResource`.
   Accept `.dme`, `.dmm` and `.tgm`, and ignore anything else.
2. For maps, resolve the nearest ancestor `.dme`. If it differs from the loaded
   environment, prompt **Switch environment** (through the existing close/save
   guards) or **Open in current**. If a directory has several `.dme` files,
   prefer one whose stem matches the directory, otherwise ask.
3. Accept `.tgm` in program arguments. Unit-test the resolver with temporary
   directory trees.

## L. Placement guards from repository lint rules (P2)

**Current state.** The Add and Fill tools have no duplicate check. The
Meridian-Rift repository already ships mechanical rules in
`tools/maplint/lints/*.yml`, for example `multiple_windows`,
`identical_cables`, `identical_pipes`, `multiple_lattice`, `multiple_grilles`,
`multiple_firelocks`, `multiple_airlocks` and `wall_stacking`, in a
`banned_neighbors` / `identical` / `banned_variables` format.
`gopkg.in/yaml.v3` is already in `go.mod`.

**Plan.**

1. Add an Aphelion-owned evaluator (`internal/aphelion/maplint`) for the subset
   of rule semantics that matters here. Write parity fixtures from the
   repository's own `tools/maplint/source/lint.py` behaviour. Read the rule
   files as data from the loaded environment's repository root. Never execute
   Python. Report unknown rule keys rather than guessing their meaning.
2. Placement: Add, Fill, paste and Move check the destination tile before
   committing. Behaviour per decision L1: warn and allow, block, or replace the
   existing instance. Use one precomputed rule index per environment.
3. A *Lint map* panel lists violations, with navigation, for the open map.

## M. Docking-port footprint overlay (P3)

**Plan.** Add a read-only overlay for `/obj/docking_port` instances. Draw the
`width`/`height`/`dwidth`/`dheight` rectangle rotated by `dir`, using the
environment's definitions. Confirm the variable semantics against the
repository's shuttle code before coding. Show warnings when the footprint
crosses the map edge or overlaps non-space turfs or another port. Toggle it from
the view menu. It does not change map data or the protocol.

## N. Lighting preview (P3, spike)

**Finding.** The vendored SpacemanDMM code is only the `dreammaker` parser
crate (`third_party/sdmmparser/src/vendor/spacemandmm-0290db5`). As far as I
know, SpacemanDMM's map renderer (`dmm-tools`) has no lighting pass, but that
needs checking against the upstream source before deciding. Even if one
exists, it would be a CPU image renderer, not a live overlay.

**Spike.** In under two days, prototype an Aphelion-owned approximate
light-map overlay. Light sources come from `light_range`/`light_power`/
`light_color` (exact variable names per codebase). Occlusion comes from
`opacity`. Use per-tile falloff and upload it as one texture per level chunk.
Measure the cost on MiniStation and DeltaStation2. Present the trade-offs
(fidelity versus the game's real lighting, refresh cost after edits, licence
review if any upstream code is reused) before committing to the feature.

## X. Diagnostics hygiene (ongoing)

1. Remove the per-chunk debug line. Aggregate or rate-limit the per-update
   `render/bucket` and `chunks.go` debug records into one summary per frame
   batch (L7).
2. Add structured collaboration lifecycle logs: create/join/leave, state
   transitions, close code and reason, reconnect attempts and outcome, and
   draft counts. Include session and actor IDs only, never tokens or map
   content. These were missing on October 7.
3. Before Save, if key length would grow (L6), warn that every tile key in the
   saved file will change and show the prefab count. After C lands, re-check
   whether MiniStation still exceeds the 2-letter key space.

## Decisions needed

| ID | Question | Default this plan assumes |
| --- | --- | --- |
| F1 | Can the app run `git fetch`/`switch` for joiners, or only show commands? | Show copyable commands only |
| F2 | May the descriptor include a repo-relative map path or remote URL? | No: SHA, branch, DME name and hash only |
| H1 | Cursor colour: local-only, or broadcast palette index? | Broadcast palette index (additive protocol field) |
| H2 | Identity-provider avatars? | No remote images; drawn initials |
| I1 | Visibility toggle: modifier-click on icon, or eye column? | Eye column plus Alt+click |
| L1 | Duplicate placement: warn, block, or replace? | Warn with one-click replace |
| N1 | Proceed past the lighting spike? | Decide after the measurements |

## Verification gates

Each workstream starts with a failing test at the narrowest level, then runs the
shipped entry point (hidden workspace with `APHELIONDMM_GL_TEST=1`, or two real
clients for A, B, F, G and H). Then run:

```powershell
go test ./internal/aphelion/... -count=1
go test -race ./internal/aphelion/collab/...
$env:APHELIONDMM_GL_TEST = '1'
go test ./internal/app/ui/cpwsarea/wsmap/... -count=1 -timeout 300s
Remove-Item Env:APHELIONDMM_GL_TEST
$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
task verify
```

Protocol changes (F2, H1) also need updated `api/collaboration` contracts and
v1 compatibility fixtures. Finish with a second human play-test that repeats the
October 7 sequence, with the diagnostics from X in place. Report human
acceptance separately from automated evidence.
