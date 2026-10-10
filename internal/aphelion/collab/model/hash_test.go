package model

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testDocumentID      = DocumentID("01890f3e-7b5c-7abc-8def-0123456789ab")
	testEnvironmentHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestSnapshotHashIsCanonical(t *testing.T) {
	t.Parallel()

	left := fixtureSnapshot()
	right := fixtureSnapshot()
	right.Tiles[0], right.Tiles[1] = right.Tiles[1], right.Tiles[0]
	right.Tiles[1].State.Prefabs[0].Vars = map[string]string{
		"pixel_x": "-1",
		"dir":     "2",
	}

	want, err := left.Hash()
	if err != nil {
		t.Fatalf("hash left snapshot: %v", err)
	}
	got, err := right.Hash()
	if err != nil {
		t.Fatalf("hash right snapshot: %v", err)
	}
	if got != want {
		t.Fatalf("canonical hashes differ: got %q, want %q", got, want)
	}

	for range 100 {
		repeated, repeatErr := right.Hash()
		if repeatErr != nil {
			t.Fatalf("repeat hash: %v", repeatErr)
		}
		if repeated != want {
			t.Fatalf("repeated hash changed: got %q, want %q", repeated, want)
		}
	}
}

func TestUUIDv7GenerationIsCanonicalUniqueAndMonotonic(t *testing.T) {
	const count = 10_000
	seen := make(map[DocumentID]struct{}, count)
	var previous DocumentID
	for range count {
		id, err := NewDocumentID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("generated ID %q is not canonical UUID text", id)
		}
		if id[14] != '7' {
			t.Fatalf("generated ID %q has version nibble %q", id, id[14])
		}
		if !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatalf("generated ID %q has RFC variant nibble %q", id, id[19])
		}
		if err := id.Validate(); err != nil {
			t.Fatalf("generated ID %q is invalid: %v", id, err)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("generated duplicate ID %q", id)
		}
		seen[id] = struct{}{}
		if previous != "" && id < previous {
			t.Fatalf("generated IDs decreased lexically: %q then %q", previous, id)
		}
		previous = id
	}
}

func TestUUIDv7ValidationRejectsWrongVersionAndVariant(t *testing.T) {
	for _, id := range []DocumentID{
		"01890f3e-7b5c-6abc-8def-0123456789ab",
		"01890f3e-7b5c-7abc-7def-0123456789ab",
	} {
		if err := id.Validate(); err == nil {
			t.Fatalf("DocumentID(%q).Validate() error = nil", id)
		}
	}
}

func TestSnapshotHashPreservesPrefabOrder(t *testing.T) {
	t.Parallel()

	left := fixtureSnapshot()
	right := fixtureSnapshot()
	right.Tiles[0].State.Prefabs[0], right.Tiles[0].State.Prefabs[1] =
		right.Tiles[0].State.Prefabs[1], right.Tiles[0].State.Prefabs[0]

	leftHash, err := left.Hash()
	if err != nil {
		t.Fatalf("hash left snapshot: %v", err)
	}
	rightHash, err := right.Hash()
	if err != nil {
		t.Fatalf("hash right snapshot: %v", err)
	}
	if leftHash == rightHash {
		t.Fatalf("prefab order did not affect hash %q", leftHash)
	}
}

func TestSnapshotHashLengthPrefixesFields(t *testing.T) {
	t.Parallel()

	left := fixtureSnapshot()
	right := fixtureSnapshot()
	left.Tiles[0].State.Prefabs[0].Vars = map[string]string{"a": "bc"}
	right.Tiles[0].State.Prefabs[0].Vars = map[string]string{"ab": "c"}

	leftHash, err := left.Hash()
	if err != nil {
		t.Fatalf("hash left snapshot: %v", err)
	}
	rightHash, err := right.Hash()
	if err != nil {
		t.Fatalf("hash right snapshot: %v", err)
	}
	if leftHash == rightHash {
		t.Fatalf("field boundaries did not affect hash %q", leftHash)
	}
}

func TestSnapshotHashExcludesSessionMetadata(t *testing.T) {
	t.Parallel()

	left := fixtureSnapshot()
	right := fixtureSnapshot()
	right.DocumentID = DocumentID("01890f3e-7b5c-7abc-8def-0123456789ba")
	right.Revision = 99
	right.EnvironmentHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	leftHash, err := left.Hash()
	if err != nil {
		t.Fatalf("hash left snapshot: %v", err)
	}
	rightHash, err := right.Hash()
	if err != nil {
		t.Fatalf("hash right snapshot: %v", err)
	}
	if rightHash != leftHash {
		t.Fatalf("session metadata affected map hash: got %q, want %q", rightHash, leftHash)
	}
}

