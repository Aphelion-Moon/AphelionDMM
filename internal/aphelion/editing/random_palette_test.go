package editing

import (
	"math"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
	"testing"
)

func TestRandomPaletteStableAcrossOrderAndScope(t *testing.T) {
	p := RandomPalette{Version: 1, Entries: []PaletteEntry{{ID: "a", Weight: 1, Prefab: model.PrefabState{Path: "/obj/a", Vars: map[string]string{}}}, {ID: "b", Weight: 3, Prefab: model.PrefabState{Path: "/obj/b", Vars: map[string]string{}}}}}
	a, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	p.Entries[0], p.Entries[1] = p.Entries[1], p.Entries[0]
	b, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for x := 1; x < 30; x++ {
		coord := util.Point{X: x, Y: 2, Z: 1}
		pa, ok := a.Choose(123, coord, 1)
		pb, ok2 := b.Choose(123, coord, 1)
		if !ok || !ok2 || pa.Path != pb.Path {
			t.Fatal("order changed deterministic choices")
		}
		if _, ok := a.Choose(123, coord, 0); ok {
			t.Fatal("zero density authored a cell")
		}
	}
	for _, w := range []float64{-1, math.NaN(), math.Inf(1)} {
		p.Entries[0].Weight = w
		if _, err = p.Compile(); err == nil {
			t.Fatal("accepted invalid weight")
		}
	}
}

func TestRandomPaletteRejectsMixedChannelsAndZeroTotal(t *testing.T) {
	p := RandomPalette{Version: 1, Entries: []PaletteEntry{{ID: "a", Weight: 0, Prefab: model.PrefabState{Path: "/turf/a", Vars: map[string]string{}}}}}
	if _, err := p.Compile(); err == nil {
		t.Fatal("accepted all-zero palette")
	}
	p.Entries[0].Weight = 1
	p.Entries = append(p.Entries, PaletteEntry{ID: "b", Weight: 1, Prefab: model.PrefabState{Path: "/obj/b", Vars: map[string]string{}}})
	if _, err := p.Compile(); err == nil {
		t.Fatal("accepted mixed channels")
	}
}

func TestRandomPaletteVersionOneKnownDraws(t *testing.T) {
	p := RandomPalette{Version: 1, Entries: []PaletteEntry{{ID: "a", Weight: 1, Prefab: model.PrefabState{Path: "/obj/a", Vars: map[string]string{}}}, {ID: "b", Weight: 3, Prefab: model.PrefabState{Path: "/obj/b", Vars: map[string]string{}}}}}
	compiled, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	// Independently evaluated SHA-256 v1 fixture: seed 123, y=z=0.
	for x, want := range []string{"/obj/b", "/obj/b", "", "", "/obj/b", "/obj/a", "", "/obj/b"} {
		got, chosen := compiled.Choose(123, util.Point{X: x}, 0.5)
		if chosen != (want != "") || got.Path != want {
			t.Fatalf("draw %d: %q %v, want %q", x, got.Path, chosen, want)
		}
	}
}
