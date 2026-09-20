# Multiple interrupted writers with mixed pending outcomes

At production revision `721b043b`, two writers each recover sixteen simultaneous
pending edits while a healthy editor continues 64 scheduled offers at 40/second
and publishes presence after every second offer. Memory and SQLite stores both
pass replay and snapshot recovery, including five race-instrumented repetitions
per combination. This adds test coverage; production code and dependencies do
not change.

Each writer has six operations genuinely committed by the server with their
acknowledgements withheld, five locally queued operations withheld before socket
write, and five genuine server precondition rejections with responses withheld.
The rejection checks include the operation identity, coordinate, authoritative
tile value and durable revision hash. All sixteen original offers per writer
remain pending together before both transports are interrupted.

The test holds reconnect long enough to verify every original draft and durable
outcome. Replay resolves the six known commits per writer and retains the other
ten drafts. Snapshot recovery retains all sixteen because it cannot establish
operation identity. Both paths rotate credentials, retain the existing executor
and recover before the healthy editor finishes its scheduled offers. A counter
across all writer transports verifies that recovery does not resend intent.

After all clients converge at revision 76, the test uses the session client's
explicit conflict actions. Already-present snapshot commits cannot be rebuilt
and are explicitly discarded. Each remaining draft is rebuilt with a fresh ID,
current revision/hash and current before-values, preserving its intended
after-values, then undone through an actor-scoped inverse. The exact remaining
draft set is checked after every action. Each writer sends sixteen original
offers and twenty explicit recovery operations. All three clients and full
stored reconstruction agree at revision 116, accounting for the 64 healthy
edits, twelve original commits and forty explicit recovery operations without
unknown or duplicate durable IDs. Inverses preserve canonical v1's explicit
empty-tile membership.

Validation used the pinned Go 1.25.13 toolchain:

- Initial focused run: all four cases passed, package duration 6.777 s.
- Final source, including exact rejection tile checks: all twenty race cases
  passed in 41.999 s, with no failures or timeouts.
- Focused UI package lint: zero issues.

Raw logs are retained in ignored `.artifacts/mixed-writer-recovery-2026-09-20/`.
The inherited ImGui compiler warning remains. Full application and external
service gates were not rerun for this test-only change.

This covers two interrupted writers and 32 pending intents on loopback sockets.
Transport loss is injected; it is not combined real slow-consumer queue overflow
or a capacity measurement. Representative load/resource qualification, larger
histories, PostgreSQL, database close/reopen and crash recovery remain outside
this case. The broader performance and recovery gates remain open.
