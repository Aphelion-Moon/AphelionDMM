# Aphelion Toolset Integration Implementation Plan

> **Scope update (2026-09-20):** The user removed Aphelion Content Tools support and Rift build tooling. Historical implementation and acceptance steps for those integrations below are superseded, not remaining work. Meridian-MCP parsing, map inspection and diagnostics remain supported. See [current integration scope](../../integration/meridian-stack.md).

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate AphelionDMM with Meridian-MCP, aphelion-content-tools, and Meridian-Rift through versioned, contained contracts and staged acceptance without turning collaboration into an arbitrary process or filesystem gateway.

**Architecture:** AphelionDMM owns the collaboration and integration coordinator; Meridian-MCP remains a bounded diagnostic sidecar; Content Tools uses a backend adapter plus the public OpenAPI/AsyncAPI contracts; Meridian-Rift receives only staged, hashed map artifacts and retains build/runtime authority.

**Tech Stack:** Go 1.24, MCP JSON-RPC over stdio, OpenAPI/AsyncAPI, Python/FastAPI, TypeScript/Solid frontend, PowerShell, BYOND DreamMaker.

**Spec:** `docs/superpowers/specs/2026-08-24-multiplayer-design.md`

> **Status reconciliation (2026-08-25):** AphelionDMM and Meridian integration contracts are implemented. All Aphelion Content Tools work is frozen and excluded by explicit direction; its historical checked items are not being revalidated in this pass. See `2026-08-25-multiplayer-human-test-readiness.md`. Changes remain uncommitted by repository policy.

## Global Constraints

- Before changing another repository, read its root `AGENTS.md` and routed guidance in that repository.
- Use Meridian-MCP after `dm_parse_environment` for DreamMaker inspection; use PowerShell for Windows builds/tests.
- Never pass arbitrary paths, commands, executable locations, credentials, or MCP method names from collaboration clients.
- Content Tools browser code receives no service, repository, MCP, or build credentials. User-facing Content Tools activity follows the existing Parsec feedback policy while durable diagnostics remain accessible.
- Do not change Meridian-Rift human build/bootstrap/CI/release files or Content Tools launch/build entry points without exact-file approval.
- Do not commit or push in any repository without explicit authorization.

---

### Task 1: Define the cross-repository compatibility manifest

**Files:**
- Create: `api/integration/aphelion-manifest.schema.json`
- Create: `internal/aphelion/integration/manifest/manifest.go`
- Create: `internal/aphelion/integration/manifest/manifest_test.go`
- Create: `internal/aphelion/integration/manifest/testdata/valid.json`
- Create: `internal/aphelion/integration/manifest/testdata/incompatible.json`

- [x] Write strict schema/Go decode tests for repository identity, repository revision, DME path identifier, map target identifier, protocol version, environment hash, input/output map hash, accepted revision, content manifest hash, and producing tool versions.

```go
type Manifest struct {
	SchemaVersion       uint16 `json:"schema_version"`
	RepositoryIdentity string `json:"repository_identity"`
	RepositoryRevision string `json:"repository_revision"`
	DMEIdentifier      string `json:"dme_identifier"`
	MapTargetID        string `json:"map_target_id"`
	ProtocolVersion    uint16 `json:"protocol_version"`
	EnvironmentSHA256  string `json:"environment_sha256"`
	InputMapSHA256     string `json:"input_map_sha256"`
	OutputMapSHA256    string `json:"output_map_sha256"`
	AcceptedRevision   uint64 `json:"accepted_revision"`
	ContentSHA256      string `json:"content_manifest_sha256,omitempty"`
	Producer           Tool   `json:"producer"`
}
```

- [x] Run `go test ./internal/aphelion/integration/manifest -count=1` and confirm failure.
- [x] Implement strict decoding, lowercase SHA-256 validation, supported schema/protocol checks, and canonical JSON hash generation.
- [x] Keep identifiers logical; resolve them to trusted local paths only in a local adapter.
- [x] Validate fixtures against both JSON Schema and Go decoding.
- [ ] If authorized, commit with `feat(integration): define Aphelion compatibility manifest`.

### Task 2: Add a bounded Meridian-MCP client adapter

**Files:**
- Create: `internal/aphelion/integration/meridian/client.go`
- Create: `internal/aphelion/integration/meridian/mcp.go`
- Create: `internal/aphelion/integration/meridian/mcp_test.go`
- Create: `internal/aphelion/integration/meridian/testdata/fake_mcp.ps1`

