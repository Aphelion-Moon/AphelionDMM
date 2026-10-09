package theme

import "github.com/SpaiR/imgui-go"

// SpectrumAt samples the site's --spectrum gradient (cyan, green, yellow,
// orange, red, magenta, evenly spaced) at t in 0..1.
func SpectrumAt(t float32) imgui.Vec4 {
	stops := Meridian.Spectrum()
	t = min(1, max(0, t))
	pos := t * float32(len(stops)-1)
	i := min(int(pos), len(stops)-2)
	return Mix(stops[i], stops[i+1], pos-float32(i))
}

// DrawSpectrumStripe fills [min, max] with the spectrum gradient, left to
// right, as the site's 2 px header stripe (--stripe-h).
func DrawSpectrumStripe(list imgui.DrawList, from, to imgui.Vec2) {
	width := to.X - from.X
	if width <= 0 {
		return
	}
	const step = 4 // px per band; narrow enough to read as a gradient
	for x := from.X; x < to.X; x += step {
		end := min(to.X, x+step)
		c := SpectrumAt((x + step/2 - from.X) / width)
		list.AddRectFilled(imgui.Vec2{X: x, Y: from.Y}, imgui.Vec2{X: end, Y: to.Y}, imgui.PackedColorFromVec4(c))
	}
}
