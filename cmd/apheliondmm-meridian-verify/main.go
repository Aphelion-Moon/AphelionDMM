package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
	"sdmm/internal/aphelion/integration/meridian"
)

const (
	defaultMCPTimeout = 5 * time.Minute
)

type repeatedStrings []string

func (values *repeatedStrings) String() string { return strings.Join(*values, " ") }
func (values *repeatedStrings) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type commandResult struct {
	meridian.CoordinationResult
	Error string `json:"error,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("apheliondmm-meridian-verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var mcpArguments repeatedStrings
	repositoryRoot := flags.String("repository-root", "", "trusted map repository checkout root")
	repositoryIdentity := flags.String("repository-identity", "", "trusted logical repository identity")
	dmeIdentifier := flags.String("dme", "", "trusted logical DME identifier")
	mapTargetID := flags.String("map-target-id", "", "trusted logical map target identifier")
	mapTarget := flags.String("map-target", "", "repository-relative source map target")
	stageRoot := flags.String("stage-root", "", "trusted immutable staging root")
	manifestPath := flags.String("manifest", "", "trusted manifest JSON path")
	candidatePath := flags.String("candidate", "", "candidate DMM path")
	mcpExecutable := flags.String("mcp-executable", "", "Meridian-MCP executable")
	flags.Var(&mcpArguments, "mcp-arg", "fixed Meridian-MCP argument; may be repeated")
	allowDirty := flags.Bool("allow-dirty", false, "permit staging from a dirty source checkout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "trailing arguments are not permitted")
		return 2
	}
	required := map[string]string{
		"repository-root": *repositoryRoot, "repository-identity": *repositoryIdentity, "dme": *dmeIdentifier,
		"map-target-id": *mapTargetID, "map-target": *mapTarget, "stage-root": *stageRoot,
		"manifest": *manifestPath, "candidate": *candidatePath, "mcp-executable": *mcpExecutable,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			_, _ = fmt.Fprintf(stderr, "--%s is required\n", name)
			return 2
		}
	}

	ctx := context.Background()
	manifestFile, err := os.Open(*manifestPath)
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, fmt.Errorf("open manifest: %w", err))
	}
	manifest, decodeErr := integrationmanifest.Decode(manifestFile)
	closeErr := manifestFile.Close()
	if decodeErr != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, decodeErr)
	}
	if closeErr != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, closeErr)
	}
	manifestHash, err := integrationmanifest.CanonicalSHA256(manifest)
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, err)
	}
	expectedArtifact := filepath.Join(*stageRoot, manifestHash, "map.dmm")
	commonRoot, err := commonAncestor(*repositoryRoot, expectedArtifact)
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, err)
	}
	dmePath, err := filepath.Rel(commonRoot, filepath.Join(*repositoryRoot, filepath.FromSlash(*dmeIdentifier)))
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, fmt.Errorf("resolve DME path: %w", err))
	}
	stagedMapPath, err := filepath.Rel(commonRoot, expectedArtifact)
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, fmt.Errorf("resolve staged map path: %w", err))
	}
	repository := meridian.Repository{
		Identity: *repositoryIdentity, Root: *repositoryRoot, DME: *dmeIdentifier,
		Targets: map[string]string{*mapTargetID: *mapTarget},
	}
	stager, err := meridian.NewStager(meridian.StageConfig{
		Repository: repository, StageRoot: *stageRoot, EnvironmentSHA256: manifest.EnvironmentSHA256, AllowDirty: *allowDirty,
	})
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, err)
	}
	coordinator, err := meridian.NewCoordinatorWithVerifierFactory(stager, func(factoryCtx context.Context, artifact meridian.StagedArtifact) (meridian.Verifier, func(context.Context) error, error) {
		artifactRelative, relErr := filepath.Rel(commonRoot, artifact.MapFile)
		if relErr != nil {
			return nil, nil, fmt.Errorf("resolve published staged map path: %w", relErr)
		}
		if !strings.EqualFold(filepath.Clean(artifactRelative), filepath.Clean(stagedMapPath)) {
			return nil, nil, fmt.Errorf("published staged map does not match the trusted manifest path")
		}
		mcpClient, openErr := meridian.NewMCP(factoryCtx, meridian.Config{
			Executable: *mcpExecutable, Arguments: mcpArguments,
			Environment: map[string]string{"MERIDIAN_MCP_MODE": "analysis", "MERIDIAN_MCP_ROOTS": commonRoot},
			Roots: map[string]meridian.Repository{*repositoryIdentity: {
				Identity: *repositoryIdentity, Root: commonRoot, DME: *dmeIdentifier, DMEPath: filepath.ToSlash(dmePath),
				Targets: map[string]string{*mapTargetID: filepath.ToSlash(artifactRelative)},
			}},
			Timeout: defaultMCPTimeout,
		})
		if openErr != nil {
			return nil, nil, openErr
		}
		verifier, verifierErr := meridian.NewMCPVerifier(meridian.VerifierConfig{
			Repository: repository, StageRoot: *stageRoot,
			EnvironmentSHA256: manifest.EnvironmentSHA256, MCP: mcpClient,
		})
		if verifierErr != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = mcpClient.Close(closeCtx)
			return nil, nil, verifierErr
		}
		closeClient := func(context.Context) error {
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return mcpClient.Close(closeCtx)
		}
		return verifier, closeClient, nil
	})
	if err != nil {
		return writeFailure(stdout, meridian.ExitVerificationFailed, err)
	}
	manifestFile, err = os.Open(*manifestPath)
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, err)
	}
	defer func() { _ = manifestFile.Close() }()
	candidateFile, err := os.Open(*candidatePath)
	if err != nil {
		return writeFailure(stdout, meridian.ExitStageFailed, err)
	}
	defer func() { _ = candidateFile.Close() }()
	result, err := coordinator.Run(ctx, manifestFile, candidateFile)
	if err != nil {
		return writeCoordinationFailure(stdout, result, err)
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		_, _ = fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	return 0
}

func writeFailure(writer io.Writer, classification meridian.ExitClassification, err error) int {
	return writeCoordinationFailure(writer, meridian.CoordinationResult{
		VerifierVersion: meridian.MCPVerifierVersion, ExitClassification: classification,
	}, err)
}

func writeCoordinationFailure(writer io.Writer, result meridian.CoordinationResult, err error) int {
	if encodeErr := json.NewEncoder(writer).Encode(commandResult{CoordinationResult: result, Error: err.Error()}); encodeErr != nil {
		return 1
	}
	return 1
}

func commonAncestor(paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("at least one path is required")
	}
	ancestor, err := filepath.Abs(paths[0])
	if err != nil {
		return "", err
	}
	ancestor = filepath.Clean(ancestor)
	for _, path := range paths[1:] {
		candidate, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		candidate = filepath.Clean(candidate)
		for {
			relative, relErr := filepath.Rel(ancestor, candidate)
			if relErr == nil && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				break
			}
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				return "", fmt.Errorf("repository and stage root have no safe common ancestor")
			}
			ancestor = parent
		}
	}
	return ancestor, nil
}
