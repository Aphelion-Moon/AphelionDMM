package meridian

import (
	"context"
	"fmt"
	"io"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
)

const (
	// MCPVerifierVersion identifies the evidence contract implemented by this verifier.
	MCPVerifierVersion = "2"
	maxCandidateBytes  = int64(256 << 20)
)

// ExitClassification identifies the stage of the shipped verification flow that completed or failed.
type ExitClassification string

const (
	ExitInspected          ExitClassification = "inspected"
	ExitStageFailed        ExitClassification = "stage_failed"
	ExitVerificationFailed ExitClassification = "verification_failed"
)

// Staging publishes a validated candidate as an immutable artifact.
type Staging interface {
	Stage(ctx context.Context, manifest integrationmanifest.Manifest, candidate []byte) (StagedArtifact, error)
}

// CoordinationResult is the machine-readable result of one shipped stage-and-verify operation.
type CoordinationResult struct {
	ManifestSHA256     string             `json:"manifest_sha256,omitempty"`
	ArtifactSHA256     string             `json:"artifact_sha256,omitempty"`
	VerifierVersion    string             `json:"verifier_version"`
	ExitClassification ExitClassification `json:"exit_classification"`
	Artifact           StagedArtifact     `json:"artifact,omitempty"`
	Evidence           Evidence           `json:"evidence,omitempty"`
}

// Coordinator composes strict manifest loading, immutable staging, and MCP inspection.
type Coordinator struct {
	stager          Staging
	verifierFactory VerifierFactory
}

// VerifierFactory opens verification only after the immutable artifact has been published.
type VerifierFactory func(ctx context.Context, artifact StagedArtifact) (Verifier, func(context.Context) error, error)

// NewCoordinator creates the shipped stage-and-verify composition.
func NewCoordinator(stager Staging, verifier Verifier) (*Coordinator, error) {
	if stager == nil || verifier == nil {
		return nil, fmt.Errorf("stager and verifier are required")
	}
	return NewCoordinatorWithVerifierFactory(stager, func(context.Context, StagedArtifact) (Verifier, func(context.Context) error, error) {
		return verifier, func(context.Context) error { return nil }, nil
	})
}

// NewCoordinatorWithVerifierFactory creates a composition whose verifier is opened after staging.
func NewCoordinatorWithVerifierFactory(stager Staging, factory VerifierFactory) (*Coordinator, error) {
	if stager == nil || factory == nil {
		return nil, fmt.Errorf("stager and verifier factory are required")
	}
	return &Coordinator{stager: stager, verifierFactory: factory}, nil
}

// Run strictly loads one manifest and bounded candidate before staging and verification.
func (coordinator *Coordinator) Run(ctx context.Context, manifestReader io.Reader, candidateReader io.Reader) (CoordinationResult, error) {
	result := CoordinationResult{VerifierVersion: MCPVerifierVersion, ExitClassification: ExitStageFailed}
	manifest, err := integrationmanifest.Decode(manifestReader)
	if err != nil {
		return result, err
	}
	candidate, err := readBoundedCandidate(candidateReader)
	if err != nil {
		return result, err
	}
	result.ArtifactSHA256 = manifest.OutputMapSHA256
	artifact, err := coordinator.stager.Stage(ctx, manifest, candidate)
	if err != nil {
		return result, fmt.Errorf("stage candidate: %w", err)
	}
	result.ManifestSHA256 = artifact.ManifestSHA256
	result.Artifact = artifact
	result.ExitClassification = ExitVerificationFailed
	verifier, closeVerifier, err := coordinator.verifierFactory(ctx, artifact)
	if err != nil {
		return result, fmt.Errorf("open staged artifact verifier: %w", err)
	}
	evidence, verifyErr := verifier.Verify(ctx, manifest, artifact)
	closeErr := closeVerifier(context.WithoutCancel(ctx))
	if verifyErr != nil {
		return result, fmt.Errorf("verify staged artifact: %w", verifyErr)
	}
	if closeErr != nil {
		return result, fmt.Errorf("close staged artifact verifier: %w", closeErr)
	}
	result.Evidence = evidence
	result.ExitClassification = ExitInspected
	return result, nil
}

func readBoundedCandidate(reader io.Reader) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxCandidateBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read candidate map: %w", err)
	}
	if int64(len(contents)) > maxCandidateBytes {
		return nil, fmt.Errorf("candidate map exceeds %d bytes", maxCandidateBytes)
	}
	return contents, nil
}
