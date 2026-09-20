# Verification

## Evidence levels

Report the exact level reached:

1. Static review: source and contracts inspected.
2. Focused test: the changed package or behavior passed.
3. Repository test: all Go and Rust tests passed with pinned toolchains.
4. Cross-stack build: the actual Task build completed for the target platform.
5. Entry-point exercise: the produced desktop or collaboration executable completed its real smoke path.

Meridian-MCP, Aphelion Content Tools and Rift build tooling were removed from
scope on September 20. None is a required acceptance gate. Representative map
checks use the native parser, operation model and validated atomic save path.

Never present a lower level as a higher one. A successful focused test does not establish repository, runtime, or integration completion.

## Current authoritative versions

- Go is selected by `go.mod` and currently declares Go 1.25.13.
- CI currently selects Rust 1.82 target-qualified toolchains.
- CI currently selects golangci-lint 2.12.2.
- The normal cross-stack build entry point is `task build` with an explicit `RUST_TARGET` matching CI.
- Task is not pinned in the build job. Record `task --version` with evidence until protected infrastructure is explicitly approved for a reproducibility change.

Do not replace a pinned command with the ambient `go`, `cargo`, or `rustc` and call the result equivalent.

## Narrow-first commands

Run PowerShell commands from the repository root. Examples:

```powershell
go test ./internal/aphelion/collab/engine -run TestDocumentApply -count=1
go test ./internal/aphelion/collab/server -run TestTwoClientsConverge -count=1
go test ./internal/dmapi/dmmap/dmmdata -run TestAtomicSave -count=1
```

Then broaden:

```powershell
go test ./... -count=1
go test -race ./internal/aphelion/...
rustup run 1.82.0-x86_64-pc-windows-gnu cargo test --manifest-path third_party/sdmmparser/src/Cargo.toml --locked
rustup run 1.82.0-x86_64-pc-windows-gnu cargo fmt --manifest-path third_party/sdmmparser/src/Cargo.toml --all -- --check
rustup run 1.82.0-x86_64-pc-windows-gnu cargo clippy --manifest-path third_party/sdmmparser/src/Cargo.toml --all-targets --locked -- -D warnings
```

For the Windows cross-stack build:

```powershell
$env:RUST_TARGET = '1.82.0-x86_64-pc-windows-gnu'
task task_win:gen_syso
task build
```

Check `$LASTEXITCODE` after every native command. The intended desktop smoke target remains `dst/StrongDMM.exe` until a human-approved branding migration changes it.

The real workspace Save gate needs a working OpenGL context:

```powershell
$env:APHELIONDMM_GL_TEST = '1'
go test ./internal/app/ui/cpwsarea/wsmap -run '^TestSaveAcknowledgementBoundaries$' -count=1 -timeout 60s
Remove-Item Env:APHELIONDMM_GL_TEST
```

It creates a hidden window and exercises `PaneMap` construction and `WsMap.Save`.
Its default skip is not desktop evidence, and a successful hidden-context test
does not replace human interaction testing. PostgreSQL tests also skip unless
`APHELION_POSTGRES_TEST_DSN` is configured; logical backup/restore additionally
requires `APHELION_POSTGRES_BIN`. Use disposable test databases.

## Multiplayer acceptance

A multiplayer phase needs deterministic automated evidence for:

- two clients reaching identical map hashes after interleaved edits;
- duplicate operation delivery remaining idempotent;
- reconnect replay producing the same revision and hash as an uninterrupted client;
- actor-scoped undo accepting safe inverses and rejecting stale preconditions;
- presence loss not changing durable state;
- process interruption preserving the last acknowledged durable revision;
- snapshot plus replay reconstructing the authoritative hash;
- rejected environment/hash mismatches leaving state unchanged;
- atomic save preserving the old target on validation or replacement failure.

Use fixed seeds for repeatable tests and record randomized seeds when a property or fuzz test fails.

## Protected gates

Changes to `.github/workflows/ci.yml`, `Taskfile.yml`, `Taskfile_windows.yml`, signing, updater publication, or release steps require exact-file review and explicit user confirmation. Plans may describe those changes; executors may not apply them without that confirmation.