- [x] Write process-protocol tests for initialize/capability negotiation, mandatory `dm_parse_environment` ordering, map inspection, diagnostics, timeout, oversized response, process exit, malformed JSON-RPC, and secret redaction.

```go
type Client interface {
	ParseEnvironment(ctx context.Context, repositoryID, dmeID string) (EnvironmentResult, error)
	InspectMap(ctx context.Context, repositoryID, mapTargetID string) (MapResult, error)
	CheckErrors(ctx context.Context, repositoryID string) (DiagnosticResult, error)
	Close(ctx context.Context) error
}

type Config struct {
	Executable string
	Roots      map[string]Repository
	Timeout    time.Duration
	MaxBytes   int64
}
```

- [x] Run `go test ./internal/aphelion/integration/meridian -run TestMCP -count=1` and confirm failure.
- [x] Implement a fixed stdio MCP client initialized from trusted local configuration. Do not invoke a shell; the PowerShell fixture is test-only and launched by the test harness.
- [x] Allow only the required fixed capability set. Do not expose a generic `CallTool` method outside the adapter package.
- [x] Require successful parse before map inspection/diagnostics and record MCP version/state generation.
- [x] Bound stdout/stderr capture and terminate the complete child process tree on timeout/cancellation.
- [x] Run focused tests and the installed Meridian-MCP real entry-point contract test.
- [ ] If authorized, commit with `feat(integration): add bounded Meridian-MCP diagnostics adapter`.

### Task 3: Declare AphelionDMM compatibility in Meridian-MCP

**Repository:** `C:\Repositories\meridian-mcp`

**Files:**
- Create: `tests/compatibility/aphelion-dmm.json`
- Modify: `tests/compatibility_manifest.rs`
- Modify: `tests/workflow_contract.rs`
- Modify: `README.md`

- [x] Read Meridian-MCP `AGENTS.md`, `rust-toolchain.toml`, and testing guidance. (No `AGENTS.md` exists in this checkout; repository trust/testing documents were read.)
- [x] Add failing Rust tests that load an AphelionDMM compatibility fixture and require parse-before-map/diagnostic workflow semantics.
- [x] Run the exact pinned Meridian-MCP test command and confirm the new tests fail for the absent fixture/contract.
- [x] Add an `aphelion-dmm.json` compatibility entry naming only currently supported tools, request/response limits, repository identity expectations, and required result metadata.
- [x] Update documentation without adding a universal bypass, hidden restriction, remote-identity obfuscation, or generic process access.
- [x] Run pinned fmt, Clippy, tests, MCP conformance, and the exact installed binary smoke path.
- [ ] If authorized, commit in Meridian-MCP with `docs(compat): declare AphelionDMM workflow contract`.

### Task 4: Add Content Tools backend collaboration adapter

**Repository:** `C:\Repositories\aphelion-content-tools`

**Files:**
- Create: `webapp/api/routes/collaboration.py`
- Modify: `webapp/api/models.py`
- Modify: `webapp/api/app.py`
- Create: `webapp/tests/test_collaboration_api.py`
- Modify: `webapp/dump_openapi.py`

- [x] Read Content Tools `AGENTS.md` and the backend, export-safety, verification, and Meridian integration references.
- [x] Write FastAPI tests for version query, session metadata, scoped join-token creation, checkpoint request, unavailable service, protocol incompatibility, path rejection, and credential redaction.
- [x] Run the focused pytest file and confirm failure.
- [x] Implement a backend-only HTTP client with configured base URL, strict timeouts/size limits, and model validation. Do not add a generic proxy endpoint.
- [x] Expose only the specific collaboration lifecycle routes required by Content Tools and return bounded stable errors.
- [x] Preserve existing staged export rules and never write collaboration data into the canonical lore store.
- [x] Regenerate/check the Content Tools OpenAPI document using its real entry point.
- [x] Run focused tests, complete backend tests, and the shipped launcher smoke path.
- [ ] If authorized, commit in Content Tools with `feat(api): add bounded AphelionDMM collaboration adapter`.

### Task 5: Add Content Tools collaboration status UI

**Repository:** `C:\Repositories\aphelion-content-tools`

**Files:**
- Create: `webapp/frontend/src/features/collaboration/api.ts`
- Create: `webapp/frontend/src/features/collaboration/state.ts`
- Create: `webapp/frontend/src/features/collaboration/CollaborationStatus.tsx`
- Create: `webapp/frontend/src/features/collaboration/CollaborationStatus.test.tsx`
- Modify: `webapp/frontend/src/App.tsx`

