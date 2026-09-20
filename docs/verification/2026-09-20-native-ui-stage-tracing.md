# Native UI stage tracing

Baseline: `154e9b0e`. This adds an owned Go execution-trace wrapper and a
small native correctness/timing workload. It does not claim a speedup or close
the representative desktop performance campaign.

## Recording and measurement boundaries

Set `APHELIONDMM_UI_TRACE` to a new local file path before launching the editor.
The editor records up to 30 seconds from startup, flushes on timeout or normal
shutdown, and refuses to overwrite an existing file. The parent directory must
exist. An unavailable path or an already active Go trace logs an error and leaves
the editor running. Unset the variable for ordinary use. Inspect recordings with
`go tool trace <file>`; the pinned Go 1.25 decoder uses `-d=parsed` for text events.
The shipped startup hook was compiled; an interactive desktop recording through
that hook was not exercised in this pass.

The maintained wrapper is `internal/aphelion/diagnostics/uistage`. Regions cover
tile capture, commit, edit/history dispatch, visible projection, projection
application, collaboration refresh, bucket rebuild, canvas draw, window frame,
swap buffers, parser calls, icon decoding, and texture upload. These are elapsed
CPU-side boundaries including nested work and waits, not exclusive CPU usage.
Dispatch includes synchronous execution/callbacks but stops before asynchronous
acknowledgement. Parser regions currently include native parsing, transfer and
Go decoding together; they do not isolate those costs. Canvas/upload/swap call
return is not proof of GPU completion or display visibility.

A deferred bucket task spans queueing through completion; its nested region
covers execution. Missing task ends are censored work, never zero-duration
samples. The wrapper emits stage names, not map contents or operation payloads.
Go's trace also contains runtime stacks and scheduling information.

## Fixed native probe

Five fresh processes used the same compiled test binary, each with one warmup
cycle outside the trace, followed by ten measured cycles. Each cycle invokes the
real Alt+Right selection shortcut, undo, redo, undo, then explicitly rebuilds and
draws the canvas and calls `gl.Finish`. Every cycle checks revision increased by
four and the canonical map hash was restored. The input is a synthetic 4x4x2 DMM
with 96 prefab instances and three prefab types; the object has one `dir` override.
Input SHA256: `2b0d1e4aed89a9ad3dc062eb8bfb20c0507ed98f078be4ee5b454e3748221f30`.
Canonical hashes vary across processes because instance IDs are generated; each
trial's exact before/after hash is retained in its log.

Per trial: 10 edits, 20 undos, 10 redos; five passed, zero workload failures or
timeouts. All decoded stage regions balanced. There are 40 unfinished bucket
tasks per trial: the fixture does not drain the production window queue. Direct
bucket/draw calls provide component evidence only. Frame/swap, network visible
projection, cold parser and icon regions were not sampled by this workload.

Host: Windows Server 2022 build 20348, Ryzen 7 9800X3D, AMD Radeon(TM) Graphics,
driver 32.0.21002.27. The driver reports device error code zero; native renderer
reports `3.3.0 Core Profile Context 25.10.02.250325`. Toolchain: Go 1.25.13,
GCC 15.2.0 UCRT, Rust 1.82.0 Windows GNU. Test binary SHA256:
`29BD951C580797FA68F737633C2A87B67A49BF2C7ABD53D6F92BD642489F0FC4`.

## Observations

Microseconds, nearest-rank percentiles within each trial; medians/ranges below
are across five trials. Small samples, trace overhead and logging are included.
Do not sum nested regions or interpret these as production latency percentiles.

| Stage | Samples/trial | Median trial p50 | Trial p50 range | Trial p95 range |
| --- | ---: | ---: | ---: | ---: |
| Tile capture | 20 | 0.547 | 0.495–0.655 | 1.926–3.941 |
| Commit | 10 | 98.862 | 89.952–102.681 | 163.809–501.258 |
| Edit/history dispatch | 40 | 99.731 | 89.077–109.348 | 155.381–323.098 |
| Projection application | 30 | 33.994 | 30.149–36.924 | 39.613–197.104 |
| Collaboration refresh | 40 | 16.830 | 15.867–17.804 | 26.971–67.652 |
| Bucket rebuild | 20 | 16.790 | 15.600–18.787 | 20.928–43.805 |
| Canvas draw call | 10 | 18.474 | 15.801–20.570 | 26.520–36.832 |

Whole traced workload durations were 9.348, 9.663, 9.212, 8.001 and 9.752 ms.
They include correctness hashing and explicit GPU completion waits; they are not
input-to-visible latency. The disabled-wrapper benchmark (one begin/end plus one
deferred no-op per iteration, five 200 ms samples) measured 3.960–4.063 ns/op,
median 4.022 ns/op, zero bytes and allocations. This measures wrapper overhead
only, not total editor overhead or the cost of enabled tracing.

## Validation and retained evidence

Ignored `.artifacts/ui-stages-2026-09-20/` contains raw binary traces, decoded
events, trial logs, compiled test binary, per-region CSV, per-trial summaries,
unfinished-task counts, the local summary script, source/artifact hash manifests,
recorder race-test output, disabled-wrapper benchmark and full verification log.

- Native workspace tests pass with `APHELIONDMM_GL_TEST=1`.
- Recorder race tests pass, exercising timed flush, idempotent close, refusal to
  overwrite and refusal to stop another recorder.
- `task verify` passes with native graphics enabled: lint, contracts, Go tests,
  Rust checks, parser build and Windows editor build. Rust has zero unit tests;
  the inherited ImGui C++ warning remains. Unconfigured external gates may skip.
- An initial full-suite failure exposed the fixture destroying GL contexts while
  the process-wide brush cache retained their handles. Workspace tests now share
  one native context, matching the existing canvas test fixture, and detach it
  between serial tests. No renderer assertions were weakened.
- A pretrial command-line quoting error exited before running the workload;
  its log is retained separately. It is excluded from the five measurements.

Representative fixture approval, actual frame-queue/next-visible attribution,
parser transfer separation, cold/warm matrix, GPU timers and lifecycle campaign
remain open. No protected infrastructure or dependency manifest was changed.
The unrelated staged server inventory remains untouched.