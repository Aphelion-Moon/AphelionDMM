# AphelionDMM Audit Remediation Implementation Plan

> **Scope update (2026-09-20):** The user removed Aphelion Content Tools support and Rift build tooling. Historical implementation and acceptance steps for those integrations below are superseded, not remaining work. Meridian-MCP parsing, map inspection and diagnostics remain supported. See [current integration scope](../../integration/meridian-stack.md).

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. `superpowers:subagent-driven-development` may be used only after Zoe explicitly approves subagent use. Track progress with the checkboxes below.

**Goal:** Remove the verified correctness, security, contract, integration, and release gaps recorded in the 2026-08-30 code audit, then replace readiness claims with fresh end-to-end evidence.

**Architecture:** Keep the server authoritative and the operation engine shared by local and network collaboration. Reject invalid resource claims at the model boundary, reconcile durable state before acknowledging it, represent export checkpoints as immutable server-side records, and keep external repository paths and commands in trusted local integration configuration rather than the collaboration protocol. Preserve inherited StrongDMM code behind narrow, marked adapters.

**Tech stack:** Go 1.25, Rust 1.82 GNU, SQLite, PostgreSQL, HTTP/WebSocket, OpenAPI/AsyncAPI, PowerShell, Task, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-08-24-multiplayer-design.md`

**Audit:** `docs/verification/2026-08-30-code-audit.md`

## Execution constraints

- Use PowerShell for every local command on Windows.
- Preserve unrelated working-tree changes. Do not reset, checkout, merge, commit, or push.
- Add the narrow failing test before each behavioral implementation. Run that test before the broader package and repository gates.
- Use `apply_patch` for hand-authored file changes.
- Do not edit `.github/workflows/ci.yml`, `Taskfile.yml`, `Taskfile.windows.yml`, `scripts/deploy/*`, or another build/release/deployment entry point until Zoe confirms the exact file and effect described in Task 8.
- Do not dispatch subagents unless Zoe explicitly approves them. The default execution mode is one task at a time in the current task.
- Keep new Aphelion-owned implementation under `internal/aphelion/` and new commands under `cmd/apheliondmm-*`. Mark the smallest unavoidable inherited spans with `APHELION EDIT` comments.
- Do not add filesystem paths, process commands, tokens, or executable locations to the collaboration protocol.
- Do not update readiness documents until the corresponding gate has fresh evidence.

---

## Task 1: Reject hostile or impossible snapshot dimensions

**Files:**

- Create: `internal/aphelion/collab/model/limits.go`
- Modify: `internal/aphelion/collab/model/hash.go`
- Modify: `internal/aphelion/collab/model/hash_test.go`
- Modify: `internal/aphelion/collab/mapadapter/export.go`
- Modify: `internal/aphelion/collab/mapadapter/roundtrip_test.go`
- Modify: `internal/aphelion/collab/server/hosted_test.go`
- Modify: `internal/aphelion/collab/server/recovery_test.go`
- Modify: `api/collaboration/openapi.yaml`

- [x] **1.1 Add model-boundary regression tests.**

  Add table cases proving that `Snapshot.Validate` rejects a zero dimension, any dimension greater than `4096`, a tile coordinate outside the dimensions, and a dimension product greater than `16_777_216`. Include `math.MaxInt`, `2`, `1` to prove multiplication overflow is rejected before allocation.

  ```go
  func TestSnapshotValidateRejectsUnsafeDimensions(t *testing.T) {
  	t.Parallel()

  	tests := []struct {
  		name     string
  		snapshot model.Snapshot
  	}{
  		{name: "dimension limit", snapshot: snapshotWithDimensions(4097, 1, 1)},
  		{name: "cell limit", snapshot: snapshotWithDimensions(4096, 4096, 2)},
  		{name: "integer overflow", snapshot: snapshotWithDimensions(math.MaxInt, 2, 1)},
  	}
  	for _, test := range tests {
  		test := test
  		t.Run(test.name, func(t *testing.T) {
  			t.Parallel()
  			if err := test.snapshot.Validate(); err == nil {
  				t.Fatal("Validate() succeeded for an unsafe snapshot")
  			}
  		})
  	}
  }
  ```

  Run: `go test ./internal/aphelion/collab/model -run 'TestSnapshotValidateRejectsUnsafeDimensions' -count=1`

  Expected initial result: compile failure because `Snapshot.Validate` does not exist.

- [x] **1.2 Add projection and hosted-entry regression tests.**

  Add `TestApplySnapshotRejectsUnsafeDimensionsWithoutPanic` around the public map-adapter entry point and a hosted session-creation test that submits a compact JSON snapshot with the overflowing dimensions. The adapter must return an error without allocating; HTTP must return `400 Bad Request` without registering a session.

  Run: `go test ./internal/aphelion/collab/mapadapter ./internal/aphelion/collab/server -run 'UnsafeDimensions|OversizedSnapshot' -count=1`

  Expected initial result: the model accepts the snapshot or the map adapter panics. Keep the panic-catching assertion only in the regression test, not production code.

- [x] **1.3 Implement one checked validation path.**

  In `limits.go`, define `MaxMapDimension = 4096` and `MaxMapCells = 16_777_216`. Add `Snapshot.Validate() error` and an unexported checked cell-count helper that divides before multiplying, so it never computes an overflowing intermediate product. Validate dimensions, total cell count, duplicate coordinates, coordinate bounds, and existing canonical tile rules.

  Make `Snapshot.Hash`, document construction, recovery, import, and projection call this validator before sorting, allocating, or iterating. Replace every direct capacity expression based on `MaxX * MaxY * MaxZ` with the validated count returned by the helper or with incremental allocation that remains bounded.

- [x] **1.4 Publish and verify the same contract limits.**

  Add OpenAPI `maximum: 4096` to each dimension and `maxItems: 16777216` to the tile collection. Extend `TestContractsAreValidPinnedYAMLDocuments` to decode the snapshot schema and assert these exact values rather than searching strings.

  Run: `go test ./internal/aphelion/collab/model ./internal/aphelion/collab/mapadapter ./internal/aphelion/collab/server ./internal/aphelion/collab/protocol -count=1`

- [x] **1.5 Run the security regression under the race detector.**

  Run: `go test -race ./internal/aphelion/collab/model ./internal/aphelion/collab/mapadapter ./internal/aphelion/collab/server -count=1`

---

## Task 2: Reconcile ambiguous durable append outcomes

**Files:**

- Modify: `internal/aphelion/collab/server/document.go`
- Modify: `internal/aphelion/collab/server/document_test.go`
- Modify: `internal/aphelion/collab/server/recovery.go`
- Modify: `internal/aphelion/collab/server/recovery_test.go`
- Modify: `internal/aphelion/collab/store/conformance.go`
- Modify: `internal/aphelion/collab/store/memory_test.go`
- Modify: `internal/aphelion/collab/store/sqlite/fault_test.go`
- Modify: `internal/aphelion/collab/store/postgres/store_test.go`

- [x] **2.1 Reproduce a commit-then-error outcome.**

  Add a `committedThenFailedStore` test wrapper whose `Append` delegates to `SessionStore.Append`, then returns a sentinel error once after the delegated append succeeds. Submit one tile operation and assert that the owner returns the stored accepted operation, reports revision `1`, and accepts the next operation at revision `2`.

  Also test a retry where `LookupOperation` returns an exact duplicate at a revision ahead of the current document. The owner must rebuild from the store before returning the duplicate.

  Run: `go test ./internal/aphelion/collab/server -run 'TestSubmitReconcilesCommittedAppendError|TestSubmitReconcilesDuplicateAheadOfMemory' -count=1`

  Expected initial result: the first submit returns the sentinel error and the snapshot remains at revision `0`.

- [x] **2.2 Extract deterministic store replay.**

  In `recovery.go`, extract the existing `store.Load` plus verified replay loop into:

  ```go
  func loadStoredDocument(ctx context.Context, documentID model.DocumentID, store SessionStore, observability *collabtelemetry.Telemetry) (*engine.Document, error)
  ```

  Preserve the current `reflect.DeepEqual` check between replayed and stored accepted operations. Make `RecoverDocumentWithConfig` call this helper.

- [x] **2.3 Reconcile append failures before replying.**

  After any `Append` error, create `context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)`, look up the operation, and accept only an exact `reflect.DeepEqual` match. If found, reload and replay the document, require its revision/hash to match the stored accepted operation, and return the accepted operation as success. If absent, return the original append error. If lookup or replay cannot prove one state, return an error that joins the append and reconciliation failures; do not advance memory or broadcast.

  Apply the same reload rule when the pre-append duplicate lookup finds a stored exact operation whose revision is ahead of the in-memory document. Keep mismatched operation-ID reuse rejected.

- [x] **2.4 Add the failure mode to store conformance.**

  Extend conformance coverage so each store proves that an exact operation can be found after its successful append and that replay reconstructs the same accepted operation. For SQLite and PostgreSQL, add backend-specific transaction/fault tests where their hooks permit it; keep the server-level wrapper as the authoritative ambiguous-outcome regression.

  Run: `go test ./internal/aphelion/collab/server ./internal/aphelion/collab/store/... -count=1`

- [x] **2.5 Run race and persistence gates.**

  Run: `go test -race ./internal/aphelion/collab/server ./internal/aphelion/collab/store/... -count=1`

  If PostgreSQL variables are available, run: `go test ./internal/aphelion/collab/store/postgres -count=1`

  Record PostgreSQL as unrun if `APHELION_POSTGRES_TEST_DSN` or `APHELION_POSTGRES_BIN` is absent.

---

## Task 3: Restore the source-quality baseline

**Files:**

- Modify: `internal/aphelion/collab/client/conformance_test.go`
- Modify: `internal/aphelion/collab/client/executor_test.go`
- Modify: `internal/aphelion/collab/ui/session_client_test.go`
- Modify: `internal/app/ui/cpwsarea/wsmap/pmap/editor/collaboration_test.go`

- [x] **3.1 Confirm the four lint failures.**

  Run: `golangci-lint run`

  Expected result before editing: four `errcheck` failures for ignored `NetworkExecutor.Receive` results at the audit locations.

- [x] **3.2 Check each result in its test.**

  Replace every ignored call with an assertion that reports the input context:

  ```go
  if err := executor.Receive(message); err != nil {
  	t.Fatalf("receive %s: %v", message.Type, err)
  }
  ```

  Do not suppress `errcheck` and do not discard the result into `_`.

- [x] **3.3 Verify the focused tests and linter.**

  Run: `go test ./internal/aphelion/collab/client -count=1`

  Run: `golangci-lint run`

  Expected result: both pass with no configured lint findings.

---

## Task 4: Implement durable export checkpoints

**Files:**

- Create: `internal/aphelion/collab/model/checkpoint.go`
- Create: `internal/aphelion/collab/model/checkpoint_test.go`
- Modify: `internal/aphelion/collab/store/store.go`
- Modify: `internal/aphelion/collab/store/memory.go`
- Modify: `internal/aphelion/collab/store/memory_test.go`
- Create: `internal/aphelion/collab/store/sqlite/schema/003_export_checkpoints.sql`
- Modify: `internal/aphelion/collab/store/sqlite/migrations.go`
- Modify: `internal/aphelion/collab/store/sqlite/store.go`
- Modify: `internal/aphelion/collab/store/sqlite/store_test.go`
- Create: `internal/aphelion/collab/store/postgres/schema/003_export_checkpoints.sql`
- Modify: `internal/aphelion/collab/store/postgres/migrations.go`
- Modify: `internal/aphelion/collab/store/postgres/store.go`
- Modify: `internal/aphelion/collab/store/postgres/store_test.go`
- Modify: `internal/aphelion/collab/server/http.go`
- Modify: `internal/aphelion/collab/server/http_test.go`
- Modify: `api/collaboration/openapi.yaml`
- Modify: `internal/aphelion/collab/protocol/envelope_test.go`

- [x] **4.1 Specify the checkpoint model with tests.**

  Define an immutable `ExportCheckpoint` containing checkpoint ID, document ID, session ID, requested revision, snapshot hash, requesting actor, creation time, and status. Status values are `pending`, `accepted`, and `rejected`; terminal metadata contains only trusted artifact hash, verifier name/version, completion time, and a bounded diagnostic code. It must not contain repository paths, executable paths, commands, or secrets.

  Tests must reject empty IDs, revision/hash mismatch, invalid state transitions, terminal state without completion metadata, and any diagnostic exceeding the protocol bound.

  Run: `go test ./internal/aphelion/collab/model -run 'ExportCheckpoint' -count=1`

- [x] **4.2 Extend `SessionStore` and conformance.**

  Add methods to create a pending checkpoint idempotently, retrieve it by document/checkpoint ID, and transition it once from pending to a terminal result. Require the same request ID plus same immutable fields to return the original record; conflicting reuse must fail. Add these cases to memory, SQLite, PostgreSQL, and shared conformance tests.

  SQL schemas must enforce a primary key on checkpoint ID, a document/session foreign key, a unique idempotency key scoped to the document, a status check, and non-null terminal fields only for terminal states. Apply migrations transactionally and prove upgrade from schema version 2 in tests.

- [x] **4.3 Replace the `501` handler with an owner-only preconditioned request.**

  Decode one strict JSON document and reject trailing data. Require the current session owner, exact current revision, exact snapshot hash, and a bounded idempotency key. Ask the document owner for the authoritative snapshot in the same serialized owner loop, create the pending record, and return `202 Accepted` with the immutable checkpoint. A retry returns the same checkpoint. Stale revision/hash returns `409 Conflict`; non-owner returns `403 Forbidden`.

  Add handler tests for all status codes, idempotent retry, conflicting reuse, trailing JSON, body limit, missing session, and unavailable store. Remove the hard-coded `http.StatusNotImplemented` branch.

- [x] **4.4 Make OpenAPI semantic tests executable.**

  Update the `/exports` request/response schema and add a test table that binds every declared method/path to the expected handler and status behavior. Parse the YAML into typed maps; assert required fields, limits, enum values, and response codes rather than string presence.

  Run: `go test ./internal/aphelion/collab/model ./internal/aphelion/collab/store/... ./internal/aphelion/collab/server ./internal/aphelion/collab/protocol -count=1`

- [x] **4.5 Verify restart durability.**

  Create a pending checkpoint, close and reopen SQLite, finish it, reopen again, and require byte-equivalent immutable metadata. Run the equivalent PostgreSQL test when the configured service is available.

  Evidence note: SQLite restart/completion passed on 2026-08-31. PostgreSQL remained unrun because `APHELION_POSTGRES_TEST_DSN` and `APHELION_POSTGRES_BIN` were not configured.

---

## Task 5: Make Meridian staging and acceptance one shipped flow

**Files:**

- Modify: `internal/aphelion/integration/meridian/verify.go`
- Modify: `internal/aphelion/integration/meridian/verify_test.go`
- Modify: `internal/aphelion/integration/meridian/stage_real_test.go`
- Create: `internal/aphelion/integration/meridian/coordinator.go`
- Create: `internal/aphelion/integration/meridian/coordinator_test.go`
- Create: `cmd/apheliondmm-meridian-verify/main.go`
- Create: `cmd/apheliondmm-meridian-verify/main_test.go`
- Modify after protected-file confirmation: `scripts/integration/verify-aphelion-stack.ps1`

- [x] **5.1 Reproduce the normal-path composition failure.**

  Rewrite the verifier fixture so the configured repository target is inside the Meridian-Rift checkout while the staged artifact remains in its hash-named directory under `.aphelion-stages`. Assert that verification of the legitimate `StagedArtifact` succeeds and that a file outside the stage root, a changed artifact hash, or a mismatched manifest is rejected before MCP is called.

  Run: `go test ./internal/aphelion/integration/meridian -run 'TestAcceptanceVerifierUsesImmutableStagedArtifact' -count=1`

  Expected initial result: the legitimate staged artifact is rejected by the same-path check.

- [x] **5.2 Change the verifier boundary.**

  Change `AcceptanceVerifier.Verify` to accept the complete `StagedArtifact`, not an arbitrary staged-file string. Validate its directory with `filepath.Rel`, reject symlink/reparse-point escapes, recompute the file hash, and require the artifact's manifest hash and map target to match the trusted manifest. Remove the requirement that the immutable staged file already equals the repository target path.

  Configure the MCP request to inspect the staged artifact. The verifier may use a trusted local repository root and configured tool locations, but none may come from checkpoint protocol data.

- [x] **5.3 Add a coordinator and real command.**

  Add a coordinator that loads the trusted manifest, stages the snapshot, verifies the returned artifact, and emits one JSON result containing manifest hash, staged artifact hash, verifier version, and exit classification. `cmd/apheliondmm-meridian-verify` must expose this flow with explicit flags for trusted local configuration, fail closed on unknown flags or trailing arguments, and return non-zero on stage or verification failure.

  Test the shipped command with the fake MCP fixture and a temporary repository. Do not invoke a `_test.go` function as production orchestration.

  Run: `go test ./internal/aphelion/integration/meridian ./cmd/apheliondmm-meridian-verify -count=1`

- [x] **5.4 Request protected integration-script approval.**

  Before editing `scripts/integration/verify-aphelion-stack.ps1`, present the exact proposed effects: replace the real-test-as-command invocation with `go run ./cmd/apheliondmm-meridian-verify`; consume its JSON result; and replace monolithic Content Tools discovery with that repository's `tools/testing/run-python-suites.ps1`. Proceed only after Zoe confirms this file.

- [x] **5.5 Run real cross-stack evidence when all repositories and services are available.**

  Run the approved PowerShell coordinator against the configured AphelionDMM, Meridian-MCP, Meridian-Rift, and Content Tools checkouts. Require real MCP parsing, staged map verification, Meridian-Rift compile/build acceptance, and the bounded Content Tools suites. Record unavailable repositories or credentials as unrun, not passed.

  Evidence note: the three sibling repositories were present on 2026-08-31, but no installed `meridian-mcp.exe` was available in `PATH`, the Cargo bin directory, the Meridian-MCP target tree, or the Codex directory. The real cross-stack run therefore remains unrun; parser validation, command tests with the fake MCP process, race tests, and PowerShell plan-mode validation passed locally.

---

## Task 6: Fail closed on native Windows file secrets

**Files:**

- Modify: `internal/aphelion/collab/server/hosted_config.go`
- Create: `internal/aphelion/collab/server/hosted_secret_permissions_other.go`
- Create: `internal/aphelion/collab/server/hosted_secret_permissions_windows.go`
- Create: `internal/aphelion/collab/server/hosted_secret_permissions_windows_test.go`
- Modify: `internal/aphelion/collab/server/hosted_config_test.go`

- [x] **6.1 Add a Windows fail-closed regression.**

  On Windows, create a temporary secret file, grant `BUILTIN\\Users` read access with `icacls`, and assert that hosted configuration rejects it. Remove the test ACE in cleanup and assert cleanup succeeds. Add a positive test whose DACL grants read only to the current user, `SYSTEM`, and `Administrators`.

  Run: `go test ./internal/aphelion/collab/server -run 'TestWindowsSecretFilePermissions' -count=1`

  Expected initial result: the broad file is accepted.

- [x] **6.2 Split OS-specific permission checks.**

  Move POSIX mode validation to `_other.go`. In `_windows.go`, obtain the owner and DACL with `golang.org/x/sys/windows`, expand generic read rights, and reject allow ACEs granting read to Everyone, Authenticated Users, Users, Guests, or any SID other than the file owner, LocalSystem, or Builtin Administrators. Reject null DACLs, inherited broad access, unsupported ACE types, lookup errors, and unavailable security descriptors.

  Change the helper to return `(bool, error)` so a failed ACL inspection cannot be mistaken for permission. Include the path and failing check in the startup diagnostic but never secret contents.

- [x] **6.3 Verify both configuration paths.**

  Run: `go test ./internal/aphelion/collab/server -count=1`

  Evidence note: focused Windows DACL tests, the full server package, and the server race package passed on 2026-08-31. The non-Windows implementation cross-compiled successfully for `linux/amd64`; it was not executed on Windows.

  Run: `go test -race ./internal/aphelion/collab/server -count=1`

---

## Task 7: Restore ownership boundaries and enforce model dependencies

**Files:**

- Modify: `internal/aphelion/collab/model/ids.go`
- Modify: `internal/aphelion/collab/model/hash_test.go`
- Create: `internal/aphelion/collab/model/dependencies_test.go`
- Modify the 11 inherited implementation files listed in `docs/verification/2026-08-30-code-audit.md` under ADMM-AUDIT-007
- Modify: `internal/app/ui/cpwsarea/wsmap/pmap/editor/collaboration.go`
- Modify: `internal/app/ui/cpwsarea/wsmap/pmap/editor/collaboration_presence.go`
- Modify: `internal/dmmdata/save_atomic.go`
- Modify: `internal/dmmdata/save_atomic_other.go`
- Modify: `internal/dmmdata/save_atomic_windows.go`
- Create or modify narrow Aphelion-owned helpers under `internal/aphelion/` as identified by the per-file extraction pass
- Create: `docs/verification/2026-08-30-upstream-drift-review.md`

- [x] **7.1 Enforce the standard-library-only model rule.**

  Add a test using `go/parser` and `parser.ImportsOnly` to inspect non-test files in `internal/aphelion/collab/model`. Fail when an import path contains a dot. This permits standard-library imports and rejects external modules without invoking the network.

  Run: `go test ./internal/aphelion/collab/model -run 'TestModelUsesOnlyStandardLibrary' -count=1`

  Expected initial result: `github.com/google/uuid` is reported.

- [x] **7.2 Replace the model's UUID dependency.**

  Implement UUIDv7 ID generation with `crypto/rand`, `encoding/hex`, `sync`, and `time`. Preserve the existing string format, version nibble `7`, RFC variant bits, and process-local monotonic ordering when multiple IDs share a millisecond. Tests must validate format, version, variant, uniqueness over 10,000 IDs, and nondecreasing lexical order.

  Run: `go test ./internal/aphelion/collab/model -count=1`

- [x] **7.3 Perform a file-by-file extraction/marker pass.**

  For each audited inherited file, diff it against `5241698a`, identify the smallest Aphelion-owned span, and either move separable logic into `internal/aphelion/` or wrap only that span in the required marker. Do not mark an entire inherited file when a function or call-site span is sufficient. Keep package-private editor or atomic-replacement access in a thin marked adapter when moving it would require exporting inherited internals.

  For each file, run its package test immediately after the edit. Then run: `go test ./internal/app/... ./internal/dmmdata ./internal/aphelion/... -count=1`

- [x] **7.4 Record fresh upstream drift before reconciliation.**

  Fetch the configured StrongDMM upstream without merging, record the fetched revision/date, list divergent inherited files, and classify each as clean, marked conflict, or manual semantic review. Do not rebase, merge, checkout, or bulk rename. If network access is unavailable, record the comparison as unavailable and keep the local `5241698a` result clearly labeled stale.

  Evidence note: `git fetch upstream main --prune` succeeded on 2026-08-31 and resolved to the unchanged `5241698aeca9e83fd61048d835e9318d3610c16a`. The full classification is recorded in `docs/verification/2026-08-30-upstream-drift-review.md`; no reconciliation action was required.

---

## Task 8: Harden verification and release infrastructure

**Protected files:**

- Modify only after exact-file confirmation: `.github/workflows/ci.yml`
- Modify only after exact-file confirmation: `Taskfile.yml`
- Possibly modify only after exact-file confirmation: `Taskfile.windows.yml`

- [x] **8.1 Present the protected-file change request.**

  Ask Zoe to approve these exact effects:

  1. `.github/workflows/ci.yml`: make release require lint, build, collaboration resilience, hosted collaboration, vulnerability/container, and toolset integration jobs; pin each third-party action to a reviewed commit SHA while retaining the release tag in a comment; keep draft releases.
  2. `Taskfile.yml`: add the configured linter and contract checks to the full local `verify` aggregate so a red CI source gate cannot coexist with a green aggregate.
  3. `Taskfile.windows.yml` only if the aggregate cannot invoke the existing Windows build task without duplication.

  Stop this task if approval is not granted; Tasks 1-7 remain independently executable.

- [x] **8.2 Add failing workflow-policy tests before YAML changes.**

  Extend the existing CI source tests to parse the workflow and assert that `release.needs` contains every required job and that every `uses:` value is an immutable 40-character commit SHA. Exempt only local `./` actions. Test that comments retain the human-readable upstream action version.

  Run the package containing the existing workflow source tests and confirm it fails against the current workflow.

- [x] **8.3 Resolve and review immutable action revisions.**

  For each action currently referenced by a mutable tag, resolve the tag with GitHub's API, inspect the target commit and release provenance, and record the selected action/release/SHA mapping in the pull-request or handoff evidence. Do not accept a moving branch or a lightweight tag without resolving its commit object.

  Use concrete commands such as:

  ```powershell
  gh api repos/actions/checkout/git/ref/tags/v6
  gh api repos/actions/setup-go/git/ref/tags/v6
  gh api repos/golangci/golangci-lint-action/git/ref/tags/v8
  ```

  Repeat for every non-local action in the workflow, then place the reviewed 40-character SHA directly in `uses:`.

- [x] **8.4 Update release dependencies and the local aggregate.**

  Make `release.needs` enumerate every release-relevant job and keep artifact provenance/attestation inputs tied to the successful build. Add lint and semantic contract checks to `task verify` without duplicating tool installation. Do not suppress the four inherited `go vet` diagnostics; document whether vet remains informational or add a narrowly scoped, reviewed exclusion in a separate decision.

- [x] **8.5 Verify protected infrastructure.**

  Run: `actionlint`

  Run the workflow source-policy tests.

  Run: `$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'; task verify`

  Trigger hosted GitHub Actions only after the working-tree review path chosen by Zoe permits it. Local workflow parsing is not hosted CI evidence.

  Evidence note: `actionlint`, workflow source-policy tests, `golangci-lint run`, and the expanded pinned-target `task verify` passed locally on 2026-08-31. The run included Go tests, semantic contracts, Rust tests, rustfmt, Clippy, the release parser build, and the Windows editor build. Hosted GitHub Actions were not triggered; `Taskfile.windows.yml` did not require modification. Reviewed action pins are recorded in `docs/verification/2026-08-31-github-action-pins.md`.

---

## Task 9: Run full qualification and correct readiness claims

**Files:**

- Modify after evidence: `docs/verification/multiplayer-implementation-readiness.md`
- Modify after evidence: `docs/verification/online-pilot-readiness.md`
- Modify after evidence: `docs/verification/public-hosting-readiness.md`
- Modify after evidence: `docs/verification/protocol-compatibility-matrix.md`
- Modify: `docs/verification/2026-08-30-code-audit.md`

- [x] **9.1 Run local repository gates from a clean process.**

  Run in order:

  ```powershell
  go test ./... -count=1
  go test -race ./internal/aphelion/... -count=1
  golangci-lint run
  govulncheck ./...
  $env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
  task verify
  go run ./cmd/apheliondmm-doctor
  go run ./cmd/apheliondmm-smoke
  actionlint
  git diff --check
  ```

  Capture exit code, tool version, and the exact revision. Keep the inherited ImGui warning and `go vet` findings distinct from failures introduced by Aphelion-owned code.

- [x] **9.2 Run external/local-service gates.**

  With explicit configured services, run PostgreSQL conformance and restore, container-tagged hosted lifecycle, Trivy image scan, backup/restore rehearsal, OTLP metrics, OIDC fixture and external-provider login, pilot-tagged 25-editor load, and the real Meridian-MCP/Meridian-Rift/Content Tools coordinator. Do not substitute helper-only tests for shipped commands or deployment entry points.

- [ ] **9.3 Run human desktop acceptance.**

  Complete two real desktop windows, simultaneous edit conflict, undo ownership, reconnect/replay, save/reopen, keyboard-only operation, narrow-window layout, ordinary single-user edit/save regression, and unknown-type preservation. Record OS, display scale, map/environment hashes, steps, expected/actual result, screenshots where relevant, and the tester's name or role.

  Unrun: this requires a named human tester operating two real desktop windows. No human acceptance result is inferred from the automated smoke or container gates.

- [x] **9.4 Update documentation only from fresh evidence.**

  Mark export checkpoints and Meridian acceptance implemented only after their shipped entry points pass. Keep absent external credentials, public DNS/tunnel, named-user pilot, branding approval, updater signing key, and multi-replica support explicitly unresolved. Link each completion claim to a command log, hosted run, or human evidence record.

- [x] **9.5 Final review.**

  Review the full diff against the audit IDs, confirm every finding is either fixed and verified or explicitly deferred with an owner/gate, rerun `git status --short --branch`, and leave all changes uncommitted for Zoe's review.

  Evidence note: all ordered local gates passed on 2026-08-31 at base revision `c3469c60d5cd39c21268e14ee38643910601b2e9` plus the uncommitted remediation diff. A fresh hosted image lifecycle and Trivy scan passed. Live PostgreSQL backup/restore, real Meridian cross-stack acceptance, external OIDC/OTLP, reference load/fault, hosted CI, and human desktop acceptance were unavailable and are recorded as unrun in the readiness reports.

## Completion criteria

This plan is complete only when:

1. Unsafe snapshots are rejected without allocation or panic at every entry point.
2. A committed-then-error append cannot leave durable and in-memory revisions split.
3. The public export route creates and persists an owner-authorized checkpoint instead of returning `501`.
4. One shipped command composes Meridian staging and acceptance against an immutable artifact.
5. Native Windows file secrets fail closed on broad or unreadable ACLs.
6. `golangci-lint run`, repository tests, race tests, Rust/build gates, semantic contract tests, and actionlint pass.
7. Inherited edits have narrow ownership markers or thin adapters, and model dependency policy is mechanically enforced.
8. Protected workflow changes, if approved, prevent release publication when any required quality/security gate fails and use reviewed immutable action revisions.
9. Readiness documents distinguish fresh automated, hosted, external-service, and human evidence from unrun gates.
