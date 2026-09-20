# AphelionDMM Authoritative Multiplayer Design

**Status:** Approved planning baseline  
**Date:** 2026-08-24  
**Reviewed source:** AphelionDMM local revision `5241698a`  
**Audience:** AphelionDMM and Meridian-MCP maintainers

## Summary

AphelionDMM will add multiplayer through a central authoritative collaboration service. The desktop editor remains the primary client and uses the same deterministic operation engine in single-user, embedded-loopback, LAN, and hosted modes. HTTP carries lifecycle and snapshot traffic; secure WebSockets carry ordered durable operations and lossy presence.

The server validates, orders, durably records, and broadcasts map operations. Clients may render speculative changes for responsiveness but reconcile to server-accepted revisions. Presence never enters the durable log. Undo is an actor-scoped inverse operation with preconditions, not a shared history rewind.

New Aphelion code is isolated under `internal/aphelion/`, `cmd/apheliondmm-*`, and `api/collaboration/`. Inherited StrongDMM edits remain narrow and marked. The integration boundary supports Meridian-MCP inspection of immutable staged maps. Aphelion Content Tools and Rift build tooling were removed from scope by the September 20 user decision.

## Goals

- Let multiple authorized editors collaborate on one DMM/TGM document with deterministic convergence.
- Preserve DreamMaker map fidelity, including unknown types and variables.
- Keep local single-user editing available without a hosted dependency.
- Provide explicit conflict, reconnect, undo, durability, and compatibility semantics.
- Make collaboration contracts usable by Aphelion tools without exposing editor internals.
- Support embedded, LAN, and hosted deployments with identical operation semantics.
- Establish verification, observability, backup, recovery, and security requirements before deployment.
- Preserve StrongDMM upstream maintainability and human control of branding and creative assets.

## Non-goals

- Peer-to-peer networking.
- Offline multi-master editing or a general-purpose CRDT.
- Browser delivery of the full Dear ImGui desktop application.
- Collaborative editing of DM source code, lore, art, sound, or repository configuration.
- Arbitrary remote execution of DreamMaker, Git, shells, MCP servers, or build scripts.
- A branding, logo, executable-name, or module-path migration.
- Replacement of Meridian-MCP parsing.
- Compatibility with clients that cannot preserve the loaded map's unknown content.

## Audit baseline

### Repository accounting

At the reviewed revision, AphelionDMM is an unmodified StrongDMM clone with approximately 215 Go files, three Rust files in the vendored parser bridge, and one Go test file containing eight parser-oriented tests. The repository contains no Aphelion multiplayer packages, protocol contracts, product guidance, or Aphelion branding.

The current stack is:

- Go 1.24 as declared by `go.mod`;
- Dear ImGui, OpenGL, and GLFW for desktop UI and rendering;
- a Rust static library under `third_party/sdmmparser` derived from SpacemanDMM;
- Task for cross-language builds;
- GitHub Actions for lint, platform builds, artifacts, and draft releases.

### Existing feature surfaces

| Surface | Current role | Multiplayer consequence |
| --- | --- | --- |
| `internal/app` | Desktop lifecycle, projects, commands, layouts | Must remain a client shell, not own authoritative state |
| `internal/app/ui/cpwsarea/wsmap/pmap/editor` | Direct mutations and snapshot commits | Needs a narrow operation executor seam |
| `internal/dmapi/dmmap` | Mutable in-memory DMM and instances | Needs deterministic conversion to and from operation state |
| `internal/dmapi/dmmsnap` | Detects changes by comparing full map snapshots | Retained during migration, then replaced for collaborative undo |
| `internal/dmapi/dmmap/dmmdata` | Parse representation and DMM/TGM serialization | Needs error-returning, fidelity-preserving atomic writes |
| `third_party/sdmmparser` | Rust parser static library | Remains parser authority behind a tested boundary |
| `.github/workflows/ci.yml` | Lint, platform build, artifact, draft release | Lacks collaboration, race, persistence, and protocol gates |
| `Taskfile.yml` | Local and CI build orchestration | Currently selects floating Rust stable locally unless overridden |

### Material risks found

