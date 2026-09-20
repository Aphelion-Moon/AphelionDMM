package pmap

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func TestStampDialogLoadSaveAndExplicitPreview(t *testing.T) {
	point := util.Point{X: 1, Y: 1, Z: 1}
	s, err := stamps.Capture("User stamp", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", &dmmap.Dmm{MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{{Coord: point}}}, []util.Point{point}, dm.NewPathsFilterEmpty())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "selection.admmstamp")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	placed, remembered := 0, 0
	d := &stampDialog{openPath: func() (string, error) { return path, nil }, savePath: func() (string, error) { return path, nil },
		matches: func(*stamps.Stamp) bool { return false }, remember: func(*stamps.Stamp) { remembered++ },
		place: func(_ *stamps.Stamp, allow bool) error {
			if !allow {
				t.Fatal("preview omitted environment acknowledgement")
			}
			placed++
			return errors.New("busy editor")
		}}
	d.load()
	if placed != 0 || remembered != 1 || d.stamp == nil {
		t.Fatal("load placed map data or lost the stamp")
	}
	d.save()
	if placed != 0 {
		t.Fatal("save started placement")
	}
	if d.preview() {
		t.Fatal("different environment did not require acknowledgement")
	}
	d.allowDifferent = true
	if d.preview() || placed != 1 || d.status != "busy editor" {
		t.Fatal("failed preview closed dialog or lost refusal")
	}
	retained := d.stamp
	d.openPath = func() (string, error) { return filepath.Join(path, "invalid"), nil }
	d.load()
	if d.stamp != retained || remembered != 1 {
		t.Fatal("failed load replaced retained stamp")
	}
	// Drive the actual review widget's Preview button. A refused placement must
	// keep the dialog and its loaded data available for another user action.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 900})
	io.Fonts().TextureDataRGBA32()
	d.canCapture = func() bool { return false }
	d.canPlace = func() bool { return true }
	frame := func(pos imgui.Vec2, down bool) imgui.Vec2 {
		io.SetMousePosition(pos)
		io.SetMouseButtonDown(0, down)
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 950, Y: 850})
		imgui.Begin("Stamp controls")
		d.Process()
		min, max := imgui.ItemRectMin(), imgui.ItemRectMax()
		imgui.End()
		imgui.EndFrame()
		return imgui.Vec2{X: (min.X + max.X) / 2, Y: (min.Y + max.Y) / 2}
	}
	button := frame(imgui.Vec2{X: -1, Y: -1}, false)
	button = frame(button, false)
	frame(button, true)
	frame(button, false)
	if placed != 2 || d.stamp != retained || d.status != "busy editor" {
		t.Fatal("Preview button lost its guarded placement action")
	}
}
