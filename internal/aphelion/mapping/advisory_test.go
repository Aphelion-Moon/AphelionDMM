package mapping

import (
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
	"testing"
)

func TestAuthoredSpawnScoringAndIndependentOverMatches(t *testing.T) {
	s := &Source{Identity: Identity{Environment: &dmenv.Dme{Objects: map[string]*dmenv.Object{}}}, Size: util.Point{X: 7, Y: 7, Z: 1}, grid: map[util.Point]dmmdata.Key{}, dictionary: map[dmmdata.Key][]Atom{
		"floor": {{Path: "/turf/open/floor"}, {Path: "/area/test"}},
		"wall":  {{Path: "/turf/closed"}, {Path: "/area/test"}},
		"open":  {{Path: "/turf/open"}, {Path: "/area/test"}},
		"host":  {{Path: "/turf/open/floor"}, {Path: "/area/test"}, {Path: "/obj/host", Vars: map[string]string{"density": "0", "layer": "3"}}},
	}}
	for _, atoms := range s.dictionary {
		for _, atom := range atoms {
			s.Identity.Environment.Objects[atom.Path] = &dmenv.Object{Path: atom.Path}
		}
	}
	for y := 1; y <= 7; y++ {
		for x := 1; x <= 7; x++ {
			s.grid[util.Point{X: x, Y: y, Z: 1}] = "floor"
		}
	}
	p := util.Point{X: 4, Y: 4, Z: 1}
	rules := spawnScoringRules{}
	if got := s.scoreSpawn(p, 0, rules); got.Score != 14 || got.Unknown {
		t.Fatalf("open floor score: %+v", got)
	}
	s.grid[util.Point{X: 4, Y: 5, Z: 1}] = "wall"
	// This synthetic environment has no inherited types: explicitly author the
	// three second-neighbor open turfs used by the wall-hug rule.
	for _, point := range []util.Point{{X: 4, Y: 2, Z: 1}, {X: 2, Y: 4, Z: 1}, {X: 6, Y: 4, Z: 1}} {
		s.grid[point] = "open"
	}
	if got := s.scoreSpawn(p, 1, rules); got.Score != 1413 || got.Unknown {
		t.Fatalf("wall-hug score: %+v", got)
	}
	if got := s.scoreSpawn(p, 2, rules); got.Score != 13 || got.Unknown {
		t.Fatalf("wall-mount score: %+v", got)
	}
	s.grid[p] = "host"
	delete(s.Identity.Environment.Objects, "/obj/host")
	if result := s.scoreSpawn(p, 0, rules); !result.Unknown {
		t.Fatal("local density/layer hid missing inheritance", result)
	}
	if result := s.scoreOver(p, SpawnRule{Desired: "/obj/result", Over: []string{"/obj/host"}}); !result.Unknown {
		t.Fatal("unknown host treated as resolved", result)
	}
	s.Identity.Environment.Objects["/obj/host"] = &dmenv.Object{Path: "/obj/host"}
	match := s.scoreOver(p, SpawnRule{Desired: "/obj/result", Over: []string{"/obj/host"}})
	if match.Score != 1 {
		t.Fatalf("over host match: %+v", match)
	}
	s.dictionary["host"] = append(s.dictionary["host"], Atom{Path: "/obj/result"})
	s.Identity.Environment.Objects["/obj/result"] = &dmenv.Object{Path: "/obj/result"}
	if s.scoreOver(p, SpawnRule{Desired: "/obj/result", Over: []string{"/obj/host"}}).Score != 0 {
		t.Fatal("over duplicated existing desired atom")
	}
}

func TestUnknownAuthoredTurfDoesNotBecomeKnownRejection(t *testing.T) {
	p := util.Point{X: 1, Y: 1, Z: 1}
	s := &Source{Identity: Identity{Environment: &dmenv.Dme{Objects: map[string]*dmenv.Object{}}}, grid: map[util.Point]dmmdata.Key{p: "a"}, dictionary: map[dmmdata.Key][]Atom{"a": {{Path: "/turf/custom_missing"}}}}
	if _, known := s.turfIs(p, "/turf/open/floor"); known {
		t.Fatal("missing inheritance metadata treated as known turf")
	}
	if result := s.scoreSpawn(p, 0, spawnScoringRules{}); !result.Unknown {
		t.Fatal("missing turf treated as definite rejection", result)
	}
}

func TestDMStaticListAndEmptyVariableComparison(t *testing.T) {
	values, err := staticList("list(/area/one, \"Two, three\")")
	if err != nil || len(values) != 2 || values[1] != "Two, three" {
		t.Fatal(values, err)
	}
	if _, err := staticList("list(/area/one = 3)"); err == nil {
		t.Fatal("associated list accepted as ordered targets")
	}
	if diff := CompareCell([]Atom{{Path: "/obj/a"}}, []Atom{{Path: "/obj/a", Vars: map[string]string{}}}); diff.Objects {
		t.Fatal("empty vars produce false difference")
	}
}
