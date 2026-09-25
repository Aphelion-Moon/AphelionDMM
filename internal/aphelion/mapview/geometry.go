// Package mapview shares the editor's source/destination/screen conventions
// between ordinary maps and document-bound composition views.
package mapview

import (
	"github.com/SpaiR/imgui-go"
	"math"
	"sdmm/internal/app/render"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

const ScaleFactor float32 = math.Sqrt2

func Center(camera *render.Camera, size imgui.Vec2, point util.Point) {
	camera.ShiftX = size.X/(2*camera.Scale) - (float32(point.X)-.5)*float32(dmmap.WorldIconSize)
	camera.ShiftY = size.Y/(2*camera.Scale) - (float32(point.Y)-.5)*float32(dmmap.WorldIconSize)
	camera.Level = point.Z
}

// Fit frames inclusive destination cells; callers choose the deck explicitly.
func Fit(camera *render.Camera, size imgui.Vec2, lower, upper util.Point) {
	if size.X <= 0 || size.Y <= 0 || dmmap.WorldIconSize <= 0 {
		return
	}
	w, h := float32((upper.X-lower.X+1)*dmmap.WorldIconSize), float32((upper.Y-lower.Y+1)*dmmap.WorldIconSize)
	if w <= 0 || h <= 0 {
		return
	}
	camera.Scale = min(size.X/(w+64), size.Y/(h+64))
	camera.ShiftX = size.X/(2*camera.Scale) - float32((lower.X-1)*dmmap.WorldIconSize) - w/2
	camera.ShiftY = size.Y/(2*camera.Scale) - float32((lower.Y-1)*dmmap.WorldIconSize) - h/2
	camera.Level = lower.Z
}
func Screen(camera render.Camera, size, origin imgui.Vec2, point util.Point) imgui.Vec2 {
	return imgui.Vec2{X: origin.X + (float32((point.X-1)*dmmap.WorldIconSize)+camera.ShiftX)*camera.Scale, Y: origin.Y + size.Y - (float32((point.Y-1)*dmmap.WorldIconSize)+camera.ShiftY)*camera.Scale}
}
func Tile(camera render.Camera, size, origin, screen imgui.Vec2) util.Point {
	x := (screen.X-origin.X)/camera.Scale - camera.ShiftX
	y := (size.Y-screen.Y+origin.Y)/camera.Scale - camera.ShiftY
	return util.Point{X: int(math.Floor(float64(x/float32(dmmap.WorldIconSize)))) + 1, Y: int(math.Floor(float64(y/float32(dmmap.WorldIconSize)))) + 1, Z: camera.Level}
}
func Pan(camera *render.Camera, delta imgui.Vec2) {
	camera.Translate(delta.X/camera.Scale, -delta.Y/camera.Scale)
}
func Zoom(camera *render.Camera, size, pointer imgui.Vec2, in bool) {
	scale := camera.Scale
	camera.Zoom(in, ScaleFactor)
	camera.Translate(pointer.X/camera.Scale-pointer.X/scale, (size.Y-pointer.Y)/camera.Scale-(size.Y-pointer.Y)/scale)
}
