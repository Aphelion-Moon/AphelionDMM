package meridian

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
)

const (
	defaultAcceptanceTimeout = 10 * time.Minute
	defaultAcceptanceBytes   = int64(4 << 20)
)

// Verifier accepts a staged map only through bounded diagnostics and a fixed acceptance gate.
type Verifier interface {
	Verify(ctx context.Context, manifest integrationmanifest.Manifest, artifact StagedArtifact) (Evidence, error)
}

// AcceptanceRunner invokes one trusted repository-owned PowerShell acceptance entry point.
type AcceptanceRunner interface {
	Run(ctx context.Context, request AcceptanceRequest) (AcceptanceResult, error)
}

// AcceptanceRequest contains only paths and identifiers resolved from trusted local configuration.
type AcceptanceRequest struct {
	RepositoryRoot string
	StagedMap      string
	MapTargetID    string
}

// AcceptanceResult records the configured entry point result without interpreting its text.
type AcceptanceResult struct {
	EntryPoint string
	ExitCode   int
	Output     string
}

// Evidence is the durable successful verification result for one immutable stage.
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
	BuildEntryPoint          string            `json:"build_entry_point"`
	BuildExitCode            int               `json:"build_exit_code"`
	Duration                 time.Duration     `json:"duration"`
}

// VerifierConfig freezes the local paths and adapters used by acceptance.
type VerifierConfig struct {
	Repository        Repository
	AcceptanceRoot    string
	StageRoot         string
	EnvironmentSHA256 string
	MCP               Client
	Runner            AcceptanceRunner
	Timeout           time.Duration
}

// AcceptanceVerifier coordinates fixed MCP diagnostics and PowerShell acceptance.
type AcceptanceVerifier struct {
	repository        Repository
	acceptanceRoot    string
	stageRoot         string
	environmentSHA256 string
	mcp               Client
	runner            AcceptanceRunner
	timeout           time.Duration
}

// NewAcceptanceVerifier validates and freezes the verification boundary.
func NewAcceptanceVerifier(config VerifierConfig) (*AcceptanceVerifier, error) {
	repository, err := normalizeRepository(config.Repository)
	if err != nil {
		return nil, err
	}
	if config.MCP == nil || config.Runner == nil {
		return nil, fmt.Errorf("MCP client and acceptance runner are required")
	}
	if !validSHA256(config.EnvironmentSHA256) {
		return nil, fmt.Errorf("trusted environment hash is invalid")
	}
	stageRoot, err := canonicalExistingDirectory(config.StageRoot, "stage root")
	if err != nil {
		return nil, err
	}
	acceptanceRoot := config.AcceptanceRoot
	if acceptanceRoot == "" {
		acceptanceRoot = repository.Root
	}
	acceptanceRoot, err = canonicalExistingDirectory(acceptanceRoot, "acceptance root")
	if err != nil {
		return nil, err
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultAcceptanceTimeout
	}
	return &AcceptanceVerifier{
		repository: repository, acceptanceRoot: acceptanceRoot, stageRoot: stageRoot, environmentSHA256: config.EnvironmentSHA256,
		mcp: config.MCP, runner: config.Runner, timeout: timeout,
	}, nil
}

