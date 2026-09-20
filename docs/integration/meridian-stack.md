# Meridian toolset acceptance

The [September 20 real MCP/staging gate](../verification/2026-09-20-meridian-staging-gate.md)
records a successful staged-map inspection, a dirty source checkout and 50 reported
diagnostics. It is not full stack or authoritative game-build acceptance.

AphelionDMM owns the integration coordinator. Meridian-MCP remains a read-only diagnostic sidecar,
aphelion-content-tools reaches collaboration only through its backend adapter, and Meridian-Rift keeps
authority over DreamMaker compilation and runtime acceptance.

## Trust boundary

Collaboration messages contain logical repository, DME, and map-target identifiers. Trusted local
configuration resolves those identifiers to canonical paths. The integration API does not accept shell
commands, executable paths, arbitrary MCP methods, or repository credentials from a collaboration client.

A map candidate is bound to a versioned manifest containing repository revision, environment hash, input
and output map hashes, accepted collaboration revision, and producer version. Staging rejects identity,
revision, containment, protocol, and hash mismatches before writing an immutable hash-named directory.
Publishing uses a temporary sibling directory and rename; it never replaces a Meridian-Rift source file.

## Local acceptance wrapper

The shipped composition entry point is `go run ./cmd/apheliondmm-meridian-verify`. It strictly loads a trusted manifest, publishes the immutable artifact, configures MCP inspection against that artifact, invokes one fixed PowerShell acceptance script, and emits a single JSON result with manifest hash, artifact hash, verifier version, and exit classification.

The protected wrapper is `scripts/integration/verify-aphelion-stack.ps1`. It is Aphelion-owned orchestration
and delegates to repository-owned gates; it does not replace `task build`, Content Tools' launcher or CI,
Meridian-MCP's pinned Cargo workflow, or Meridian-Rift's `RIFT_BUILD.cmd`.

Run it from PowerShell with explicit local roots:

```powershell
& .\scripts\integration\verify-aphelion-stack.ps1 `
	-AphelionRoot 'C:\Repositories\AphelionDMM' `
	-MeridianMcpRoot 'C:\Repositories\meridian-mcp' `
	-ContentToolsRoot 'C:\Repositories\aphelion-content-tools' `
	-MeridianRiftRoot 'C:\Repositories\Meridian-Rift' `
	-InstalledMcp 'C:\Tools\meridian-mcp\meridian-mcp.exe'
```

Network use is disabled by default for the Meridian build. `-AllowNetwork` is an explicit operator choice.
Every child gate has a timeout, Windows process-tree cleanup, bounded retained logs, and its own evidence
record. A failed, timed-out, skipped, or unavailable gate makes `stack_accepted` false.

The staged candidate is first inspected by the installed MCP after `dm_parse_environment`. Authoritative
game acceptance occurs only in a detached clean Git worktree at the recorded revision. The wrapper copies
the candidate into that disposable checkout, invokes `RIFT_BUILD.cmd`, then removes the worktree. It never
applies the candidate to the maintainer's existing Meridian-Rift working tree.

`-SkipLauncher` is intended only for focused iteration. It records the launcher gate as unavailable and
therefore cannot produce full stack acceptance. `-PlanOnly` is the CI-safe mode: it records which sibling
repositories and installed tools are unavailable in a single-repository runner without downloading or
pretending to run them.

## Evidence interpretation

Evidence is written atomically to `.artifacts/stack/evidence.json` unless `-EvidencePath` is provided.
The JSON keeps every repository gate separate. MCP diagnostics completing successfully does not mean the
diagnostic count is zero, and a successful stage does not mean the Meridian build passed. Only
`stack_accepted: true` means every gate in that invocation passed.

Retain the stage manifest, map hashes, MCP evidence, per-gate logs, Git revision, Task version/build output,
and Meridian build markers when handing work to a maintainer. Authentication, pushes, pull requests, and
complex merges remain GitHub Desktop operations.
