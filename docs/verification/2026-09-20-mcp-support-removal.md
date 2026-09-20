# MCP support removal

The September 20 user decision removes Meridian-MCP support from AphelionDMM.
The change removes `cmd/apheliondmm-meridian-verify`, the entire owned
Meridian adapter/staging/coordinator package and the integration manifest package,
including their fake-process and external-process tests. Active design, ownership,
verification guidance and plans now use native map fidelity checks. Historical
MCP failures are superseded, not remaining acceptance blockers.

The generic durable export-checkpoint protocol remains intact. Historical verifier
strings in checkpoint fixtures still exercise persistence compatibility; they do
not provide an MCP integration. The native parser, DME environment hash, local and
network operation engines, atomic saves and user-selected IceBox fixture remain.
No sibling checkout, installed tool or dependency pin changed.

Validation at source baseline `c1c0f2f2` plus the removal:

- `go test ./... -count=1` passed with pinned Go 1.25.13 and native GL checks
  enabled via `APHELIONDMM_GL_TEST=1`.
- Repository lint passed with zero issues.
- After applying the approved protected changes, `task verify-contracts` passed
  protocol/OpenAPI checks and build-check tests, including CI YAML parsing,
  retained release prerequisites and immutable action revision checks.
- The exact protected patch passed `git apply --check` before application;
  diff whitespace checks pass.

Logs are retained in ignored `.artifacts/mcp-removal-2026-09-20/`, with draft
workflow/Task validation in the task's `work/mcp-removal-review/` directory.
The inherited ImGui compiler warning remains. External fixtures and database
gates without their opt-in configuration do not become passes. Rust/build and
hosted CI were not rerun for this removal.

The user explicitly approved updates to the three listed protected files on
September 20, and the prepared patch was applied. `Taskfile.yml` retains the
protocol and build-check commands without the deleted manifest package.
`.github/workflows/ci.yml` removes the obsolete toolset integration job and its
release dependency while retaining the other quality and security gates.
`scripts/integration/verify-aphelion-stack.ps1` is deleted with its obsolete
external-stack command. Local Go/Rust/build checks remain available through Task.
