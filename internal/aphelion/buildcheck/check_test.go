package buildcheck

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type runnerResponse struct {
	output string
	err    error
}

type fakeRunner struct {
	responses map[string]runnerResponse
	calls     []string
}

func (runner *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := commandKey(name, args...)
	runner.calls = append(runner.calls, key)
	response, ok := runner.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s", key)
	}
	return []byte(response.output), response.err
}

func TestCheckReportsCompatibleToolchain(t *testing.T) {
	t.Parallel()

	runner := compatibleRunner()
	results := Check(context.Background(), runner, testManifest())

	if len(results) != 5 {
		t.Fatalf("Check() returned %d results, want 5", len(results))
	}
	for _, result := range results {
		if !result.OK {
			t.Errorf("Check() result %q failed: %#v", result.Name, result)
		}
	}

	wantCalls := []string{
		commandKey("go", "version"),
		commandKey("rustup", "run", "1.82.0-x86_64-pc-windows-gnu", "rustc", "--version"),
		commandKey("task", "--version"),
		commandKey("golangci-lint", "version"),
		commandKey("gcc", "--version"),
	}
	if strings.Join(runner.calls, "\n") != strings.Join(wantCalls, "\n") {
		t.Fatalf("Check() calls = %q, want %q", runner.calls, wantCalls)
	}
}

func TestCheckRejectsVersionMismatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		command    string
		output     string
		resultName string
	}{
		{name: "go", command: commandKey("go", "version"), output: "go version go1.25.0 windows/amd64", resultName: "go"},
		{name: "rust", command: commandKey("rustup", "run", "1.82.0-x86_64-pc-windows-gnu", "rustc", "--version"), output: "rustc 1.83.0 (test)", resultName: "rust"},
		{name: "task major", command: commandKey("task", "--version"), output: "4.0.0", resultName: "task"},
		{name: "golangci-lint", command: commandKey("golangci-lint", "version"), output: "golangci-lint has version v2.2.0 built with go1.24.0", resultName: "golangci-lint"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runner := compatibleRunner()
			runner.responses[test.command] = runnerResponse{output: test.output}
			result := resultByName(t, Check(context.Background(), runner, testManifest()), test.resultName)
			if result.OK || result.Detail != "version mismatch" {
				t.Fatalf("Check() result = %#v, want version mismatch", result)
			}
		})
	}
}

func TestCheckReportsMissingTool(t *testing.T) {
	t.Parallel()

	runner := compatibleRunner()
	runner.responses[commandKey("gcc", "--version")] = runnerResponse{err: errors.New("executable file not found")}
	result := resultByName(t, Check(context.Background(), runner, testManifest()), "gcc")
	if result.OK || !strings.Contains(result.Detail, "executable file not found") {
		t.Fatalf("Check() result = %#v, want missing-tool error", result)
	}
}

func TestCheckRejectsUnexpectedCommandOutput(t *testing.T) {
	t.Parallel()

	runner := compatibleRunner()
	runner.responses[commandKey("go", "version")] = runnerResponse{output: "not a Go version"}
	result := resultByName(t, Check(context.Background(), runner, testManifest()), "go")
	if result.OK || result.Detail != "unrecognized version output" {
		t.Fatalf("Check() result = %#v, want unrecognized-output error", result)
	}
}

func TestCheckRejectsUnexpectedGCCOutput(t *testing.T) {
	for _, output := range []string{"clang version 15.2.0", "unrelated (GCC) 15.2.0", "gcc (WinLibs) unknown", "gcc (WinLibs)\n15.2.0"} {
		runner := compatibleRunner()
		runner.responses[commandKey("gcc", "--version")] = runnerResponse{output: output}
		result := resultByName(t, Check(context.Background(), runner, testManifest()), "gcc")
		if result.OK || result.Detail != "unrecognized version output" {
			t.Fatalf("GCC banner %q: result = %#v", output, result)
		}
	}
}

func compatibleRunner() *fakeRunner {
	return &fakeRunner{responses: map[string]runnerResponse{
		commandKey("go", "version"): {output: "go version go1.25.13 windows/amd64"},
		commandKey("rustup", "run", "1.82.0-x86_64-pc-windows-gnu", "rustc", "--version"): {output: "rustc 1.82.0 (test)"},
		commandKey("task", "--version"):        {output: "3.53.1"},
		commandKey("golangci-lint", "version"): {output: "golangci-lint has version v2.12.2 built with go1.26.2"},
		commandKey("gcc", "--version"):         {output: "gcc.exe (GCC) 15.2.0"},
	}}
}

func testManifest() Manifest {
	return Manifest{
		Go:           "1.25.13",
		Rust:         "1.82.0",
		RustTarget:   "x86_64-pc-windows-gnu",
		TaskMajor:    3,
		GolangCILint: "2.12.2",
	}
}

func resultByName(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, result := range results {
		if result.Name == name {
			return result
		}
	}
	t.Fatalf("Check() did not return result %q", name)
	return Result{}
}

func commandKey(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), "\x00")
}