1. `Editor.CommitChanges` starts a goroutine, and some tools invoke it from another goroutine. Mutation discovery and command history can race with subsequent edits.
2. Map saves report success to the UI even though the DMM/TGM writers do not return failures.
3. Writers use direct target creation/truncation rather than staged atomic replacement.
4. Unknown map types can be warned about and then dropped during project loading/saving.
5. Instance IDs come from a process-local counter, so they are not stable collaboration identifiers.
6. Undo derives patches after mutation by comparing whole tile collections and rewinds a local history pointer.
7. Updater requests lack the complete timeout, size-limit, and verification posture required for a networked product.
8. The lint configuration disables important analyzers including error checking, vet, and static analysis.
9. CI builds but does not run the Go/Rust test suites, race checks, formatting, Clippy, fuzz/property tests, or vulnerability checks.
10. Local reproduction is not guaranteed until the exact CI-selected Go, Rust, Task, C compiler, and native dependencies are installed and recorded.

These risks establish the order of work: deterministic operations and safe persistence precede WebSocket collaboration.

## Considered approaches

### Central authoritative service

A single document owner orders operations, validates preconditions, persists accepted records, and broadcasts authoritative results.

Advantages:

- deterministic ordering and straightforward authorization;
- bounded conflict semantics tailored to tile/property edits;
- practical reconnect, audit, backup, and hosted operations;
- the same engine works in-process and across a network.

Costs:

- server availability is required for shared editing;
- clients need speculation and reconciliation for responsive remote use;
- hosted deployments need persistence and operational care.

### Peer-to-peer operation exchange

Clients exchange edits without a central ordering authority.

Advantages are lower hosting dependence and direct LAN discovery. Costs are identity, NAT, split-brain, authorization, persistence, and conflict complexity. This does not fit the intended tool integrations or hosted evolution.

### General-purpose CRDT document model

A CRDT can support offline multi-master work, but DMM tile stacks, map resize, environment replacement, unknown DreamMaker values, and actor-safe undo require application-specific semantics anyway. The additional metadata and migration complexity are not justified for the initial product.

### Decision

Use a central authoritative service with deterministic domain operations. Do not use peer-to-peer transport or a general-purpose CRDT for the first multiplayer system.

## System architecture

### Components

```text
AphelionDMM desktop
  UI tools
    -> operation builder
    -> executor interface
       -> local engine executor, or
       -> collaboration client over HTTPS/WSS

Collaboration service
  HTTP control plane
  WebSocket connection manager
  authentication and authorization
  per-document owner loop
    -> operation engine
    -> operation store
    -> snapshot store
    -> durable event broadcast
  separate presence fanout
  telemetry and health

Integration boundary
  OpenAPI control contract
  AsyncAPI stream contract
  Meridian adapter
  staged map inspection coordinator
```

### Process forms

- `cmd/apheliondmm-collab` is the collaboration service executable.
- Embedded mode starts the same service logic on loopback with an ephemeral port and single-use launch token.
- LAN mode is explicitly configured and uses TLS or a trusted TLS-terminating proxy.
- Hosted mode runs the service against PostgreSQL behind TLS and OIDC.

The desktop must also support a pure in-process local executor for development and emergency single-user use. Both executors call the same operation engine behavior and pass the same conformance tests.

### Package boundaries

```text
internal/aphelion/collab/model
internal/aphelion/collab/engine
internal/aphelion/collab/protocol
internal/aphelion/collab/server
internal/aphelion/collab/client
internal/aphelion/collab/store
internal/aphelion/integration/meridian
internal/aphelion/integration/contenttools
cmd/apheliondmm-collab
api/collaboration
```

`model` and `engine` remain independent of ImGui, WebSockets, SQL, and process execution. `protocol` owns wire representations. `server` owns ordering. Stores persist already accepted events but do not make conflict decisions.

## Domain model

The implementation starts with these values:

