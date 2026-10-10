package model

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func hasherFixture(r *rand.Rand, maxX, maxY, maxZ int, sparse bool) Snapshot {
	snapshot := Snapshot{ProtocolVersion: ProtocolVersion, SchemaVersion: SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), MaxX: maxX, MaxY: maxY, MaxZ: maxZ}
	id := 0
	// Insertion order differs from canonical order so indexes matter.
	for z := maxZ; z >= 1; z-- {
		for x := 1; x <= maxX; x++ {
			for y := maxY; y >= 1; y-- {
				if sparse && r.Intn(3) == 0 {
					continue
				}
				state := TileState{}
				for n := r.Intn(4); n > 0; n-- {
					id++
					state.Prefabs = append(state.Prefabs, PrefabState{StableID: StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", id)), Path: "/obj/thing", Vars: map[string]string{"dir": fmt.Sprint(r.Intn(8)), "name": `"n"`}})
				}
				snapshot.Tiles = append(snapshot.Tiles, Tile{Coord: Coord{X: x, Y: y, Z: z}, State: state})
			}
		}
	}
	return snapshot
}

func tileIndexes(snapshot Snapshot) map[Coord]int {
	indexes := make(map[Coord]int, len(snapshot.Tiles))
	for i, tile := range snapshot.Tiles {
		indexes[tile.Coord] = i
	}
	return indexes
}

func TestCanonicalHasherUpdateMatchesFullHash(t *testing.T) {
	const seed = 20261010
	r := rand.New(rand.NewSource(seed))
	for round := 0; round < 30; round++ {
		snapshot := hasherFixture(r, 1+r.Intn(9), 1+r.Intn(9), 1+r.Intn(3), round%2 == 1)
		hasher, err := NewCanonicalHasher(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		nextID := 1 << 20
		for step := 0; step < 25; step++ {
			snapshot.Tiles = append([]Tile(nil), snapshot.Tiles...)
			var changed []Coord
			for n := 1 + r.Intn(3); n > 0; n-- {
				coord := Coord{X: 1 + r.Intn(snapshot.MaxX), Y: 1 + r.Intn(snapshot.MaxY), Z: 1 + r.Intn(snapshot.MaxZ)}
				nextID++
				state := TileState{Prefabs: []PrefabState{{StableID: StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", nextID)), Path: "/obj/edited", Vars: map[string]string{"step": fmt.Sprint(step)}}}}
				if index, found := tileIndexes(snapshot)[coord]; found {
					snapshot.Tiles[index].State = state
				} else {
					snapshot.Tiles = append(snapshot.Tiles, Tile{Coord: coord, State: state})
				}
				changed = append(changed, coord)
			}
			want, err := snapshot.Hash()
			if err != nil {
				t.Fatal(err)
			}
			next, got, err := hasher.Update(snapshot, tileIndexes(snapshot), changed)
			if err != nil {
				t.Fatal(err)
			}
			if got != want || next.Digest() != want {
				t.Fatalf("seed %d round %d step %d: incremental hash %s, full hash %s", seed, round, step, got, want)
			}
			hasher = next
		}
	}
}

func TestCanonicalHasherUpdateKeepsEarlierHashersUsable(t *testing.T) {
	snapshot := hasherFixture(rand.New(rand.NewSource(1)), 6, 6, 1, false)
	base := newCanonicalHasher(snapshot)
	baseHash := base.Digest()
	edited := snapshot
	edited.Tiles = append([]Tile(nil), snapshot.Tiles...)
	edited.Tiles[0].State = TileState{}
	if _, _, err := base.Update(edited, tileIndexes(edited), []Coord{edited.Tiles[0].Coord}); err != nil {
		t.Fatal(err)
	}
	// A rejected candidate must leave the base hasher describing the base.
	other := snapshot
	other.Tiles = append([]Tile(nil), snapshot.Tiles...)
	other.Tiles[len(other.Tiles)-1].State = TileState{}
	_, got, err := base.Update(other, tileIndexes(other), []Coord{other.Tiles[len(other.Tiles)-1].Coord})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := other.Hash(); got != want || base.Digest() != baseHash {
		t.Fatalf("branching updates interfered: got %s want %s", got, want)
	}
}

func TestCanonicalHasherRejectsMismatchedIndexes(t *testing.T) {
	snapshot := hasherFixture(rand.New(rand.NewSource(2)), 4, 4, 1, false)
	hasher := newCanonicalHasher(snapshot)
	edited := snapshot
	edited.Tiles = append([]Tile(nil), snapshot.Tiles...)
	edited.Tiles[5].State = TileState{}
	stale := tileIndexes(snapshot)
	stale[edited.Tiles[9].Coord] = 2 // wrong position for a later tile
	_, got, err := hasher.Update(edited, stale, []Coord{edited.Tiles[5].Coord})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := edited.Hash(); got != want {
		t.Fatalf("stale index produced %s, want %s", got, want)
	}
}

func TestNilCanonicalHasherValidates(t *testing.T) {
	snapshot := hasherFixture(rand.New(rand.NewSource(3)), 2, 2, 1, false)
	snapshot.Tiles = append(snapshot.Tiles, snapshot.Tiles[0])
	if _, _, err := (*CanonicalHasher)(nil).Update(snapshot, tileIndexes(snapshot), nil); err == nil {
		t.Fatal("nil hasher hashed a snapshot with a duplicate coordinate")
	}
}

// BenchmarkCanonicalHasherSingleTile is one edit at a random position of a
// station-sized level (255x255, four prefabs per tile).
func BenchmarkCanonicalHasherSingleTile(b *testing.B) {
	snapshot := Snapshot{ProtocolVersion: ProtocolVersion, SchemaVersion: SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), MaxX: 255, MaxY: 255, MaxZ: 1}
	id := 0
	for y := 1; y <= 255; y++ {
		for x := 1; x <= 255; x++ {
			state := TileState{}
			for _, path := range []string{"/area/station/hallway", "/turf/open/floor/iron/dark", "/obj/machinery/light/directional/north", "/obj/effect/turf_decal/tile/blue"} {
				id++
				state.Prefabs = append(state.Prefabs, PrefabState{StableID: StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", id)), Path: path, Vars: map[string]string{"dir": "4"}})
			}
			snapshot.Tiles = append(snapshot.Tiles, Tile{Coord: Coord{X: x, Y: y, Z: 1}, State: state})
		}
	}
	indexes := tileIndexes(snapshot)
	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := snapshot.Hash(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("incremental", func(b *testing.B) {
		hasher, err := NewCanonicalHasher(snapshot)
		if err != nil {
			b.Fatal(err)
		}
		r := rand.New(rand.NewSource(4))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			coord := snapshot.Tiles[r.Intn(len(snapshot.Tiles))].Coord
			if _, _, err := hasher.Update(snapshot, indexes, []Coord{coord}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
