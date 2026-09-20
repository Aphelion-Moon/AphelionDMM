# Multiplayer implementation roadmap

> **Scope update (2026-09-20):** The user removed Aphelion Content Tools support and Rift build tooling. Historical implementation and acceptance steps for those integrations below are superseded, not remaining work. Meridian-MCP parsing, map inspection and diagnostics remain supported. See [current integration scope](../../integration/meridian-stack.md).

> **Current status (2026-08-25):** Automated implementation and local verification are complete through the pre-human-test boundary, excluding Aphelion Content Tools by explicit direction. The authoritative status and remaining gates are in `2026-08-25-multiplayer-human-test-readiness.md`. The original phase checklists below are retained as implementation history; an unchecked historical red-test or conditional commit step is not automatically current backlog.

Execute these plans in order. Each phase is independently reviewable and has an explicit acceptance boundary.

| Phase | Plan | Produces | Entry condition |
| --- | --- | --- | --- |
| 1 | `2026-08-24-repository-foundation-and-reproducibility.md` | Pinned evidence, doctor command, baseline quality gates | Approved design |
| 2 | `2026-08-24-deterministic-operation-core.md` | Deterministic local operation engine and atomic saves | Phase 1 gates |
| 3 | `2026-08-24-local-collaboration-service.md` | Loopback authoritative service and two-client convergence | Phase 2 gates |
| 4 | `2026-08-24-collaboration-client-and-ux.md` | Desktop multiplayer, presence, conflicts, reconnect, undo | Phase 3 gates |
| 5 | `2026-08-24-collaboration-durability-and-security.md` | SQLite durability, recovery, security, telemetry | Phase 4 gates |
| 6 | `2026-08-24-aphelion-toolset-integration.md` | Versioned Meridian and Content Tools adapters | Phase 5 gates |
| 7 | `2026-08-24-hosted-rollout-and-operations.md` | PostgreSQL/OIDC hosted service and operational rollout | Phase 6 gates |

Do not skip the deterministic local core in order to demonstrate networking. Do not begin hosted rollout before restart recovery, authorization, and cross-repository acceptance are proven.

Every conditional commit step requires explicit user authorization. Without that authorization, leave verified changes in the working tree and report them as uncommitted.

No plan in this directory authorizes a commit or push. The current implementation remains uncommitted for review.
