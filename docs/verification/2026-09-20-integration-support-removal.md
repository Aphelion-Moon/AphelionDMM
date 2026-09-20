# Content Tools and Rift build support removed

The September 20 user decision removes Aphelion Content Tools integration and
Rift build tooling from AphelionDMM. The user separately approved the prepared
change to protected `scripts/integration/verify-aphelion-stack.ps1`.

The wrapper no longer accepts Content Tools or Rift-build switches, runs Content
Tools checks/launcher, stages Rift dependencies, creates acceptance worktrees,
copies candidates into a game checkout, or invokes `RIFT_BUILD.cmd`. It retains
AphelionDMM checks/builds, Meridian-MCP tests and staged-map inspection. The Rift
checkout remains a read-only map/environment fixture, without a build dependency.

The Go verifier no longer contains the PowerShell acceptance runner or accepts
its three CLI options. Its evidence contract is version 2: success means
`inspected`, and build-entry-point/exit-code fields are absent. Wrapper evidence
is schema version 2 with `integration_verified`, replacing `stack_accepted`.
Manifest schema version 1 and collaboration protocol semantics are unchanged.
Active design and acceptance requirements reflect the removal; dated integration
plans explicitly mark the old requirements as superseded.

Validation:

- Focused integration/CLI tests passed. The race run passed manifest, Meridian
  and CLI packages in 1.272 s, 3.337 s and 1.529 s respectively.
- Focused lint passed with zero issues. Tests reject removed CLI options and
  verify version 2 inspection evidence without build-result fields.
- The approved wrapper parses. Its installed `-PlanOnly` mode completes with
  three retained entries and `integration_verified: false`; unavailable gates
  do not become passes.
- The produced CLI completed a real installed Meridian-MCP inspection in
  11.513 s, exit 0. It staged the existing 5-by-5-by-1 test map from repository
  revision `aa32fadb56c62957b51f2cfe510d5f99a7ab5548`, using `--allow-dirty`, and
  returned verifier version 2 / `inspected`. The source map hash was unchanged.
- Diagnostic evidence retained all 1,045 diagnostics (127 errors, 2 warnings,
  916 hints), with 50 returned entries and truncation recorded. This proves
  inspection and accurate reporting, not zero-error or game-runtime acceptance.

The CLI binary SHA256 is
`19e40bed27448d2c12dffbaf1b549548063705d283ab9749ba27c658a574ca1a`;
the staged map SHA256 is
`863c445d7731e06392803ec57485cd254a464780176670365d7b489652a2d085`.
Raw results are retained in ignored `.artifacts/integration-removal-2026-09-20/`.
Full desktop builds, all wrapper gates and hosted CI were not rerun. No sibling
repository, dependency installation, deployment or collaboration endpoint changed.
