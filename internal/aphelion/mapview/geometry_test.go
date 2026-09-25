package mapview

import (
	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/render"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"testing"
)

func TestScreenCoordinatesRoundTripAcrossPaneAndScale(t *testing.T) {
	old := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = old })
	for _, scale := range []float32{.25, 1, 1.5, 2} {
		camera := render.Camera{Scale: scale, ShiftX: 37, ShiftY: -19, Level: 3}
		size, origin := imgui.Vec2{X: 533, Y: 379}, imgui.Vec2{X: 287, Y: 53}
		point := util.Point{X: 12, Y: 7, Z: 3}
		screen := Screen(camera, size, origin, point)
		screen.X += 16 * scale
		screen.Y -= 16 * scale
		if got := Tile(camera, size, origin, screen); got != point {
			t.Fatalf("scale %g: %v", scale, got)
		}
		before := Tile(camera, size, origin, screen)
		Zoom(&camera, size, screen.Minus(origin), true)
		if got := Tile(camera, size, origin, screen); got != before {
			t.Fatalf("zoom lost pointer anchor: %v != %v", got, before)
		}
	}
}

func TestFitUsesDestinationBoundsAndDeck(t *testing.T) {
	old := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = old })
	size := imgui.Vec2{X: 450, Y: 290}
	lo, hi := util.Point{X: 20, Y: 32, Z: 3}, util.Point{X: 37, Y: 40, Z: 3}
	camera := render.Camera{Scale: 1, ShiftX: 900, ShiftY: -50, Level: 1}
	Fit(&camera, size, lo, hi)
	bottom := Screen(camera, size, imgui.Vec2{}, lo)
	hi.X++
	hi.Y++
	top := Screen(camera, size, imgui.Vec2{}, hi)
	if camera.Level != 3 || bottom.X < 0 || top.X > size.X || top.Y < 0 || bottom.Y > size.Y {
		t.Fatal("fit clipped transformed placement", camera, bottom, top)
	}
}
