// APHELION EDIT ADDITION START - OWNED MAP OPEN
package bucket

import (
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func (b *Bucket) PrepareLevel(dmm *dmmap.Dmm, level int) []util.Point {
	l := b.getOrCreateLevel(dmm, level)
	if l.ChunksByLayers == nil {
		l.ChunksByLayers = make(map[float32][]*chunk.Chunk)
	}
	points := make([]util.Point, 0, len(l.Chunks))
	for point := range l.Chunks {
		point.Z = level
		points = append(points, point)
	}
	return points
}

// APHELION EDIT ADDITION END
