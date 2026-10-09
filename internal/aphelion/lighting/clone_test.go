package lighting

import "testing"

func TestLevelCloneIsIndependent(t *testing.T) {
	lvl := NewLevel(4, 3)
	lvl.Ambient = make([]RGB, 12)
	lvl.Unlit = make([]bool, 12)
	lvl.Space = make([]bool, 12)
	lvl.Sources = []Source{white(1, 1, 3, 1)}
	lvl.SetOpaque(2, 1, true)
	c := lvl.Clone()
	lvl.SetOpaque(2, 1, false)
	lvl.SetOpaque(0, 0, true)
	lvl.Ambient[5] = RGB{1, 1, 1}
	lvl.Unlit[5] = true
	lvl.Space[5] = true
	lvl.Sources[0].Range = 9
	if !c.Occ.Opaque(2, 1) || c.Occ.Opaque(0, 0) || c.Ambient[5] != (RGB{}) || c.Unlit[5] || c.Space[5] || c.Sources[0].Range != 3 {
		t.Fatal("clone shares storage with the original")
	}
}

func TestResultCloneUpdateMatchesFullCompute(t *testing.T) {
	lvl := NewLevel(20, 20)
	lvl.Sources = []Source{white(5, 5, 6, 1), white(15, 14, 5, 1)}
	opts := DefaultOptions()
	base := Compute(lvl, opts)
	clone := base.Clone()
	lvl.SetOpaque(6, 5, true)
	dirty := RectAround(6, 5)
	clone.Update(lvl, dirty)
	assertSameResult(t, 0, clone, Compute(lvl, opts))
	// The original must still describe the old level.
	assertSameResult(t, 1, base, Compute(NewLevelWith(lvl, 6, 5, false), opts))
}

// NewLevelWith returns a clone of lvl with one opacity edit; test helper.
func NewLevelWith(lvl *Level, x, y int, opaque bool) *Level {
	c := lvl.Clone()
	c.SetOpaque(x, y, opaque)
	return c
}

func TestAffectedTilesCoversEveryTileUpdateCanChange(t *testing.T) {
	const w, h = 60, 12
	lvl := NewLevel(w, h)
	lvl.Sources = []Source{white(10, 6, 16, 1), white(50, 6, 16, 1)}
	opts := DefaultOptions()
	before := Compute(lvl, opts)
	lvl.SetOpaque(30, 6, true)
	lvl.Sources = append(lvl.Sources, white(30, 5, 4, 1))
	dirty := Rect{30, 5, 31, 7}
	after := before.Clone()
	after.Update(lvl, dirty)
	aff := opts.AffectedTiles(dirty, w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if before.Tile(lvl, x, y) != after.Tile(lvl, x, y) && (x < aff.X0 || x >= aff.X1 || y < aff.Y0 || y >= aff.Y1) {
				t.Fatalf("tile (%d,%d) changed outside affected rect %+v", x, y, aff)
			}
		}
	}
	if aff.X0 < 0 || aff.Y0 < 0 || aff.X1 > w || aff.Y1 > h {
		t.Fatalf("affected rect %+v exceeds the level", aff)
	}
}
