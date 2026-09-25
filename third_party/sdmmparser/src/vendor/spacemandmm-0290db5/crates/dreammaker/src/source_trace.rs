//! Opt-in filesystem provenance for persistent environment parse reuse.

use serde::Serialize;
use sha2::{Digest, Sha256};
use std::fmt::Write as _;
use std::path::Path;

/// The file-access event captured for an environment parse.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum SourceAccessKind {
    /// Bytes were read and supplied to a lexer.
    Read,
    /// A duplicate include was successfully opened but not lexed.
    OpenOnly,
}

/// A source file access, preserving both the parser path and its effective path.
#[derive(Clone, Debug, Serialize)]
pub struct SourceAccess {
    pub kind: SourceAccessKind,
    /// Exact path passed to the parser's file operation, if representable as UTF-8.
    pub path: Option<String>,
    /// Lexical absolute path resolved against the parse working directory.
    pub resolved_path: Option<String>,
    /// Lowercase SHA-256 of the exact bytes consumed by the lexer; absent for open-only events.
    pub sha256: Option<String>,
}

/// The reason a path existence check affects preprocessing.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum SourceProbeKind {
    IncludeCandidate,
    FExists,
    ConfigDiscovery,
}

/// An ordered path existence check made while preparing an environment.
#[derive(Clone, Debug, Serialize)]
pub struct SourceProbe {
    pub kind: SourceProbeKind,
    /// Exact path passed to `Path::exists()`, if representable as UTF-8.
    pub path: Option<String>,
    /// Lexical absolute path resolved against the parse working directory.
    pub resolved_path: Option<String>,
    pub exists: bool,
}

/// Filesystem provenance captured by one parser context.
#[derive(Clone, Debug, Serialize)]
pub struct SourceTrace {
    pub schema_version: u32,
    /// False means the trace cannot safely support a persistent cache hit.
    pub complete: bool,
    pub working_dir: Option<String>,
    pub config_mode: String,
    pub accesses: Vec<SourceAccess>,
    pub probes: Vec<SourceProbe>,
    #[serde(skip)]
    estimated_bytes: usize,
}

impl SourceTrace {
    pub(crate) fn new() -> Self {
        let working_dir = std::env::current_dir()
            .ok()
            .and_then(|path| path.to_str().map(str::to_owned));
        Self {
            schema_version: 1,
            complete: working_dir.is_some(),
            working_dir,
            config_mode: "default-no-autodetect".to_owned(),
            accesses: Vec::new(),
            probes: Vec::new(),
            estimated_bytes: 0,
        }
    }

    // Trace admission must not reject an otherwise valid parse. Stop recording
    // and make the result uncacheable before allocating unbounded event strings.
    fn admit(&mut self, path: &Path) -> bool {
        if !self.complete {
            return false;
        }
        let path_len = path.as_os_str().len();
        let cwd_len = self.working_dir.as_ref().map_or(0, String::len);
        let charge = 256usize
            .saturating_add(path_len.saturating_mul(2))
            .saturating_add(cwd_len);
        if self.accesses.len() + self.probes.len() >= 100_000
            || path_len > 4096
            || cwd_len > 4096
            || self.estimated_bytes.saturating_add(charge) > 16 * 1024 * 1024
        {
            self.complete = false;
            return false;
        }
        self.estimated_bytes += charge;
        true
    }

    fn paths(&mut self, path: &Path) -> (Option<String>, Option<String>) {
        let raw = path.to_str().map(str::to_owned);
        if raw.is_none() {
            self.complete = false;
        }

        let current_dir = std::env::current_dir().ok();
        let current_dir_text = current_dir
            .as_ref()
            .and_then(|current_dir| current_dir.to_str().map(str::to_owned));
        if current_dir_text.as_deref() != self.working_dir.as_deref() {
            self.complete = false;
        }

        let resolved = if path.is_absolute() {
            Some(path.to_owned())
        } else {
            current_dir.map(|current_dir| current_dir.join(path))
        };
        let resolved = resolved.and_then(|path| path.to_str().map(str::to_owned));
        if resolved.is_none() {
            self.complete = false;
        }
        (raw, resolved)
    }

    pub(crate) fn record_read(&mut self, path: &Path, bytes: &[u8]) {
        if !self.admit(path) {
            return;
        }
        let (path, resolved_path) = self.paths(path);
        let digest = Sha256::digest(bytes);
        let mut sha256 = String::with_capacity(64);
        for byte in digest {
            write!(&mut sha256, "{byte:02x}").expect("writing to a String cannot fail");
        }
        self.accesses.push(SourceAccess {
            kind: SourceAccessKind::Read,
            path,
            resolved_path,
            sha256: Some(sha256),
        });
    }

    pub(crate) fn record_open_only(&mut self, path: &Path) {
        if !self.admit(path) {
            return;
        }
        let (path, resolved_path) = self.paths(path);
        self.accesses.push(SourceAccess {
            kind: SourceAccessKind::OpenOnly,
            path,
            resolved_path,
            sha256: None,
        });
    }

    pub(crate) fn record_probe(&mut self, kind: SourceProbeKind, path: &Path, exists: bool) {
        if !self.admit(path) {
            return;
        }
        let (path, resolved_path) = self.paths(path);
        self.probes.push(SourceProbe {
            kind,
            path,
            resolved_path,
            exists,
        });
    }

    pub(crate) fn set_config_mode(&mut self, mode: &str) {
        self.config_mode.clear();
        self.config_mode.push_str(mode);
    }
}