```go
package model

type DocumentID string
type ActorID string
type OperationID string
type Revision uint64

type Coord struct {
	X int `json:"x"`
	Y int `json:"y"`
	Z int `json:"z"`
}

type PrefabState struct {
	StableID string            `json:"stable_id"`
	Path     string            `json:"path"`
	Vars     map[string]string `json:"vars"`
}

type TileState struct {
	Prefabs []PrefabState `json:"prefabs"`
}

type TileChange struct {
	Coord  Coord     `json:"coord"`
	Before TileState `json:"before"`
	After  TileState `json:"after"`
}

type Operation struct {
	ProtocolVersion  uint16       `json:"protocol_version"`
	DocumentID       DocumentID   `json:"document_id"`
	ActorID          ActorID      `json:"actor_id"`
	OperationID      OperationID  `json:"operation_id"`
	BaseRevision     Revision     `json:"base_revision"`
	EnvironmentHash  string       `json:"environment_sha256"`
	BaseMapHash      string       `json:"base_map_sha256"`
	Kind             OperationKind `json:"kind"`
	Changes          []TileChange `json:"changes"`
	InverseOf        *OperationID `json:"inverse_of,omitempty"`
}

type AcceptedOperation struct {
	Operation
	Revision   Revision  `json:"revision"`
	AcceptedAt time.Time `json:"accepted_at"`
}
```

The exact Go representation may add bounded value types, but it may not weaken the wire invariants. Map and environment hashes are lowercase SHA-256 hex of canonical representations specified in compatibility fixtures.

### Stable identifiers

Document, actor, and operation IDs use random UUIDv7 values generated by the trusted owner of that identity. Prefab instances receive stable IDs when the map is imported into a collaboration document. The stable ID is collaboration metadata and does not need to be emitted into DMM text unless a future explicit format decision approves it.

Stable IDs must survive snapshot/replay and reconnect. They do not use the existing process-local `dmminstance.Instance` counter.

### Canonical representation

Canonical hash input uses:

1. dimensions in `max_x,max_y,max_z` order;
2. tiles ordered by `z,y,x` with a documented coordinate origin;
3. prefab order preserved within each tile;
4. paths encoded as UTF-8 bytes;
5. variable names sorted by raw UTF-8 byte order;
6. variable values preserved as parser-normalized DreamMaker text;
7. length-prefixed fields to prevent concatenation ambiguity.

Golden fixtures prove Windows, Linux, and macOS produce identical hashes.

## Operation semantics

### Submission

The client converts a completed UI intent into explicit changes before submission. `BaseRevision` and `BaseMapHash` identify the exact acknowledged state from which the client built the operation. The server retains enough revision-to-hash metadata to validate that pair while allowing non-overlapping operations based on an older acknowledged revision. The server checks:

1. protocol and document compatibility;
2. authenticated actor and role;
3. unique operation ID or prior idempotent result;
4. environment hash, document identity, and a valid base-revision/base-hash pair;
5. payload byte, count, and coordinate bounds;
6. whether the operation kind requires exclusive ownership;
7. each `Before` value against current authoritative state;
8. map invariants after the proposed change.

Validation is all-or-nothing. The server applies normalized changes in deterministic coordinate order, assigns the next revision, durably appends the event, updates state/hash, and broadcasts the accepted event.

### Operation kinds

Initial kinds are:

- `tile_set`: one or more explicit tile-stack replacements;
- `prefab_property_set`: an explicit property replacement on a stable prefab instance;
- `map_resize`: exclusive dimensions and resulting boundary changes;
- `environment_replace`: exclusive parser environment and compatibility hash replacement;
- `import_replace`: exclusive complete document replacement after validation;
- `inverse`: actor-scoped inverse of an accepted operation;
- `export_checkpoint`: exclusive durable checkpoint associated with staged output metadata.

Large brush or fill actions remain one logical operation but are bounded. Clients split an oversized action into independently valid operations and communicate that batching in UI state rather than the wire protocol.

### Conflict result

A rejection contains a stable error code, current revision and map hash, and bounded authoritative values relevant to the failed preconditions. It never includes unrelated map content.

Different tiles can merge by server order. Different properties of one prefab can merge when their preconditions are independent. Competing replacements of the same tile stack or property cannot silently overwrite; the later stale precondition is rejected.

The client retains the rejected operation only as an in-memory draft associated with the conflict. Refresh synchronizes the editor to the acknowledged authoritative projection while keeping that draft and conflict open. Discard removes the draft and conflict without sending a mutation. Rebuild creates a fresh operation from current authoritative before-values to the rejected draft's intended after-values, submits it through ordinary validation, and dismisses the old conflict only after acknowledgement. None of these actions bypass preconditions or provide force overwrite.

### Speculation and reconciliation