func TestSnapshotHashRejectsInvalidState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Snapshot)
		wantErr string
	}{
		{
			name: "invalid dimensions",
			mutate: func(snapshot *Snapshot) {
				snapshot.MaxX = 0
			},
			wantErr: "dimensions",
		},
		{
			name: "coordinate below one",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tiles[0].Coord.X = 0
			},
			wantErr: "coordinate",
		},
		{
			name: "coordinate beyond dimensions",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tiles[0].Coord.Z = 2
			},
			wantErr: "coordinate",
		},
		{
			name: "duplicate coordinate",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tiles[1].Coord = snapshot.Tiles[0].Coord
			},
			wantErr: "duplicate coordinate",
		},
		{
			name: "duplicate stable id",
			mutate: func(snapshot *Snapshot) {
				snapshot.Tiles[1].State.Prefabs = []PrefabState{{
					StableID: snapshot.Tiles[0].State.Prefabs[0].StableID,
					Path:     "/obj/foo3",
				}}
			},
			wantErr: "duplicate stable id",
		},
		{
			name: "malformed environment hash",
			mutate: func(snapshot *Snapshot) {
				snapshot.EnvironmentHash = "ABC"
			},
			wantErr: "environment hash",
		},
		{
			name: "malformed document id",
			mutate: func(snapshot *Snapshot) {
				snapshot.DocumentID = "not-a-uuid"
			},
			wantErr: "document id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := fixtureSnapshot()
			test.mutate(&snapshot)
			_, err := snapshot.Hash()
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Hash() error = %v, want error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestSnapshotValidateRejectsUnsafeDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		maxX    int
		maxY    int
		maxZ    int
		wantErr string
	}{
		{name: "dimension limit", maxX: 4097, maxY: 1, maxZ: 1, wantErr: "maximum"},
		{name: "cell limit", maxX: 4096, maxY: 4096, maxZ: 2, wantErr: "cell count"},
		{name: "integer overflow", maxX: math.MaxInt, maxY: 2, maxZ: 1, wantErr: "maximum"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := fixtureSnapshot()
			snapshot.MaxX = test.maxX
			snapshot.MaxY = test.maxY
			snapshot.MaxZ = test.maxZ
			if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestCanonicalMapGolden(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "canonical-map.json"))
	if err != nil {
		t.Fatalf("read golden snapshot: %v", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode golden snapshot: %v", err)
	}

	got, err := snapshot.Hash()
	if err != nil {
		t.Fatalf("hash golden snapshot: %v", err)
	}
	const want = "12aaa1816719e155615275d8d53aa18252d356b369ed12e1dc994b65285fa839"
	if oracle := bufferedCanonicalHash(snapshot); oracle != want {
		t.Fatalf("golden hash %q disagrees with the specification oracle %q", want, oracle)
	}
	if got != want {
		t.Fatalf("golden hash changed: got %q, want %q", got, want)
	}
}

func fixtureSnapshot() Snapshot {
	return Snapshot{
		ProtocolVersion: ProtocolVersion,
		SchemaVersion:   SchemaVersion,
		DocumentID:      testDocumentID,
		Revision:        4,
		EnvironmentHash: testEnvironmentHash,
		MaxX:            2,
		MaxY:            1,
		MaxZ:            1,
		Tiles: []Tile{
			{
				Coord: Coord{X: 1, Y: 1, Z: 1},
				State: TileState{Prefabs: []PrefabState{
					{
						StableID: "01890f3e-7b5c-7abc-8def-0123456789ac",
						Path:     "/obj/foo1",
						Vars: map[string]string{
							"dir":     "2",
							"pixel_x": "-1",
						},
					},
					{
						StableID: "01890f3e-7b5c-7abc-8def-0123456789ad",
						Path:     "/obj/foo2",
						Vars:     map[string]string{},
					},
				}},
			},
			{
				Coord: Coord{X: 2, Y: 1, Z: 1},
				State: TileState{Prefabs: []PrefabState{}},
			},
		},
	}
}

// Protocol v1 hashes are retired: a snapshot stamped with another protocol
// version cannot be hashed or applied, so a v1 session is refused.
func TestSnapshotRejectsRetiredProtocolVersion(t *testing.T) {
	snapshot := fixtureSnapshot()
	snapshot.ProtocolVersion = 1
	if _, err := snapshot.Hash(); err == nil || !strings.Contains(err.Error(), "protocol version is 1, want 2") {
		t.Fatalf("v1 snapshot hash error = %v", err)
	}
}
