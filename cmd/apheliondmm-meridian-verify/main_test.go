package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
	"sdmm/internal/aphelion/integration/meridian"
)

func TestRunRejectsTrailingArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unexpected"}, &stdout, &stderr); code == 0 {
		t.Fatalf("run() code = %d, want non-zero", code)
	}
}

func TestRunRejectsRemovedBuildOptions(t *testing.T) {
	for _, option := range []string{"acceptance-executable", "acceptance-script", "acceptance-root"} {
		t.Run(option, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"--" + option, "unused"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("removed option returned %d: %s", code, stderr.String())
			}
		})
	}
}

func TestRunStagesAndVerifiesWithFakeMCP(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell MCP fixture is Windows-only")
	}
	pwsh, err := exec.LookPath("pwsh.exe")
	if err != nil {
		t.Skip("pwsh.exe is unavailable")
	}
	root := t.TempDir()
	dme := []byte("#include \"fixture.dm\"\n")
	mapBytes := []byte("candidate map")
	if err := os.WriteFile(filepath.Join(root, "tgstation.dme"), dme, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "maps"), 0o700); err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(root, "maps", "main.dmm")
	if err := os.WriteFile(mapPath, mapBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "fixture@example.invalid")
	runGit(t, root, "config", "user.name", "Fixture")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "fixture")
	revision := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	manifest := integrationmanifest.Manifest{
		SchemaVersion: integrationmanifest.SupportedSchemaVersion, RepositoryIdentity: "meridian-rift", RepositoryRevision: revision,
		DMEIdentifier: "tgstation.dme", MapTargetID: "main-map", ProtocolVersion: integrationmanifest.SupportedProtocolVersion,
		EnvironmentSHA256: hashCommandBytes(dme), InputMapSHA256: hashCommandBytes(mapBytes), OutputMapSHA256: hashCommandBytes(mapBytes),
		AcceptedRevision: 1, Producer: integrationmanifest.Tool{Name: "AphelionDMM", Version: "test"},
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "internal", "aphelion", "integration", "meridian", "testdata", "fake_mcp.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	stageRoot := filepath.Join(t.TempDir(), "stages")
	args := []string{
		"--repository-root", root, "--repository-identity", "meridian-rift", "--dme", "tgstation.dme",
		"--map-target-id", "main-map", "--map-target", filepath.Join("maps", "main.dmm"), "--stage-root", stageRoot,
		"--manifest", manifestPath, "--candidate", mapPath, "--mcp-executable", pwsh,
		"--mcp-arg=-NoLogo", "--mcp-arg=-NoProfile", "--mcp-arg=-NonInteractive", "--mcp-arg=-File", "--mcp-arg=" + fixture,
	}
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("run() code = %d, stderr = %s, stdout = %s", code, stderr.String(), stdout.String())
	}
	var result meridian.CoordinationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, stdout.String())
	}
	if result.ExitClassification != meridian.ExitInspected || result.VerifierVersion != "2" || result.ArtifactSHA256 != manifest.OutputMapSHA256 {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(stdout.String(), "build_entry_point") || strings.Contains(stdout.String(), "build_exit_code") {
		t.Fatal("inspection evidence still claims a build result")
	}
	unchanged, err := os.ReadFile(mapPath)
	if err != nil || !bytes.Equal(unchanged, mapBytes) {
		t.Fatalf("inspection changed the source map: %v", err)
	}
}

func runGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git.exe", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return string(output)
}

func hashCommandBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
