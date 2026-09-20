package meridian

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
)

type fakeVerificationClient struct {
	failAt      string
	calls       []string
	diagnostics *DiagnosticResult
}

func (client *fakeVerificationClient) ParseEnvironment(context.Context, string, string) (EnvironmentResult, error) {
	client.calls = append(client.calls, "parse")
	if client.failAt == "parse" {
		return EnvironmentResult{}, errors.New("parse failed")
	}
	return EnvironmentResult{StateGeneration: 9, MCPVersion: "1.2.3"}, nil
}

func (client *fakeVerificationClient) InspectMap(context.Context, string, string) (MapResult, error) {
	client.calls = append(client.calls, "map")
	if client.failAt == "map" {
		return MapResult{}, errors.New("map failed")
	}
	return MapResult{StateGeneration: 9, MCPVersion: "1.2.3", Width: 2, Height: 3, Levels: 1}, nil
}

func (client *fakeVerificationClient) CheckErrors(context.Context, string) (DiagnosticResult, error) {
	client.calls = append(client.calls, "diagnostics")
	if client.failAt == "diagnostics" {
		return DiagnosticResult{}, errors.New("diagnostics failed")
	}
	if client.diagnostics != nil {
		return *client.diagnostics, nil
	}
	return DiagnosticResult{StateGeneration: 9, MCPVersion: "1.2.3", Count: 0}, nil
}

func TestVerifierPreservesDiagnosticTotalAndPageEvidence(t *testing.T) {
	client := &fakeVerificationClient{diagnostics: &DiagnosticResult{
		StateGeneration: 9, MCPVersion: "1.2.3", Count: 1045, ReturnedCount: 50, Truncated: true,
		SeverityCounts: map[string]uint64{"error": 127, "warning": 2, "hint": 916},
	}}
	verifier, manifest, staged := newVerifyFixture(t, client, fakeAcceptanceRunner{})
	evidence, err := verifier.Verify(context.Background(), manifest, staged)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Diagnostics != 1045 || evidence.DiagnosticsReturned != 50 || !evidence.DiagnosticsTruncated || evidence.DiagnosticSeverityCounts["error"] != 127 {
		t.Fatalf("verification discarded the full diagnostic summary: %+v", evidence)
	}
}

func (*fakeVerificationClient) Close(context.Context) error { return nil }

type fakeAcceptanceRunner struct {
	result AcceptanceResult
	err    error
	wait   bool
}

func (runner fakeAcceptanceRunner) Run(ctx context.Context, _ AcceptanceRequest) (AcceptanceResult, error) {
	if runner.wait {
		<-ctx.Done()
		return AcceptanceResult{}, ctx.Err()
	}
	return runner.result, runner.err
}

func TestVerifierRejectsInvalidStageManifest(t *testing.T) {
	verifier, manifest, staged := newVerifyFixture(t, &fakeVerificationClient{}, fakeAcceptanceRunner{})
	manifest.OutputMapSHA256 = strings.Repeat("f", 64)

	_, err := verifier.Verify(context.Background(), manifest, staged)
	if err == nil || !strings.Contains(err.Error(), "manifest hash") {
		t.Fatalf("Verify() error = %v, want manifest hash rejection", err)
	}
}

func TestVerifierReportsMCPFailure(t *testing.T) {
	client := &fakeVerificationClient{failAt: "map"}
	verifier, manifest, staged := newVerifyFixture(t, client, fakeAcceptanceRunner{})

	_, err := verifier.Verify(context.Background(), manifest, staged)
	if err == nil || !strings.Contains(err.Error(), "inspect staged map") {
		t.Fatalf("Verify() error = %v, want MCP failure", err)
	}
	if strings.Join(client.calls, ",") != "parse,map" {
		t.Fatalf("MCP calls = %v, want parse then map", client.calls)
	}
}

func TestVerifierEnforcesPowerShellTimeout(t *testing.T) {
	verifier, manifest, staged := newVerifyFixture(t, &fakeVerificationClient{}, fakeAcceptanceRunner{wait: true})
	verifier.timeout = 20 * time.Millisecond

	_, err := verifier.Verify(context.Background(), manifest, staged)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Verify() error = %v, want timeout", err)
	}
}

func TestVerifierReportsBuildFailure(t *testing.T) {
	runner := fakeAcceptanceRunner{result: AcceptanceResult{ExitCode: 1, Output: "compile failed"}}
	verifier, manifest, staged := newVerifyFixture(t, &fakeVerificationClient{}, runner)

	_, err := verifier.Verify(context.Background(), manifest, staged)
	if err == nil || !strings.Contains(err.Error(), "exit code 1") {
		t.Fatalf("Verify() error = %v, want build failure", err)
	}
}

func TestVerifierReturnsSuccessfulEvidence(t *testing.T) {
	client := &fakeVerificationClient{}
	runner := fakeAcceptanceRunner{result: AcceptanceResult{ExitCode: 0, Output: "0 errors", EntryPoint: "RIFT_BUILD.cmd"}}
	verifier, manifest, staged := newVerifyFixture(t, client, runner)

	evidence, err := verifier.Verify(context.Background(), manifest, staged)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if evidence.RepositoryRevision != manifest.RepositoryRevision || evidence.OutputMapSHA256 != manifest.OutputMapSHA256 {
		t.Fatalf("evidence identity = %+v", evidence)
	}
	if evidence.MCPVersion != "1.2.3" || evidence.StateGeneration != 9 || evidence.Diagnostics != 0 || evidence.BuildExitCode != 0 {
		t.Fatalf("evidence result = %+v", evidence)
	}
	if strings.Join(client.calls, ",") != "parse,map,diagnostics" {
		t.Fatalf("MCP calls = %v", client.calls)
	}
}

