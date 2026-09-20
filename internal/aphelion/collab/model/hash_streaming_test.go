package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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

// Compatibility oracle: the pre-streaming wire hash, encoded independently into
// one complete buffer. Keep this deliberately simple; it is not production code.
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
	var buffer bytes.Buffer
	number := func(v uint64) {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], v)
		buffer.Write(encoded[:])
	}
	text := func(v string) { number(uint64(len(v))); buffer.WriteString(v) }
	text("apheliondmm.map.v1")
	for _, v := range []int{snapshot.MaxX, snapshot.MaxY, snapshot.MaxZ, len(tiles)} {
		number(uint64(v))
	}
	for _, tile := range tiles {
		for _, v := range []int{tile.Coord.X, tile.Coord.Y, tile.Coord.Z, len(tile.State.Prefabs)} {
			number(uint64(v))
		}
		for _, prefab := range tile.State.Prefabs {
			text(string(prefab.StableID))
			text(prefab.Path)
			var keys []string
			for key := range prefab.Vars {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			number(uint64(len(keys)))
			for _, key := range keys {
				text(key)
				text(prefab.Vars[key])
			}
		}
	}
	digest := sha256.Sum256(buffer.Bytes())
	return hex.EncodeToString(digest[:])
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
