package window_test

import (
	"context"
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"reflect"
	"runtime"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestCurrentFrameErasePressAndRelease(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	tools.SetSelected(tools.TNDelete)
	frame(false, 1, 1)
	frame(false, 1, 1)
	frame(true, 1, 1)
	if len(e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()) != 2 {
		t.Fatal("press used previous-frame button or pick")
	}
	if app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("stroke committed before release")
	}
	frame(false, 2, 1)
	if !app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("release was delayed despite consumed samples")
	}
	snapshot, err := e.SaveSnapshot(context.Background())
	if err != nil || snapshot.Revision != 1 {
		t.Fatal("release did not publish one coherent revision", err)
	}
	if len(e.Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()) != 2 {
		t.Fatal("release omitted its final pointer position")
	}
}

func TestQueuedSelectionMoveUsesLatestValidPoseOnRelease(t *testing.T) {
	for _, scenario := range []struct {
		name                                             string
		finalX, finalY                                   int
		sparse, rotate, cancel, allInvalid, priorInvalid bool
		bounds                                           util.Bounds
	}{
		{name: "latest", finalX: 3, finalY: 2, bounds: util.Bounds{X1: 3, Y1: 2, X2: 4, Y2: 2}},
		{name: "selection_leaves_map", finalX: 4, finalY: 2, bounds: util.Bounds{X1: 3, Y1: 2, X2: 4, Y2: 2}},
		{name: "cursor_leaves_map", finalX: 5, finalY: 2, bounds: util.Bounds{X1: 3, Y1: 2, X2: 4, Y2: 2}},
		{name: "all_invalid", finalX: 4, finalY: 4, allInvalid: true, bounds: util.Bounds{X1: 2, Y1: 1, X2: 3, Y2: 1}},
		{name: "sparse", finalX: 4, finalY: 2, sparse: true, bounds: util.Bounds{X1: 3, Y1: 2, X2: 4, Y2: 3}},
		{name: "rotated", finalX: 4, finalY: 4, rotate: true, bounds: util.Bounds{X1: 3, Y1: 2, X2: 3, Y2: 3}},
		{name: "rotated_after_invalid", finalX: 4, finalY: 1, rotate: true, priorInvalid: true, bounds: util.Bounds{X1: 4, Y1: 1, X2: 4, Y2: 2}},
		{name: "cancel", finalX: 3, finalY: 2, cancel: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ws, app := newMouseNetworkWorkspace(t)
			e := ws.Map().Editor()
			initial, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
			if scenario.sparse {
				if err := grab.SelectMask([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 1, Y: 2, Z: 1}}); err != nil {
					t.Fatal(err)
				}
			}
			count := grab.Selection().Len()
			ws.Map().SetShortcutsVisible(true)
			frame := mouseWorkspaceFrame(t, ws, app.mouse, shortcut.Process)
			frame(false, 1, 1)
			frame(false, 1, 1)
			frame(true, 1, 1)
			if !tools.OwnsGesture(e) {
				t.Fatal("fixture did not start a selection move")
			}
			if scenario.rotate {
				if scenario.priorInvalid {
					frame(true, 2, 1)
					frame(true, 4, 1) // Cursor advances while the wider footprint stays at 2.
				}
				imgui.CurrentIO().KeyPress(int(glfw.KeyE))
				if scenario.priorInvalid {
					frame(true, 4, 1)
				} else {
					frame(true, 1, 1)
				}
				imgui.CurrentIO().KeyRelease(int(glfw.KeyE))
				if !scenario.priorInvalid {
					frame(true, 1, 1)
				}
			}
			if scenario.allInvalid {
				frame(true, 2, 1)
			}
			sample := func(x, y int) { app.mouse(uint((x-1)*32+16), uint(128-((y-1)*32+16))) }
			for range 1024 {
				if scenario.allInvalid {
					sample(4, 4)
				} else {
					sample(2, 1)
				}
			}
			if !scenario.allInvalid {
				sample(3, 2)
				if scenario.finalX != 3 || scenario.finalY != 2 {
					for range 64 {
						sample(scenario.finalX, scenario.finalY)
					}
				}
			}
			if scenario.cancel {
				imgui.CurrentIO().KeyPress(int(glfw.KeyEscape))
			}
			frame(false, scenario.finalX, scenario.finalY)
			imgui.CurrentIO().KeyRelease(int(glfw.KeyEscape))
			if tools.OwnsGesture(e) {
				t.Fatal("release waited for obsolete absolute pointer samples")
			}
			if scenario.cancel {
				// Escape on release also executes the normal deselect shortcut.
				if grab.HasSelectedArea() {
					t.Fatal("Escape retained a released selection")
				}
			} else if grab.Bounds() != scenario.bounds || grab.Selection().Len() != count {
				t.Fatalf("latest valid selection = %v (%d cells), want %v (%d cells)", grab.Bounds(), grab.Selection().Len(), scenario.bounds, count)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				committed, err := e.SaveSnapshot(context.Background())
				if err == nil && e.CanStartMapEdit() {
					if scenario.cancel {
						if !reflect.DeepEqual(committed.Tiles, initial.Tiles) || app.commands.HasUndoV(ws.CommandStackId()) {
							t.Fatal("cancelling a queued move changed authority or history")
						}
					} else {
						if committed.Revision != initial.Revision+1 {
							t.Fatalf("release revision = %d, want %d", committed.Revision, initial.Revision+1)
						}
						app.commands.UndoV(ws.CommandStackId())
						undone, err := e.SaveSnapshot(context.Background())
						if err != nil || !reflect.DeepEqual(undone.Tiles, initial.Tiles) {
							t.Fatal("undo lost exact source contents", err)
						}
						app.commands.RedoV(ws.CommandStackId())
						redone, err := e.SaveSnapshot(context.Background())
						if err != nil || !reflect.DeepEqual(redone.Tiles, committed.Tiles) {
							t.Fatal("redo lost exact destination contents", err)
						}
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("released move did not settle", err)
				}
				frame(false, 1, 1)
				runtime.Gosched()
			}
		})
	}
}

func TestQueuedBrushKeepsIntermediateTiles(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	tools.SetSelected(tools.TNDelete)
	frame(false, 1, 1)
	frame(false, 1, 1)
	frame(true, 1, 1)
	path := []util.Point{{X: 2, Y: 1, Z: 1}, {X: 2, Y: 2, Z: 1}, {X: 3, Y: 2, Z: 1}, {X: 3, Y: 3, Z: 1}}
	for _, p := range path {
		app.mouse(uint((p.X-1)*32+16), uint(128-((p.Y-1)*32+16)))
	}
	frame(false, 4, 3)
	for _, p := range append(path, util.Point{X: 4, Y: 3, Z: 1}) {
		if len(e.Dmm().GetTile(p).Instances()) != 2 {
			t.Fatal("brush lost intermediate stroke tile", p)
		}
	}
}
