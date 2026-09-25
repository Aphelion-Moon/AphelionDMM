package window_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/filterprofiles"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
)

func (a *mouseNetworkApp) SetFilterVisibility(path string, scope filterprofiles.Scope, visible bool) error {
	if a.visibilitySession == nil {
		return errors.New("visibility is unavailable in this fixture")
	}
	return a.visibilitySession.SetVisibility(path, scope, visible, a.environment, a.PathsFilter())
}

// Actual ImGui Alt/mouse frames exercise tool input, hit selection, the editor
// adapter and shared filter semantics. Queue ordering is checked by the panel
// tests; this native fixture keeps publication synchronous to isolate input.
func TestNativeAltPickHidesOnceAndPreservesMap(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	app.visibilitySession = &filterprofiles.Session{}
	if err := app.visibilitySession.Apply(filterprofiles.DefaultProfile(), app.environment, app.PathsFilter()); err != nil {
		t.Fatal(err)
	}
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tools.SetSelected(tools.TNPick)
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	frame(false, 1, 1)
	frame(false, 1, 1)
	target := e.HoveredInstance()
	if target == nil {
		t.Fatal("actual canvas hit did not resolve")
	}
	path := target.Prefab().Path()
	io := imgui.CurrentIO()
	io.KeyPress(int(glfw.KeyLeftAlt))
	frame(true, 1, 1)
	for range 4 {
		frame(true, 2, 2)
	}
	frame(false, 2, 2)
	io.KeyRelease(int(glfw.KeyLeftAlt))
	frame(false, 2, 2)
	if hidden := app.PathsFilter().HiddenPaths(); !reflect.DeepEqual(hidden, []string{path}) {
		t.Fatalf("physical click hid wrong or multiple types: %v, target %s", hidden, path)
	}
	after, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) || app.commands.HasUndoV(ws.CommandStackId()) || app.selectedPrefab != nil {
		t.Fatal("view-only Alt-Pick changed map authority, history, or placement prefab", err)
	}
	if err := app.visibilitySession.UnhideLast(app.environment, app.PathsFilter()); err != nil {
		t.Fatal(err)
	}
	frame(false, 1, 1)
	if !app.PathsFilter().IsVisiblePath(path) || tools.Selected().Name() != tools.TNPick {
		t.Fatal("same-session recovery did not retain Pick")
	}
}
