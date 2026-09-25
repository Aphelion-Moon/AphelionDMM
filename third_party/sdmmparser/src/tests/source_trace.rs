use dreammaker::preprocessor::Preprocessor;
use dreammaker::{Context, Severity, SourceAccessKind, SourceProbeKind};
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Mutex;

#[test]
fn trace_budget_marks_uncacheable_without_rejecting_parse() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    let path = fixture.path().join("budget.dme");
    let mut input = "#if fexists(\"missing.txt\")\n#endif\n".repeat(100_001);
    input.push_str("/obj/after_budget\n");
    fs::write(&path, input).expect("write trace-heavy valid source");
    let context = Context::default();
    context.enable_source_trace();
    context
        .parse_environment(&path)
        .expect("trace budget must not reject parsing");
    assert!(context
        .errors()
        .iter()
        .all(|error| error.severity() != Severity::Error));
    let trace = context.take_source_trace().expect("trace available");
    assert!(!trace.complete, "exhausted trace must never be reusable");
    assert!(trace.accesses.len() + trace.probes.len() <= 100_000);
}

static NEXT_TEMP: AtomicUsize = AtomicUsize::new(0);
static TEST_LOCK: Mutex<()> = Mutex::new(());

struct TestDirectory(PathBuf);

impl TestDirectory {
    fn new() -> Self {
        let serial = NEXT_TEMP.fetch_add(1, Ordering::Relaxed);
        let path = std::env::temp_dir().join(format!(
            "spacemandmm-source-trace-{}-{serial}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&path);
        fs::create_dir_all(&path).expect("create test directory");
        Self(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for TestDirectory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

struct CurrentDirectory(PathBuf);

impl CurrentDirectory {
    fn change_to(path: &Path) -> Self {
        let old = std::env::current_dir().expect("read original working directory");
        std::env::set_current_dir(path).expect("set fixture working directory");
        Self(old)
    }
}

impl Drop for CurrentDirectory {
    fn drop(&mut self) {
        std::env::set_current_dir(&self.0).expect("restore working directory");
    }
}

fn path_text(path: &Path) -> String {
    path.to_string_lossy().into_owned()
}

#[test]
fn records_consumed_bytes_and_ordered_include_and_fexists_probes() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    fs::create_dir(fixture.path().join("nested")).expect("create nested directory");

    let root_dme = fixture.path().join("env.dme");
    let nested_dm = fixture.path().join("nested").join("start.dm");
    let root_shared_dm = fixture.path().join("shared.dm");
    fs::write(&root_dme, b"#include \"nested/start.dm\"\n").expect("write DME");
    fs::write(
        &nested_dm,
        b"#include \"shared.dm\"\n#if fexists(\"present.txt\")\n/obj/from_present\n#endif\n#if fexists(\"missing.txt\")\n/obj/from_missing\n#endif\n",
    )
    .expect("write nested DM source");
    fs::write(&root_shared_dm, b"/obj/from_root_shared\n").expect("write fallback DM source");
    fs::write(
        fixture.path().join("nested").join("present.txt"),
        b"present",
    )
    .expect("write positive fexists target");

    let _cwd = CurrentDirectory::change_to(fixture.path());
    let context = Context::default();
    context.enable_source_trace();
    let _tree = context
        .parse_environment(&root_dme)
        .expect("parse fixture environment");
    assert!(
        context
            .errors()
            .iter()
            .all(|error| error.severity() != Severity::Error),
        "the fixture must parse without compiler errors"
    );

    let trace = context
        .take_source_trace()
        .expect("enabled trace is available");
    assert!(trace.complete, "all fixture paths should be traceable");
    assert_eq!(trace.schema_version, 1);
    let expected_working_dir = path_text(fixture.path());
    assert_eq!(
        trace.working_dir.as_deref(),
        Some(expected_working_dir.as_str())
    );

    let reads: Vec<_> = trace
        .accesses
        .iter()
        .filter(|event| event.kind == SourceAccessKind::Read)
        .map(|event| (event.path.clone(), event.sha256.clone()))
        .collect();
    assert_eq!(
        reads,
        vec![
            (
                Some(path_text(&root_dme)),
                Some("d242af2dc6733b91be393be5b7454f226dc05f2d43fd16b57ea107b4e6b46f8a".to_owned()),
            ),
            (
                Some(path_text(&fixture.path().join("nested/start.dm"))),
                Some("8b13b6abe4b66f3a43e3d0f3e7d0b1ac3f80a1f50de5309eeaf88cec36b8b930".to_owned()),
            ),
            (
                Some(path_text(&root_shared_dm)),
                Some("bd3caf6fe567a33aa13edcedb0a576c7b7597b7bd8c61811da9fe697765d2c8d".to_owned()),
            ),
        ],
        "only root and active DM source buffers are consumed, in parse order"
    );

    let probes: Vec<_> = trace.probes.iter().collect();
    assert_eq!(
        probes
            .iter()
            .map(|probe| (probe.kind, probe.path.clone(), probe.exists))
            .collect::<Vec<_>>(),
        vec![
            (
                SourceProbeKind::IncludeCandidate,
                Some(path_text(&fixture.path().join("nested/start.dm"))),
                true,
            ),
            (
                SourceProbeKind::IncludeCandidate,
                Some(path_text(&fixture.path().join("nested").join("shared.dm"))),
                false,
            ),
            (
                SourceProbeKind::IncludeCandidate,
                Some(path_text(&root_shared_dm)),
                true,
            ),
            (
                SourceProbeKind::FExists,
                Some(path_text(Path::new("nested").join("present.txt").as_path())),
                true,
            ),
            (
                SourceProbeKind::FExists,
                Some(path_text(Path::new("nested").join("missing.txt").as_path())),
                false,
            ),
        ],
        "include search keeps its two-path order and fexists records true and false"
    );
}

#[test]
fn duplicate_include_records_open_only_without_hashing_unconsumed_bytes() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    let root_dme = fixture.path().join("env.dme");
    let child_dm = fixture.path().join("child.dm");
    fs::write(&root_dme, b"#include \"child.dm\"\n#include \"child.dm\"\n").expect("write DME");
    fs::write(&child_dm, b"/obj/child\n").expect("write child source");

    let context = Context::default();
    context.enable_source_trace();
    let _tree = context
        .parse_environment(&root_dme)
        .expect("parse duplicate-include environment");
    assert!(
        context
            .errors()
            .iter()
            .all(|error| error.severity() != Severity::Error),
        "duplicate include is a warning, not a parse error"
    );

    let trace = context
        .take_source_trace()
        .expect("enabled trace is available");
    let child_path = path_text(&child_dm);
    assert_eq!(
        trace
            .accesses
            .iter()
            .filter(|event| event.kind == SourceAccessKind::Read
                && event.path.as_deref() == Some(child_path.as_str()))
            .count(),
        1,
        "the duplicate include is not lexed a second time"
    );
    assert_eq!(
        trace
            .accesses
            .iter()
            .filter(|event| event.kind == SourceAccessKind::OpenOnly
                && event.path.as_deref() == Some(child_path.as_str()))
            .count(),
        1,
        "the second include open remains an accessibility dependency"
    );
}

#[test]
fn tracing_is_disabled_until_explicitly_enabled() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    let root_dme = fixture.path().join("env.dme");
    fs::write(&root_dme, b"/obj/only\n").expect("write DME");

    let context = Context::default();
    let _tree = context
        .parse_environment(&root_dme)
        .expect("parse without trace");

    assert!(context.take_source_trace().is_none());
}

#[test]
fn failed_root_open_marks_the_trace_incomplete() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    let context = Context::default();
    context.enable_source_trace();

    assert!(context
        .parse_environment(&fixture.path().join("missing.dme"))
        .is_err());
    let trace = context
        .take_source_trace()
        .expect("enabled trace is available");
    assert!(
        !trace.complete,
        "a missing root cannot be replayed as a complete parse"
    );
}

#[test]
fn in_memory_preprocessor_input_marks_the_trace_incomplete() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    let context = Context::default();
    context.enable_source_trace();

