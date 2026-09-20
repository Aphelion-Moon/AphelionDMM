# Recovery after a lost rejection response

The writer recovery test now also covers a real server precondition rejection
whose response never reaches the writer. At production revision `7a3c4caa`, all
four memory/SQLite and replay/snapshot combinations passed five race-instrumented
repetitions. This is a test extension; production code and dependencies did not
change.

The setup extends the [uncertain writer case](2026-09-20-writer-recovery-under-load.md).
A healthy editor continues 32 fixed-schedule offers at 40/second. The interrupted
writer targets the first healthy edit's coordinate while its transport withholds
accepted events, leaving its acknowledged prefix intact. Its locally valid draft
crosses the real socket and fails the server's authoritative before-value check.
The wrapper captures and withholds the actual `precondition_failed` response.
The test checks its operation ID, coordinate, authoritative tile value and map
hash against the durable revision ledger. All observed rejections used revision 1.

After interruption, replay or snapshot recovery must complete while healthy offers
continue. The writer retains the exact `delivery_unconfirmed` draft because it
never received the rejection. A counter across every writer transport verifies
that reconnect does not resubmit it. An explicit rebuild must use a fresh ID,
the current revision/hash and current before-values while preserving the intended
after-values. The rebuild and its inverse both execute through the session client.
Both clients and full stored reconstruction converge at revision 34, accounting
for all 32 healthy operations and the two explicit recovery operations. The
original rejected ID is absent from durable history.

Validation used the pinned Go 1.25.13 toolchain:

- Expanded focused test: all 12 outcome/backend/recovery combinations passed,
  package duration 9.745 s.
- Expanded focused race run: all 12 combinations passed, 11.074 s.
- Four additional repetitions of the rejection cases: 16 cases passed, 14.406 s.
- Focused UI package lint: zero issues.

The total is 28 passing race cases, including five repetitions of each new
rejection combination. Raw logs, recovery observations and hashes are retained
in ignored `.artifacts/lost-rejection-2026-09-20/`. The inherited ImGui compiler
warning remains. Full application and external-service gates were not rerun for
this test-only change.

This establishes one lost rejection during independent offers, with replay and
snapshot recovery. Multiple pending intents, multiple failing writers, combined
slow-consumer pressure, long histories, resource qualification, PostgreSQL and
hosted-network campaigns remain open. SQLite reconstruction here does not include
database close/reopen or crash recovery.