Clients may speculatively render a submitted operation. Speculative changes are tracked separately from acknowledged state. On acceptance, the client advances the acknowledged revision and reapplies remaining compatible speculation. On rejection, it removes the rejected speculation, applies authoritative values, and reports a conflict through accessible UI state.

No speculative operation is written to the map file or reported as durable.

## Undo and redo

The server retains the normalized before/after state needed to form an inverse. A user requests an inverse of one of their accepted operations. The engine creates a new operation with:

- a fresh operation ID;
- `InverseOf` naming the target;
- changes swapping target before/after values;
- preconditions that the current values still match the target's accepted after-values.

The inverse is accepted only when it will not overwrite later conflicting work. A rejected inverse leaves state unchanged and explains which bounded values changed. Owner role does not bypass safe inverse semantics; administrative document replacement is a separate audited operation.

Redo submits a new forward operation using current preconditions. Shared revision history never moves backward.

## Presence

Presence messages include actor display metadata authorized for the session, cursor coordinate, selection bounds, viewport, tool preview category, and client sequence. They are:

- rate-limited and coalesced;
- bounded independently of durable operations;
- sent over a separate message class and queue;
- dropped under backpressure;
- expired after timeout or disconnect;
- excluded from snapshots, operation logs, hashes, and acknowledgements.

The server never accepts map mutation through a presence message.

Viewport is a normalized, same-level rectangle clipped to current map dimensions. Tool preview is a closed protocol category describing behavior, not an arbitrary client tool name or serialized tool payload. Protocol v1 categories are `none`, `add`, `delete`, `replace`, `fill`, `move`, `select`, and `edit`; unknown categories are rejected.

## Protocol

### HTTP control plane

The initial OpenAPI contract exposes:

- `POST /v1/sessions` to create a session from a validated snapshot reference;
- `GET /v1/sessions/{session_id}` for metadata and compatibility;
- `POST /v1/sessions/{session_id}/join-tokens` for short-lived scoped joins;
- `GET /v1/sessions/{session_id}/snapshot` with revision and entity tag;
- `POST /v1/sessions/{session_id}/exports` to request a staged checkpoint;
- `GET /v1/health/live` and `GET /v1/health/ready`;
- `GET /v1/version` for build, protocol, and schema compatibility.

Hosted identity and session role management use OIDC-derived principals and owner-authorized APIs. Loopback mode exposes only the lifecycle needed by the launching desktop.

### WebSocket stream

The AsyncAPI contract defines client messages:

- `join`;
- `operation_submit`;
- `inverse_request`;
- `presence_update`;
- `acknowledged_revision`;
- `ping`.

Server messages are:

- `joined`;
- `operation_accepted`;
- `operation_rejected`;
- `replay_complete`;
- `presence_snapshot`;
- `presence_update`;
- `session_notice`;
- `pong`.

Every envelope contains protocol version, message type, message ID, and session ID. Durable messages also contain document ID and revision metadata. Binary compression is disabled initially; bounded JSON keeps inspection and compatibility straightforward.

After initial authentication, the server sends a dedicated opaque resumption credential scoped to the session and actor. It is short-lived, held only in client memory, never placed in a URL or persistent configuration, and rotated on every successful reconnect. Invitation and join credentials remain one-purpose and are erased after join. A reconnect presents the last acknowledged revision and the current resumption credential; authentication or protocol failure stops retries and clears the credential. When that revision predates retained replay history, the server sends `snapshot_required` and closes without a partial replay. The client fetches the authoritative snapshot through authenticated HTTP, installs the compatible non-rollback baseline, and reconnects again from its revision so operations accepted during the fallback window arrive through ordinary replay.

### Versioning

Protocol version 1 begins when conformance fixtures and contracts are accepted. During implementation, pre-release versions use `0.x` in build metadata but still reject incompatible schemas. Additive optional fields require a defined absent meaning. Changed semantics require a new negotiated protocol version.

## Persistence and recovery

### Store interface

```go
type SessionStore interface {
	Create(ctx context.Context, snapshot model.Snapshot) error
	Append(ctx context.Context, accepted model.AcceptedOperation) error
	Load(ctx context.Context, documentID model.DocumentID) (model.Snapshot, []model.AcceptedOperation, error)
	SaveSnapshot(ctx context.Context, snapshot model.Snapshot) error
	LookupOperation(ctx context.Context, documentID model.DocumentID, operationID model.OperationID) (model.AcceptedOperation, bool, error)
}
```

