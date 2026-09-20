# Queued native frame tracing

Baseline: `99ef0be1`. `TestQueuedNativeUIStageTrace` extends the earlier direct
render probe through `Window.runFrame`, the actual platform backends and
`WsMap.Process`. A test-only frame runner supplies the shared hidden native
window without changing production code or scheduling. Deferred bucket work is
never drained manually or replaced by a direct rebuild during measurement.

Each cycle sends Alt+Right through the shortcut dispatcher, then performs undo,
redo and undo through the real command storage. Every action runs after its
frame's queue drain; the following frame must consume the resulting bucket job.
The probe checks that work was queued, the next frame leaves no pending jobs,
the revision advances by four per cycle and the original map/selection return.
Canvas readback confirms the nudge changes pixels and final undo restores them
exactly. Readback/hashing checks are outside the action timing regions.

## Workload and limits

Five fresh processes used one compiled binary, each with one warmup cycle before
tracing and ten measured cycles: 10 nudges, 20 undos, 10 redos and 80 frames. Each
trace contains 40 completed bucket tasks and zero unfinished tasks; all stage
regions balance. All five measured workloads passed without failures/timeouts.

The synthetic fixture is 4x4x1, with 48 instances of three prefab types and one
object `dir` override. The native window and canvas are 640x480. Fixture SHA-256:
`6470d3b87f42e6cdda07aab90441796b9bfb2c9e0a8aa7a09afc852795059697`.
Canonical hashes vary with generated instance IDs; each process records and
checks its own exact initial/final hash. The canvas initial/final pixel hash is
`26844b558f975fa4dcce03e8df45be0879692226f9242194dac37aed037dd67c`.

This is a warm hidden-window baseline, not a performance improvement or physical
input-to-visible measurement. The probe calls `runFrame` directly, excludes
`Window.Process`'s frame-rate wait, disables swap interval, injects keyboard input
below OS delivery and uses a minimal application around the real map pane.
Following-frame regions include two frames and CPU-side swap return. A separate
outer region includes `gl.Finish`, which establishes completion of preceding GPU
commands, not screen presentation. It is not a GPU timer-query measurement.
Trace overhead, logging and nested work are included. Small sample percentiles
must not be interpreted as production latency, capacity or FPS.

## Observations

Microseconds; nearest-rank percentiles within each process. Median and ranges
below are across the five trials. Nested regions must not be summed.

| Region | Samples/trial | Median p50 | p50 range | p95 range |
| --- | ---: | ---: | ---: | ---: |
| Deferred queue wait | 40 | 167.55 | 160.87–177.82 | 214.13–232.95 |
| Deferred rebuild execution | 40 | 19.36 | 18.10–19.59 | 26.95–31.43 |
| Queue to rebuild completion | 40 | 186.21 | 183.78–198.32 | 235.88–255.59 |
| Native frame | 80 | 213.94 | 204.85–223.21 | 315.52–340.19 |
| Swap call | 80 | 10.36 | 10.02–11.63 | 18.29–39.11 |
| Nudge to following-frame return | 10 | 472.32 | 449.99–501.87 | 543.00–1,291.78 |
| Nudge through explicit GPU completion | 10 | 747.23 | 720.65–784.44 | 1,023.38–1,879.96 |

Whole traced workload duration was 47.69–50.00 ms, including verification and
readback outside the action samples. Queue wait is enqueue-to-deferred-region
start; queue-to-complete ends at the corresponding task end. All 200 measured
tasks across the five processes are accounted for.

## Validation and provenance

The focused native probe passed, then `task verify` passed with
`APHELIONDMM_GL_TEST=1`: lint/contracts, Go tests, pinned Rust checks, parser and
Windows editor build. Rust contains zero unit tests; the inherited ImGui compiler
warning remains. Timing trials ran only after validation and compilation ended.
An initial fixture setup timed out while resizing the shared native window from
a test thread. The final fixture sets its size on the creating thread; that setup
failure is retained separately and excluded from the five measurements.

Host: Windows Server 2022, Ryzen 7 9800X3D, AMD Radeon(TM) Graphics driver
32.0.21002.27; GL reports `3.3.0 Core Profile Context 25.10.02.250325`.
Toolchains: Go 1.25.13, GCC 15.2 UCRT, golangci-lint 2.12.2, Task 3.53.1 and
Rust `1.82.0-x86_64-pc-windows-gnu`. Test binary SHA-256:
`57e72b3f37bf38e61f0405d46b37c00a5c4c6bff959e1b0e0894cd1f64900d57`.

Ignored `.artifacts/ui-frames-2026-09-20/` retains raw traces, decoded events,
logs, binary/source hashes, per-region samples/summaries and the analysis script.
The shared test-window size changed; no production code, dependency, protected
entry point or deployment changed. Representative fixtures, cold/warm campaign,
physical display latency, OS interactions and the full lifecycle/GPU resource
campaign remain open.
