package buildcheck

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var (
	goVersionPattern       = regexp.MustCompile(`^go version go([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)
	rustVersionPattern     = regexp.MustCompile(`^rustc ([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)
	taskVersionPattern     = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:\s|$)`)
	golangCIVersionPattern = regexp.MustCompile(`^golangci-lint has version v?([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)
	gccVersionPattern      = regexp.MustCompile(`(?i)^(?:[^\s]+-)?gcc(?:\.exe)?\s+\([^\r\n)]+\)\s+([0-9]+(?:\.[0-9]+)+)(?:\s|$)`)
)

// Runner executes one fixed environment probe without involving a shell.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner executes environment probes as child processes.
type ExecRunner struct{}

// Run executes one command directly and returns its combined output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Result records the expectation and observed value for one tool.
type Result struct {
	Name     string
	Expected string
	Actual   string
	OK       bool
	Detail   string
}

// Check runs the repository's fixed, non-mutating environment probes.
func Check(ctx context.Context, runner Runner, manifest Manifest) []Result {
	return []Result{
		checkExactVersion(ctx, runner, "go", "go"+manifest.Go, goVersionPattern, "go", "version"),
		checkExactVersion(ctx, runner, "rust", manifest.Rust, rustVersionPattern, "rustup", "run", manifest.Rust+"-"+manifest.RustTarget, "rustc", "--version"),
		checkTaskVersion(ctx, runner, manifest.TaskMajor),
		checkExactVersion(ctx, runner, "golangci-lint", manifest.GolangCILint, golangCIVersionPattern, "golangci-lint", "version"),
		checkGCC(ctx, runner),
	}
}

func checkExactVersion(ctx context.Context, runner Runner, name, expected string, pattern *regexp.Regexp, command string, args ...string) Result {
	result := runProbe(ctx, runner, name, expected, command, args...)
	if result.Detail != "" {
		return result
	}

	matches := pattern.FindStringSubmatch(result.Actual)
	if len(matches) != 2 {
		result.Detail = "unrecognized version output"
		return result
	}
	result.Actual = matches[1]
	if name == "go" {
		result.Actual = "go" + result.Actual
	}
	if result.Actual != expected {
		result.Detail = "version mismatch"
		return result
	}
	result.OK = true
	return result
}

func checkTaskVersion(ctx context.Context, runner Runner, expectedMajor int) Result {
	result := runProbe(ctx, runner, "task", fmt.Sprintf("major %d", expectedMajor), "task", "--version")
	if result.Detail != "" {
		return result
	}

	matches := taskVersionPattern.FindStringSubmatch(result.Actual)
	if len(matches) != 4 {
		result.Detail = "unrecognized version output"
		return result
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		result.Detail = "unrecognized version output"
		return result
	}
	if major != expectedMajor {
		result.Detail = "version mismatch"
		return result
	}
	result.OK = true
	return result
}

func checkGCC(ctx context.Context, runner Runner) Result {
	result := runProbe(ctx, runner, "gcc", "available", "gcc", "--version")
	if result.Detail != "" {
		return result
	}

	firstLine, _, _ := strings.Cut(result.Actual, "\n")
	matches := gccVersionPattern.FindStringSubmatch(strings.TrimSpace(firstLine))
	if len(matches) != 2 {
		result.Detail = "unrecognized version output"
		return result
	}
	result.Actual = matches[1]
	result.OK = true
	return result
}

func runProbe(ctx context.Context, runner Runner, name, expected, command string, args ...string) Result {
	output, err := runner.Run(ctx, command, args...)
	result := Result{
		Name:     name,
		Expected: expected,
		Actual:   strings.TrimSpace(string(output)),
	}
	if err != nil {
		result.Detail = fmt.Sprintf("command failed: %v", err)
	}
	return result
}