The append and idempotency lookup are transactionally consistent. Acknowledgement occurs after `Append` reaches the configured durability boundary.

### Embedded store

Embedded and single-host deployments use SQLite with WAL and a bundled SQLite release of at least 3.51.3. The service configures busy timeout, foreign keys, bounded WAL checkpoint behavior, integrity checks, and orderly shutdown. It records the actual SQLite version at startup and refuses an unsafe version.

### Hosted store

Hosted deployments use PostgreSQL with transactionally assigned document revisions, unique `(document_id, operation_id)`, migration locking, bounded connection pools, and point-in-time-recoverable backups.

### Snapshots

Snapshots include schema/protocol version, document ID, revision, canonical map state, environment hash, map hash, and creation time. The service periodically snapshots by accepted operation count and elapsed time. Recovery loads the latest valid snapshot and replays subsequent events, then confirms the canonical hash before serving the document.

Corrupt or discontinuous logs fail closed. Recovery never guesses a missing revision.

Map resize remains unavailable in network sessions until it is implemented as an owner-only exclusive maintenance operation. The server accepts it only with no pending edits, records the new dimensions and boundary effects deterministically, produces a new canonical snapshot and hash, and blocks concurrent durable operations for the maintenance window.

## DMM/TGM fidelity and saving

The existing writers are replaced behind error-returning APIs:

```go
func (d DmmData) WriteDM(w io.Writer) error
func (d DmmData) WriteTGM(w io.Writer) error
func SaveAtomic(path string, write func(io.Writer) error, validate func(string) error) error
```

`SaveAtomic` creates a temporary file in the destination directory, writes and flushes it, reparses and validates it, verifies expected dimensions/hash, then atomically replaces the target. On failure it removes only the staged file and preserves the original.

Unknown types and variables remain opaque fidelity-preserved prefab data. If a client cannot represent them, it cannot enter editable mode. Export records source and output hashes and the accepted revision.

## Security

### Embedded mode

- loopback bind only;
- random ephemeral port;
- short-lived single-use launch token passed through a protected local channel, not a URL;
- origin allowlist for the known desktop client;
- no remote repository or process control APIs.

### LAN and hosted modes

- HTTPS/WSS only;
- authenticated upgrade and per-operation authorization;
- hosted OIDC Authorization Code with PKCE using `github.com/coreos/go-oidc/v3`;
- viewer, editor, and owner roles;
- request/message byte limits, collection limits, deadlines, and rate limits;
- bounded error responses and violation-driven disconnects;
- secret-free structured logs;
- trusted configuration for roots and external tools;
- no shell invocation and no client-supplied executable paths.

### Updater and dependencies

Network-product readiness includes hardening the existing updater with client timeouts, response-size limits, TLS verification, signed release metadata/artifacts, and rollback. Dependency upgrades are pinned and reviewed. Vulnerability scans complement tests but do not replace them.

## Observability

Use OpenTelemetry Go APIs with service, protocol, document, and revision attributes. Do not attach raw map content or secrets.

Required metrics include:

- active connections and sessions;
- accepted, rejected, duplicate, and conflicted operations;
- operation validation, append, apply, and broadcast latency;
- reconnect replay count and duration;
- presence drops and queue depth;
- snapshot duration and failure count;
- store transaction and checkpoint latency;
- recovery, integrity, and hash mismatch failures.

Traces connect HTTP join, WebSocket session, operation validation, durable append, and broadcast. Health endpoints distinguish process liveness from store/document readiness.

## User experience

The desktop provides:

- create local session, join session, copy invite, and leave actions;
- clear viewer/editor/owner role display;
- participant list and accessible presence labels;
- collaborator cursors/selections that do not obscure map content;
- connection, syncing, caught-up, conflict, read-only, and reconnecting states;
- explicit exclusive-maintenance state for resize/import/environment changes;
- conflict details with safe refresh/reapply choices;
- actor-scoped undo status and rejection explanations;
- export/checkpoint revision and hash information.

Presence colors, icons, branding, and sounds require human-approved assets. The implementation can use text and existing neutral UI primitives until that decision.

