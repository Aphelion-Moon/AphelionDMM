# Concurrent SQLite offered-rate sweep

Five measured fresh-process trials passed at each of 40 and 80 offered
operations/second. All five trials at 160 failed: three exceeded the configured
250 ms schedule-to-acknowledgement p95 limit after convergence, and two received
the server's `4408 durable consumer fell behind` close before convergence.
No trial timed out. This is a bounded synthetic service result, not a production
capacity limit, desktop measurement or successful load-recovery qualification.

## Workload and execution

Source was `228d75a5ea9aa6fa5755e20fa04b1f3802cd5d77` plus the retained
`source.patch`: the produced-command test accepts an optional absolute
`APHELIONDMM_LOADTEST_SCENARIO` path and checks the chosen scenario's expected
counts instead of its previous nine-operation constants. The ordinary test
retains that original fixture. Production runner/service code was unchanged.

Every scenario used seed 20260920, four clients, 320 operations, 64 conflict
pairs, and a 16-by-16-by-1 map. Each operation changes one tile from an initially
empty map. Expected results are 256 acceptances, 64 precondition rejections,
1,024 client applications, revision 256 and canonical hash
`ec8036cca93104c5475c87dacbcd5d64f8cd21eb2462ff59097d6b2f6b08bf4e`.
All operations use revision-zero bases. The generator places conflict pairs
first, followed by independent edits; the 20% rejection fraction is therefore
not uniformly distributed through the trial. Map size and retained history
increase during each run.

Operation schedules were fixed at 40, 80 and 160/second, with 160, 80 and 40
presence updates per client respectively, each offered at 20/second. Thus the
nominal operation/presence windows were approximately eight, four and two
seconds. Presence ran concurrently. No acknowledgement released the next send,
and no workload, deadline or latency threshold was changed after a failure.

Each test process hosted the real authenticated HTTP/WebSocket service with a
fresh temporary SQLite database and launched the same compiled load executable
as a separate process. `sqlite.Open` enforces and verifies WAL, synchronous FULL,
and foreign keys; successful reopen logs report SQLite 3.53.3. Service presence
interval was 16 ms. CLI timeout remained 10 seconds, fixture context 15 seconds,
and test-process timeout 30 seconds.

One independent warmup per rate preceded all measured runs. Warmup order was
40/80/160; measured rounds were 40/80/160, 80/160/40, 160/40/80, 40/80/160 and
80/160/40. Runs were sequential. Warmup p95 schedule-to-ack values were 14.3943,
27.3789 and 728.4557 ms; the last warmup failed the latency gate after convergence.
Warmups and the later instrumented diagnostic are excluded from measured ranges.

Host: Windows Server 2022 Standard, build 20348; Ryzen 7 9800X3D, eight cores and
16 logical processors. The database used the normal temporary directory on this
workstation; no dedicated storage or host-noise isolation was configured.
Loopback service and clients share the host. No other benchmark or build was
deliberately run during the measured sweep; background desktop activity was not
controlled. Builds used pinned Go 1.25.13, Windows amd64, without race profiling.

## Measured results

Ranges below are minima/maxima of the five per-trial summaries, not percentiles
of pooled observations. Each successful trial has 256 acknowledgement and
all-client-application samples and 64 rejection samples.

| Offered operations/s | Passes | Schedule-to-ack p95, ms | Send-to-ack p95, ms | Send-to-all-applied p95, ms | Achieved accepted/s |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 40 | 5/5 | 13.1662–14.1577 | 13.0163–13.8140 | 13.5790–14.6975 | 32.0422–32.0441 |
| 80 | 5/5 | 14.2454–27.8907 | 14.1512–27.6456 | 26.3235–45.6924 | 63.4667–63.8420 |
| 160 | 0/5 | 641.1190–792.5599 | 641.0293–792.2826 | 688.1270–862.4585 | 101.3261–106.4745 |

The 160-rate ranges include incomplete observations from the two disconnects.
Their achieved-rate field is partial outcome evidence, not a capacity pass.
Accepted rate also differs from offered rate because 64 of 320 intents are
expected to be rejected. Schedule-to-ack p99 ranges were 14.2679–15.6576,
22.3990–46.1093 and 692.3622–872.7204 ms respectively.

| Rate | Trial 1 p95 | Trial 2 p95 | Trial 3 p95 | Trial 4 p95 | Trial 5 p95 |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 40 | 14.1577 | 13.5973 | 13.1662 | 13.2507 | 13.2468 |
| 80 | 14.9041 | 27.8907 | 14.2454 | 14.9771 | 14.4420 |
| 160 | 792.5599 | 783.3701 | 654.8622 | 641.1190 | 708.1403 |

