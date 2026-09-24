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

func TestMoveSourceDefaultsRemainAtSourceWhenPayloadTranslates(t *testing.T) {
	p := &Presentation{Anchor: util.Point{X: 4, Y: 3, Z: 1}, IconSize: 32}
	source := Appearance{Coord: util.Point{}, Layer: 2, Bounds: util.Bounds{X1: 0, Y1: 0, X2: 32, Y2: 32}, WorldSpace: true}
	payload := source
	payload.WorldSpace = false
	p.Add(source)
	p.Add(payload)
	p.Finish()
	if len(p.groups[2]) != 2 {
		t.Fatal("fixed source and moving payload shared culling group")
	}
	for _, anchor := range []util.Point{{X: 4, Y: 3}, {X: 20, Y: 1}} {
		p.Anchor = anchor
		for _, group := range p.groups[2] {
			dx, dy := p.groupTranslation(group)
			bounds := group.bounds.Plus(dx, dy)
			if group.worldSpace {
				if bounds != source.Bounds {
					t.Fatal("regenerated source defaults followed the destination")
				}
			} else if bounds.X1 != float32((anchor.X-1)*32) || bounds.Y1 != float32((anchor.Y-1)*32) {
				t.Fatal("payload lost destination translation")
			}
		}
	}
}