## Toolset integration

### Meridian-MCP

Meridian-MCP remains a separately configured trusted diagnostic process. The adapter:

1. resolves a configured repository identity to an approved root;
2. calls `dm_parse_environment` before DM source/map inspection;
3. requests map information and diagnostics through versioned MCP methods;
4. records MCP version, repository revision, and environment hash;
5. returns bounded diagnostics without exposing arbitrary MCP execution to collaboration clients.

The collaboration server never becomes an unrestricted MCP proxy.

### Map repository inspection

AphelionDMM produces an immutable staged map artifact and manifest. The coordinator:

1. confirms the configured repository identity, revision, environment and target containment;
2. writes staged output without replacing the source map;
3. runs Meridian-MCP parsing, map inspection and diagnostic reporting;
4. records repository revision, hashes, MCP version and diagnostic totals.

Inspection results do not claim compilation or runtime acceptance. Content Tools
adapters, repository build scripts and Rift build dependencies are unsupported.
Applying a staged map to a game repository remains a separately authorized action.

## Recommended support libraries and standards

The market review supports:

- [`github.com/coder/websocket`](https://github.com/coder/websocket) for context-aware WebSocket I/O and maintained protocol behavior;
- [OpenAPI](https://spec.openapis.org/oas/latest.html) for HTTP control contracts;
- [AsyncAPI 3](https://www.asyncapi.com/docs/reference/specification/v3.0.0) for WebSocket event contracts;
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/) for vendor-neutral telemetry;
- [`github.com/coreos/go-oidc/v3`](https://github.com/coreos/go-oidc) for hosted OIDC verification;
- SQLite for embedded/single-host persistence and PostgreSQL for hosted persistence;
- the [OWASP WebSocket Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html) as the minimum WebSocket review baseline.

Figma's published multiplayer architecture and reliability material supports a server-ordered, compact operation stream with explicit reconnect/recovery behavior rather than treating transport delivery as durability. WAD Together is a useful mapper-specific UX reference, but it does not replace AphelionDMM's protocol, fidelity, and trust requirements.

Avoid a heavy WebSocket framework, a Redis requirement, or a CRDT library in the first implementation. Add infrastructure only when measured load or deployment requirements justify it.

## Migration sequence

1. Establish governance, reproducible local evidence, and contract directories.
2. Add deterministic model/engine operations and atomic fidelity-preserving saves.
3. Route local editor mutation through an executor while preserving single-user behavior.
4. Add an in-memory loopback collaboration service and two-client conformance tests.
5. Add desktop collaboration UX, speculation, reconciliation, presence, reconnect, and actor undo.
6. Add durable SQLite storage, security controls, telemetry, recovery, and updater hardening.
7. Add versioned Meridian-MCP inspection and immutable map staging.
8. Add PostgreSQL, OIDC, deployment, backups, load/fault gates, and hosted rollout.

Each phase has an implementation plan under `docs/superpowers/plans/`. A later phase may begin only when the prior phase's acceptance gates pass or the user explicitly accepts the recorded exception.

## Acceptance criteria

The multiplayer design is implemented only when:

- local and network executors pass the same operation conformance suite;
- two clients converge under interleaved edits, duplicates, reconnects, and safe undo;
- unknown map data survives parse, edit, snapshot, replay, and atomic save;
- acknowledged operations survive restart at the configured durability level;
- snapshot plus replay reconstructs the exact canonical map hash;
- viewer/editor/owner authorization and WebSocket abuse tests pass;
- embedded mode works without hosted services;
- hosted mode passes backup/restore, fault, load, and OIDC acceptance;
- the produced map completes Meridian-MCP parsing and inspection with diagnostic totals recorded;
- shipped entry points are exercised on supported platforms;
- documentation and protocol compatibility fixtures match the shipped behavior.

## Deliberately deferred product decisions

These decisions do not block the architecture because the default is explicit:

- Branding remains inherited StrongDMM until a human approves a separate migration.
- Hosted identity remains standards-based OIDC with no provider-specific behavior in core packages.
- Deployment remains vendor-neutral and container-compatible; no hosting vendor is selected here.
- Presence uses text and existing neutral UI primitives until human-approved visual assets exist.
- Public Internet session discovery is absent; joins require explicit scoped invitations.
