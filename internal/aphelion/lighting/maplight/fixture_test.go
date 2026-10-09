package maplight

import (
	"testing"
	"time"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// typ builds the immutable variables of an environment type that inherits parent.
func typ(parent *dmvars.Variables, kv ...string) *dmvars.Variables {
	v := dmvars.FromParent(parent)
	for i := 0; i+1 < len(kv); i += 2 {
		v = dmvars.Set(v, kv[i], kv[i+1])
	}
	return v
}

func prefab(path string, vars *dmvars.Variables) *dmmprefab.Prefab {
	return dmmprefab.New(dmmprefab.IdNone, path, vars)
}

// synthetic is a tiny environment: it exposes the prefabs the fixtures need.
type synthetic struct {
	root                                         *dmvars.Variables
	floor, wall, space, station, spaceArea, lamp *dmmprefab.Prefab
}

func newSynthetic() *synthetic {
	root := (&dmvars.MutableVariables{}).ToImmutable()
	s := &synthetic{root: root}
	s.floor = prefab("/turf/open/floor", typ(root))
	s.wall = prefab("/turf/closed/wall", typ(root, "opacity", "1"))
	s.space = prefab("/turf/open/space", typ(root, "light_range", "2", "light_power", "1", "light_on", "FALSE"))
	s.station = prefab("/area/station", typ(root))
	s.spaceArea = prefab("/area/space", typ(root, "static_lighting", "FALSE", "base_lighting_alpha", "255", "base_lighting_color", `"#8589fa"`))
	s.lamp = prefab("/obj/lamp", typ(root, "light_range", "5", "light_power", "1", "light_color", `"#ff0000"`))
	return s
}

// mapOf returns a one-level map of w by h floor tiles in the station area.
func (s *synthetic) mapOf(w, h int) *dmmap.Dmm {
	d := &dmmap.Dmm{MaxX: w, MaxY: h, MaxZ: 1, Tiles: make([]*dmmap.Tile, w*h)}
	for y := 1; y <= h; y++ {
		for x := 1; x <= w; x++ {
			d.Tiles[(y-1)*w+x-1] = &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}}
			s.set(d, x, y, s.floor, s.station)
		}
	}
	return d
}

func (s *synthetic) set(d *dmmap.Dmm, x, y int, prefabs ...*dmmprefab.Prefab) {
	pt := util.Point{X: x, Y: y, Z: 1}
	instances := make(dmmap.Instances, 0, len(prefabs))
	for _, p := range prefabs {
		instances = append(instances, dmminstance.New(pt, p))
	}
	d.GetTile(pt).Set(instances)
}

func fullSettings() Settings { return DefaultSettings() }

func buildAll(t *testing.T, d *dmmap.Dmm, set Settings) (*levelState, *Classifier) {
	t.Helper()
	cls := NewClassifier(set)
	st := newLevelState(d.MaxX, d.MaxY)
	b := newBuilder(d, 1, cls, st)
	if !b.Step(0) {
		t.Fatal("unbounded step must finish")
	}
	return st, cls
}

func TestAdapterBuildsLevelFromSyntheticMap(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(9, 5)
	s.set(d, 3, 2, s.floor, s.lamp, s.station)
	s.set(d, 5, 2, s.wall, s.station)
	fixture := prefab("/obj/machinery/light/small", typ(s.root, "dir", "4"))
	s.set(d, 7, 2, s.floor, fixture, s.station)
	for x := 1; x <= 9; x++ {
		s.set(d, x, 5, s.space, s.spaceArea)
	}

	st, _ := buildAll(t, d, fullSettings())
	lvl := st.snapshot()
	if lvl.W != 9 || lvl.H != 5 {
		t.Fatalf("level %dx%d", lvl.W, lvl.H)
	}
	if !lvl.Occ.Opaque(4, 1) || lvl.Occ.Opaque(2, 1) {
		t.Fatal("wall must be the only opaque tile")
	}
	if len(lvl.Sources) != 2 {
		t.Fatalf("want lamp and fixture, got %+v", lvl.Sources)
	}
	lamp, fix := lvl.Sources[0], lvl.Sources[1]
	if lamp.X != 2 || lamp.Y != 1 || lamp.Range != 5 || lamp.Color != (lighting.RGB{R: 1}) {
		t.Fatalf("lamp %+v", lamp)
	}
	if fix.X != 6 || fix.Y != 1 || fix.Range != 8 || fix.Dir != lighting.DirWest || fix.ShiftX != 0.5 || fix.Angle != 170 {
		t.Fatalf("wall fixture profile %+v", fix)
	}
	for x := 0; x < 9; x++ {
		i := 4*9 + x
		if !lvl.Space[i] || !lvl.Unlit[i] {
			t.Fatalf("space tile %d must be space and have no lighting object", x)
		}
		star := lighting.DefaultStarlight().Color
		if lvl.Ambient[i] != star {
			t.Fatalf("space ambient %v want starlight %v", lvl.Ambient[i], star)
		}
	}
	if lvl.Space[0] || lvl.Unlit[0] || lvl.Ambient[0] != (lighting.RGB{}) {
		t.Fatal("station floor must be lit, non-space with no ambient")
	}
	if !lvl.Starlight.Enabled || lvl.Starlight.Range != 2 {
		t.Fatalf("starlight %+v", lvl.Starlight)
	}
}

