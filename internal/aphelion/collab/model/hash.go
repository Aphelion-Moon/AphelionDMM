package model

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"runtime"
	"sort"
	"sync"
)

// Protocol v2 canonical map hash. A tile digest covers one tile, a row digest
// covers the tile digests of one (z, y) row in x order, and the map hash covers
// the dimensions and every row digest in (z, y) order. Every integer is an
// unsigned 64-bit big-endian value and every string is length-prefixed; each
// level has its own domain. An edit therefore rehashes only its tiles, their
// rows and the map root. docs/superpowers/specs/2026-08-24-multiplayer-design.md
// specifies the bytes.
const (
	canonicalTileDomain = "apheliondmm.tile.v2"
	canonicalRowDomain  = "apheliondmm.row.v2"
	canonicalMapDomain  = "apheliondmm.map.v2"
)

type digest = [sha256.Size]byte

func (snapshot Snapshot) Hash() (string, error) {
	if err := snapshot.Validate(); err != nil {
		return "", err
	}
	return newCanonicalHasher(snapshot).digest, nil
}

// canonicalTiles returns the snapshot's tiles in canonical (z, y, x) order.
func canonicalTiles(snapshot Snapshot) []Tile {
	tiles := append([]Tile(nil), snapshot.Tiles...)
	sort.Slice(tiles, func(left int, right int) bool {
		return coordLess(tiles[left].Coord, tiles[right].Coord)
	})
	return tiles
}

func coordLess(left, right Coord) bool {
	if left.Z != right.Z {
		return left.Z < right.Z
	}
	if left.Y != right.Y {
		return left.Y < right.Y
	}
	return left.X < right.X
}

// tileHasher computes tile digests, reusing its SHA-256 state and scratch.
type tileHasher struct {
	canonical canonicalEncoder
	keys      []string
}

func newTileHasher() *tileHasher {
	return &tileHasher{canonical: canonicalEncoder{digest: sha256.New()}}
}

func (hasher *tileHasher) digest(tile Tile) digest {
	canonical := &hasher.canonical
	canonical.digest.Reset()
	canonical.used = 0
	writeString(canonical, canonicalTileDomain)
	writeUint64(canonical, uint64(tile.Coord.X))
	writeUint64(canonical, uint64(tile.Coord.Y))
	writeUint64(canonical, uint64(tile.Coord.Z))
	writeUint64(canonical, uint64(len(tile.State.Prefabs)))
	for _, prefab := range tile.State.Prefabs {
		writeString(canonical, string(prefab.StableID))
		writeString(canonical, prefab.Path)

		hasher.keys = hasher.keys[:0]
		for key := range prefab.Vars {
			hasher.keys = append(hasher.keys, key)
		}
		sort.Strings(hasher.keys)
		writeUint64(canonical, uint64(len(hasher.keys)))
		for _, key := range hasher.keys {
			writeString(canonical, key)
			writeString(canonical, prefab.Vars[key])
		}
	}
	canonical.flush()
	var sum digest
	canonical.digest.Sum(sum[:0])
	return sum
}

// parallelTileThreshold is the tile count above which a full hash computes
// tile digests on several goroutines. The result does not depend on it.
const parallelTileThreshold = 4096

// tileDigests returns the digest of every tile, in the order given.
func tileDigests(tiles []Tile) []digest {
	digests := make([]digest, len(tiles))
	workers := min(runtime.GOMAXPROCS(0), len(tiles)/parallelTileThreshold+1)
	if workers <= 1 {
		hasher := newTileHasher()
		for i, tile := range tiles {
			digests[i] = hasher.digest(tile)
		}
		return digests
	}
	var wait sync.WaitGroup
	chunk := (len(tiles) + workers - 1) / workers
	for start := 0; start < len(tiles); start += chunk {
		end := min(start+chunk, len(tiles))
		wait.Add(1)
		go func(start, end int) {
			defer wait.Done()
			hasher := newTileHasher()
			for i := start; i < end; i++ {
				digests[i] = hasher.digest(tiles[i])
			}
		}(start, end)
	}
	wait.Wait()
	return digests
}

func (buffer *canonicalEncoder) writeDigest(value digest) {
	if len(buffer.data)-buffer.used < len(value) {
		buffer.flush()
	}
	buffer.used += copy(buffer.data[buffer.used:], value[:])
}

