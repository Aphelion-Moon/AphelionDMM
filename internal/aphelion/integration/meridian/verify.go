package meridian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
)

// Verifier inspects a staged map through bounded Meridian-MCP diagnostics.
type Verifier interface {
	Verify(ctx context.Context, manifest integrationmanifest.Manifest, artifact StagedArtifact) (Evidence, error)
}

// Evidence is the successful inspection result for one immutable stage.
type Evidence struct {
	RepositoryIdentity       string            `json:"repository_identity"`
	RepositoryRevision       string            `json:"repository_revision"`
	MapTargetID              string            `json:"map_target_id"`
	OutputMapSHA256          string            `json:"output_map_sha256"`
	MCPVersion               string            `json:"mcp_version"`
	StateGeneration          uint64            `json:"state_generation"`
	MapWidth                 uint64            `json:"map_width"`
	MapHeight                uint64            `json:"map_height"`
	MapLevels                uint64            `json:"map_levels"`
	Diagnostics              uint64            `json:"diagnostics"`
	DiagnosticsReturned      uint64            `json:"diagnostics_returned"`
	DiagnosticsTruncated     bool              `json:"diagnostics_truncated"`
	DiagnosticSeverityCounts map[string]uint64 `json:"diagnostic_severity_counts,omitempty"`
	Duration                 time.Duration     `json:"duration"`
}

// VerifierConfig freezes the local paths and adapters used by inspection.
type VerifierConfig struct {
	Repository        Repository
	StageRoot         string
	EnvironmentSHA256 string
	MCP               Client
}

// MCPVerifier coordinates fixed MCP parsing, map inspection and diagnostics.
type MCPVerifier struct {
	repository        Repository
	stageRoot         string
	environmentSHA256 string
	mcp               Client
}

// NewMCPVerifier validates and freezes the verification boundary.
func NewMCPVerifier(config VerifierConfig) (*MCPVerifier, error) {
	repository, err := normalizeRepository(config.Repository)
	if err != nil {
		return nil, err
	}
	if config.MCP == nil {
		return nil, fmt.Errorf("MCP client is required")
	}
	if !validSHA256(config.EnvironmentSHA256) {
		return nil, fmt.Errorf("trusted environment hash is invalid")
	}
	stageRoot, err := canonicalExistingDirectory(config.StageRoot, "stage root")
	if err != nil {
		return nil, err
	}
	return &MCPVerifier{
		repository: repository, stageRoot: stageRoot, environmentSHA256: config.EnvironmentSHA256,
		mcp: config.MCP,
	}, nil
}

