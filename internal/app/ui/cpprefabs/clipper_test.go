package cpprefabs

import (
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
)

func TestPrefabClipperStrideAndSelectedScrollAcrossIconScales(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	context := imgui.CreateContext(nil)
	defer context.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()

	const count = 100
	const selected = 50
	for _, iconSize := range []float32{16, 32, 48, 64} {
		childID := fmt.Sprintf("rows_%d", int(iconSize))
		var rowStride float32
		positions := make(map[int]float32)
		visibleStart, visibleEnd := -1, -1

		frame := func(scrollToSelection bool) {
			imgui.NewFrame()
			imgui.Begin("prefab_clipper_geometry")
			imgui.BeginChildV(childID, imgui.Vec2{X: 320, Y: 90}, false, 0)
			rowStride = prefabRowHeight(iconSize)
			if scrollToSelection {
				imgui.SetScrollY(prefabScrollY(imgui.CursorPosY(), selected, rowStride, imgui.ContentRegionAvail().Y))
			}
			var clipper imgui.ListClipper
			clipper.BeginV(count, rowStride)
			for clipper.Step() {
				if visibleStart < 0 {
					visibleStart = clipper.DisplayStart
				}
				visibleEnd = clipper.DisplayEnd
				for i := clipper.DisplayStart; i < clipper.DisplayEnd; i++ {
					positions[i] = imgui.CursorScreenPos().Y
					drawPrefabRowContent(
						1, iconSize,
						imgui.Vec2{}, imgui.Vec2{X: 1, Y: 1},
						imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1},
						"prefab name", "icon = test; icon_state = default",
					)
				}
			}
			imgui.EndChild()
			imgui.End()
			imgui.Render()
		}

		frame(false)
		positions = make(map[int]float32)
		visibleStart, visibleEnd = -1, -1
		frame(true)
		positions = make(map[int]float32)
		visibleStart, visibleEnd = -1, -1
		frame(false)

		if visibleStart < 0 || selected < visibleStart || selected >= visibleEnd {
			t.Fatalf("icon size %.0f: selected row %d not in clipped range [%d,%d)", iconSize, selected, visibleStart, visibleEnd)
		}
		if len(positions) > 1 {
			for i := visibleStart; i+1 < visibleEnd; i++ {
				top, hasTop := positions[i]
				next, hasNext := positions[i+1]
				if hasTop && hasNext && math.Abs(float64(next-top-rowStride)) > 0.1 {
					t.Fatalf("icon size %.0f: rows %d and %d are %.2f px apart, want %.2f", iconSize, i, i+1, next-top, rowStride)
				}
			}
		}
	}
}
