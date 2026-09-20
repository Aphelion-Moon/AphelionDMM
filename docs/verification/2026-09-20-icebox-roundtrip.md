# IceBox inventory and representative round-trip evidence

The user selected Meridian-Rift's DME and IceBox for representative acceptance.
At Aphelion production revision `4e232bbc`, the native DME parser and the
collaboration map import/export path preserve this map through atomic DMM and
TGM saves. Five fresh-process samples pass with identical counts and hashes.
Meridian-MCP inspection remains blocked by its response-size limit, including
when inspecting the original source map.

## Fixture and fidelity

The local Meridian-Rift checkout's tracked files match revision
`aa32fadb56c62957b51f2cfe510d5f99a7ab5548`; existing untracked tooling remains
untouched. `_maps/icebox.json` selects
`_maps/map_files/IceBoxStation/IceBoxStation.dmm`. Although its extension is
`.dmm`, the source uses TGM format.

| Input | Bytes | SHA-256 |
| --- | ---: | --- |
| `tgstation.dme` | 644,567 | `0a0f65cc07db67423a4a63021ee026ca98baa256b787548abfaac0a325c296e2` |
| IceBox map | 3,212,240 | `f9e3a4ac79e06e22910adca4d6828528e55bd59d5d3ddd4c3b409d6cb76c1d8c` |

| Inventory | Count |
| --- | ---: |
| Dimensions | 255 x 255 x 3 |
| Cells | 195,075; 65,025 per level |
| Dictionary entries / entries used | 11,241 / 11,241 |
| Distinct placed type paths | 3,003 |
| Distinct prefab path + explicit variable combinations | 5,599 |
| Placed prefab instances | 431,102 |
| Distinct explicit variable names | 77 |
| Explicit variable occurrences across placed instances | 15,364 |
| Maximum prefabs per cell | 20 |
| Native parsed environment types | 65,227 |
| Placed types missing from that environment | 0 |

The native environment hash is
`641c9a0c21f16bef88fba4bf5a46ec06f800836498482c6b47cf7e95815038c0`.
This uses the existing environment-hash contract over parsed types and variables;
it is distinct from the DME file digest above. Deterministic test-only stable
instance IDs produce collaboration snapshot hash
`880265c3ba2648f585ee46856bcc1928fce3bcaac2e75b68351bd4c3962e1fec`.

The test builds the real editor map model, imports collaboration state, exports
both formats, and calls the normal staged/validated/flushed atomic save path.
It independently compares every source coordinate, ordered prefab path and
explicit variable value against the reparse. Reimport using the original
collaboration metadata must reproduce the canonical snapshot hash. It checks
source map and DME file hashes again afterward. Output files go into new ignored
artifact directories; no source map is overwritten.

All five samples produce DMM file SHA-256
`df5eddfc9fe7e595fa75b7b32f18f6717fce09b99b6897bd846e04c34dcf9c88`
and TGM file SHA-256
`6d51969bc05ad9eb540ea44fcee882bfda394f09d3b52d72b2a266f82ebccfbf`.
Different source/output byte hashes reflect serialization, with exact parsed
values and collaboration identity verified separately.

## Timing scope and validation

Pinned Go 1.25.13 ran one exploratory case, a separate final-binary warmup and
five measured fresh processes. All passed with no failures/timeouts. OS file
caches were not flushed. Measurements use the existing native Rust parser and
Windows atomic-save implementation on the local Ryzen 9800X3D/NVMe host.

| Stage | Median ms | Range ms |
| --- | ---: | ---: |
| DMM/TGM source parsing | 71.439 | 70.143-72.437 |
| Native DME parse and environment construction | 3,214.047 | 3,196.674-3,231.266 |
| Editor map construction, deterministic IDs, import and hash | 329.734 | 319.086-336.845 |
| DMM export plus validated atomic save | 374.878 | 361.040-387.358 |
| TGM export plus validated atomic save | 459.309 | 449.306-472.870 |

Export/save includes staging reparse and semantic validation; the later explicit
reparse, full value comparison and collaboration reimport are outside that
stage timer. These are baseline component measurements, not a speedup claim,
startup/frame latency or a complete interactive Save measurement. Rendering,
icons, UI gestures, GPU/native retention and endurance remain unmeasured here.

Focused lint reports zero issues. The inherited ImGui compiler warning remains.
Full application, race and hosted-service suites were not rerun for this
test-only representative fixture gate. Without the two explicit fixture
environment variables, the external-fixture test skips; a skip is not acceptance.

The test binary SHA-256 is
`1ae909b11cb9882e48833f4fad2f2096a9c83accd683e4e24623a1d22afb830e`.
Logs, sample CSV, timing summaries, exported maps and binary are retained in
ignored `.artifacts/icebox-roundtrip-2026-09-20/`.

## Meridian-MCP blocker

The existing verified Aphelion CLI stages the DMM export successfully but returns
`verification_failed` during `dm_map_info`. Direct JSON-RPC inspection identifies
the same `limit_exceeded` error for all three inputs:

| Map | Reported response bytes | Server cap |
| --- | ---: | ---: |
| Original IceBox | 2,467,380 | 1,048,576 |
| DMM export | 2,467,408 | 1,048,576 |
| TGM export | 2,467,407 | 1,048,576 |

Meridian-MCP source revision `f8bd6fd501aadbf8ab3afb48d31abb72f81afc08`
adds detailed dictionary/model-use data to `dm_map_info`; the advertised input
schema accepts only `dmm_path`, with no pagination/compact-response control.
The first full verifier failure and direct responses are retained in the artifact
directory. This is an oversized response, not evidence of map corruption. The
TGM export was checked directly after the full verifier stopped at the DMM error.
Neither format has passed full staged MCP inspection. Bounded summary or
pagination support is needed in Meridian-MCP before repeating that gate; raising
or bypassing the transport cap was not used as a substitute.
