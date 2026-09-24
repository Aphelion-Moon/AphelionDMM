package level

import (
	"reflect"
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/util"
	"testing"
)

func TestChangedChunkUpdatesOnlyLayerMembership(t *testing.T) {
	a, b := chunk.New(1, 1, 25, 25, 32), chunk.New(26, 1, 50, 25, 32)
	a.UnitsByLayers = map[float32][]unit.Unit{1: {{}}}
	b.UnitsByLayers = map[float32][]unit.Unit{1: {{}}, 3: {{}}}
	l := &Level{Chunks: map[util.Point]*chunk.Chunk{{X: 1, Y: 1}: a, {X: 26, Y: 1}: b}}
	l.createChunksLayers()
	old := a.UnitsByLayers
	a.UnitsByLayers = map[float32][]unit.Unit{1: nil, 2: {{}}}
	l.updateChunkLayers(a, old)
	if !reflect.DeepEqual(l.Layers, []float32{1, 2, 3}) || !reflect.DeepEqual(l.ChunksByLayers[1], []*chunk.Chunk{b}) || !reflect.DeepEqual(l.ChunksByLayers[2], []*chunk.Chunk{a}) {
		t.Fatal("layer index does not match changed chunk", l.Layers, l.ChunksByLayers)
	}
	old = b.UnitsByLayers
	b.UnitsByLayers = map[float32][]unit.Unit{}
	l.updateChunkLayers(b, old)
	if !reflect.DeepEqual(l.Layers, []float32{2}) {
		t.Fatal("empty layers retained", l.Layers)
	}
}

func TestChunkBoundMatchesGeneratedEdges(t *testing.T) {
	for _, n := range []int{1, 24, 25, 26, 49, 50, 51, 512} {
		b := findChunkBound(n)
		if b > n || n > b+chunk.Size || (b-1)%(chunk.Size+1) != 0 {
			t.Fatal(n, b)
		}
	}
}
