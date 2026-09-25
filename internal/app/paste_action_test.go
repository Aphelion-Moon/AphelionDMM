package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/app/command"
	"sdmm/internal/app/config"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/ui/cpwsarea"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/layout"
	"sdmm/internal/app/ui/menu"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/rsc"
	"sdmm/internal/util"
)

// Application actions and active-workspace resolution are real. External panels,
// configuration persistence and native window callbacks use in-memory adapters.
type pasteActionUI struct {
	*app
	errors []error
	jobs   chan func()
}

func (a *pasteActionUI) ConfigRegister(c config.Config) {
	if c.Name() == "cpwsarea" {
		_ = json.Unmarshal([]byte(fmt.Sprintf(`{"LastChangelogHash":%d}`, util.Djb2(rsc.ChangelogMd))), c)
	}
	a.configs[c.Name()] = c
}
func (*pasteActionUI) Prefs() prefs.Prefs {
	return prefs.Prefs{Editor: prefs.Editor{SaveFormat: prefs.SaveFormatDMM}}
}
func (*pasteActionUI) AddMouseChangeCallback(func(uint, uint)) int                           { return 0 }
func (*pasteActionUI) RemoveMouseChangeCallback(int)                                         {}
func (*pasteActionUI) SyncPrefabs()                                                          {}
func (*pasteActionUI) SyncVarEditor()                                                        {}
func (*pasteActionUI) OnWorkspaceSwitched()                                                  {}
func (*pasteActionUI) IsLayoutReset() bool                                                   { return false }
func (*pasteActionUI) AvailableMaps() []string                                               { return nil }
func (*pasteActionUI) RecentMapsByLoadedEnvironment() []string                               { return nil }
func (*pasteActionUI) SelectedPrefab() (*dmmprefab.Prefab, bool)                             { return nil, false }
func (*pasteActionUI) SelectedInstance() (*dmminstance.Instance, bool)                       { return nil, false }
func (*pasteActionUI) HasSelectedPrefab() bool                                               { return false }
func (*pasteActionUI) HasSelectedInstance() bool                                             { return false }
func (*pasteActionUI) CollaborationPresence() []collabui.ObservedPresence                    { return nil }
func (*pasteActionUI) PublishCollaborationPresence(model.Coord, *protocol.PresenceSelection) {}
func (a *pasteActionUI) RunLater(job func())                                                 { a.jobs <- job }
func (a *pasteActionUI) drainJobs() {
	for {
		select {
		case job := <-a.jobs:
			job()
		default:
			return
		}
	}
}
func (a *pasteActionUI) ReportCollaborationError(_ string, err error) {
	a.errors = append(a.errors, err)
}

