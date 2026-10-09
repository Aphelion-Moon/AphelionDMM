package lighting

import (
	"math/rand"
	"testing"
)

func assertSameResult(t *testing.T, step int, a, b *Result) {
	t.Helper()
	if len(a.sum) != len(b.sum) {
		t.Fatalf("size mismatch")
	}
	for i := range a.sum {
		if a.sum[i] != b.sum[i] {
			w := a.W + 1
			t.Fatalf("step %d: corner (%d,%d) incremental %v != full %v", step, i%w, i/w, a.sum[i], b.sum[i])
		}
	}
}

func randomSource(r *rand.Rand, w, h int) Source {
	s := white(r.Intn(w), r.Intn(h), 1+r.Float64()*9, 0.3+r.Float64()*1.2)
	s.Color = RGB{r.Float64(), r.Float64(), r.Float64()}
	if r.Intn(4) == 0 {
		s.Angle = 60 + float64(r.Intn(200))
		s.Dir = []Dir{DirNorth, DirSouth, DirEast, DirWest, DirNorth | DirEast}[r.Intn(5)]
	}
	if r.Intn(5) == 0 {
		s.ShiftX = r.Float64() - 0.5
		s.ShiftY = r.Float64() - 0.5
	}
	if r.Intn(10) == 0 {
		s.System = SystemOverlayDirectional
	}
	return s
}

func TestIncrementalEqualsFullOnRandomEdits(t *testing.T) {
	const w, h = 48, 40
	r := rand.New(rand.NewSource(20261009))
	lvl := NewLevel(w, h)
	lvl.Space = make([]bool, w*h)
	lvl.Starlight = DefaultStarlight()
	for i := 0; i < w*h; i++ {
		if r.Intn(9) == 0 {
			lvl.Occ.(*BoolGrid).Set(i%w, i/w, true)
		}
		lvl.Space[i] = i%w > 38
	}
	for i := 0; i < 25; i++ {
		lvl.Sources = append(lvl.Sources, randomSource(r, w, h))
	}
	opts := DefaultOptions()
	res := Compute(lvl, opts)
	assertSameResult(t, -1, res, Compute(lvl, opts))

	for step := 0; step < 150; step++ {
		var dirty Rect
		switch r.Intn(5) {
		case 0, 1: // toggle opacity
			x, y := r.Intn(w), r.Intn(h)
			g := lvl.Occ.(*BoolGrid)
			g.Set(x, y, !g.Opaque(x, y))
			dirty = RectAround(x, y)
		case 2: // add a source
			s := randomSource(r, w, h)
			lvl.Sources = append(lvl.Sources, s)
			dirty = RectAround(s.X, s.Y)
		case 3: // move or remove a source
			if len(lvl.Sources) == 0 {
				continue
			}
			i := r.Intn(len(lvl.Sources))
			old := lvl.Sources[i]
			dirty = RectAround(old.X, old.Y)
			if r.Intn(2) == 0 {
				lvl.Sources = append(lvl.Sources[:i], lvl.Sources[i+1:]...)
			} else {
				n := lvl.Sources[i]
				n.X, n.Y = r.Intn(w), r.Intn(h)
				n.Range = 1 + r.Float64()*9
				lvl.Sources[i] = n
				dirty = dirty.Union(RectAround(n.X, n.Y))
			}
		case 4: // flip space/floor (changes derived starlight)
			x, y := r.Intn(w), r.Intn(h)
			lvl.Space[y*w+x] = !lvl.Space[y*w+x]
			dirty = RectAround(x, y)
		}
		res.Update(lvl, dirty)
		assertSameResult(t, step, res, Compute(lvl, opts))
	}
}

func TestUpdateWithEmptyRectIsNoop(t *testing.T) {
	lvl := NewLevel(10, 10)
	lvl.Sources = []Source{white(5, 5, 4, 1)}
	res := Compute(lvl, DefaultOptions())
	before := append([]RGB(nil), res.sum...)
	res.Update(lvl, Rect{})
	for i := range before {
		if before[i] != res.sum[i] {
			t.Fatalf("empty rect changed result")
		}
	}
}

func TestUpdateOnlyTouchesNearbyCorners(t *testing.T) {
	lvl := NewLevel(120, 40)
	lvl.Sources = []Source{white(5, 20, 6, 1), white(110, 20, 6, 1)}
	opts := DefaultOptions()
	res := Compute(lvl, opts)
	lvl.Occ.(*BoolGrid).Set(6, 20, true)
	n := res.Update(lvl, RectAround(6, 20))
	if n <= 0 || n >= 41*121/2 {
		t.Fatalf("update touched %d corners; expected a small neighbourhood", n)
	}
	assertSameResult(t, 0, res, Compute(lvl, opts))
}