    let _preprocessor = Preprocessor::from_buffer(
        &context,
        fixture.path().join("env.dme"),
        "/obj/from_buffer\n",
    );
    let trace = context
        .take_source_trace()
        .expect("enabled trace is available");
    assert!(
        !trace.complete,
        "synthetic input has no replayable file bytes"
    );
}

#[test]
fn autodetected_config_records_negative_positive_probe_and_consumed_bytes() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    let root_dme = fixture.path().join("env.dme");
    let config_path = fixture.path().join("SpacemanDMM.toml");
    fs::write(&root_dme, b"").expect("write DME");

    let mut context = Context::default();
    context.enable_source_trace();
    context.autodetect_config(&root_dme);
    fs::write(&config_path, b"").expect("write empty config");
    context.autodetect_config(&root_dme);

    let trace = context
        .take_source_trace()
        .expect("enabled trace is available");
    assert_eq!(trace.config_mode, "autodetect");
    let config_path_text = path_text(&config_path);
    assert_eq!(
        trace
            .probes
            .iter()
            .filter(|probe| probe.kind == SourceProbeKind::ConfigDiscovery)
            .map(|probe| (probe.path.clone(), probe.exists))
            .collect::<Vec<_>>(),
        vec![
            (Some(config_path_text.clone()), false),
            (Some(config_path_text.clone()), true),
        ],
        "config discovery records absent then present using the same candidate"
    );
    assert!(
        trace.accesses.iter().any(|event| {
            event.kind == SourceAccessKind::Read
                && event.path.as_deref() == Some(config_path_text.as_str())
                && event.sha256.as_deref()
                    == Some("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
        }),
        "the config digest describes the exact empty byte stream parsed"
    );
}

#[test]
fn forced_config_records_its_mode_and_consumed_bytes() {
    let _test_lock = TEST_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let fixture = TestDirectory::new();
    let config_path = fixture.path().join("forced.toml");
    fs::write(&config_path, b"[langserver]\ndreamchecker = true\n").expect("write valid config");

    let mut context = Context::default();
    context.enable_source_trace();
    context.force_config(&config_path);

    let trace = context
        .take_source_trace()
        .expect("enabled trace is available");
    let config_path_text = path_text(&config_path);
    assert!(trace.complete);
    assert_eq!(trace.config_mode, "forced");
    assert_eq!(trace.accesses.len(), 1);
    assert_eq!(trace.accesses[0].kind, SourceAccessKind::Read);
    assert_eq!(
        trace.accesses[0].path.as_deref(),
        Some(config_path_text.as_str())
    );
    assert_eq!(
        trace.accesses[0].sha256.as_deref(),
        Some("b272c3c09045bfcbf0ba552483a52ed79dfadd34da1ed84d9cca38256f369be5")
    );
}
