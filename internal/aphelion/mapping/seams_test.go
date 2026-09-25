package mapping

import (
	"context"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
	"testing"
)

func TestSeamsUseSelectedAuthoredFootprintsAndKeepDeckUncertainty(t *testing.T) {
	a := util.Point{X: 1, Y: 1, Z: 1}
	b := util.Point{X: 2, Y: 1, Z: 1}
	s := &Source{Identity: Identity{Environment: &dmenv.Dme{Objects: map[string]*dmenv.Object{}}}, grid: map[util.Point]dmmdata.Key{a: "a", b: "b"}, dictionary: map[dmmdata.Key][]Atom{
		"a": {{Path: "/turf/open/floor"}, {Path: "/obj/structure/cable", Vars: map[string]string{"cable_layer": "1", "banned_links": "0"}}, {Path: "/obj/structure/cable/multilayer/multiz"}},
		"b": {{Path: "/turf/closed"}, {Path: "/obj/structure/cable", Vars: map[string]string{"cable_layer": "2", "banned_links": "0"}}},
	}}
	for _, atoms := range s.dictionary {
		for _, atom := range atoms {
			s.Identity.Environment.Objects[atom.Path] = &dmenv.Object{Path: atom.Path}
		}
	}
	p := &Projection{Source: s, Provenance: map[util.Point]*CellProvenance{a: {Turf: &Contribution{Source: "template", Occurrence: "selected"}}, b: {Turf: &Contribution{Source: "base"}}}}
	d, err := p.AnalyzeSeams(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, entry := range d {
		codes[entry.Code] = true
		if entry.Source == "" || entry.Destination.Z == 0 {
			t.Fatal("missing witness provenance")
		}
	}
	for _, code := range []string{"seam-substrate", "seam-cable", "seam-deck-unknown"} {
		if !codes[code] {
			t.Fatalf("missing %s: %+v", code, d)
		}
	}
	p.Provenance[b].Turf = p.Provenance[a].Turf
	d, err = p.AnalyzeSeams(context.Background())
	if err != nil || len(d) != 0 {
		t.Fatal("scanned interior outside selected boundary", d, err)
	}
}

func TestSeamObjectWitnessAndResolvedZeroPorts(t *testing.T) {
	a, b := util.Point{X: 1, Y: 1, Z: 1}, util.Point{X: 2, Y: 1, Z: 1}
	s := &Source{Identity: Identity{Environment: &dmenv.Dme{Objects: map[string]*dmenv.Object{}}}, grid: map[util.Point]dmmdata.Key{a: "a", b: "b"}, dictionary: map[dmmdata.Key][]Atom{
		"a": {{Path: "/turf/open/floor"}, {Path: "/obj/decoration"}, {Path: "/obj/structure/cable", Vars: map[string]string{"cable_layer": "1", "banned_links": "0"}}, {Path: "/obj/machinery/atmospherics", Vars: map[string]string{"initialize_directions": "0", "piping_layer": "1"}}},
		"b": {{Path: "/turf/open/floor"}},
	}}
	for _, atoms := range s.dictionary {
		for _, atom := range atoms {
			s.Identity.Environment.Objects[atom.Path] = &dmenv.Object{Path: atom.Path}
		}
	}
	p := &Projection{Source: s, Provenance: map[util.Point]*CellProvenance{
		a: {Turf: &Contribution{Source: "base"}, Objects: []Contribution{{Source: "decoration", Occurrence: "first"}, {Source: "cable-source", Occurrence: "second", Local: util.Point{X: 5, Y: 7, Z: 1}}, {Source: "atmos-source"}}},
		b: {Turf: &Contribution{Source: "neighbor"}},
	}}
	diagnostics, err := p.AnalyzeSeams(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range diagnostics {
		if d.Code == "seam-cable" {
			found = true
			if d.Source != "cable-source" || d.Occurrence != "second" || d.Local.X != 5 {
				t.Fatal("wrong object witness", d)
			}
		}
		if d.Code == "seam-pipe-unknown" {
			t.Fatal("resolved zero ports labeled unknown", d)
		}
	}
	if !found {
		t.Fatal("missing cable witness")
	}
}