func (buffer *canonicalEncoder) sum() string {
	buffer.flush()
	var sum digest
	return hex.EncodeToString(buffer.digest.Sum(sum[:0]))
}

func ValidateSHA256(name string, value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("%s: length is %d, want %d lowercase hexadecimal characters", name, len(value), sha256.Size*2)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return fmt.Errorf("%s: must be lowercase hexadecimal SHA-256", name)
	}
	return nil
}

// Validate checks whether a snapshot is safe and canonical enough to hash or apply.
func (snapshot Snapshot) Validate() error {
	if snapshot.ProtocolVersion != ProtocolVersion {
		return &UnsupportedProtocolError{Version: snapshot.ProtocolVersion}
	}
	if snapshot.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema version is %d, want %d", snapshot.SchemaVersion, SchemaVersion)
	}
	if err := snapshot.DocumentID.Validate(); err != nil {
		return err
	}
	if err := ValidateSHA256("environment hash", snapshot.EnvironmentHash); err != nil {
		return err
	}
	if _, err := snapshot.CellCount(); err != nil {
		return err
	}

	coordinates := make(map[Coord]struct{}, len(snapshot.Tiles))
	prefabs := 0
	for _, tile := range snapshot.Tiles {
		prefabs += len(tile.State.Prefabs)
	}
	stableIDs := make(map[StableID]struct{}, prefabs)
	for _, tile := range snapshot.Tiles {
		if !snapshot.Contains(tile.Coord) {
			return fmt.Errorf("coordinate (%d,%d,%d) is outside dimensions (%d,%d,%d)", tile.Coord.X, tile.Coord.Y, tile.Coord.Z, snapshot.MaxX, snapshot.MaxY, snapshot.MaxZ)
		}
		if _, exists := coordinates[tile.Coord]; exists {
			return fmt.Errorf("duplicate coordinate (%d,%d,%d)", tile.Coord.X, tile.Coord.Y, tile.Coord.Z)
		}
		coordinates[tile.Coord] = struct{}{}

		for _, prefab := range tile.State.Prefabs {
			if err := prefab.StableID.Validate(); err != nil {
				return err
			}
			if _, exists := stableIDs[prefab.StableID]; exists {
				return fmt.Errorf("duplicate stable id %q", prefab.StableID)
			}
			stableIDs[prefab.StableID] = struct{}{}
		}
	}
	return nil
}

func (snapshot Snapshot) Contains(coord Coord) bool {
	return coord.X >= 1 && coord.X <= snapshot.MaxX &&
		coord.Y >= 1 && coord.Y <= snapshot.MaxY &&
		coord.Z >= 1 && coord.Z <= snapshot.MaxZ
}

// canonicalEncoder streams exactly the v1 canonical bytes with bounded scratch
// space. Batching small fields avoids an allocation/interface call per integer.
// The 2 KiB buffer limits scratch allocation during repeated history validation
// while retaining batched writes. Each invocation owns its buffer and digest;
// there is no shared mutable cache.
type canonicalEncoder struct {
	digest hash.Hash
	data   [2048]byte
	used   int
}

func (buffer *canonicalEncoder) flush() {
	if buffer.used != 0 {
		_, _ = buffer.digest.Write(buffer.data[:buffer.used])
		buffer.used = 0
	}
}

func writeString(buffer *canonicalEncoder, value string) {
	writeUint64(buffer, uint64(len(value)))
	for len(value) != 0 {
		copied := copy(buffer.data[buffer.used:], value)
		buffer.used += copied
		value = value[copied:]
		if buffer.used == len(buffer.data) {
			buffer.flush()
		}
	}
}

func writeUint64(buffer *canonicalEncoder, value uint64) {
	if len(buffer.data)-buffer.used < 8 {
		buffer.flush()
	}
	binary.BigEndian.PutUint64(buffer.data[buffer.used:buffer.used+8], value)
	buffer.used += 8
}

// UnsupportedProtocolError reports map data stamped with a protocol version
// this build cannot interpret, such as a session stored before protocol v2
// changed the canonical map hash. Such data is refused, never reinterpreted.
type UnsupportedProtocolError struct {
	Version uint16
}

func (err *UnsupportedProtocolError) Error() string {
	return fmt.Sprintf("protocol version is %d, want %d", err.Version, ProtocolVersion)
}
