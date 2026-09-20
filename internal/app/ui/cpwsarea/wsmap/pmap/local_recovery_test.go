package pmap

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing/recovery"
	"sdmm/internal/dmapi/dmmap"
)

func TestLocalRecoveryDialogRequiresExplicitDiscard(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 900})
	io.Fonts().TextureDataRGBA32()
	record, err := recovery.Capture(&dmmap.Dmm{}, model.Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	d := &localRecoveryDialog{record: record, discard: func() error { calls++; return errors.New("map changed") }}
	frame := func(pos imgui.Vec2, down bool) imgui.Vec2 {
		io.SetMousePosition(pos)
		io.SetMouseButtonDown(0, down)
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{X: 0, Y: 0})
		imgui.SetNextWindowSize(imgui.Vec2{X: 950, Y: 850})
		imgui.Begin("Recovery controls")
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
	if calls != 0 || !d.confirm {
		t.Fatal("first click discarded without confirmation")
	}
	button = frame(imgui.Vec2{X: -1, Y: -1}, false)
	frame(button, true)
	frame(button, false)
	if calls != 1 || d.status != "map changed" {
		t.Fatal("confirmation did not call guarded discard or display failure", calls, d.status)
	}
}

func TestLocalRecoveryDialogExportKeepsRecord(t *testing.T) {
	record, err := recovery.Capture(&dmmap.Dmm{}, model.Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recovery.json")
	d := &localRecoveryDialog{record: record, exportPath: func() (string, error) { return path, nil }, discard: func() error { t.Fatal("export discarded record"); return nil }}
	d.export()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if d.record != record || d.confirm {
		t.Fatal("export changed retained record or approved discard")
	}
}
