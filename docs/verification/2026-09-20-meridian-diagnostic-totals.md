# Meridian diagnostic pagination evidence

Baseline: `a52975a3`. The integration adapter copied `dm_check_errors.count` into
the verification diagnostic total. Current Meridian-MCP uses that field for the
returned page length. Its `total_count` and `summary.by_severity` describe all
matching diagnostics, even when only the first page is returned. The previous
staging evidence therefore reported 50 instead of the complete total.

The adapter now uses `total_count` when supplied and preserves the returned page
count, truncation indication and severity totals. Legacy unpaginated responses
retain their `count` behavior. A truncated response without a total, a page count
that disagrees with its entries, a total below the page count, or inconsistent
summary/severity totals is rejected rather than presented as complete evidence.
Severity summation checks bounds before addition. No extra pages or unbounded
diagnostic payloads are fetched.

Successful verifier JSON and the real staging fixture now expose:

```json
{
  "diagnostics": 1045,
  "diagnostics_returned": 50,
  "diagnostics_truncated": true,
  "diagnostic_severity_counts": {"error": 127, "hint": 916, "warning": 2}
}
```

The existing authoritative build decision remains separate from MCP diagnostic
counts; this change corrects reporting and validates response consistency.

## Evidence

Process-fixture tests reproduced the incorrect page count and acceptance of
missing/inconsistent totals before the change. They now pass, including the
existing legacy response case. A verifier test checks propagation into successful
stage evidence. Affected package/CLI race checks and `task verify` pass with
`APHELIONDMM_GL_TEST=1`; Rust has zero unit tests and the inherited ImGui compiler
warning remains. Unconfigured external gates may skip.

The real staging-only gate passed after the change in 16.582 seconds and emitted
the counts above. It used the same map, manifest, MCP binary and Meridian revision
recorded in [the earlier staging report](2026-09-20-meridian-staging-gate.md).
The source checkout remained dirty. Independent cached MCP summary inspection
also reported 1,045 diagnostics: 127 errors, 2 warnings and 916 hints.

Failure, focused, maintained-gate and real-stage logs, plus raw parse/diagnostic
triage responses and corrected staging evidence, remain in ignored
`.artifacts/meridian-diagnostic-totals-2026-09-20/`. The real gate does not establish
that the reported DreamChecker errors are resolved or that a game build passes.

## Full-stack prerequisites still unresolved

The known GitHub workspace and saved project inventory did not contain an
`aphelion-content-tools` checkout. At Meridian-Rift HEAD
`aa32fadb56c62957b51f2cfe510d5f99a7ab5548`, `RIFT_BUILD.cmd`, `RIFT.cmd` and
`tools/rift/rift.ts` are untracked working files, absent from the committed tree.
The approved wrapper creates a clean detached checkout, so those entry points
would not be present there. No clean acceptance checkout was created and no
unrelated Meridian work was committed or copied into one to bypass that boundary.
The Content Tools root and committed authoritative build-tool revision have been
requested from the user. No protected infrastructure or dependencies changed.
