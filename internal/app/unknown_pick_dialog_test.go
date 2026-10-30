package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"sdmm/internal/app/command"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/app/ui/layout"
	"sdmm/internal/app/ui/menu"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// visibilityProbe is a modal that records, for every rendered frame, whether
// its window was (re)opened and the opacity its opaque text was drawn with.
type visibilityProbe struct {
	appearing *[]bool
	alpha     *[]byte
}

func (visibilityProbe) Name() string         { return "Unknown Pick Probe" }
func (visibilityProbe) HasCloseButton() bool { return true }
func (p visibilityProbe) Process() {
	*p.appearing = append(*p.appearing, imgui.IsWindowAppearing())
	imgui.TextColored(imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}, "probe")
	vertices, size := imgui.WindowDrawList().VertexBuffer()
	stride, _, _, colorOffset := imgui.VertexBufferLayout()
	var alpha byte
	if size >= stride {
		alpha = unsafe.Slice((*byte)(vertices), size)[size-stride+colorOffset+3]
	}
	*p.alpha = append(*p.alpha, alpha)
}

// TestPickingUnknownTypeKeepsDialogsVisible drives the real application frame
// (menu, docked layout, workspace area and dialog layer) after the Pick tool
// selects an instance whose type the environment does not define.
func TestPickingUnknownTypeKeepsDialogsVisible(t *testing.T) {
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		t.Skip("set APHELIONDMM_GL_TEST=1 for the application dialog frame")
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
	win, err := glfw.CreateWindow(1280, 800, "Unknown pick dialog", nil, nil)
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
	io.SetConfigFlags(imgui.ConfigFlagsDockingEnable)
	io.SetDisplaySize(imgui.Vec2{X: 1280, Y: 800})
	io.Fonts().TextureDataRGBA32()

	dir := t.TempDir()
	objects := map[string]*dmenv.Object{}
	for _, path := range []string{"/world", "/area", "/area/foo", "/turf", "/turf/foo", "/obj", "/mob"} {
		vars := &dmvars.MutableVariables{}
		if path == "/world" {
			vars.Put("area", "/area/foo")
			vars.Put("turf", "/turf/foo")
			vars.Put("icon_size", "32")
		}
		objects[path] = &dmenv.Object{Path: path, Vars: vars.ToImmutable()}
	}
	objects["/area"].DirectChildren = []string{"/area/foo"}
	objects["/turf"].DirectChildren = []string{"/turf/foo"}
	environment := &dmenv.Dme{Name: "unknown", RootDir: dir, Objects: objects}

	a := &app{
		internalDir: dir,
		backupDir:   filepath.Join(dir, "backup"),
		configDir:   filepath.Join(dir, "config"),
	}
	a.loadConfig()
	a.loadProjectConfig()
	a.loadPreferencesConfig()
	a.loadPathMigrationConfig()
	a.shortcutsEnabled = true
	a.commandStorage = command.NewStorage()
	a.pathsFilter = dm.NewPathsFilterEmpty()
	a.clipboard = dmmclip.New()
	a.menu = menu.New(a)
	a.layout = layout.New(a)
	a.updateScale()
	a.updateLayoutState()

	dmmap.PrefabStorage.Free()
	defer dmmap.PrefabStorage.Free()
	a.loadedEnvironment = environment
	a.pathsFilter = newPathsFilter(environment)
	a.layout.Environment.BindFilterEnvironment(environment)
	dmmap.Init(environment)
	defer dmmap.Free()
	defer brush.Dispose()

	frame := func() {
		imgui.NewFrame()
		a.Process()
		imgui.Render()
		a.PostProcess()
	}
	for i := 0; i < 4; i++ {
		frame()
	}

	path := filepath.Join(dir, "unknown.dmm")
	if err := os.WriteFile(path, []byte("\"a\" = (/area/hallway/primary/aft,/turf/foo)\n(1,1,1) = {\"\naa\naa\n\"}\n"), 0600); err != nil {
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
	for i := 0; i < 8; i++ {
		frame()
	}
	ws, ok := a.activeWsMap()
	if !ok {
		t.Fatal("map workspace is not active")
	}
	defer ws.Map().Editor().Close()

	var unknown *dmminstance.Instance
	for _, instance := range m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances() {
		if instance.Prefab().Path() == "/area/hallway/primary/aft" {
			unknown = instance
		}
	}
	if unknown == nil {
		t.Fatal("unknown area instance was not preserved")
	}

	probeDialog := func(label string) {
		t.Helper()
		var appearing []bool
		var alpha []byte
		probe := visibilityProbe{appearing: &appearing, alpha: &alpha}
		dialog.Open(probe)
		for i := 0; i < 8; i++ {
			frame()
		}
		dialog.Close(probe)
		frame()
		if len(appearing) < 8 {
			t.Fatalf("%s: modal rendered on %d of 8 frames", label, len(appearing))
		}
		for i, value := range appearing[1:] {
			if value {
				t.Fatalf("%s: modal was reopened on frame %d and never settled visible", label, i+1)
			}
		}
		for i, value := range alpha {
			if value != 255 {
				t.Fatalf("%s: modal text drawn with alpha %d on frame %d, want 255", label, value, i)
			}
		}
	}
	probeDialog("before pick")

	ws.Map().Editor().InstanceSelect(unknown)
	for i := 0; i < 4; i++ {
		frame()
	}
	probeDialog("after pick")
}
