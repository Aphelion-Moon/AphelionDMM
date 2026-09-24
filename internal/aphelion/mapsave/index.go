package mapsave

import (
	"sort"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

// TileStack is the save-normalized prefab stack for one map location. Hash is
// computed once and Prefabs may share an immutable slice with equal stacks.
type TileStack struct {
	Prefabs dmmdata.Prefabs
	Hash    uint64
}

// Normalize sorts each tile stack once and interns equal normalized slices.
// Hash collisions are checked with full prefab equality before sharing data.
func Normalize(source *dmmap.Dmm) map[util.Point]TileStack {
	stacks := make(map[util.Point]TileStack, len(source.Tiles))
	canonical := make(map[uint64][]dmmdata.Prefabs)
	for _, tile := range source.Tiles {
		prefabs := tile.Instances().Prefabs()
		sort.SliceStable(prefabs, func(i, j int) bool {
			return dm.PathWeight(prefabs[i].Path()) < dm.PathWeight(prefabs[j].Path())
		})
		hash := prefabs.Hash()
		interned := false
		for _, prior := range canonical[hash] {
			if prefabs.Equals(prior) {
				prefabs = prior
				interned = true
				break
			}
		}
		if !interned {
			canonical[hash] = append(canonical[hash], prefabs)
		}
		stacks[tile.Coord] = TileStack{Prefabs: prefabs, Hash: hash}
	}
	return stacks
}

// ContentIndex finds reusable dictionary keys by hash and verifies full
// content inside each bucket so hash collisions never alias map stacks.
type ContentIndex struct {
	byHash    map[uint64][]contentEntry
	keyHashes map[dmmdata.Key]uint64
}

type contentEntry struct {
	key     dmmdata.Key
	prefabs dmmdata.Prefabs
}

func NewContentIndex(dictionary dmmdata.DataDictionary) *ContentIndex {
	index := &ContentIndex{
		byHash:    make(map[uint64][]contentEntry, len(dictionary)),
		keyHashes: make(map[dmmdata.Key]uint64, len(dictionary)),
	}
	keys := make([]dmmdata.Key, 0, len(dictionary))
	for key := range dictionary {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].ToNum() < keys[j].ToNum() })
	for _, key := range keys {
		prefabs := dictionary[key]
		index.Add(prefabs.Hash(), key, prefabs)
	}
	return index
}

func (index *ContentIndex) Find(hash uint64, prefabs dmmdata.Prefabs) (dmmdata.Key, bool) {
	if index == nil {
		return "", false
	}
	for _, entry := range index.byHash[hash] {
		if prefabs.Equals(entry.prefabs) {
			return entry.key, true
		}
	}
	return "", false
}

// Add accepts an explicit hash so callers can retain precomputed values and
// tests can exercise deliberate collisions.
func (index *ContentIndex) Add(hash uint64, key dmmdata.Key, prefabs dmmdata.Prefabs) {
	if index.byHash == nil {
		index.byHash = make(map[uint64][]contentEntry)
	}
	if index.keyHashes == nil {
		index.keyHashes = make(map[dmmdata.Key]uint64)
	}
	if previousHash, exists := index.keyHashes[key]; exists && previousHash != hash {
		previous := index.byHash[previousHash]
		for position, entry := range previous {
			if entry.key == key {
				previous = append(previous[:position], previous[position+1:]...)
				break
			}
		}
		if len(previous) == 0 {
			delete(index.byHash, previousHash)
		} else {
			index.byHash[previousHash] = previous
		}
	}
	bucket := index.byHash[hash]
	for position, entry := range bucket {
		if entry.key == key {
			bucket[position].prefabs = prefabs
			index.byHash[hash] = bucket
			index.keyHashes[key] = hash
			return
		}
	}
	index.byHash[hash] = append(bucket, contentEntry{key: key, prefabs: prefabs})
	index.keyHashes[key] = hash
}

// LocationsByKey stores the original locations for each parsed key. Reuse can
// inspect only candidate locations for an unused key instead of scanning the
// full set of locations once per key.
func LocationsByKey(data *dmmdata.DmmData) map[dmmdata.Key][]util.Point {
	locations := make(map[dmmdata.Key][]util.Point, len(data.Dictionary))
	for location, key := range data.Grid {
		locations[key] = append(locations[key], location)
	}
	for key := range locations {
		sort.Slice(locations[key], func(i, j int) bool {
			a, b := locations[key][i], locations[key][j]
			if a.Z != b.Z {
				return a.Z < b.Z
			}
			if a.Y != b.Y {
				return a.Y < b.Y
			}
			return a.X < b.X
		})
	}
	return locations
}
