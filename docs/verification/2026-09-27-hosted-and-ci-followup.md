# Hosted deployment and CI follow-up

Status date: September 27, 2026. This record supersedes the August readiness
documents for the Windows reference installation. Historical measurements remain
historical; unchecked implementation-plan items are not a current backlog.

## Reference service

The single-replica service at https://mapcollab.a13.info runs revision
`3221379b400cab009e9c77a3519b2d8335037c9e`, build
`mapcollab-windows-20260927`. Its executable SHA-256 is
`604cae207bf4a4ae5849f8a4bb888a55c0d17b83012031d09db45560cf2b6eb7`.
PostgreSQL, hosted application and dedicated tunnel are automatic Windows
services. Public readiness and version were verified after cutover and rehearsal.

Discord is the selected provider. The operator confirmed the application redirect
and bot installation; the restricted client secret is installed. Community and
Private session browsing is implemented and advertised. Real member/nonmember
login and the named-user pilot have not been completed.

The 12-hour application credential checks guild membership at login. Removing a
user from the guild does not immediately invalidate an existing application
credential. Restart clears in-memory login sessions.

## Source repairs

Follow-up source commit: `506ba32b`, on top of the deployed race fix. These
editor/platform/lint repairs have not replaced the running service binary.

- Captured the immutable WebSocket session ID before starting its reader,
  eliminating the race with reauthorization (commit `3221379b`). The existing
  reauthorization regression passed 20 repetitions with the race detector.
- Canonicalized existing cache directories, including Windows short names.
  Accepted Administrator-owned Windows cache paths within the existing
  user/SYSTEM/Administrators trust boundary; foreign write grants, reparse
  points and incorrect path kinds remain rejected.
- Kept the relative-path snapshot fixture on the checkout volume.
- Replaced unavailable Darwin memory sysctls with Mach host statistics when cgo
  is enabled. Free and inactive pages are counted once; the host port is released.
  Non-cgo builds conservatively use the supported free-page count.
- Resolved lint findings without disabling checks. Removed obsolete Aphelion
  helpers and preserved inherited removals. Local recovery remains accessible
  through the edit-status control. The NaN cache check now exercises insertion
  and retrieval instead of comparing an expression with itself.

The two new Windows cache regressions failed before the fixes. Focused
cache/resources/parser, authentication/client, configuration, editing,
render-cache and editor tests subsequently passed. This machine's normal profile
has additional inherited write grants, which the cache correctly refuses; cache
tests ran with a restricted disposable LOCALAPPDATA directory.

The macOS render scheduler and bulk-edit timeout trace to failed memory admission.
The latter opened a blocking Cocoa error dialog. This causal diagnosis is source
and CI-log evidence. The subsequent CI run passed all macOS Go tests, including
the Mach path; a separate Rust fixture path assertion then blocked the desktop
build. That test now expects the OS working-directory spelling, preserving
separate lexical source-path assertions.

The complete Windows `task verify` passed with Go 1.25.13, Rust 1.82 GNU,
golangci-lint 2.12.2 and Task 3.53.1: zero lint issues, contract checks, all Go
tests, Rust tests/fmt/clippy and the native parser/editor build. The inherited
ImGui compiler warning remains. PostgreSQL/container/GL opt-in tests are not
silently included in that claim. The hosted browser PostgreSQL lifecycle passed
separately on an isolated database, and the Darwin arm64 non-cgo resources test
binary cross-compiled successfully.

## Additional exercised boundaries

The hidden-OpenGL `TestSaveAcknowledgementBoundaries` passed with
`APHELIONDMM_GL_TEST=1`. This exercises real PaneMap/WsMap save paths, including
expected failure cases; it does not replace human desktop acceptance.

The existing 25-client loopback pilot passed: 250 accepted operations, 500 presence
updates, p50 1.00 ms, p95 5.00 ms, p99 20.48 ms, final revision 250 and hash
`71913e2be13a2206141dd93bc0d4243eef7a47f33a92b7425ce399966189f05f`.
This is the paced public-contract fixture, not a measured capacity limit or
an external Discord-hosted pilot.

The encrypted pre-upgrade archive `20260927-152449-640.dump.age`, SHA-256
`2e63cef0f525817eed02b1375dd01263460544b83be4a24eb2780c1bac29cbe3`,
was restored to isolated database `apheliondmm_rollback_20260927`. It contained
migrations 1/2/3 and zero documents. The retained old executable
`81a89615c4ae13f7e704df566dde2db62ab79389` and old provider configuration started
on loopback port 18083 and passed readiness/version checks. The temporary process
was then stopped and plaintext restore staging removed. The live Discord service
remained on revision 3221379b. This validates the coordinated artifact/database
rollback on a clone, not a live rollback or an old-provider login.

## Remaining acceptance and operator inputs

- Complete the new main CI run after the fixture repairs described below.
- Complete Discord member/nonmember sign-in, logout/expiry and the two-user
  Community/Private/restart procedure, followed by human desktop acceptance.
- Supply disposable authenticated users for the reference-hosted load/fault
  profile. Loopback evidence does not satisfy that gate.
- Select an OTLP destination and alert ownership; neither is configured.
- Backups remain daily encrypted archives under the operator-selected
  `D:\Backups\AphelionDMM`. The schedule succeeded with result 0. This is
  same-machine storage, and daily dumps do not meet the five-minute recovery
  point target. Off-machine copies/key custody, retention and a revised backup
  policy remain operator decisions.
- Assign signing-key ownership before signed updater publication. Protected
  release/deployment entry points require exact-file approval under AGENTS.md.
- Representative-map performance and extended resource/history qualification
  remain separate from these bounded correctness and loopback checks.

Multi-replica operation remains unsupported. Removed Meridian-MCP, Content Tools
and Rift integrations are not acceptance requirements.

## Main integration and fixture follow-up

The operator approved publication and then directed that commits belong on main.
The main checkout was clean and was fast-forwarded to the reviewed source. The
initial branch run (36324162167) passed lint, Linux build and collaboration
resilience. macOS passed all Go tests but exposed the Rust path expectation above.
Hosted conformance, auth and vulnerability checks passed before the container
lifecycle fixture reached an outdated empty login request.

The container fixture now supplies a desktop verifier challenge, opens the real
browser-start endpoint, carries its Secure/HttpOnly/SameSite cookie across the
fixture's loopback HTTP transport, completes the provider callback without a
credential response, and exchanges the verifier for the application credential.
It retains the restart, database interruption, invitation and telemetry checks.
No production auth or CI entry point was weakened. Container-tagged compilation
and focused lint passed locally; Linux container execution requires CI because
this host's Docker daemon runs Windows containers.

Native window checks exposed two blocked fixture workers per lifetime: the
fixture sent to a full notification channel after the production queue had
already accepted each job. The production queue does not contain that send.
A regression failed before changing the hint to a nonblocking, coalesced send;
both actual queued callbacks are still required to execute. The complete native
window package passed with GL enabled. Two representative-asset checks skipped
because their external fixture inputs were not configured.

A 70-second follow-up completed 697 measured synthetic lifetimes, including
per-cycle authority/save, editor reachability and texture-disposal checks.
Goroutines remained 2 at all three checkpoints; post-GC heap allocation went from
2,700,072 to 2,774,256 bytes. Before the fixture fix, a comparable 698-cycle run
grew from 2 to 1,398 goroutines. This repairs the measurement fixture and adds
bounded native evidence; it does not claim representative-map or full-editor
endurance qualification.
