package render

import (
	"reflect"
	"sdmm/internal/util"
	"testing"
)

func TestPresentationInterleavesLayersAndKeepsSparseOverhang(t *testing.T) {
	p := &Presentation{}
	p.Add(Appearance{Coord: util.Point{X: 0, Y: 0}, Layer: 2, Bounds: util.Bounds{X1: -64, Y1: 0, X2: 32, Y2: 32}})
	p.Add(Appearance{Coord: util.Point{X: 500, Y: 500}, Layer: 4, Bounds: util.Bounds{X1: 16000, Y1: 16000, X2: 16100, Y2: 16100}})
	p.Finish()
	var order []float32
	eachPresentationLayer([]float32{1, 3, 4, 6}, p.Layers, func(layer float32) { order = append(order, layer) })
	if !reflect.DeepEqual(order, []float32{1, 2, 3, 4, 6}) {
		t.Fatalf("ghost layers no longer merge with retained destination layers: %v", order)
	}
	if len(p.groups) != 2 || len(p.groups[2]) != 1 || len(p.groups[4]) != 1 {
		t.Fatal("sparse source allocated groups for bounding holes")
	}
	if !p.groups[2][0].bounds.ContainsV(util.Bounds{X1: -60, Y1: 1, X2: -40, Y2: 20}) {
		t.Fatal("sprite overhang would be culled by tile bounds")
	}
}