// Verify validates the immutable stage, parses first, then runs map, diagnostic, and build gates.
func (verifier *AcceptanceVerifier) Verify(ctx context.Context, manifest integrationmanifest.Manifest, artifact StagedArtifact) (Evidence, error) {
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

	buildCtx, cancel := context.WithTimeout(ctx, verifier.timeout)
	defer cancel()
	result, err := verifier.runner.Run(buildCtx, AcceptanceRequest{
		RepositoryRoot: verifier.acceptanceRoot, StagedMap: canonicalStage, MapTargetID: manifest.MapTargetID,
	})
	if buildCtx.Err() != nil {
		return Evidence{}, fmt.Errorf("PowerShell acceptance timed out")
	}
	if err != nil {
		return Evidence{}, fmt.Errorf("run PowerShell acceptance: %w", err)
	}
	if result.ExitCode != 0 {
		return Evidence{}, fmt.Errorf("PowerShell acceptance failed with exit code %d", result.ExitCode)
	}
	return Evidence{
		RepositoryIdentity: manifest.RepositoryIdentity, RepositoryRevision: manifest.RepositoryRevision,
		MapTargetID: manifest.MapTargetID, OutputMapSHA256: manifest.OutputMapSHA256,
		MCPVersion: parse.MCPVersion, StateGeneration: parse.StateGeneration,
		MapWidth: mapResult.Width, MapHeight: mapResult.Height, MapLevels: mapResult.Levels,
		Diagnostics: diagnostics.Count, BuildEntryPoint: result.EntryPoint, BuildExitCode: result.ExitCode,
		DiagnosticsReturned: diagnostics.ReturnedCount, DiagnosticsTruncated: diagnostics.Truncated,
		DiagnosticSeverityCounts: diagnostics.SeverityCounts,
		Duration:                 time.Since(started),
	}, nil
}

// PowerShellRunnerConfig names one trusted script and bounds its execution and output.
type PowerShellRunnerConfig struct {
	Executable string
	Script     string
	MaxBytes   int64
}

// PowerShellAcceptanceRunner runs a fixed script without a shell command string.
type PowerShellAcceptanceRunner struct {
	executable string
	script     string
	maxBytes   int64
}

// NewPowerShellAcceptanceRunner freezes the executable and script paths.
func NewPowerShellAcceptanceRunner(config PowerShellRunnerConfig) (*PowerShellAcceptanceRunner, error) {
	if config.Executable == "" {
		config.Executable = "powershell.exe"
	}
	script, err := filepath.Abs(config.Script)
	if err != nil || config.Script == "" {
		return nil, fmt.Errorf("resolve PowerShell acceptance script")
	}
	script, err = filepath.EvalSymlinks(script)
	if err != nil {
		return nil, fmt.Errorf("resolve PowerShell acceptance script: %w", err)
	}
	maxBytes := config.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultAcceptanceBytes
	}
	return &PowerShellAcceptanceRunner{executable: config.Executable, script: filepath.Clean(script), maxBytes: maxBytes}, nil
}

// Run invokes the configured script with a fixed named-argument contract.
func (runner *PowerShellAcceptanceRunner) Run(ctx context.Context, request AcceptanceRequest) (AcceptanceResult, error) {
	command := exec.Command(runner.executable,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", runner.script,
		"-RepositoryRoot", request.RepositoryRoot, "-StagedMap", request.StagedMap, "-MapTargetID", request.MapTargetID,
	)
	output := &boundedBuffer{remaining: runner.maxBytes}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		return AcceptanceResult{}, err
	}
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		terminateCommand(command)
		<-done
		return AcceptanceResult{}, ctx.Err()
	}
	result := AcceptanceResult{EntryPoint: runner.script, ExitCode: command.ProcessState.ExitCode(), Output: output.String()}
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return AcceptanceResult{}, err
		}
	}
	if output.exceeded {
		return AcceptanceResult{}, fmt.Errorf("PowerShell acceptance output limit exceeded")
	}
	return result, nil
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	remaining int64
	exceeded  bool
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if int64(len(value)) > buffer.remaining {
		value = value[:max(buffer.remaining, 0)]
		buffer.exceeded = true
	}
	written, err := buffer.buffer.Write(value)
	buffer.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	return original, nil
}

func (buffer *boundedBuffer) String() string { return buffer.buffer.String() }

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

func terminateCommand(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		killer := exec.Command("taskkill.exe", "/PID", fmt.Sprint(command.Process.Pid), "/T", "/F")
		killer.Stdout = io.Discard
		killer.Stderr = io.Discard
		_ = killer.Run()
	}
	_ = command.Process.Kill()
}
