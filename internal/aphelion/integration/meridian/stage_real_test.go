package meridian

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
)

func TestRealMeridianStagingOnly(t *testing.T) {
	executable := os.Getenv("APHELION_MERIDIAN_MCP_REAL")
	repositoryRoot := os.Getenv("APHELION_MERIDIAN_RIFT_ROOT")
	stageRoot := os.Getenv("APHELION_MERIDIAN_STAGE_ROOT")
	if executable == "" || repositoryRoot == "" || stageRoot == "" {
		t.Skip("set APHELION_MERIDIAN_MCP_REAL, APHELION_MERIDIAN_RIFT_ROOT, and APHELION_MERIDIAN_STAGE_ROOT for staging-only acceptance")
	}

	const targetID = "virtual-domains/test-only"
	const targetRelative = "_maps/virtual_domains/test_only.dmm"
	state, err := (GitRepositoryStateReader{}).Read(context.Background(), repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(targetRelative)))
	if err != nil {
		t.Fatal(err)
	}
	dme, err := os.ReadFile(filepath.Join(repositoryRoot, "tgstation.dme"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := integrationmanifest.Manifest{
		SchemaVersion: integrationmanifest.SupportedSchemaVersion, RepositoryIdentity: "meridian-rift", RepositoryRevision: state.Revision,
		DMEIdentifier: "tgstation.dme", MapTargetID: targetID, ProtocolVersion: integrationmanifest.SupportedProtocolVersion,
		EnvironmentSHA256: hashData(dme), InputMapSHA256: hashData(current), OutputMapSHA256: hashData(current), AcceptedRevision: 1,
		Producer: integrationmanifest.Tool{Name: "AphelionDMM", Version: "staging-only-test"},
	}
	stager, err := NewStager(StageConfig{
		Repository: Repository{Identity: "meridian-rift", Root: repositoryRoot, DME: "tgstation.dme", Targets: map[string]string{targetID: targetRelative}},
		StageRoot:  stageRoot, EnvironmentSHA256: manifest.EnvironmentSHA256, AllowDirty: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := stager.Stage(context.Background(), manifest, current)
	if err != nil {
		t.Fatal(err)
	}

	commonRoot := filepath.Dir(filepath.Clean(repositoryRoot))
	dmePath, err := filepath.Rel(commonRoot, filepath.Join(repositoryRoot, "tgstation.dme"))
	if err != nil {
		t.Fatal(err)
	}
	stagePath, err := filepath.Rel(commonRoot, artifact.MapFile)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewMCP(context.Background(), Config{
		Executable:  executable,
		Environment: map[string]string{"MERIDIAN_MCP_MODE": "analysis", "MERIDIAN_MCP_ROOTS": commonRoot},
		Roots: map[string]Repository{"meridian-rift": {
			Identity: "meridian-rift", Root: commonRoot, DME: "tgstation.dme", DMEPath: filepath.ToSlash(dmePath),
			Targets: map[string]string{targetID: filepath.ToSlash(stagePath)},
		}},
		Timeout: 5 * time.Minute, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Close(closeCtx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()
	parsed, err := client.ParseEnvironment(context.Background(), "meridian-rift", "tgstation.dme")
	if err != nil {
		t.Fatal(err)
	}
	mapResult, err := client.InspectMap(context.Background(), "meridian-rift", targetID)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := client.CheckErrors(context.Background(), "meridian-rift")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.StateGeneration != mapResult.StateGeneration || parsed.StateGeneration != diagnostics.StateGeneration {
		t.Fatal("Meridian-MCP state generation changed during staging-only verification")
	}
	evidence := struct {
		Stage                    StagedArtifact    `json:"stage"`
		RepositoryDirty          bool              `json:"repository_dirty"`
		RepositoryRevision       string            `json:"repository_revision"`
		MCPVersion               string            `json:"mcp_version"`
		StateGeneration          uint64            `json:"state_generation"`
		MapDimensions            string            `json:"map_dimensions"`
		Diagnostics              uint64            `json:"diagnostics"`
		DiagnosticsReturned      uint64            `json:"diagnostics_returned"`
		DiagnosticsTruncated     bool              `json:"diagnostics_truncated"`
		DiagnosticSeverityCounts map[string]uint64 `json:"diagnostic_severity_counts,omitempty"`
	}{
		Stage: artifact, RepositoryDirty: state.Dirty, RepositoryRevision: state.Revision,
		MCPVersion: parsed.MCPVersion, StateGeneration: parsed.StateGeneration,
		MapDimensions:       strings.Join([]string{uintString(mapResult.Width), uintString(mapResult.Height), uintString(mapResult.Levels)}, "x"),
		Diagnostics:         diagnostics.Count,
		DiagnosticsReturned: diagnostics.ReturnedCount, DiagnosticsTruncated: diagnostics.Truncated,
		DiagnosticSeverityCounts: diagnostics.SeverityCounts,
	}
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join(stageRoot, artifact.ManifestSHA256+".mcp-evidence.json")
	if err := os.WriteFile(evidencePath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("staging-only evidence: %s", evidencePath)
}

func uintString(value uint64) string {
	return strconv.FormatUint(value, 10)
}
