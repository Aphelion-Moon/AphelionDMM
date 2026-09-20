# Static-analysis baseline

Date: 2026-08-24

Toolchain:

- Go 1.24.0
- golangci-lint 2.1.5, built with Go 1.24.0
- Configuration: `.golangci.yml`

The inherited configuration explicitly disabled `errcheck`, `govet`, and `staticcheck`. Before source changes, each analyzer was run independently with unlimited issue reporting:

```powershell
golangci-lint run --enable-only=errcheck --max-issues-per-linter=0 --max-same-issues=0
golangci-lint run --enable-only=govet --max-issues-per-linter=0 --max-same-issues=0
golangci-lint run --enable-only=staticcheck --max-issues-per-linter=0 --max-same-issues=0
```

Recorded baseline:

| Analyzer | Findings | Resolution |
| --- | ---: | --- |
| `errcheck` | 7 | Close results are now explicitly acknowledged while preserving the existing public APIs and behavior. |
| `govet` | 4 | Four line-local exclusions document OpenGL buffer-offset arguments that are intentionally represented as pointer values. |
| `staticcheck` | 7 | Embedded selectors, boolean logic, and parser branches were simplified without changing behavior. |

The original OpenGL exclusions were confined to `internal/platform/gl.go`.
September 20 follow-up: [native renderer buffer offsets](2026-09-20-renderer-buffer-offsets.md)
replaces all four conversions with the pinned binding's integer-offset entry
points and removes those exclusions. Native pixel readback and window-package
race checks pass without manufacturing Go pointers from GPU buffer offsets.

Final gate:

```text
golangci-lint run --max-issues-per-linter=0 --max-same-issues=0
0 issues.
```
