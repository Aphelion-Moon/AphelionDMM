# Multiplayer Human-Test Readiness

> **Scope update (2026-09-20):** The user removed Meridian-MCP, Aphelion Content Tools and Rift build tooling. Historical implementation and acceptance steps for those integrations are superseded, not remaining work. Native map fidelity checks remain required. See [current integration scope](../../integration/meridian-stack.md).

> **Superseded on 2026-08-31:** Use `../../verification/multiplayer-implementation-readiness.md`, `../../verification/online-pilot-readiness.md`, and `../../verification/public-hosting-readiness.md` for current evidence. The older completion claims below are retained as historical evidence and do not establish current PostgreSQL, cross-stack, external-service, hosted-CI, or human acceptance.

**Status date:** 2026-08-25

**Decision:** AphelionDMM has reached the human desktop-test boundary. Automated work that can be completed safely on the local workstation is implemented or explicitly reconciled below. Aphelion Content Tools is excluded from this readiness decision and must be evaluated by its separate owner.

This ledger is the authoritative current status. The dated phase plans remain the implementation history and intentionally retain original red-test and conditional commit steps.

## Readiness accounting

| Phase | Current state | Automated evidence | Remaining gate |
| --- | --- | --- | --- |
| 1. Repository foundation | Automated scope complete | Toolchain doctor, pinned verification, static analysis, repository tests, cross-stack build, produced headless smoke | Human launch and ordinary desktop behavior |
| 2. Deterministic core | Automated scope complete | Canonical hashes, authoritative operations, actor-scoped inverse operations, unknown-content round trip, atomic-save failure tests, local executor conformance | Human single-user edit/undo/redo/save regression |
| 3. Local service | Automated scope complete | Contract fixtures, single owner, idempotency, bounded presence, secure loopback lifecycle, repeated two-client convergence and reconnect | Human interaction through two real desktop windows |
| 4. Desktop client and UX | Code and headless gates complete | Transport/reconciliation/controller/view-model/presence/conflict/reconnect tests, race tests, produced two-client headless smoke | Visual, keyboard, narrow-layout, conflict-language, close/reopen, and full two-window workflow |
| 5. Durability and security | Automated local scope complete | SQLite restart/recovery, fault/fuzz tests, security controls, telemetry, updater fail-closed verification | Human desktop interruption behavior and production signing ownership |
| 6. Toolset integration | AphelionDMM and Meridian contracts implemented; Content Tools frozen | Versioned manifest, bounded Meridian-MCP adapter, staged verification wrapper and recorded contract evidence | Content Tools is explicitly out of scope; cross-repository human acceptance is a later expansion gate |
| 7. Hosted service | Local implementation and container gates complete | PostgreSQL conformance/recovery, signed fixture OIDC, roles/invitations, hardened image, restore rehearsal, compatibility matrix, deterministic load, restart/database recovery, OTLP export, scans | External OIDC, reference deployment, slow-consumer/presence-flood run, rollback, named-user pilot, updater minisign publication |

## Reconciled plan decisions

- Historical “run and confirm failure” items are test-driven implementation history. Current green tests cannot truthfully reproduce their pre-implementation state, so they are not current backlog.
- Conditional commit steps are complete as workflow decisions: leave changes uncommitted and unpushed until the user requests repository integration.
- A fabricated snapshot schema migration is not added while the default supported matrix contains only the current release at schema version 1. The v1 golden snapshot is verified. A real migration fixture becomes mandatory before adding schema version 2 or declaring a previous release compatible.
- No cross-version rolling pair is currently declared. Overlapping old/new hosted replicas are not a supported deployment mode, and the rollout uses stop-then-start replacement because cross-instance event fanout is not implemented. Compatibility is gated before join; previous/current and multi-replica rolling tests become mandatory before either mode is supported.
- The local 25-editor run demonstrates deterministic public-contract convergence and latency on one host. It does not replace the reference hosted deployment gate.
- Local restore evidence meets the current test targets. Deployment-specific backup credentials, storage, RPO, and rollback remain operational rehearsal items.
- GitHub release provenance is configured. The updater remains fail-closed until a human-controlled minisign key and publication process exist.
- Public discovery remains disabled. Hosted expansion stops on data loss, divergent hashes, authorization bypass, or unrecoverable compatibility failure.

## Human test boundary

Human testing should now concentrate on behavior automated tests cannot establish:

1. Two real `StrongDMM.exe` windows complete start, invite, join, simultaneous editing, conflict handling, undo/redo, reconnect, leave, save, close, and reopen.
2. Collaboration state remains understandable without relying on color, at narrow panel widths, and with keyboard navigation where the inherited ImGui framework supports it.
3. Cursor/selection presence appears on the correct z-level, expires after disconnect, and never changes saved output.
4. Normal single-user editing remains usable before and after a collaboration session.
5. A later controlled hosted pilot validates the selected external OIDC provider, deployment telemetry, slow consumers, sustained presence, rollback, and named-user operating procedures.

Use `../../testing/multiplayer-human-test-guide.md` for the tester procedure and report template.

## Final local verification

The following gates were rerun on the final pre-human-test working tree:

| Gate | Result |
| --- | --- |
| `go run ./cmd/apheliondmm-doctor` | Go 1.25.13, Rust 1.82.0, Task 3.53.1, golangci-lint 2.12.2, and GCC 15.2.0 reported `ok` |
| `task verify` with `RUST_TARGET=1.82.0-x86_64-pc-windows-gnu` | Passed Go tests, locked Rust tests, rustfmt, Clippy with warnings denied, release parser build, and static Windows desktop build |
| `go test ./... -count=1` | Passed |
| `go test -race ./internal/aphelion/... -count=1` | Passed after repairing a test-only asynchronous `testing.T` logger race; the server package also passed ten race repetitions |
| `golangci-lint run` | `0 issues.` |
| `actionlint` | Passed |
| Redocly 2.47.0 OpenAPI lint | Valid with four warning-only recommendations for 4xx responses on logout/health/version endpoints |
| `govulncheck ./...` | Zero called or imported-package vulnerabilities; six findings exist only in required modules on unreachable code paths |
| Produced `apheliondmm-smoke.exe` | Parser save/reparse, two authenticated clients, revisions 1 and 2, identical final hash, leave, and shutdown passed |
| Docker-tagged `TestHostedImageLifecycle` | Latest rebuild passed in 8.25 seconds against image `sha256:13ba75f18d3559a2db5624fa1a1aa92c137e45027f7defb995e70909d3609bd1` |
| `git diff --check` | Passed; Git reported only expected Windows LF-to-CRLF checkout notices |

The desktop build retains the inherited ImGui C++ `memset` compiler warning documented by prior verification. It is not a new multiplayer diagnostic.

## Not authorized by this readiness decision

- modifying or revalidating Aphelion Content Tools;
- committing, pushing, publishing images, provisioning cloud resources, or sending real invitations;
- enabling multiple hosted-service replicas;
- enabling public session discovery;
- enabling updater downloads without verified minisign publication.
