# Source authority

## Repository roles

- AphelionDMM owns multiplayer design, Aphelion adapters, repository guidance, and new product behavior.
- StrongDMM remains the authority for inherited editor behavior until Aphelion deliberately replaces a narrow seam.
- SpacemanDMM remains the authority for the vendored parser behavior under `third_party/sdmmparser` unless an Aphelion compatibility patch is documented.
- BYOND/DreamMaker defines the accepted DME, DMM, and TGM semantics.
- Game repositories own their own compilation and runtime acceptance outside AphelionDMM.
- AphelionDMM uses its native parser and map model for editor fidelity checks; external MCP services are unsupported.

The audit establishing this guidance inspected local revision `5241698a` on 2026-08-24, with local `main` matching the configured StrongDMM upstream-tracking revision at that time. Reconfirm both facts before an upstream reconciliation.

## Precedence for behavior

1. The current approved Aphelion design and versioned protocol contracts.
2. The current Aphelion implementation and tests.
3. Inherited StrongDMM behavior and upstream history.
4. Similar behavior in adjacent repositories, used as evidence rather than copied as authority.

Do not resolve a disagreement by silently changing formats. Record the incompatible authorities, select one explicitly, and add a migration or compatibility test.

## Downstream change rules

- Put new behavior in Aphelion-owned packages whenever possible.
- Keep inherited source edits narrow and marked with `APHELION EDIT` comments.
- Preserve upstream comments and provenance.
- Do not copy a complete inherited subsystem to change a small behavior.
- Do not rebrand executable names, module paths, update endpoints, assets, or release artifacts without an approved branding and migration decision.
- Do not alter parser output merely to make collaboration simpler. Normalize at the Aphelion operation boundary and prove round-trip compatibility.

## Cross-repository acceptance

A map integration change requires:

1. AphelionDMM round-trip and operation tests.
2. Staged output with a content hash and repository identity.
3. Native parse/reparse comparison, preserving map values and collaboration identity.

Per the September 20 scope decisions, Meridian-MCP, Aphelion Content Tools and
Rift build tooling are unsupported. Their checkouts, dependencies, inspections
and build gates are not AphelionDMM acceptance requirements. Game repositories
remain usable as user-selected map/environment fixtures.

Credentials, repository roots, and executable paths stay in local trusted configuration. They are never map data or collaboration messages.

