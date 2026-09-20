# Pending writers recover after real queue overflow

At production revision `dd65c6a0`, the mixed pending-writer test now also stalls
both writers' actual server socket writes until their durable subscription queues
overflow. Both connections must end with server close code 4408. All four
memory/SQLite and replay/snapshot cases passed five race-instrumented repetitions.
Production behavior and dependencies do not change.

The scenario reuses the [mixed writer case](2026-09-20-mixed-writer-recovery.md):
six committed-but-unacknowledged edits, five queued-unsent edits and five lost
precondition rejections per writer, with all sixteen offers pending together.
The existing transport wrappers still inject missing responses and queued losses
to establish these known uncertain outcomes. After at least 24 healthy offers
and revision 36, the test arms the existing server socket gates on both writer
connections. It waits for both writes to block, then for twelve further accepted
events. This exceeds each configured eight-entry durable queue. The healthy
editor keeps its absolute 40/second offer schedule and publishes presence every
second offer; it never waits on the stall/recovery loop.

Releasing the socket gates allows the server's actual slow-consumer closure to
reach each client. The test does not manually close either writer in this mode.
It verifies code 4408, every released submission callback, every retained draft
and every original durable outcome before allowing reconnect. Both clients must
rotate credentials and recover while healthy offers continue. Replay clears
known committed drafts; snapshot fallback retains them for explicit disposition.
Counters across all transports reject automatic resubmission. Normal explicit
rebuild/discard and inverse checks remain intact, ending with all three clients
and full stored reconstruction at revision 116 with exactly the expected map
and durable operation identities.

Validation with pinned Go 1.25.13:

- Initial focused run: all four new cases passed in 6.880 s.
- Combined race run: all eight existing/new cases passed in 17.664 s.
- Four further repetitions of the new cases: sixteen cases passed in 33.762 s.
- Focused UI package lint: zero issues; diff whitespace check passed.

This totals 24 passing race cases, including five repetitions of every new
combination, with no failures or timeouts. Raw logs are retained in ignored
`.artifacts/mixed-slow-writer-recovery-2026-09-20/`. The inherited ImGui compiler
warning remains. Full application and external-service gates were not rerun for
this test-only extension.

The socket stall and uncertain original outcomes are controlled injections, but
queue overflow, close codes, authenticated reconnect, replay/snapshot handling
and explicit recovery edits use the real service and session clients. Eight is
a test queue depth, below the production default of 64. This establishes bounded
combined recovery correctness, not capacity, representative desktop performance,
resource retention, PostgreSQL behavior, database restart or crash recovery.
Those broader campaign requirements remain open.
