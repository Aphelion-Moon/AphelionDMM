# PostgreSQL persistence and restore gate

Revision: `9343ae04`. The complete PostgreSQL package was exercised against an
isolated PostgreSQL 17.11 cluster using the existing installed binaries, matching
17.11 backup tools, and the repository's pinned Go 1.25.13 toolchain.

```text
go test -race ./internal/aphelion/collab/store/postgres -count=1 -v -timeout 180s
```

All 12 tests passed with no skips; package duration was 5.429 seconds. This covers
store conformance, concurrent duplicate append across independent pools, row-lock
cancellation, concurrent migrations, forced backend termination and pool recovery,
cross-document operation-ID collision, SQLite recovery parity, retry classification,
hosted membership and invitation persistence/expiry, and logical backup/restore.

## Isolation and durability

A new scratch data directory and random test credential were created for this
run. The server listened only on `127.0.0.1`, port 64258, used SCRAM authentication,
and had data checksums enabled. Runtime queries confirmed `fsync`,
`synchronous_commit`, `full_page_writes`, and `data_checksums` were all on.
The password was passed through process environment and removed from the
initialization file; it was not written into the evidence manifest or command
arguments. The existing SS14 PostgreSQL service was not reconfigured or restarted.

Test cleanup left zero `aphelion_test_` schemas and zero `aphelion_restore_`
databases. The disposable server shut down cleanly with exit code zero, and its
loopback listening port closed. No service was installed.

The first setup started a healthy isolated server but PowerShell retained an
inherited startup pipe before reaching the tests. That server was stopped cleanly.
The successful setup launched `pg_ctl` with `Start-Process`, separate stdout/stderr
files, and `Process.WaitForExit` for the controller only. Do not use `Start-Process
-Wait` for this daemon startup, since waiting on its process tree defeats detachment.
The initial setup failure is retained separately and is not a failed database test.

## Restore evidence and limits

Both custom-format archives were restored into new databases and reconstructed
through snapshot plus replay. Recovered canonical hashes matched retained revision
hashes before and after the known operation:

| Point | Revision | Canonical map hash | Archive SHA-256 |
| --- | ---: | --- | --- |
| Before | 1 | `8ebda484eb043259ba3bfd10c6ff4eca0e679160a3a12d7756515118c799c536` | `63cce7e34088269eeac4eb35ba6e30b6f948cea6d658c9d0d97c66f29ef33a52` |
| After | 2 | `f3e1bdfd81c3209ac922c40396a479694eff70c5cb32d8b9cc4c9a258216d65b` | `83d825f26aeaa1bbed970da77e7b161081dae2a57d8867872c360510fd04830d` |

Observed local restore durations were 787.2611 ms and 955.0644 ms. These are single
fixture observations with race instrumentation and other host work running, not
performance comparisons, hosted RTO measurements or load qualification.

Raw test, settings, cleanup and shutdown logs plus binary/revision provenance are
retained in ignored `.artifacts/postgres-gate-2026-09-20-retry/`; the first setup
logs are in `.artifacts/postgres-gate-2026-09-20/`. This closes the currently skipped
local PostgreSQL package gate at the recorded revision. It does not establish
Meridian acceptance, deployed OIDC/hosting, concurrent service-load recovery,
production backup operations, or PostgreSQL 18 qualification on this host.
No source, dependency or protected infrastructure changed for this run.