All 18 warmup/measured runs scheduled, started and sent all 320 operations,
observed 256 unique acceptances and 64 rejections, and ended with zero unsent
backlog. Peak send backlog was one throughout; this measures write completion,
not server work or unacknowledged operations.

The ten passing measured trials applied all 1,024 deliveries and verified every
client and the final HTTP snapshot against the expected revision/hash. Each
then shut down and reopened SQLite and restored that same revision/hash. The
two lower-rate warmups did likewise. This is orderly reopen, not process-crash
or power-loss evidence.

At rate 160, measured trials 1 and 2 ended on `4408` with 1,018 and 1,010 applied
deliveries, and two and three unresolved sender outcomes respectively. Client
revisions at cutoff were `[256,256,256,250]` and `[247,251,256,256]`. Those runs
did not reach final HTTP snapshot verification. Their zero `server_revision`
and empty `server_map_hash` fields mean unmeasured, not an empty database. The
other three trials converged completely but failed the p95 threshold. The test
exits on any nonzero CLI result, so none of these five failures or the failed
warmup reached the SQLite reopen check. No recovery or missing outcome was
inferred from an acceptance observed by a different client.

All planned presence updates were sent, with zero local coalescing. Unobserved
receiver deliveries at the bounded cutoff were 493–618 out of 2,560 at rate 40,
228–302 out of 1,280 at rate 80, and 196–221 out of 640 at rate 160. These include
the sender as a receiver. They are not identified as packet loss; server
coalescing, delay and the failure cutoff can affect observation.

## Separate diagnostic profile

After the sweep, one additional 160-rate run collected Go CPU and allocation
profiles in the test-host process, which contains the service and SQLite but
not the child load generator. It failed the unchanged latency gate at
579.8517 ms p95 after convergence. It is not a sixth measured trial.

The allocation profile estimates 1,195.41 MB total allocated, of which SQLite
`Store.Append` accounts for 1,144.49 MB cumulatively (95.74%) and
`RecoveryState.RestoreAt` for 997.41 MB (83.44%). These cumulative paths overlap
and must not be summed. This is sampled total allocation, not retained heap.
Source inspection confirms each durable append loads and restores retained
history before applying and verifying the new operation. This supports the
existing history-replay allocation hypothesis and prioritizes further profiling;
it does not establish the cause of the latency or consumer disconnects.

The CPU profile is dominated by Windows syscall stacks, including child-process
pipe reads and waits (73.99% cumulatively at `runtime.cgocall`). It is retained
but cannot separate active service CPU, SQL, replay, commit/fsync and waiting
time. No performance repair or durability/cache change was made on this evidence.

## Reproduction and remaining gates

The focused command package passed (`go test ./cmd/apheliondmm-loadtest -count=1`,
0.269 s). Then the pinned toolchain built both executables:

```text
go build -trimpath -o <artifacts>/apheliondmm-loadtest.exe ./cmd/apheliondmm-loadtest
go test -c -o <artifacts>/loadtest-gate.test.exe ./cmd/apheliondmm-loadtest
```

Set `APHELIONDMM_LOADTEST_BINARY` to the absolute CLI path and
`APHELIONDMM_LOADTEST_SCENARIO` to one of the retained scenario JSON paths, then
run `loadtest-gate.test.exe -test.run=^TestConcurrentProducedBinary$ -test.v
-test.timeout=30s`. The test generates temporary credentials internally.

Raw evidence is retained locally in ignored
`.artifacts/concurrent-rate-sweep-2026-09-20/`: three scenario manifests, both
binaries, source patch, build metadata, all 18 logs, ordered `runs.jsonl`, full
parsed `results.json`, `summary.csv`, and separate diagnostic logs/profiles.
The provenance manifest records full SHA-256 values. CLI SHA-256 is
`bd76e6e7236a11453089d329ac65a3c21261579978886fcc04c069aa8732414d`;
test binary SHA-256 is
`98b4082ad6ace0c18eb0ad6c7f819789c60221467135476ceb749bd2a79c809f`.
No broader repository gate was rerun for this test-only extension and evidence.

Representative maps/history lengths, longer steady-state trials, driver/service
resource attribution, PostgreSQL/hosted load, and independently scheduled
disconnect recovery remain open. The observed high-rate disconnects make exact
reconnect and ambiguous-send accounting a concrete next acceptance requirement;
increasing queues or weakening the gate would not satisfy it.
