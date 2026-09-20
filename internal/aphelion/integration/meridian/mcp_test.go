package meridian

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testMCPTimeout = 10 * time.Second

func TestMCPDiagnosticTotalsAreNotPageCounts(t *testing.T) {
	for _, mode := range []string{"diagnostic-page", "diagnostic-missing-total", "diagnostic-small-total", "diagnostic-bad-severity"} {
		t.Run(mode, func(t *testing.T) {
			client := newTestMCP(t, mode, testMCPTimeout, 64<<10)
			defer closeTestMCP(t, client)
			if _, err := client.ParseEnvironment(context.Background(), "rift", "tgstation.dme"); err != nil {
				t.Fatal(err)
			}
			diagnostics, err := client.CheckErrors(context.Background(), "rift")
			if mode != "diagnostic-page" {
				if err == nil {
					t.Fatal("incomplete or inconsistent diagnostic totals were accepted")
				}
				return
			}
			if err != nil || diagnostics.Count != 1045 {
				t.Fatalf("diagnostics = %d, error = %v; want full total 1045, not page count 1", diagnostics.Count, err)
			}
			if diagnostics.ReturnedCount != 1 || !diagnostics.Truncated || diagnostics.SeverityCounts["error"] != 127 || diagnostics.SeverityCounts["warning"] != 2 || diagnostics.SeverityCounts["hint"] != 916 {
				t.Fatalf("diagnostic page/summary evidence was lost: %+v", diagnostics)
			}
		})
	}
}

func TestMCPNegotiatesAndRequiresParseBeforeInspection(t *testing.T) {
	client := newTestMCP(t, "normal", testMCPTimeout, 64<<10)
	defer closeTestMCP(t, client)

	if _, err := client.InspectMap(context.Background(), "rift", "main-map"); err == nil {
		t.Fatal("InspectMap() before ParseEnvironment error = nil, want ordering error")
	}
	parsed, err := client.ParseEnvironment(context.Background(), "rift", "tgstation.dme")
	if err != nil {
		t.Fatalf("ParseEnvironment() error = %v", err)
	}
	if parsed.StateGeneration != 7 || parsed.MCPVersion != "0.1.0-test" {
		t.Fatalf("ParseEnvironment() = %#v, want generation 7 and negotiated version", parsed)
	}
	mapResult, err := client.InspectMap(context.Background(), "rift", "main-map")
	if err != nil {
		t.Fatalf("InspectMap() error = %v", err)
	}
	if mapResult.StateGeneration != 7 || mapResult.Width != 20 || mapResult.Height != 30 || mapResult.Levels != 2 {
		t.Fatalf("InspectMap() = %#v, want parsed map metadata", mapResult)
	}
	diagnostics, err := client.CheckErrors(context.Background(), "rift")
	if err != nil {
		t.Fatalf("CheckErrors() error = %v", err)
	}
	if diagnostics.StateGeneration != 7 || diagnostics.Count != 1 || diagnostics.ReturnedCount != 1 || diagnostics.Truncated {
		t.Fatalf("CheckErrors() = %#v, want one generation-7 diagnostic", diagnostics)
	}
}

func TestMCPRejectsUnknownRepositoryAndIdentifiers(t *testing.T) {
	client := newTestMCP(t, "normal", testMCPTimeout, 64<<10)
	defer closeTestMCP(t, client)

	if _, err := client.ParseEnvironment(context.Background(), "missing", "tgstation.dme"); err == nil {
		t.Fatal("ParseEnvironment(unknown repository) error = nil")
	}
	if _, err := client.ParseEnvironment(context.Background(), "rift", "../secret.dme"); err == nil {
		t.Fatal("ParseEnvironment(path identifier) error = nil")
	}
}

func TestMCPTimeoutIsBounded(t *testing.T) {
	started := time.Now()
	_, err := NewMCP(context.Background(), testConfig(t, "timeout", 150*time.Millisecond, 64<<10))
	if err == nil {
		t.Fatal("NewMCP() error = nil, want timeout")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("NewMCP() took %v, want bounded timeout", time.Since(started))
	}
}

func TestMCPRejectsOversizedResponse(t *testing.T) {
	_, err := NewMCP(context.Background(), testConfig(t, "oversized", testMCPTimeout, 1024))
	if err == nil || !strings.Contains(err.Error(), "response limit") {
		t.Fatalf("NewMCP() error = %v, want response limit error", err)
	}
}

func TestMCPHandlesProcessExitAndMalformedJSON(t *testing.T) {
	for _, mode := range []string{"exit", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			_, err := NewMCP(context.Background(), testConfig(t, mode, testMCPTimeout, 64<<10))
			if err == nil {
				t.Fatal("NewMCP() error = nil")
			}
		})
	}
}

func TestMCPRedactsRemoteSecrets(t *testing.T) {
	_, err := NewMCP(context.Background(), testConfig(t, "secret-error", testMCPTimeout, 64<<10))
	if err == nil {
		t.Fatal("NewMCP() error = nil")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("NewMCP() error exposed remote secret: %v", err)
	}
}

func newTestMCP(t *testing.T, mode string, timeout time.Duration, maxBytes int64) Client {
	t.Helper()
	client, err := NewMCP(context.Background(), testConfig(t, mode, timeout, maxBytes))
	if err != nil {
		t.Fatalf("NewMCP() error = %v", err)
	}
	return client
}

func closeTestMCP(t *testing.T, client Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func testConfig(t *testing.T, mode string, timeout time.Duration, maxBytes int64) Config {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell process fixture is Windows-only")
	}
	executable, err := exec.LookPath("pwsh.exe")
	if err != nil {
		t.Fatalf("exec.LookPath(pwsh.exe) error = %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tgstation.dme"), []byte("#include \"fixture.dm\"\n"), 0o600); err != nil {
		t.Fatalf("write DME fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "maps"), 0o700); err != nil {
		t.Fatalf("create maps fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "maps", "main.dmm"), []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write map fixture: %v", err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "fake_mcp.ps1"))
	if err != nil {
		t.Fatalf("filepath.Abs(fake_mcp.ps1) error = %v", err)
	}
	return Config{
		Executable: executable,
		Arguments:  []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-File", fixture},
		Environment: map[string]string{
			"FAKE_MCP_MODE": mode,
		},
		Roots: map[string]Repository{
			"rift": {
				Identity: "rift",
				Root:     root,
				DME:      "tgstation.dme",
				Targets:  map[string]string{"main-map": "maps/main.dmm"},
			},
		},
		Timeout:  timeout,
		MaxBytes: maxBytes,
	}
}
