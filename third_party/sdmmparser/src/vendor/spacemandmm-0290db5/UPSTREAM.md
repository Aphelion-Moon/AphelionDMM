Source: https://github.com/SpaceManiac/SpacemanDMM
Tag: suite-1.11
Revision: 0290db5c752fa292f9824521515b603a2afd11dc

The three crate directories are copied from that pinned revision. `dreammaker` adds opt-in Context source tracing: exact consumed-byte SHA-256, successful opens, include-resolution and fexists probes, configuration provenance, and incomplete-trace rejection. Owned spans in config, constants, error, lexer, lib and preprocessor are marked APHELION; source_trace.rs is new and sha2 is the only added dependency. Tracing is disabled by default. The wrapper keeps Context::default without configuration discovery.

The dependency sources are formatted by the repository-pinned Rust 1.82 formatter because the existing `cargo fmt --all` gate also visits local dependencies. Outside the marked tracing changes, these are formatting-only changes; `interval-tree` and `builtins-proc-macro` retain upstream behavior. The interval-tree package declares `license-file = "LICENSE"` but its pinned crate directory does not contain that file; the matching upstream root LICENSE is copied there verbatim as well as retained at this vendor root. Upstream authorship and GPL-3.0 license are retained; no external assets or branding are added.
