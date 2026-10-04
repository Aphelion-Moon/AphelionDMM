package mapping

import (
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

func TestCompareCellOrderedChannels(t *testing.T) {
	turf := Atom{Path: "/turf/floor", Vars: map[string]string{"dir": "2"}}
	area := Atom{Path: "/area/station"}
	first := Atom{Path: "/obj/unknown", Vars: map[string]string{"value": "1"}}
	second := Atom{Path: "/obj/unknown", Vars: map[string]string{"value": "2"}}
	for _, tc := range []struct {
		name string
		a, b []Atom
		want ChannelDiff
	}{
		{"empty", nil, []Atom{}, ChannelDiff{}},
		{"interleaved channels", []Atom{turf, first, area, second}, []Atom{area, first, turf, second}, ChannelDiff{}},
		{"object order", []Atom{first, second, turf, area}, []Atom{second, first, turf, area}, ChannelDiff{Objects: true}},
		{"multiplicity", []Atom{first, first}, []Atom{first}, ChannelDiff{Objects: true}},
		{"trailing channels", []Atom{first}, []Atom{first, turf, area}, ChannelDiff{Turf: true, Area: true}},
		{"root paths", []Atom{{Path: "/turf"}, {Path: "/area"}}, nil, ChannelDiff{Turf: true, Area: true}},
		{"prefix is not a channel", []Atom{{Path: "/turf_extra"}, {Path: "/area_extra"}}, nil, ChannelDiff{Objects: true}},
		{"authored variables", []Atom{turf}, []Atom{{Path: turf.Path, Vars: map[string]string{"dir": "4"}}}, ChannelDiff{Turf: true}},
		{"missing variable", []Atom{first}, []Atom{{Path: first.Path}}, ChannelDiff{Objects: true}},
		{"identity is not content", []Atom{{Path: first.Path, StableID: "a", Occurrence: "one"}}, []Atom{{Path: first.Path, StableID: "b", Occurrence: "two", Vars: map[string]string{}}}, ChannelDiff{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompareCell(tc.a, tc.b); got != tc.want {
				t.Fatalf("CompareCell() = %+v, want %+v", got, tc.want)
			}
			if got := CompareCell(tc.b, tc.a); got != tc.want {
				t.Fatalf("reverse CompareCell() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCompareCellDoesNotAllocate(t *testing.T) {
	a := comparisonAtoms()
	b := cloneAtoms(a)
	if allocations := testing.AllocsPerRun(100, func() {
		if got := CompareCell(a, b); got != (ChannelDiff{}) {
			t.Fatalf("equal cells differ: %+v", got)
		}
	}); allocations != 0 {
		t.Fatalf("read-only cell comparison allocates %.0f times per call", allocations)
	}
}

func TestSourceComparisonPreservesPrivateCells(t *testing.T) {
	point := util.Point{X: 1, Y: 2, Z: 3}
	otherPoint := util.Point{X: 4, Y: 5, Z: 6}
	a := &Source{grid: map[util.Point]dmmdata.Key{point: "a"}, dictionary: map[dmmdata.Key][]Atom{"a": comparisonAtoms()}}
	b := &Source{grid: map[util.Point]dmmdata.Key{otherPoint: "b"}, dictionary: map[dmmdata.Key][]Atom{"b": cloneAtoms(a.dictionary["a"])}}
	b.dictionary["b"][2].Vars["dir"] = "8"
	for range 2 {
		if got := a.CompareCell(point, b, otherPoint); got != (ChannelDiff{Objects: true}) {
			t.Fatalf("source comparison = %+v, want only objects", got)
		}
	}
	if got := a.CompareCell(point, b, point); got != (ChannelDiff{Turf: true, Area: true, Objects: true}) {
		t.Fatalf("missing cell comparison = %+v, want all channels", got)
	}
	cell := a.Cell(point)
	cell[2].Vars["dir"] = "8"
	if a.Cell(point)[2].Vars["dir"] != "4" || b.Cell(otherPoint)[2].Vars["dir"] != "8" {
		t.Fatal("comparison or Cell exposed/mutated private source variables")
	}
	if allocations := testing.AllocsPerRun(100, func() { a.CompareCell(point, b, otherPoint) }); allocations != 0 {
		t.Fatalf("source comparison allocates %.0f times per call", allocations)
	}
}

func comparisonAtoms() []Atom {
	return []Atom{
		{Path: "/turf/floor", Vars: map[string]string{"dir": "2"}},
		{Path: "/area/station"},
		{Path: "/obj/structure", Vars: map[string]string{"dir": "4", "pixel_x": "8"}},
		{Path: "/obj/unknown", Vars: map[string]string{"name": "\"example\""}},
	}
}

func atomsFor(atoms []Atom, c string) []Atom {
	var result []Atom
	for _, a := range atoms {
		if channel(a) == c {
			result = append(result, a)
		}
	}
	return result
}

func BenchmarkCompareCell(b *testing.B) {
	a := comparisonAtoms()
	other := cloneAtoms(a)
	b.ReportAllocs()
	for b.Loop() {
		if got := CompareCell(a, other); got != (ChannelDiff{}) {
			b.Fatal(got)
		}
	}
}

func BenchmarkCompareSourceViewport(b *testing.B) {
	const side = 64
	source := &Source{grid: make(map[util.Point]dmmdata.Key), dictionary: map[dmmdata.Key][]Atom{"a": comparisonAtoms()}}
	reference := &Source{grid: make(map[util.Point]dmmdata.Key), dictionary: map[dmmdata.Key][]Atom{"a": cloneAtoms(source.dictionary["a"])}}
	for y := 1; y <= side; y++ {
		for x := 1; x <= side; x++ {
			point := util.Point{X: x, Y: y, Z: 1}
			source.grid[point], reference.grid[point] = "a", "a"
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		for y := 1; y <= side; y++ {
			for x := 1; x <= side; x++ {
				point := util.Point{X: x, Y: y, Z: 1}
				if got := source.CompareCell(point, reference, point); got != (ChannelDiff{}) {
					b.Fatal(got)
				}
			}
		}
	}
}
