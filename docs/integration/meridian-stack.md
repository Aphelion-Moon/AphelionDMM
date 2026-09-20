# Meridian-MCP map inspection

AphelionDMM supports immutable map staging and a bounded Meridian-MCP diagnostic
sidecar. Per the September 20 user decision, Aphelion Content Tools integration
and Rift build tooling are removed from scope. Their checkouts, launchers,
dependencies and build results are not acceptance prerequisites.

## Trust boundary

Trusted local configuration maps repository, DME and map-target identifiers to
canonical paths. Collaboration clients cannot provide shell commands, executable
paths, arbitrary MCP methods or repository credentials.

Each map candidate is bound to a versioned manifest containing repository
revision, environment hash, input/output map hashes, accepted collaboration
revision and producer version. Staging rejects identity, revision, containment,
protocol and hash mismatches before publishing an immutable hash-named directory.
It never replaces the source map.

## Shipped verifier

`go run ./cmd/apheliondmm-meridian-verify` loads the manifest, stages the candidate,
then calls MCP parsing, map inspection and diagnostics against that artifact.
Supply `--repository-root`, `--repository-identity`, `--dme`, `--map-target-id`,
`--map-target`, `--stage-root`, `--manifest`, `--candidate` and `--mcp-executable`.
`--mcp-arg` supplies a fixed local MCP argument; `--allow-dirty` explicitly permits
staging from a dirty source checkout.

The old `--acceptance-script`, `--acceptance-executable` and `--acceptance-root`
options are rejected. There is no repository build runner or build-result field.
The JSON evidence contract is verifier version `2`; successful inspection returns
`exit_classification: inspected`. Version 1's `accepted` classification included
an external build gate and is not equivalent. The artifact manifest itself
remains schema version 1.

Diagnostics retain the complete `diagnostics` total, `diagnostics_returned`,
`diagnostics_truncated` and optional `diagnostic_severity_counts`. Successful
inspection means the requests completed against a consistent MCP generation;
it does not mean zero diagnostic errors, game compilation or runtime acceptance.

## Local orchestration

`scripts/integration/verify-aphelion-stack.ps1` retains AphelionDMM checks/builds,
Meridian-MCP's pinned Cargo tests and shipped staged-map inspection. It uses the
Meridian-Rift checkout only as a read-only DME/map fixture; it does not load or run
Rift build tooling. Its fixture remains `tgstation.dme` and
`_maps/virtual_domains/test_only.dmm`.

```powershell
& .\scripts\integration\verify-aphelion-stack.ps1 `
    -AphelionRoot 'C:\Repositories\AphelionDMM' `
    -MeridianMcpRoot 'C:\Repositories\meridian-mcp' `
    -MeridianRiftRoot 'C:\Repositories\Meridian-Rift' `
    -InstalledMcp 'C:\Tools\meridian-mcp\meridian-mcp.exe'
```

`-ContentToolsRoot`, `-SkipLauncher` and the Rift-specific `-AllowNetwork` option
are removed. Child gates retain timeouts, process-tree cleanup and separate logs.
`-PlanOnly` records availability without running gates or installing dependencies.
Wrapper evidence schema version 2 uses `integration_verified`, replacing
`stack_accepted`; it is true only when every retained gate in that invocation
passes. Planning and unavailable gates cannot produce a verification pass.

The [September 20 MCP/staging run](../verification/2026-09-20-meridian-staging-gate.md)
and [diagnostic pagination correction](../verification/2026-09-20-meridian-diagnostic-totals.md)
remain historical evidence. Their outstanding Content Tools and Rift-build
requirements are superseded by this scope decision. Representative exported-map
inspection and human editor acceptance remain separate work.
