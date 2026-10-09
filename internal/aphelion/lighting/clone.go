package lighting

// Clone returns a deep copy of the level. The occlusion grid is copied into a
// BoolGrid, so a clone is safe to hand to a worker while the original keeps
// changing on another goroutine.
func (l *Level) Clone() *Level {
	c := &Level{W: l.W, H: l.H, Starlight: l.Starlight}
	grid := NewBoolGrid(l.W, l.H)
	if g, ok := l.Occ.(*BoolGrid); ok {
		copy(grid.Bits, g.Bits)
	} else if l.Occ != nil {
		for y := 0; y < l.H; y++ {
			for x := 0; x < l.W; x++ {
				grid.Bits[y*l.W+x] = l.Occ.Opaque(x, y)
			}
		}
	}
	c.Occ = grid
	if l.Ambient != nil {
		c.Ambient = append([]RGB(nil), l.Ambient...)
	}
	if l.Unlit != nil {
		c.Unlit = append([]bool(nil), l.Unlit...)
	}
	if l.Space != nil {
		c.Space = append([]bool(nil), l.Space...)
	}
	if l.Sources != nil {
		c.Sources = append([]Source(nil), l.Sources...)
	}
	return c
}

// Clone returns an independent copy of the result, so Update can run on the
// copy while the original keeps being read.
func (r *Result) Clone() *Result {
	c := *r
	c.sum = append([]RGB(nil), r.sum...)
	return &c
}

// AffectedTiles returns the tile rectangle whose displayed values can differ
// after Result.Update(lvl, dirty): every tile touching a recomputed corner.
func (o Options) AffectedTiles(dirty Rect, w, h int) Rect {
	if dirty.Empty() {
		return Rect{}
	}
	return clipRect(dirty.expand(2*reach(o)+1), Rect{0, 0, w, h})
}