- [x] Write frontend tests for unavailable, incompatible, ready, joining, connected, checkpoint, and error states; assert tokens never enter URL/history/local storage.
- [x] Run the focused frontend test and confirm failure.
- [x] Implement typed calls only to the Content Tools backend adapter.
- [x] Route user-facing progress and results through the existing Parsec feedback mechanism while retaining exact diagnostics in inline accessible status/error regions.
- [x] Provide connection/checkpoint controls only where the backend reports compatible capabilities.
- [x] Do not add or alter Parsec or other creative assets.
- [x] Run focused frontend tests, the full frontend gate, and a browser playtest through the shipped launcher.
- [ ] If authorized, commit in Content Tools with `feat(ui): expose AphelionDMM collaboration status`.

### Task 6: Implement staged Meridian-Rift map acceptance

**Files in AphelionDMM:**
- Create: `internal/aphelion/integration/meridian/repository.go`
- Create: `internal/aphelion/integration/meridian/stage.go`
- Create: `internal/aphelion/integration/meridian/stage_test.go`
- Create: `internal/aphelion/integration/meridian/verify.go`
- Create: `internal/aphelion/integration/meridian/verify_test.go`

- [x] Write tests for repository identity mismatch, dirty/unexpected revision policy, target containment, changed input hash, invalid stage manifest, atomic stage creation, MCP failure, PowerShell timeout, build failure, and successful evidence record.

```go
type Repository struct {
	Identity string
	Root     string
	DME      string
	Targets  map[string]string
}

type Verifier interface {
	Verify(ctx context.Context, manifest manifest.Manifest, stagedFile string) (Evidence, error)
}
```

- [x] Run stage/verify tests and confirm failure.
- [x] Resolve repository and target IDs through immutable trusted configuration, canonicalize once, and enforce containment.
- [x] Stage the DMM in a separate configured directory, verify hashes, call Meridian-MCP parse/map/diagnostics, then invoke only the configured fixed PowerShell acceptance entry point.
- [x] Never invoke a shell command received from a client. Build arguments are fixed and repository-owned.
- [x] Require explicit user approval before applying a verified staged map into Meridian-Rift. Preserve unrelated dirty changes and hand off authentication/PR operations to GitHub Desktop.
- [x] Run tests with fake adapters, then a real staging-only integration against Meridian-Rift without applying the artifact.
- [ ] If authorized, commit with `feat(integration): stage and verify Meridian-Rift map output`.

### Task 7: Add cross-repository acceptance orchestration

**Files:**
- Create: `scripts/integration/verify-aphelion-stack.ps1`
- Create: `docs/integration/meridian-stack.md`
- Modify after approval: `.github/workflows/ci.yml`

- [x] Obtain explicit approval before creating a new authoritative integration wrapper or changing CI; explain that the wrapper delegates to existing repository-owned gates and does not replace them.
- [x] Implement a PowerShell orchestrator with explicit repository roots, no network by default, bounded timeouts, `$LASTEXITCODE` checks, process-tree cleanup, and JSON evidence output.
- [x] Run in order: AphelionDMM contract/tests/build, installed Meridian-MCP conformance, Content Tools backend/frontend/launcher gates, staged map creation, MCP parse/diagnostics, and Meridian-Rift's approved acceptance entry point.
- [x] Keep each repository's result separate in the evidence; do not report partial success as stack acceptance.
- [x] Add a CI job only where credentials and heavyweight BYOND/tool dependencies can be provided safely; keep unavailable integration gates explicit.
- [x] Exercise the real local entry points from a clean compatible test checkout and retain the manifest/hashes/log markers.
- [ ] If authorized, commit with `test(integration): verify the Aphelion mapping toolchain`.

## Phase acceptance

- [x] Cross-repository manifests reject identity, revision, protocol, environment, and hash mismatch.
- [x] Meridian-MCP is always parsed first and remains a bounded diagnostic sidecar.
- [x] Content Tools browser code contains no collaboration or repository credentials.
- [x] Content Tools feedback follows Parsec/accessibility policy without modifying creative assets.
- [x] Meridian-Rift receives only staged, contained DMM output and retains authoritative build acceptance.
- [x] The full real-entry-point evidence distinguishes every repository gate.