func TestAdapterSettingsControlStarlightAndOverlay(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(4, 4)
	beam := prefab("/obj/flash", typ(s.root, "light_system", "2", "light_range", "3"))
	s.set(d, 2, 2, s.floor, beam, s.station)
	set := fullSettings()
	set.Starlight = false
	set.OverlayLights = false
	st, _ := buildAll(t, d, set)
	rep := st.report()
	if st.snapshot().Starlight.Enabled {
		t.Fatal("starlight preference ignored")
	}
	if rep.Sources != 0 || rep.Skipped != 1 || len(rep.Groups) != 1 || rep.Groups[0].Path != "/obj/flash" {
		t.Fatalf("overlay light must be reported as skipped when disabled: %+v", rep)
	}
}

func TestSkippedAtomsAreAggregatedByPathAndReason(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(6, 3)
	broken := prefab("/obj/broken", typ(s.root, "light_range", "BROKEN_MACRO"))
	off := prefab("/obj/offlamp", typ(s.root, "light_range", "3", "light_on", "FALSE"))
	plain := prefab("/obj/plain", typ(s.root))
	for x := 1; x <= 4; x++ {
		s.set(d, x, 1, s.floor, broken, plain, s.station)
	}
	s.set(d, 5, 1, s.floor, off, s.station)
	st, _ := buildAll(t, d, fullSettings())
	rep := st.report()
	if rep.Skipped != 5 {
		t.Fatalf("plain atoms are not skipped lights; got %+v", rep)
	}
	if len(rep.Groups) != 2 || rep.Groups[0].Path != "/obj/broken" || rep.Groups[0].Count != 4 || !rep.Groups[0].Unparsable {
		t.Fatalf("groups must be sorted by count with one row per path and reason: %+v", rep.Groups)
	}
	if rep.Groups[1].Path != "/obj/offlamp" || rep.Groups[1].Reason != "light_on is false" {
		t.Fatalf("%+v", rep.Groups[1])
	}
}

func TestRecaptureReplacesTileAndReportCounts(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(5, 5)
	s.set(d, 2, 2, s.floor, s.lamp, s.station)
	st, cls := buildAll(t, d, fullSettings())
	if len(st.snapshot().Sources) != 1 {
		t.Fatal("setup")
	}
	s.set(d, 2, 2, s.wall, s.station) // lamp deleted, wall placed
	s.set(d, 4, 4, s.floor, s.lamp, s.station)
	st.recapture(d, 1, cls, []util.Point{{X: 2, Y: 2, Z: 1}, {X: 4, Y: 4, Z: 1}})
	lvl := st.snapshot()
	if len(lvl.Sources) != 1 || lvl.Sources[0].X != 3 || !lvl.Occ.Opaque(1, 1) {
		t.Fatalf("recapture left stale tile data: %+v", lvl.Sources)
	}
	if st.report().Sources != 1 {
		t.Fatal("source count not maintained")
	}
}

func TestSourceCapKeepsDeterministicOrderAndFlags(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(10, 10)
	for y := 1; y <= 10; y++ {
		for x := 1; x <= 10; x++ {
			s.set(d, x, y, s.floor, s.lamp, s.station)
		}
	}
	set := fullSettings()
	st, cls := buildAll(t, d, set)
	st.maxSources = 7
	_ = cls
	lvl := st.snapshot()
	if len(lvl.Sources) != 7 || !st.report().Capped {
		t.Fatalf("cap not applied: %d capped=%v", len(lvl.Sources), st.report().Capped)
	}
	for i := 1; i < len(lvl.Sources); i++ {
		a, b := lvl.Sources[i-1], lvl.Sources[i]
		if a.Y*10+a.X >= b.Y*10+b.X {
			t.Fatal("sources must be in row-major tile order")
		}
	}
}

func TestBuilderStepsAreBoundedAndResumable(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(64, 64)
	cls := NewClassifier(fullSettings())
	st := newLevelState(64, 64)
	b := newBuilder(d, 1, cls, st)
	b.tilesPerCheck = 16
	b.maxPerStep = 100
	steps := 0
	for !b.Step(time.Hour) { // each call yields after maxPerStep tiles
		steps++
		if steps > 64*64 {
			t.Fatal("no progress")
		}
	}
	if steps < 2 {
		t.Fatal("a capped step must spread the build over several frames")
	}
}

func TestPrefabClassificationIsCachedByPointer(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(8, 8)
	_, cls := buildAll(t, d, fullSettings())
	if got := cls.size(); got != 2 { // one floor prefab and one station area for the whole map
		t.Fatalf("classification cache has %d entries, want 2", got)
	}
}
