# Thirty-minute native map-content endurance result

The [native workspace lifetime probe](2026-09-20-workspace-lifecycle.md) completed
successfully using the frozen test binary built at `85fc01bf`. After four warmup
cycles, it completed **17,923 measured cycles in 1,800.0417 seconds**, with zero
test failures or timeout. The process exited zero after 1,801.6445 seconds total.

Each cycle parsed/opened the map, nudged a selection, performed undo/redo/undo,
saved and reparsed it, then disposed the map content and command stack through
the native deferred queue. Every cycle checked restored authority, saved content
under inherited Save ordering, collection of the closed editor after GC, removal
of its canvas texture, and absence of pending jobs, callbacks and undo/redo history.
The workload was paced at at most ten cycles per second; this is not a throughput
or input-latency benchmark.

## Resource observations

There are 31 post-GC Go checkpoints: after warmup, approximately every minute, and
at successful completion. Byte counts are exact:

| Metric | Initial checkpoint | Final checkpoint | Observed range |
| --- | ---: | ---: | ---: |
| Go heap allocation, bytes | 2,672,688 | 2,691,384 | 2,672,688–2,691,576 |
| Go heap objects | 69,993 | 70,080 | 69,993–70,083 |
| Go heap arena reservation, bytes | 8,093,696 | 12,288,000 | 8,093,696–12,320,768 |
| Goroutines | 2 | 2 | 2 |

Final live heap was 18,696 bytes above the initial post-warmup checkpoint. This
small increase does not track the number of closed workspaces; the per-cycle weak
pointer checks independently confirm release of each closed editor. Arena growth
is reported separately rather than presented as live-object retention.

An external Windows process sampler recorded 60 observations approximately 30
seconds apart. These include startup and are **not synchronized with GC**. The
last observation was at 1,772.288 seconds, before process completion:

| Metric | First sample | Last sample | All-sample range |
| --- | ---: | ---: | ---: |
| Private bytes | 146,341,888 | 136,024,064 | 134,832,128–146,341,888 |
| Working set, bytes | 123,338,752 | 137,674,752 | 123,338,752–137,994,240 |
| Process handles | 430 | 458 | 430–458 |
| OS threads | 32 | 23 | 22–32 |

For the 58 samples from 61.066 seconds onward, private bytes ranged from
135,233,536 to 136,450,048 and handles from 451 to 458. These ranges include
in-progress parse/save cycles; they are not post-close native-memory checkpoints.
Other host work, including builds and the isolated PostgreSQL gate, ran during
this observation period. No driver memory measurement was collected.

## Provenance and acceptance boundary

- Fixture: synthetic 4x4x1 map, 48 instances; SHA-256
  `6470d3b87f42e6cdda07aab90441796b9bfb2c9e0a8aa7a09afc852795059697`.
- Binary SHA-256:
  `6c006d9149e33274556ea2be11d550e88bd7d86cc3daa50c7f8921e7ef6616fc`.
- OpenGL: `3.3.0 Core Profile Context 25.10.02.250325`, hidden native window.
- UTC process interval: 2026-09-20 16:22:36 through 16:52:37.
- Raw manifest, binary, build/stdout/stderr logs, Go JSONL checkpoints, Windows
  JSONL samples and completion record remain in ignored
  `.artifacts/workspace-lifecycle-2026-09-20/endurance-1/`.

This is one long synthetic map-content run, not the full-editor resource campaign.
It does not cover representative maps, outer workspace list/close-dialog behavior,
environment reload, resize, network reconnect, crash recovery, long retained
history, all GPU object counts, driver allocations or human desktop interaction.
The later workspace-area and resize-disposal fixes were not in this binary and
were not exercised by this workload. Five-trial representative qualification and
the broader lifecycle acceptance gate remain open.
