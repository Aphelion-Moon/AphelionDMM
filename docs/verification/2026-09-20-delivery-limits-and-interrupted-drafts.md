# Delivery limits and interrupted drafts

Follow-up: [terminal retention, draft inspection/export and guarded desktop close](2026-09-20-draft-inspection-and-export.md)
now covers those recovery gaps. The remaining boundaries below describe this
earlier pass.

Baseline: `e2741245`, continuing the unsent-draft handoff. This pass covers
large-operation delivery and recoverable connection interruption. It does not
close the wider desktop, performance, integration or hosting workplans.

## Reproduced failures and repairs

- A valid request exactly 1 MiB long was committed, then its larger acceptance
  disconnected a client using the protocol read limit. The document owner now
  checks delivery size after engine validation and before durable append. The
  check reserves envelope escaping, revision/timestamp width and inverse metadata
  so new history can be delivered through acceptance, duplicate replies, replay
  and actor-scoped undo. Oversized work receives `limit_exceeded` without a
  revision, stored operation or consumed operation ID. This is conservative
  headroom within the existing 1 MiB protocol, not an increased wire limit.
- A small stale request touching two large current tiles produced an oversized
  rejection and disconnected the client. Outbound messages are now byte-bounded.
  When rejection details exceed that bound, optional authoritative tile values
  are omitted; operation ID, rejection code, revision and hash remain intact.
- Prefixing a valid maximum-length request ID made duplicate replies invalid
  (`138` bytes versus the `128`-byte limit). Only overflowing reply IDs now use
  a deterministic SHA-256-based identifier. Ordinary IDs are unchanged.
- Recoverable connection suspension discarded pending intent, including a real
  request allowed by the client but refused by a server's lower byte limit.
  Suspension now retains these drafts as `delivery_unconfirmed`, clearly stating
  that they may already have been applied. It never automatically resends them.
  Snapshot fallback preserves unresolved drafts. A hash-verified accepted replay
  clears the matching draft only when the operation's identity and intent match.
  Existing rebuild logic uses current authoritative preconditions and refuses
  intent already present in the new snapshot. The panel's revision label now says
  "Recorded" rather than incorrectly classifying uncertain delivery as rejection.

Conflicts still use the existing in-memory 100-entry retention policy. There is
no durable draft backup, automatic force overwrite, protocol/schema migration,
dependency change or protected infrastructure edit. Pre-append size checking
adds serialization work; no performance improvement is claimed.

## Evidence

Raw logs: ignored `.artifacts/delivery-2026-09-20/`.

| Gate | Result and scope |
| --- | --- |
| Red/green regressions | Both oversized replies disconnected before repair; maximum-length duplicate IDs failed decoding; interrupted drafts were lost in replay/snapshot and lower-server-limit cases. All pass after repair. |
| Large WebSocket action | Near-1-MiB action accepted once, duplicate stayed at revision 1, inverse reached revision 2, and a fresh client replayed both events to the exact authoritative hash. |
| Tile boundary | Real document owner/store accepted all 4,096 changed tiles; actor-scoped inverse restored the original hash. A 4,097-tile action was rejected without another stored operation. |
| Size rejection | Exactly-1-MiB request refused before persistence; corrected retry with the same ID accepted at revision 1. Large optional rejection details omitted without closing the socket; subsequent ping succeeded. |
| Interrupted recovery | A real 1,024-byte server input limit closed a locally valid queued request. Its complete intent survived suspension. Controlled replay and snapshot tests verified retention, exact-match resolution, fresh rebuild preconditions, already-present refusal and no automatic resend. |
| Affected race | Server, client and collaboration UI passed. |
| `task verify` | Passed lint (zero issues), contracts, full Go suite, pinned Rust test/fmt/Clippy, release parser and Windows desktop build. Rust has zero unit tests. Default GL and external-service skips are not acceptance evidence. |
| Produced smoke executable | Passed real parser save/reparse and authenticated two-client convergence. |

Desktop SHA-256:
`9eb0172a4220a114937fdd6ae2bce949735fc84f343cca832a15ccd078352a29`.
The inherited ImGui `memset` warning remains. The earlier OpenGL initialization
failure was not retried without an environmental change; this run has no new
interactive or hidden-GL runtime evidence.

## Remaining boundaries

- Existing oversized persisted history is not rewritten; outbound protection
  alone does not make such legacy events replayable. Terminal integrity failures,
  explicit shutdown and desktop command-history reconstruction after reconnect
  remain separate recovery cases to qualify.
- Full draft inspection/export, local-executor oversized-action UX and deliberate
  batching/history semantics remain open. Large engine/WebSocket fixtures are not
  full desktop Search/selection acceptance or representative latency measurements.
- OpenGL-capable desktop and named-human acceptance, the wider performance/QoL
  matrix, live PostgreSQL, actual Meridian integration, hosted CI and production
  identity/deployment/signing/pilot evidence remain outside this pass.

The pre-existing staged server-inventory document remains untouched.
