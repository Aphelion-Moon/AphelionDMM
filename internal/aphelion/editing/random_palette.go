package editing

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
	"sort"
	"strings"
)

type PaletteEntry struct {
	ID     string
	Weight float64
	Prefab model.PrefabState
}
type RandomPalette struct {
	Version int
	Name    string
	Entries []PaletteEntry
}

func (p RandomPalette) Clone() RandomPalette {
	p.Entries = append([]PaletteEntry(nil), p.Entries...)
	for i := range p.Entries {
		p.Entries[i].Prefab.Vars = maps.Clone(p.Entries[i].Prefab.Vars)
	}
	return p
}

type CompiledPalette struct {
	entries []PaletteEntry
	total   float64
}

func (p RandomPalette) Compile() (CompiledPalette, error) {
	if p.Version != 1 || len(p.Entries) == 0 {
		return CompiledPalette{}, fmt.Errorf("palette version 1 and at least one choice are required")
	}
	compiled := CompiledPalette{entries: make([]PaletteEntry, len(p.Entries))}
	seen := map[string]bool{}
	channel := ChannelForPath(p.Entries[0].Prefab.Path)
	for i, entry := range p.Entries {
		if entry.ID == "" || seen[entry.ID] || entry.Weight < 0 || math.IsNaN(entry.Weight) || math.IsInf(entry.Weight, 0) {
			return CompiledPalette{}, fmt.Errorf("choices need unique IDs and finite nonnegative weights")
		}
		if !strings.HasPrefix(entry.Prefab.Path, "/") || entry.Prefab.Vars == nil || ChannelForPath(entry.Prefab.Path) != channel {
			return CompiledPalette{}, fmt.Errorf("a palette must contain valid prefabs from one channel")
		}
		seen[entry.ID] = true
		entry.Prefab.StableID = ""
		entry.Prefab.Vars = maps.Clone(entry.Prefab.Vars)
		compiled.entries[i] = entry
	}
	sort.Slice(compiled.entries, func(i, j int) bool { return compiled.entries[i].ID < compiled.entries[j].ID })
	for _, entry := range compiled.entries {
		compiled.total += entry.Weight
	}
	if compiled.total <= 0 || math.IsInf(compiled.total, 0) {
		return CompiledPalette{}, fmt.Errorf("palette total weight must be positive and finite")
	}
	return compiled, nil
}

// Choose v1 hashes seed and fixed-anchor relative coordinates. Sorting stable
// entry IDs during compilation makes UI reordering and traversal irrelevant.
func (p CompiledPalette) Choose(seed uint64, coord util.Point, density float64) (model.PrefabState, bool) {
	if density <= 0 || density > 1 || math.IsNaN(density) || p.total <= 0 {
		return model.PrefabState{}, false
	}
	var input [33]byte
	input[0] = 1
	binary.LittleEndian.PutUint64(input[1:9], seed)
	binary.LittleEndian.PutUint64(input[9:17], uint64(int64(coord.X)))
	binary.LittleEndian.PutUint64(input[17:25], uint64(int64(coord.Y)))
	binary.LittleEndian.PutUint64(input[25:33], uint64(int64(coord.Z)))
	digest := sha256.Sum256(input[:])
	unit := func(b []byte) float64 { return float64(binary.LittleEndian.Uint64(b)>>11) / float64(uint64(1)<<53) }
	if unit(digest[:8]) >= density {
		return model.PrefabState{}, false
	}
	draw := unit(digest[8:16]) * p.total
	for _, entry := range p.entries {
		if draw < entry.Weight {
			return model.PrefabState{Path: entry.Prefab.Path, Vars: maps.Clone(entry.Prefab.Vars)}, true
		}
		draw -= entry.Weight
	}
	return model.PrefabState{}, false
}
