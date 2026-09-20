# Slow-consumer recovery qualification

Baseline: `034fe754`. This pass adds a bounded runtime acceptance case for the
existing collaboration service and desktop session client. No production code,
dependency manifest or protected infrastructure was changed.

## Observed behavior

`TestSessionClientRecoversFromSlowConsumerQueueOverflow` joins two real session
clients through loopback HTTP/WebSockets. A test socket wrapper blocks only the
second client's server writes after joining. Three acknowledged owner edits
fill and overflow its configured one-entry durable subscription queue. The test
does not invoke the server's subscription-close helper or fabricate a close frame.

After the write gate opens, the slow client observes the actual slow-consumer
close code. Its normal session monitor reconnects once with the rotated resume
credential, retains its actor and executor, and replays to the authoritative
revision 3 hash. The recovered client then submits a new edit and actor-scoped
inverse. Both clients reach revision 5 and the pre-edit map hash. Every offered
operation ID appears exactly once, in order, in authoritative store history.

The case passes with both memory and SQLite stores. SQLite uses the maintained
WAL/full-synchronous store configuration and a disposable test directory.
Five repetitions of both cases passed under the Go race detector. These are
correctness repetitions, not performance measurements or a capacity campaign.

Logs: ignored `.artifacts/slow-consumer-2026-09-20/`. The focused test passed;
the race log records all ten backend runs. The repository `task verify` gate
also passed: lint, contracts, Go tests, Rust test/fmt/Clippy, release parser and
Windows editor build. Rust currently contains zero unit tests; skipped GL and
external-service cases remain unverified. The inherited ImGui warning persists.
After the broad gate, an explicit healthy-client revision wait was added before
the inverse to avoid unrelated queue pressure in the fixture; all ten focused
race cases passed again with that final test synchronization.

## OpenGL dependency attempt

The host reports Microsoft Basic/Remote Display adapters. Mesa's
[LLVMpipe documentation](https://docs.mesa3d.org/drivers/llvmpipe.html#windows)
describes application-local DLL use, which could enable software-rendered tests
without system-driver replacement. The Windows packager's
[26.2.0 release](https://github.com/pal1000/mesa-dist-win/releases/tag/26.2.0)
was inspected, and its MinGW release archive was downloaded to the task's local
toolchain directory. Windows security blocked reading that archive as virus or
potentially unwanted software before checksum verification or extraction.
It was not installed or executed, and no security exclusions or system-driver
changes were made. OpenGL desktop validation remains blocked.

## Remaining boundaries

This is controlled socket backpressure with two clients and five accepted
operations. The concurrent load harness still reports a stalled reader as a
failed run; successful recovery in that independently scheduled workload remains
open, including uncertain/rejected offers, presence traffic, larger histories,
long-duration pressure and PostgreSQL. It is not hardware-GPU, native panel,
human interaction, throughput or latency evidence.

The pre-existing staged server-inventory document is untouched.
