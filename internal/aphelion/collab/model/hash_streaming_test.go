package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestHashMatchesBufferedCanonicalEncoding(t *testing.T) {
	random := rand.New(rand.NewSource(90210))
	for _, length := range []int{0, 1, 7, 63, 64, 2047, 2048, 2049, 4095, 4096, 4097, 8192, 65537} {
		for range 20 {
			snapshot := fixtureSnapshot()
			prefab := &snapshot.Tiles[0].State.Prefabs[0]
			prefab.Vars["opaque"] = strings.Repeat("x", length) + "\x00雪"
			prefab.Vars["empty"] = ""
			snapshot.MaxZ = 3
			snapshot.Tiles[1].Coord.Z = 3
			random.Shuffle(len(snapshot.Tiles), func(i, j int) { snapshot.Tiles[i], snapshot.Tiles[j] = snapshot.Tiles[j], snapshot.Tiles[i] })
			got, err := snapshot.Hash()
			if err != nil {
				t.Fatal(err)
			}
			if want := bufferedCanonicalHash(snapshot); got != want {
				t.Fatalf("canonical bytes changed at value length %d: %s != %s", length, got, want)
			}
		}
	}
}

// Compatibility oracle: the protocol v2 map hash as the design spec states it,
// each level encoded independently into one complete buffer. Keep this
// deliberately simple; it is not production code.
func bufferedCanonicalHash(snapshot Snapshot) string {
	tiles := append([]Tile(nil), snapshot.Tiles...)
	sort.Slice(tiles, func(i, j int) bool {
		a, b := tiles[i].Coord, tiles[j].Coord
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	number := func(buffer *bytes.Buffer, v uint64) {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], v)
		buffer.Write(encoded[:])
	}
	text := func(buffer *bytes.Buffer, v string) { number(buffer, uint64(len(v))); buffer.WriteString(v) }
	tileDigest := func(tile Tile) [32]byte {
		var buffer bytes.Buffer
		text(&buffer, "apheliondmm.tile.v2")
		for _, v := range []int{tile.Coord.X, tile.Coord.Y, tile.Coord.Z, len(tile.State.Prefabs)} {
			number(&buffer, uint64(v))
		}
		for _, prefab := range tile.State.Prefabs {
			text(&buffer, string(prefab.StableID))
			text(&buffer, prefab.Path)
			var keys []string
			for key := range prefab.Vars {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			number(&buffer, uint64(len(keys)))
			for _, key := range keys {
				text(&buffer, key)
				text(&buffer, prefab.Vars[key])
			}
		}
		return sha256.Sum256(buffer.Bytes())
	}
	type row struct {
		z, y   int
		digest [32]byte
	}
	var rows []row
	for start := 0; start < len(tiles); {
		end := start
		for end < len(tiles) && tiles[end].Coord.Z == tiles[start].Coord.Z && tiles[end].Coord.Y == tiles[start].Coord.Y {
			end++
		}
		var buffer bytes.Buffer
		text(&buffer, "apheliondmm.row.v2")
		number(&buffer, uint64(tiles[start].Coord.Z))
		number(&buffer, uint64(tiles[start].Coord.Y))
		number(&buffer, uint64(end-start))
		for _, tile := range tiles[start:end] {
			digest := tileDigest(tile)
			buffer.Write(digest[:])
		}
		rows = append(rows, row{tiles[start].Coord.Z, tiles[start].Coord.Y, sha256.Sum256(buffer.Bytes())})
		start = end
	}
	var buffer bytes.Buffer
	text(&buffer, "apheliondmm.map.v2")
	for _, v := range []int{snapshot.MaxX, snapshot.MaxY, snapshot.MaxZ, len(tiles), len(rows)} {
		number(&buffer, uint64(v))
	}
	for _, row := range rows {
		number(&buffer, uint64(row.z))
		number(&buffer, uint64(row.y))
		buffer.Write(row.digest[:])
	}
	digest := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(digest[:])
}

// The parallel full hash must equal the sequential oracle at sizes on both
// sides of the threshold, including sparse rows and several levels.
func TestParallelHashMatchesOracle(t *testing.T) {
	for _, size := range []int{1, parallelTileThreshold - 1, parallelTileThreshold * 3} {
		snapshot := hashVariableFixture(size, true)
		snapshot.MaxX, snapshot.MaxY, snapshot.MaxZ = 128, size/128+1, 2
		for i := range snapshot.Tiles {
			snapshot.Tiles[i].Coord = Coord{X: i%128 + 1, Y: i/128 + 1, Z: i%2 + 1}
		}
		got, err := snapshot.Hash()
		if err != nil {
			t.Fatal(err)
		}
		if want := bufferedCanonicalHash(snapshot); got != want {
			t.Fatalf("%d tiles: parallel hash %s, oracle %s", size, got, want)
		}
	}
}

func TestCanonicalEncodingMemoryIsBounded(t *testing.T) {
	// A reused input string must not cause a proportional temporary hash buffer.
	// Alloc counts alone would miss bytes.Buffer capacity growth.
	input := strings.Repeat("x", 1<<20)
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			encoder := canonicalEncoder{digest: sha256.New()}
			writeString(&encoder, input)
			encoder.flush()
		}
	})
	if result.AllocedBytesPerOp() > 16<<10 {
		t.Fatalf("encoding allocated %d bytes/op for a reused 1 MiB value", result.AllocedBytesPerOp())
	}
}

func hashVariableFixture(count int, withVars bool) Snapshot {
	snapshot := fixtureSnapshot()
	snapshot.MaxX = count
	snapshot.Tiles = make([]Tile, count)
	for i := range snapshot.Tiles {
		prefab := PrefabState{
			StableID: StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)),
			Path:     "/obj/test",
		}
		if withVars {
			prefab.Vars = map[string]string{"dir": "2", "pixel_x": "-1", "pixel_y": "3"}
		}
		snapshot.Tiles[i] = Tile{Coord: Coord{X: i + 1, Y: 1, Z: 1}, State: TileState{Prefabs: []PrefabState{prefab}}}
	}
	return snapshot
}

func TestHashVariableScratchIsBounded(t *testing.T) {
	allocations := func(withVars bool) float64 {
		snapshot := hashVariableFixture(1024, withVars)
		want := bufferedCanonicalHash(snapshot)
		return testing.AllocsPerRun(5, func() {
			got, err := snapshot.Hash()
			if err != nil || got != want {
				t.Fatalf("canonical hash mismatch: got %s, want %s, err %v", got, want, err)
			}
		})
	}
	// Validation and coordinate sorting allocate in both cases. Variable ordering
	// must not add a scratch allocation for every prefab in a large map.
	if extra := allocations(true) - allocations(false); extra > 16 {
		t.Fatalf("variable ordering added %.0f allocations for 1024 prefabs", extra)
	}
}

func BenchmarkSnapshotHashVariables(b *testing.B) {
	snapshot := hashVariableFixture(1024, true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := snapshot.Hash(); err != nil {
			b.Fatal(err)
		}
	}
}
