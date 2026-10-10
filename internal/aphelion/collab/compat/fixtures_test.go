package compat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func compatFixture(t *testing.T, version, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "collaboration", "compat", version, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type snapshotFixture struct {
	Snapshot        model.Snapshot `json:"snapshot"`
	ExpectedMapHash string         `json:"expected_map_hash"`
}

func decodeSnapshotFixture(t *testing.T, version string) snapshotFixture {
	t.Helper()
	var fixture snapshotFixture
	if err := json.Unmarshal(compatFixture(t, version, "snapshot.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestV2WireFixtures(t *testing.T) {
	if _, err := protocol.DecodeClient(compatFixture(t, "v2", "accepted-optional-acknowledged-revision.json")); err != nil {
		t.Fatalf("accepted v2 fixture: %v", err)
	}
	if _, err := protocol.DecodeClient(compatFixture(t, "v2", "rejected-changed-semantics.json")); err == nil {
		t.Fatal("changed-semantics fixture was accepted")
	}
}

func TestV2SnapshotFixtureHash(t *testing.T) {
	fixture := decodeSnapshotFixture(t, "v2")
	hash, err := fixture.Snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hash != fixture.ExpectedMapHash {
		t.Fatalf("snapshot hash = %q, want %q", hash, fixture.ExpectedMapHash)
	}
}

// Protocol v1 changed hash semantics in v2 and is retired: v1 wire messages
// and v1 snapshots are refused rather than reinterpreted.
func TestRetiredV1FixturesAreRefused(t *testing.T) {
	if _, err := protocol.DecodeClient(compatFixture(t, "v1", "accepted-optional-acknowledged-revision.json")); err == nil || !strings.Contains(err.Error(), "protocol version is 1") {
		t.Fatalf("v1 join decoded: %v", err)
	}
	v1 := decodeSnapshotFixture(t, "v1")
	if _, err := v1.Snapshot.Hash(); err == nil {
		t.Fatal("v1 snapshot was hashed")
	}
	// The same map content has a different v2 hash: v1 hashes cannot be reused.
	v1.Snapshot.ProtocolVersion = model.ProtocolVersion
	hash, err := v1.Snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hash == v1.ExpectedMapHash {
		t.Fatal("v2 reproduced the v1 hash")
	}
}
