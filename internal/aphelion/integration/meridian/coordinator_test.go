package meridian

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	integrationmanifest "sdmm/internal/aphelion/integration/manifest"
)

type fakeCoordinatorStager struct {
	artifact StagedArtifact
	err      error
}

func (stager fakeCoordinatorStager) Stage(context.Context, integrationmanifest.Manifest, []byte) (StagedArtifact, error) {
	return stager.artifact, stager.err
}

type fakeCoordinatorVerifier struct {
	evidence Evidence
	err      error
}

func (verifier fakeCoordinatorVerifier) Verify(context.Context, integrationmanifest.Manifest, StagedArtifact) (Evidence, error) {
	return verifier.evidence, verifier.err
}

func TestCoordinatorLoadsStagesAndVerifies(t *testing.T) {
	manifest := validCoordinatorManifest()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	artifact := StagedArtifact{ManifestSHA256: strings.Repeat("a", 64)}
	coordinator, err := NewCoordinator(fakeCoordinatorStager{artifact: artifact}, fakeCoordinatorVerifier{evidence: Evidence{MCPVersion: "1.2.3"}})
	if err != nil {
		t.Fatal(err)
	}

	result, err := coordinator.Run(context.Background(), bytes.NewReader(encoded), bytes.NewReader([]byte("candidate")))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitClassification != ExitInspected || result.ManifestSHA256 != artifact.ManifestSHA256 || result.ArtifactSHA256 != manifest.OutputMapSHA256 || result.VerifierVersion != MCPVerifierVersion {
		t.Fatalf("Run() = %+v", result)
	}
}

func TestCoordinatorClassifiesStageAndVerificationFailures(t *testing.T) {
	manifest := validCoordinatorManifest()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	stageFailure, err := NewCoordinator(fakeCoordinatorStager{err: errors.New("stage")}, fakeCoordinatorVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := stageFailure.Run(context.Background(), bytes.NewReader(encoded), bytes.NewReader(nil))
	if err == nil || result.ExitClassification != ExitStageFailed {
		t.Fatalf("Run() = (%+v, %v), want stage failure", result, err)
	}

	verificationFailure, err := NewCoordinator(fakeCoordinatorStager{artifact: StagedArtifact{}}, fakeCoordinatorVerifier{err: errors.New("verify")})
	if err != nil {
		t.Fatal(err)
	}
	result, err = verificationFailure.Run(context.Background(), bytes.NewReader(encoded), bytes.NewReader(nil))
	if err == nil || result.ExitClassification != ExitVerificationFailed {
		t.Fatalf("Run() = (%+v, %v), want verification failure", result, err)
	}
}

func validCoordinatorManifest() integrationmanifest.Manifest {
	return integrationmanifest.Manifest{
		SchemaVersion: integrationmanifest.SupportedSchemaVersion, RepositoryIdentity: "meridian-rift", RepositoryRevision: strings.Repeat("a", 40),
		DMEIdentifier: "tgstation.dme", MapTargetID: "test-map", ProtocolVersion: integrationmanifest.SupportedProtocolVersion,
		EnvironmentSHA256: strings.Repeat("0", 64), InputMapSHA256: strings.Repeat("1", 64), OutputMapSHA256: strings.Repeat("2", 64),
		AcceptedRevision: 1, Producer: integrationmanifest.Tool{Name: "AphelionDMM", Version: "test"},
	}
}