func TestPasteApplicationShortcutAndWorkspaceRouting(t *testing.T) {
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		t.Skip("set APHELIONDMM_GL_TEST=1 for application paste routing")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := glfw.Init(); err != nil {
		t.Fatal(err)
	}
	defer glfw.Terminate()
	glfw.WindowHint(glfw.Visible, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	win, err := glfw.CreateWindow(800, 600, "Application paste routing", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer win.Destroy()
	win.MakeContextCurrent()
	if err = gl.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 800, Y: 600})
	io.Fonts().TextureDataRGBA32()
	dir := t.TempDir()
	objects := map[string]*dmenv.Object{}
	for _, path := range []string{"/world", "/area/foo", "/turf/foo", "/obj/foo"} {
		vars := &dmvars.MutableVariables{}
		vars.Put("dir", "2")
		if path == "/world" {
			vars.Put("area", "/area/foo")
			vars.Put("turf", "/turf/foo")
			vars.Put("icon_size", "32")
		}
		objects[path] = &dmenv.Object{Path: path, Vars: vars.ToImmutable()}
	}
	environment := &dmenv.Dme{RootDir: dir, Objects: objects}
	dmmap.PrefabStorage.Free()
	defer dmmap.PrefabStorage.Free()
	dmmap.Init(environment)
	defer dmmap.Free()
	defer brush.Dispose()
	a := &pasteActionUI{app: &app{loadedEnvironment: environment, pathsFilter: dm.NewPathsFilterEmpty(), configs: map[string]config.Config{}, commandStorage: command.NewStorage(), clipboard: dmmclip.New(), layout: &layout.Layout{WsArea: &cpwsarea.WsArea{}}}}
	a.jobs = make(chan func(), 128)
	a.layout.WsArea.Init(a)
	a.menu = menu.New(a)
	openMap := func(name string) *wsmap.WsMap {
		path := filepath.Join(dir, name+".dmm")
		if err := os.WriteFile(path, []byte("\"a\" = (/area/foo,/turf/foo,/obj/foo{dir = 2})\n(1,1,1) = {\"\naaaa\naaaa\naaaa\naaaa\n\"}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		data, err := dmmdata.New(path)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := dmmap.New(environment, data, path)
		a.commandStorage.SetStack(path)
		if !a.layout.WsArea.OpenMap(m, nil) {
			t.Fatal("map did not open")
		}
		for frame := 0; frame < 4; frame++ {
			imgui.NewFrame()
			a.drainJobs()
			a.layout.WsArea.Process(0)
			imgui.Render()
		}
		ws, ok := a.activeWsMap()
		if !ok || ws.Map().Dmm() != m {
			t.Fatal("workspace UI did not activate requested map")
		}
		return ws
	}
	first := openMap("first")
	defer first.Map().Editor().Close()
	g := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	g.Reset()
	g.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}})
	a.DoCopy()
	if !a.clipboard.HasData() {
		t.Fatal("application copy did not fill clipboard")
	}
	first.Map().CanvasState().SetMousePosition(32, 0, 1)
	press := func(keys ...glfw.Key) {
		for _, key := range keys {
			io.KeyPress(int(key))
		}
		imgui.NewFrame()
		a.drainJobs()
		a.layout.WsArea.Process(0)
		shortcut.Process()
		imgui.Render()
		for _, key := range keys {
			io.KeyRelease(int(key))
		}
		imgui.NewFrame()
		a.drainJobs()
		a.layout.WsArea.Process(0)
		shortcut.Process()
		imgui.Render()
	}
	beforePreview, err := first.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeHash, err := beforePreview.Hash()
	if err != nil {
		t.Fatal(err)
	}
	press(glfw.KeyLeftControl, glfw.KeyV)
	if !first.Map().Editor().HasPastePlacement() || !g.Placing() || a.commandStorage.HasUndo() {
		t.Fatal("application Ctrl+V did not start an uncommitted preview")
	}
	// Floating paste is presentation-only. Save must keep the acknowledged map
	// available without including the unconfirmed preview.
	previewSnapshot, err := first.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal("presentation-only paste blocked committed Save", err)
	}
	previewHash, err := previewSnapshot.Hash()
	if err != nil || previewHash != beforeHash || previewSnapshot.Revision != beforePreview.Revision {
		t.Fatal("unconfirmed paste entered committed Save", err)
	}
	press(glfw.KeyEnter)
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, err := first.Map().Editor().SaveSnapshot(context.Background())
		if err == nil && state.Revision == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("asynchronous Enter confirmation did not settle: revision=%d err=%v", state.Revision, err)
		}
		imgui.NewFrame()
		a.drainJobs()
		a.layout.WsArea.Process(0)
		imgui.Render()
		time.Sleep(time.Millisecond)
	}
	state, err := first.Map().Editor().SaveSnapshot(context.Background())
	if err != nil || state.Revision != 1 || g.Placing() {
		t.Fatalf("application Enter did not confirm: revision=%d err=%v", state.Revision, err)
	}
	press(glfw.KeyRightControl, glfw.KeyV)
	if !g.Placing() {
		t.Fatal("right Ctrl+V did not start placement")
	}
	second := openMap("second")
	defer second.Map().Editor().Close()
	if first.Map().Editor().HasPastePlacement() || g.Placing() {
		t.Fatal("workspace switch did not cancel first map placement")
	}
	second.Map().CanvasState().SetMousePosition(64, 0, 1)
	press(glfw.KeyLeftControl, glfw.KeyV)
	if !second.Map().Editor().HasPastePlacement() || first.Map().Editor().HasPastePlacement() {
		t.Fatal("application paste routed to wrong map")
	}
	press(glfw.KeyEscape)
	state, err = second.Map().Editor().SaveSnapshot(context.Background())
	if err != nil || state.Revision != 0 || a.commandStorage.HasUndo() {
		t.Fatal("application Escape changed second map authority/history")
	}
	if len(a.errors) != 0 {
		t.Fatalf("application errors: %v", a.errors)
	}
}
