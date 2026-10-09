package theme

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/SpaiR/imgui-go"
)

// TestOverlayContrastProbe measures how often candidate map-overlay colours
// blend into real sprite pixels (WCAG contrast below 1.5). It is a design aid
// for tuning the overlay palette: set PROBE_ROOT to a codebase checkout.
func TestOverlayContrastProbe(t *testing.T) {
	root := os.Getenv("PROBE_ROOT")
	if root == "" {
		t.Skip("set PROBE_ROOT to a codebase checkout")
	}
	var files []string
	for _, pattern := range []string{"icons/turf/floors.dmi", "icons/turf/floors/*.dmi", "icons/turf/walls/*.dmi", "icons/obj/smooth_structures/*.dmi", "icons/turf/space.dmi", "icons/obj/doors/airlocks/station/*.dmi", "icons/obj/machines/*.dmi", "icons/obj/pipes_n_cables/*.dmi"} {
		m, _ := filepath.Glob(filepath.Join(root, pattern))
		files = append(files, m...)
	}
	var lum []float64
	for _, f := range files {
		r, err := os.Open(f)
		if err != nil {
			continue
		}
		img, err := png.Decode(r)
		_ = r.Close()
		if err != nil {
			continue
		}
		b := img.Bounds()
		step := max(1, b.Dx()*b.Dy()/4000)
		i := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				i++
				if i%step != 0 {
					continue
				}
				cr, cg, cb, ca := img.At(x, y).RGBA()
				if ca < 0xC000 {
					continue
				}
				lum = append(lum, luminanceOf(imgui.Vec4{X: float32(cr) / 65535, Y: float32(cg) / 65535, Z: float32(cb) / 65535, W: 1}))
			}
		}
	}
	if len(lum) == 0 {
		t.Fatal("no sprite pixels sampled")
	}
	sort.Float64s(lum)
	t.Logf("%d files, %d opaque pixels sampled", len(files), len(lum))
	report := func(name string, c imgui.Vec4) {
		cl := luminanceOf(c)
		low := 0
		for _, l := range lum {
			a, b := math.Max(cl, l), math.Min(cl, l)
			if (a+0.05)/(b+0.05) < 1.5 {
				low++
			}
		}
		t.Logf("%-24s blends into %5.1f%% of sprite pixels", name, 100*float64(low)/float64(len(lum)))
	}
	p := Meridian
	white := imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}
	for _, c := range []struct {
		name string
		v    imgui.Vec4
	}{
		{"classic green (0,1,0)", imgui.Vec4{Y: 1, W: 1}},
		{"meridian green", Mix(p.Green, white, 0.3)},
		{"classic red (1,0,0)", imgui.Vec4{X: 1, W: 1}},
		{"meridian red", Mix(p.Red, white, 0.25)},
		{"classic gold #ffd700", hex(0xffd700)},
		{"meridian yellow", Mix(p.Yellow, white, 0.2)},
		{"classic intersect blue", imgui.Vec4{X: 0.3, Y: 0.65, Z: 1, W: 1}},
		{"meridian cyan", p.Cyan},
	} {
		report(c.name, c.v)
	}
}

func luminanceOf(c imgui.Vec4) float64 {
	ch := func(v float32) float64 {
		x := float64(v)
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.X) + 0.7152*ch(c.Y) + 0.0722*ch(c.Z)
}
