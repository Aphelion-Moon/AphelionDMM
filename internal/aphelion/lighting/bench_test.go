package lighting

import (
	"math/rand"
	"testing"
)

func benchLevel() *Level {
	const n = 255
	r := rand.New(rand.NewSource(7))
	lvl := NewLevel(n, n)
	g := lvl.Occ.(*BoolGrid)
	for i := 0; i < n*n; i++ {
		if r.Intn(6) == 0 {
			g.Set(i%n, i/n, true)
		}
	}
	for i := 0; i < 500; i++ {
		s := white(r.Intn(n), r.Intn(n), 5+float64(r.Intn(5)), 1)
		if r.Intn(3) == 0 {
			s.Angle = 170
			s.Dir = DirSouth
		}
		lvl.Sources = append(lvl.Sources, s)
	}
	return lvl
}

func BenchmarkComputeFull255x255With500Sources(b *testing.B) {
	lvl := benchLevel()
	opts := DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Compute(lvl, opts)
	}
}

func BenchmarkUpdateOneTileEdit255x255With500Sources(b *testing.B) {
	lvl := benchLevel()
	opts := DefaultOptions()
	res := Compute(lvl, opts)
	g := lvl.Occ.(*BoolGrid)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, y := 100+i%50, 100+(i/50)%50
		g.Set(x, y, !g.Opaque(x, y))
		res.Update(lvl, RectAround(x, y))
	}
}

func BenchmarkTileOutput255x255(b *testing.B) {
	lvl := benchLevel()
	res := Compute(lvl, DefaultOptions())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for y := 0; y < 255; y++ {
			for x := 0; x < 255; x++ {
				_ = res.Tile(lvl, x, y)
			}
		}
	}
}
