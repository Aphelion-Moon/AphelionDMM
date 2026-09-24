# Multiplayer invariants

These rules are correctness requirements, not implementation preferences.

## Authoritative state

1. One server-side document owner serializes every durable mutation.
2. Each accepted operation receives exactly one monotonically increasing document revision.
3. An acknowledgement is sent only after the operation is durably recorded at the configured durability level.
4. Duplicate `(document_id, operation_id)` delivery returns the original result and never reapplies the operation.
5. Applying the accepted log to the same validated snapshot produces the same canonical map hash on every supported platform.

## Operations

Every durable network operation contains:

- protocol version;
- document, actor, and operation identifiers;
- base revision;
- environment hash and the canonical map hash at the named base revision;
- explicit typed changes;
- preconditions for values being replaced;
- an operation kind with bounded payload rules.

The server validates the entire operation before applying any part. A stale base revision may still merge when its base hash is authentic and all explicit value preconditions remain valid. Partial acceptance is not permitted.

Operations describe domain changes, not UI gestures. A brush drag is converted to deterministic tile changes before submission. The accepted record contains the normalized change set used by the server.

## Unshared local documents

An explicitly unshared document may submit an internal `engine.LocalRequest`
through its in-process executor. This is not a protocol flag or a wire envelope.
The opaque document-owner token and expected revision fence each request. The
same changed-tile validation checks bounds, exact before-values and stable-ID
uniqueness across the entire atomic batch. Failure publishes neither state nor
index changes. Actual edits advance one revision; no-ops do not.

Local command history owns reversible deltas with current-value preconditions.
It does not require a second network replay log. The canonical digest is explicitly
unavailable after an internal edit until a real hash consumer needs it. Saves,
exports, installation and session promotion still validate coherent state using
the unchanged canonical representation. Session promotion creates a validated
network baseline; missing local revision hashes are not invented as historical
network bases. Existing crash-recovery guarantees remain independent of this
in-memory execution contract.

A hosted document with one participant remains session-owned. Disconnection
does not enable local execution. Attach/detach and retained resize history fence
owner, attachment and history lifetimes independently.

## Conflict behavior

- Non-overlapping tile or property changes may be accepted in server order.
- A precondition mismatch rejects the conflicting change and returns current authoritative values.
- Map resize, environment replacement, import, and export-finalization are exclusive maintenance operations.
- Clients reconcile from accepted operations; they never declare local speculative state authoritative.
- A reconnect starts from an acknowledged revision and receives a snapshot or contiguous replay sufficient to reconstruct current state.
- If the acknowledged revision predates retained replay history, the server sends `snapshot_required` and closes without a partial replay. The client fetches the authenticated HTTP snapshot, installs only a compatible non-rollback baseline with no pending operations, and reconnects again from the fetched revision.
- Invitation and join credentials are redeemed on successful WebSocket join. The authenticated `joined` response supplies a short-lived session-and-actor-scoped resumption credential held only in memory and rotated on every successful reconnect. Authentication or protocol incompatibility stops retries and clears that credential.

## Undo and redo

Undo is a new inverse operation authored by the requesting actor. It names the target operation and carries preconditions proving the target values are still safely reversible. It cannot erase history, rewind other actors, or move a shared history pointer. Redo is a new forward operation subject to the same validation.

## Presence

Cursor, selection, viewport, tool preview, typing, and user status are ephemeral. They are not written to the durable operation log, do not affect map hashes, may be dropped or coalesced, and expire after disconnect or timeout.

The server advertises the permitted client publication interval in the authenticated `joined` message. Protocol v1 requires `presence_interval_ms` in the inclusive range 16 through 5000. Clients publish no faster than that interval and continue using the separate lossy presence queue; this limit never applies backpressure to durable operations.

## Map fidelity and persistence

- Unknown DreamMaker types and variables survive parse, operation, snapshot, and save round trips.
- Stable collaboration identifiers are explicit and never derived from process-local counters.
- Canonical hashes use a specified byte representation and ordering.
- Snapshots include protocol, schema, environment, map, and last-revision metadata.
- A saved DMM/TGM is staged, reparsed, hash-checked, flushed, and atomically replaced.
- The previous target remains intact on failure.

## Compatibility

Protocol negotiation is explicit. Additive fields are optional only when their absence has a defined meaning. Semantic changes require a new protocol version and compatibility fixtures. Clients that cannot preserve a document's fidelity join read-only or are rejected.
