# Delayed selection chains with an open preview

Baseline: `3b68d2c0`. The native workspace regression combines a 3x2 rectangular
rotation, a subsequent nudge submitted before the first outcome, and a newer
open move preview. It uses the actual Grab transform methods, editor preview
adapter, network executor, authoritative engine and command history. Transport
outcomes are controlled in memory; it does not test socket timing or physical
mouse events.

Three cases now pass: both transforms accepted, rotation accepted with the
nudge rejected, and both transforms rejected. For each outcome arriving while
the later preview is open, the display remains exactly equal to that preview
and Save stays blocked. After cancellation, display tiles equal acknowledged
state and the selection has the correct surviving transform bounds. Rejected
operations retain conflict entries and do not become undo commands.

Accepted history is then undone and redone through the real network operation
path. Each step checks canonical authoritative hash, complete display tiles
(including instance ordering and stable IDs), and selection bounds. Full undo
restores the initial hash; redo restores the accepted final hash. The untouched
second Z level is included in these whole-map comparisons.

The focused native cases and their race run pass. `task verify` passes with
`APHELIONDMM_GL_TEST=1`, including existing native workspace tests, lint/contracts,
Go tests, pinned Rust checks, parser and Windows editor builds. Rust contains zero
unit tests; the inherited ImGui compiler warning remains. Unconfigured external
gates may skip. Logs remain in ignored `.artifacts/selection-chain-2026-09-20/`.

These cases required no production change. Physical mouse routing, larger
hidden-type selections, broader multi-tab/temporary-tool sequences, and human
acceptance remain open. This evidence does not claim the entire lifecycle plan
is complete. No dependencies or protected infrastructure changed; the unrelated
staged server-inventory document remains untouched.