func TestAcceptanceVerifierUsesImmutableStagedArtifact(t *testing.T) {
	t.Run("legitimate stage", func(t *testing.T) {
		client := &fakeVerificationClient{}
		verifier, manifest, staged := newVerifyFixture(t, client, fakeAcceptanceRunner{result: AcceptanceResult{ExitCode: 0}})

		if _, err := verifier.Verify(context.Background(), manifest, staged); err != nil {
			t.Fatalf("Verify() error = %v", err)
		}
		if len(client.calls) == 0 {
			t.Fatal("Verify() did not call MCP")
		}
	})

	t.Run("outside stage root", func(t *testing.T) {
		client := &fakeVerificationClient{}
		verifier, manifest, staged := newVerifyFixture(t, client, fakeAcceptanceRunner{})
		outside := t.TempDir()
		staged.Directory = outside
		staged.MapFile = filepath.Join(outside, "map.dmm")
		staged.ManifestFile = filepath.Join(outside, "manifest.json")

		_, err := verifier.Verify(context.Background(), manifest, staged)
		if err == nil || !strings.Contains(err.Error(), "stage root") {
			t.Fatalf("Verify() error = %v, want stage-root rejection", err)
		}
		if len(client.calls) != 0 {
			t.Fatalf("MCP calls = %v, want none", client.calls)
		}
	})

	t.Run("changed map", func(t *testing.T) {
		client := &fakeVerificationClient{}
		verifier, manifest, staged := newVerifyFixture(t, client, fakeAcceptanceRunner{})
		if err := os.WriteFile(staged.MapFile, []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := verifier.Verify(context.Background(), manifest, staged)
		if err == nil || !strings.Contains(err.Error(), "staged map hash") {
			t.Fatalf("Verify() error = %v, want staged hash rejection", err)
		}
		if len(client.calls) != 0 {
			t.Fatalf("MCP calls = %v, want none", client.calls)
		}
	})

	t.Run("mismatched manifest", func(t *testing.T) {
		client := &fakeVerificationClient{}
		verifier, manifest, staged := newVerifyFixture(t, client, fakeAcceptanceRunner{})
		other := manifest
		other.AcceptedRevision++
		encoded, err := json.Marshal(other)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(staged.ManifestFile, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err = verifier.Verify(context.Background(), manifest, staged)
		if err == nil || !strings.Contains(err.Error(), "manifest") {
			t.Fatalf("Verify() error = %v, want manifest rejection", err)
		}
		if len(client.calls) != 0 {
			t.Fatalf("MCP calls = %v, want none", client.calls)
		}
	})
}

func TestPowerShellAcceptanceRunnerUsesFixedContract(t *testing.T) {
	powerShell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is unavailable")
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "accept.ps1")
	contents := []byte(`param([string]$RepositoryRoot, [string]$StagedMap, [string]$MapTargetID)
if ($RepositoryRoot -ne $env:EXPECTED_ROOT -or $StagedMap -ne $env:EXPECTED_MAP -or $MapTargetID -ne 'test-map') { exit 7 }
Write-Output 'accepted'
`)
	if err := os.WriteFile(script, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(directory, "map.dmm")
	if err := os.WriteFile(staged, []byte("map"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_ROOT", directory)
	t.Setenv("EXPECTED_MAP", staged)
	runner, err := NewPowerShellAcceptanceRunner(PowerShellRunnerConfig{Executable: powerShell, Script: script})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), AcceptanceRequest{RepositoryRoot: directory, StagedMap: staged, MapTargetID: "test-map"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || !strings.Contains(result.Output, "accepted") {
		t.Fatalf("Run() = %+v", result)
	}
}

func newVerifyFixture(t *testing.T, client Client, runner AcceptanceRunner) (*AcceptanceVerifier, integrationmanifest.Manifest, StagedArtifact) {
	t.Helper()
	root := t.TempDir()
	stageRoot := filepath.Join(root, ".aphelion-stages")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tgstation.dme"), []byte("dme"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "_maps"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "_maps", "test.dmm"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents := []byte("candidate")
	manifest := integrationmanifest.Manifest{
		SchemaVersion: 1, RepositoryIdentity: "Meridian-Rift", RepositoryRevision: strings.Repeat("a", 40), DMEIdentifier: "tgstation.dme",
		MapTargetID: "test-map", ProtocolVersion: 1, EnvironmentSHA256: strings.Repeat("0", 64), InputMapSHA256: strings.Repeat("1", 64),
		OutputMapSHA256: hashBytes(contents), AcceptedRevision: 7, Producer: integrationmanifest.Tool{Name: "AphelionDMM", Version: "test"},
	}
	manifestHash, err := integrationmanifest.CanonicalSHA256(manifest)
	if err != nil {
		t.Fatal(err)
	}
	stageDirectory := filepath.Join(stageRoot, manifestHash)
	if err := os.MkdirAll(stageDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := StagedArtifact{
		Directory: stageDirectory, MapFile: filepath.Join(stageDirectory, "map.dmm"),
		ManifestFile: filepath.Join(stageDirectory, "manifest.json"), ManifestSHA256: manifestHash,
	}
	if err := os.WriteFile(staged.MapFile, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged.ManifestFile, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier, err := NewAcceptanceVerifier(VerifierConfig{
		Repository: Repository{Identity: "Meridian-Rift", Root: root, DME: "tgstation.dme", Targets: map[string]string{"test-map": filepath.Join("_maps", "test.dmm")}},
		StageRoot:  stageRoot, EnvironmentSHA256: manifest.EnvironmentSHA256, MCP: client, Runner: runner, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return verifier, manifest, staged
}
