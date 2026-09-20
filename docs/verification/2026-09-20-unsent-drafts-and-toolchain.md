# Unsent collaboration drafts and local toolchain

Baseline: `f243e91339a737fb37fa06e4b90ab01aa6ec04a7`. This is a bounded
continuation of the September 6 Search stopping point, not completion of the
performance, desktop acceptance, or hosting workplans.

## Repairs

`NetworkExecutor.Execute` previously discarded operations rejected by local
projection validation. `failPending` also removed drafts when the transport
refused submission, including the existing 4,096-change and 1 MiB envelope
checks. The editor then restored authority without retaining entered intent.
Four focused cases reproduced that loss before the change.

These failures now retain a deep-copied `submission_failed` conflict, with the
current acknowledged revision/hash, using the existing refresh/discard/rebuild
controls and 100-conflict retention limit. Foreign document/environment inputs
cannot become rebuildable conflicts. No failed action enters accepted history,
no action is silently split, and no protocol limit or server rule changes.
Rebuild still uses current authoritative preconditions.

Actual non-rendering Search `doReplaceAll` coverage proves an oversized
three-tile replacement restores all results, keeps authority/history unchanged,
leaves editing usable, and retains every intended after-value. Real loopback
WebSocket coverage rebuilds a locally rejected draft, accepts it once, then
submits its actor-scoped inverse and restores the original map hash.

The installed WinLibs GCC banner also reproduced a doctor failure. The parser
required the literal vendor label `(GCC)`; it now recognizes GCC executable names
with distribution labels, including WinLibs and Ubuntu. Tests reject unrelated
programs, missing versions and versions appearing only on later lines.

## Dependencies and evidence

Installed Go 1.25.13, GCC 15.2.0 (WinLibs UCRT r7), Task 3.53.1 and
golangci-lint 2.12.2 under the current Codex task's `work/toolchains/` directory.
Installed Rust `1.82.0-x86_64-pc-windows-gnu`, rustfmt and Clippy through existing
rustup; its default toolchain was preserved. Go and GCC archives were checked
against publisher SHA-256 values. Repository dependency manifests/locks and
protected build/deployment entry points were not changed. These portable tools
were placed on PATH for validation commands; system/user PATH were not changed.

Raw logs: ignored `.artifacts/blockers-2026-09-20/`.

| Check | Result and boundary |
| --- | --- |
| New client regressions | Passed: local rejection, send failure, exact/over byte limit, 4,096/4,097 transport change limit, draft ownership, foreign document isolation and real WebSocket rebuild/inverse |
| Actual Search Replace All regression | Passed without rendering; complete oversized intent retained with restored results and unchanged authority/history |
| Affected race | Client, collaboration UI and Search packages passed |
| `task verify` | Passed lint, contracts, full Go suite, pinned Rust test/fmt/Clippy, parser release build and Windows desktop build. This preceded only the doctor parser fix; affected doctor/buildcheck tests and lint then passed on final source. Rust reported zero unit tests. Default GL/database/integration skips are not evidence for those gates. |
| Produced doctor executable | All five tools reported `status=ok` after the parser repair |
| `go mod verify` | All modules verified |
| Produced smoke executable | Passed parser save/reparse and authenticated two-client convergence |
| New hidden-workspace regression | Compiles; runtime blocked at GLFW initialization: `APIUnavailable: WGL: The driver does not appear to support OpenGL`. Async editor assertions were not reached. |

Desktop SHA-256:
`e031ec2608a0b3b816f11e7ee51318cf1e3c3da66e97f9d084f3b01ec324910c`.
The inherited ImGui `memset` compiler warning remains; the successful build does
not establish that the warning is harmless.

## Remaining boundaries

- Run `TestSearchOversizedSubmissionRetainsDraftAndAllowsRetry` with
  `APHELIONDMM_GL_TEST=1` on an OpenGL-capable Windows session, then complete
  named-human desktop acceptance. No driver or live-service changes were made.
- Qualify large accepted engine actions, response-envelope headroom, lower hosted
  message limits and disconnects after a send succeeds. This repair covers
  failures before transport queueing, not ambiguous delivery/reconnection loss.
- Local-executor oversized-action UX, full draft inspection/export UX and
  deliberate batching/history semantics remain open. Conflicts are in-memory
  and subject to existing retention limits, not durable backups.
- The wider performance/QoL matrix, real Meridian integration, hosted CI,
  production identity/deployment/signing and reference-user pilots remain open.
  This run did not exercise live PostgreSQL or deploy/push anything.

The pre-existing staged server-inventory document is unrelated and was preserved.
