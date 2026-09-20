# Real Meridian-MCP contract and staging gate

AphelionDMM revision: `7bf1f0fe`. Both opt-in real-process gates passed without
skips using the local Meridian-MCP release executable and Meridian-Rift checkout:

```text
go test ./internal/aphelion/integration/meridian -run '^(TestMCPRealInstalledContract|TestRealMeridianStagingOnly)$' -count=1 -v -timeout 12m
```

The installed-contract gate took 49.52 seconds and staging-only took 11.55 seconds;
total package duration was 61.270 seconds. These are execution observations, not
performance qualification.

The adapter started a separate MCP process in analysis mode with explicit roots,
parsed `tgstation.dme`, inspected the configured map, and retrieved diagnostics.
The staging gate published an immutable manifest-bound copy under AphelionDMM's
ignored artifact directory, then inspected that staged copy. Parse, map and
diagnostic responses all reported state generation 1 and MCP version `0.1.0`.

## Recorded identity

- Meridian-Rift revision: `aa32fadb56c62957b51f2cfe510d5f99a7ab5548`.
- Source checkout was **dirty**; the staging fixture explicitly allows dirty
  source input. No game checkout files were applied or changed by these gates.
- Target: `_maps/virtual_domains/test_only.dmm`, dimensions **5x5x1**.
- Input and staged output SHA-256:
  `863c445d7731e06392803ec57485cd254a464780176670365d7b489652a2d085`.
- DME file SHA-256:
  `0a0f65cc07db67423a4a63021ee026ca98baa256b787548abfaac0a325c296e2`.
- Stage manifest SHA-256:
  `d46064aa78f3171eea350788de50f302d2a92f4706d53e93880a565ee1080948`.
- MCP executable SHA-256:
  `aa0178b05aa8e5fcbc097319123d9d9b5020bcf10f4b147b6c969e756a1cf22f`.

## Acceptance limits

MCP reported **50 diagnostics**. Successful retrieval does not mean those
diagnostics are resolved or that DreamMaker compilation succeeds. The fixture
records the reported count and does not classify severity or establish that the
response contains every diagnostic.

The candidate was an unchanged copy of the existing test map, with fixture
manifest metadata. This verifies process contracts, containment, staged-file
inspection and matching environment generations, not interoperability of a new
editor-generated change. The DME hash identifies that file rather than hashing
the complete dirty game dependency tree.

No authoritative `RIFT_BUILD.cmd` acceptance, Content Tools launcher/build gates,
clean-checkout stack acceptance or deployment ran in this invocation. Those
remain separate requirements. Raw logs, provenance, staged map/manifest and MCP
evidence are retained in ignored `.artifacts/meridian-real-2026-09-20/`.
No source, dependencies or protected infrastructure changed for these gates.
