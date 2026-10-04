package mappingui

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
)

func TestDifferenceOverlayUsesTransformedSourceCells(t *testing.T) {
	previousIconSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = previousIconSize })
	root := t.TempDir()
	environment := &dmenv.Dme{}
	load := func(name, data string) *mapping.Source {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		source, err := mapping.LoadSource(context.Background(), path, environment)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(source.Close)
		return source
	}
	base := load("base.dmm", "\"a\" = (/turf/a,/area/a,/obj/a{v = 1})\n(1,1,1) = {\"\naaaa\n\"}\n")
	reference := load("reference.dmm", "\"a\" = (/turf/b,/area/a,/obj/a{v = 1})\n\"b\" = (/turf/a,/area/b,/obj/a{v = 1})\n\"c\" = (/turf/a,/area/a,/obj/a{v = 2})\n(1,1,1) = {\"\nabc\n\"}\n")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()
	p := New(&fixtureApp{})
	p.current = &result{sources: [2]*mapping.Source{base, reference}}
	p.camera.Scale = 1
	p.level = 1
	p.offset[0] = 1
	p.viewSize = imgui.Vec2{X: 128, Y: 32}
	wantColors := []imgui.PackedColor{
		imgui.PackedColorFromVec4(imgui.Vec4{X: 1, W: .45}),
		imgui.PackedColorFromVec4(imgui.Vec4{Y: 1, W: .45}),
		imgui.PackedColorFromVec4(imgui.Vec4{Z: 1, W: .45}),
	}
	for range 2 {
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 600, Y: 400})
		imgui.Begin("Differences")
		start := imgui.CursorScreenPos()
		_, before := imgui.WindowDrawList().VertexBuffer()
		p.drawDifferences(start, start.Plus(p.viewSize))
		vertices, after := imgui.WindowDrawList().VertexBuffer()
		stride, positionOffset, _, colorOffset := imgui.VertexBufferLayout()
		data := unsafe.Slice((*byte)(vertices), after)
		var seen [3]bool
		for offset := before; offset < after; offset += stride {
			color := binary.LittleEndian.Uint32(data[offset+colorOffset:])
			if color>>24 == 0 { // Ignore transparent antialiasing fringe vertices.
				continue
			}
			cell := -1
			for i, want := range wantColors {
				if color == uint32(want) {
					cell = i
				}
			}
			if cell < 0 {
				t.Errorf("unexpected overlay color %#x", color)
				continue
			}
			seen[cell] = true
			x := math.Float32frombits(binary.LittleEndian.Uint32(data[offset+positionOffset:])) - start.X
			y := math.Float32frombits(binary.LittleEndian.Uint32(data[offset+positionOffset+4:])) - start.Y
			if x < float32((cell+1)*32)-1 || x > float32((cell+2)*32)+1 || y < -1 || y > 33 {
				t.Errorf("channel %d drawn outside translated cell: %g,%g", cell, x, y)
			}
		}
		if seen != [3]bool{true, true, true} {
			t.Errorf("missing turf/area/object difference overlay: %v", seen)
		}
		imgui.End()
		imgui.Render()
	}
}
