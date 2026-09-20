# Search Z-level filtering

Baseline: `fc4766b3`. Search now supports an explicit inclusive Z range alongside
the existing X/Y bounds. Open the filter controls, clear **All Z levels** to
start at the active map level, then adjust **Z range (first, last)**. Default and
reset behavior select all levels. Grab continues to set X/Y bounds only; changing
the viewed map level does not retarget an explicitly selected Z range.

The filter preserves the existing grouped result order. Result navigation and
row/bulk actions use the same filtered list. Changing filter membership resets
navigation indices and invalidates an in-progress clipped table traversal.

Same-map refresh, including refresh after a Search mutation, retains X/Y and Z
filters. Previously `Sync` issued a new query and reset filters after each action;
that could make a repeated filtered bulk action operate on other results. `Sync`
now uses the existing map-version refresh path. Explicit new searches, map
switches, Reset, and disabling filters retain their clearing behavior.

A shrink never silently clamps the chosen Z range to another level. A wholly
out-of-map range yields zero results and the UI explains why. The user can adjust
it explicitly; undoing a shrink can make the original levels available again.
Ranges partly overlapping the map naturally include only existing results.

## Evidence

Ignored `.artifacts/search-levels-2026-09-20/` retains the initial missing-feature
failure, focused tests, Search race output and full verification log.

- A three-level query fixture checks Z-only filtering, combined X/Y/Z membership,
  ordering, navigation reset, same-map refresh, shrink and map-switch ownership.
- Real Search Delete All and Replace All on a three-level authoritative fixture
  each submit one revision affecting only the selected level. Other levels remain
  byte-for-byte equivalent in the model. A repeated action on the empty result
  set changes no revision; undo restores the exact original canonical hash and
  refreshes the filtered results. Disabling the filter restores all levels.
- Mouse input through the actual ImGui checkbox and two-component slider selects
  the active level, expands the range and restores all-level results. These are
  ImGui frame tests, not human desktop acceptance.
- The complete Search package and its race run pass. `task verify` passes with
  `APHELIONDMM_GL_TEST=1`: lint/contracts, Go tests including existing native
  Search/navigation cases, Rust checks, parser build and Windows editor build.
  Rust contains zero unit tests; the inherited ImGui C++ warning remains.
  Unconfigured external-service gates may skip.

No protocol, persistence, dependency or protected infrastructure changes were
made. Performance and full interactive keyboard/mouse acceptance were not
measured in this pass. The unrelated staged server-inventory document remains
untouched.