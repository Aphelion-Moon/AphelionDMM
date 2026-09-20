# IceBox native frames and deployment handoff

At source baseline `c2712a39`, the existing hidden native frame harness now
accepts explicitly configured external DME/map fixtures. Normal synthetic tests
retain their original fixture. The representative path copies the map into a
temporary workspace, parses the actual DME, loads icons from its repository,
and assigns reproducible test identities before constructing the real map pane.
Source DME and map hashes are checked again at cleanup.

The approved Meridian-Rift IceBox fixture passed one native run in 16.71 seconds:
255x255x3, 195,075 cells, selection at (12,43,1). Following one warmup cycle,
five measured cycles each perform Alt+Right, undo, redo and undo through the
shortcut dispatcher, command history and production frame loop. Forty measured
frames complete all deferred bucket jobs. Every cycle restores the exact
canonical map hash and selection; readback verifies changed pixels after the
nudge and exact original pixels after the final undo. No OpenGL errors occurred.
Cleanup disposed the workspace and 150 cached icon files.

- Map SHA-256: `f9e3a4ac79e06e22910adca4d6828528e55bd59d5d3ddd4c3b409d6cb76c1d8c`.
- DME SHA-256: `0a0f65cc07db67423a4a63021ee026ca98baa256b787548abfaac0a325c296e2`.
- Initial/final map hash: `880265c3ba2648f585ee46856bcc1928fce3bcaac2e75b68351bd4c3962e1fec`.
- Initial/final canvas pixel hash: `ec0a85da3bad7be5031e40d9204b625359b8391a950d9f1031f7b544c329e9e7`.
- Test binary SHA-256: `6b7c6499dd98a9da8f19afedacac9a21451eb7a42e3ac5f12012a4e88e96a941`.

Workspace setup took 4,649.379 ms, including native DME parse, model and pane
construction; this excludes full application startup. Post-GC Go heap was
470,532,352 bytes before tracing and 489,438,328 afterward. The workload allocated
10,791,738,336 bytes, including repeated verification snapshots/hashes, retained
undo history and tracing. The 11,001,292-byte trace remains live at the final
checkpoint. These numbers do not isolate production gesture allocation or prove
a leak. This is one fresh process, not five independent trials or a performance
improvement. The hidden 640x480 window bypasses OS keyboard delivery and frame
pacing; GPU completion is explicit, but physical presentation is unmeasured.

The synthetic native frame test also passed before this run. Initial compilation
errors in the test extension were corrected, and a first executable invocation
exited on PowerShell argument parsing before running a test. Those logs and the
successful run/trace remain under ignored `.artifacts/icebox-native-2026-09-20/`
and `.artifacts/icebox-native-*.log`. The inherited ImGui compiler warning remains.
No production code, dependency or protected infrastructure changed.

During this work the user explicitly made this the final allowed test and
directed a move to deployment and local configuration. No further repetitions,
lint, suites or performance campaigns were run. Remaining desktop, resource and
human acceptance evidence stays incomplete; it is not silently marked passed.
