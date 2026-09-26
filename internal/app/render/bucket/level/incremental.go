// APHELION EDIT ADDITION START - INCREMENTAL CHUNKS
package level

import (
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
	"slices"
	"sort"
)

func (l *Level) Update(dmm *dmmap.Dmm, tiles []util.Point, filters ...func(*dmminstance.Instance) bool) {
	if tiles == nil || l.ChunksByLayers == nil {
		for _, c := range l.Chunks {
			c.Update(dmm, l.value, filters...)
		}
		l.createChunksLayers()
		return
	}
	seen := make(map[util.Point]struct{}, len(tiles))
	for _, tile := range tiles {
		if tile.Z != l.value {
			continue
		}
		key := findChunkBounds(tile.X, tile.Y)
		if _, done := seen[key]; done {
			continue
		}
		seen[key] = struct{}{}
		if c := l.Chunks[key]; c != nil {
			old := c.UnitsByLayers
			c.Update(dmm, l.value, filters...)
			l.updateChunkLayers(c, old)
		}
	}
}

func (l *Level) updateChunkLayers(c *chunk.Chunk, old map[float32][]unit.Unit) {
	layersChanged := false
	for layer, units := range old {
		if len(units) == 0 || len(c.UnitsByLayers[layer]) != 0 {
			continue
		}
		members := slices.DeleteFunc(l.ChunksByLayers[layer], func(member *chunk.Chunk) bool { return member == c })
		if len(members) == 0 {
			delete(l.ChunksByLayers, layer)
			layersChanged = true
		} else {
			l.ChunksByLayers[layer] = members
		}
	}
	for layer, units := range c.UnitsByLayers {
		if len(units) == 0 || len(old[layer]) != 0 {
			continue
		}
		if len(l.ChunksByLayers[layer]) == 0 {
			layersChanged = true
		}
		l.ChunksByLayers[layer] = append(l.ChunksByLayers[layer], c)
		sortChunks(l.ChunksByLayers[layer])
	}
	if layersChanged {
		l.Layers = l.Layers[:0]
		for layer := range l.ChunksByLayers {
			l.Layers = append(l.Layers, layer)
		}
		slices.Sort(l.Layers)
	}
}

func sortChunks(chunks []*chunk.Chunk) {
	sort.Slice(chunks, func(i, j int) bool {
		a, b := chunks[i].MapBounds, chunks[j].MapBounds
		if a.Y1 != b.Y1 {
			return a.Y1 < b.Y1
		}
		return a.X1 < b.X1
	})
}

// APHELION EDIT ADDITION END