// Verify validates the immutable stage, parses first, then runs map inspection and diagnostics.
func (verifier *MCPVerifier) Verify(ctx context.Context, manifest integrationmanifest.Manifest, artifact StagedArtifact) (Evidence, error) {
	started := time.Now()
	if err := manifest.Validate(); err != nil {
		return Evidence{}, fmt.Errorf("invalid stage manifest: %w", err)
	}
	if manifest.RepositoryIdentity != verifier.repository.Identity || manifest.DMEIdentifier != verifier.repository.DME {
		return Evidence{}, fmt.Errorf("stage manifest does not match trusted repository configuration")
	}
	if manifest.EnvironmentSHA256 != verifier.environmentSHA256 {
		return Evidence{}, fmt.Errorf("stage environment hash does not match trusted configuration")
	}
	if _, ok := verifier.repository.Targets[manifest.MapTargetID]; !ok {
		return Evidence{}, fmt.Errorf("stage manifest has an unknown map target")
	}
	manifestHash, err := integrationmanifest.CanonicalSHA256(manifest)
	if err != nil {
		return Evidence{}, err
	}
	if artifact.ManifestSHA256 != manifestHash {
		return Evidence{}, fmt.Errorf("staged artifact manifest hash does not match manifest")
	}
	canonicalDirectory, err := canonicalContainedDirectory(verifier.stageRoot, artifact.Directory)
	if err != nil {
		return Evidence{}, err
	}
	expectedDirectory := filepath.Join(verifier.stageRoot, manifestHash)
	if !samePath(canonicalDirectory, expectedDirectory) {
		return Evidence{}, fmt.Errorf("staged artifact directory does not match manifest hash")
	}
	canonicalStage, err := canonicalContainedFile(canonicalDirectory, artifact.MapFile)
	if err != nil {
		return Evidence{}, err
	}
	if !samePath(canonicalStage, filepath.Join(canonicalDirectory, "map.dmm")) {
		return Evidence{}, fmt.Errorf("staged map does not use the immutable artifact path")
	}
	canonicalManifest, err := canonicalContainedFile(canonicalDirectory, artifact.ManifestFile)
	if err != nil {
		return Evidence{}, err
	}
	if !samePath(canonicalManifest, filepath.Join(canonicalDirectory, "manifest.json")) {
		return Evidence{}, fmt.Errorf("staged manifest does not use the immutable artifact path")
	}
	manifestFile, err := os.Open(canonicalManifest)
	if err != nil {
		return Evidence{}, fmt.Errorf("open staged manifest: %w", err)
	}
	stagedManifest, decodeErr := integrationmanifest.Decode(manifestFile)
	closeErr := manifestFile.Close()
	if decodeErr != nil {
		return Evidence{}, fmt.Errorf("read staged manifest: %w", decodeErr)
	}
	if closeErr != nil {
		return Evidence{}, fmt.Errorf("close staged manifest: %w", closeErr)
	}
	if stagedManifest != manifest {
		return Evidence{}, fmt.Errorf("staged manifest does not match trusted manifest")
	}
	contents, err := os.ReadFile(canonicalStage)
	if err != nil {
		return Evidence{}, fmt.Errorf("read staged map: %w", err)
	}
	if hashData(contents) != manifest.OutputMapSHA256 {
		return Evidence{}, fmt.Errorf("staged map hash does not match manifest")
	}

	parse, err := verifier.mcp.ParseEnvironment(ctx, verifier.repository.Identity, verifier.repository.DME)
	if err != nil {
		return Evidence{}, fmt.Errorf("parse Meridian environment: %w", err)
	}
	mapResult, err := verifier.mcp.InspectMap(ctx, verifier.repository.Identity, manifest.MapTargetID)
	if err != nil {
		return Evidence{}, fmt.Errorf("inspect staged map: %w", err)
	}
	diagnostics, err := verifier.mcp.CheckErrors(ctx, verifier.repository.Identity)
	if err != nil {
		return Evidence{}, fmt.Errorf("check Meridian diagnostics: %w", err)
	}
	if parse.StateGeneration == 0 || mapResult.StateGeneration != parse.StateGeneration || diagnostics.StateGeneration != parse.StateGeneration {
		return Evidence{}, fmt.Errorf("Meridian-MCP state generation changed during verification")
	}
	if mapResult.MCPVersion != parse.MCPVersion || diagnostics.MCPVersion != parse.MCPVersion {
		return Evidence{}, fmt.Errorf("Meridian-MCP version changed during verification")
	}

	return Evidence{
		RepositoryIdentity: manifest.RepositoryIdentity, RepositoryRevision: manifest.RepositoryRevision,
		MapTargetID: manifest.MapTargetID, OutputMapSHA256: manifest.OutputMapSHA256,
		MCPVersion: parse.MCPVersion, StateGeneration: parse.StateGeneration,
		MapWidth: mapResult.Width, MapHeight: mapResult.Height, MapLevels: mapResult.Levels,
		Diagnostics:         diagnostics.Count,
		DiagnosticsReturned: diagnostics.ReturnedCount, DiagnosticsTruncated: diagnostics.Truncated,
		DiagnosticSeverityCounts: diagnostics.SeverityCounts,
		Duration:                 time.Since(started),
	}, nil
}

func canonicalContainedFile(root, path string) (string, error) {
	canonical, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve staged map: %w", err)
	}
	canonical, err = filepath.EvalSymlinks(canonical)
	if err != nil {
		return "", fmt.Errorf("resolve staged map: %w", err)
	}
	relative, err := filepath.Rel(root, canonical)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("staged map is outside the configured stage root")
	}
	return filepath.Clean(canonical), nil
}

func canonicalContainedDirectory(root, path string) (string, error) {
	canonical, err := canonicalExistingDirectory(path, "staged artifact directory")
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, canonical)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("staged artifact directory is outside the configured stage root")
	}
	return canonical, nil
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}
