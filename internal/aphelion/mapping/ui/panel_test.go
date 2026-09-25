package mappingui

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/dmapi/dmenv"
)

type fixtureApp struct {
	env    *dmenv.Dme
	opened []string
}

func (a *fixtureApp) LoadedEnvironment() *dmenv.Dme { return a.env }
func (*fixtureApp) ActiveMappingPath() string       { return "" }
func (a *fixtureApp) DoLoadResource(path string)    { a.opened = append(a.opened, path) }

func TestInspectorLifecycleDoesNotDimOtherWindows(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	context := imgui.CreateContext(nil)
	defer context.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()
	app := &fixtureApp{env: &dmenv.Dme{}}
	p := New(app)
	for frame := range 8 {
		imgui.NewFrame()
		p.Open()
		p.Process()
		imgui.Begin("Ordinary editing window")
		imgui.TextColored(imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}, "Visible content")
		vertices, size := imgui.WindowDrawList().VertexBuffer()
		stride, _, _, offset := imgui.VertexBufferLayout()
		if size < stride || unsafe.Slice((*byte)(vertices), size)[size-stride+offset+3] != 255 {
			t.Fatalf("frame %d dimmed other content", frame)
		}
		imgui.End()
		imgui.Render()
		p.Invalidate()
	}
	if len(app.opened) != 0 {
		t.Fatal("inspector implicitly opened an editable map")
	}
}

func TestEnvironmentReplacementRejectsOldSelectionsAndQueuedSources(t *testing.T) {
	app := &fixtureApp{env: &dmenv.Dme{}}
	p := New(app)
	p.open = true
	p.environment = app.env
	p.choices = map[string]mapping.Choice{"root": {Slot: 2}}
	p.fixedChoices = map[string]int{"fixed": 3}
	p.queue(nil)
	app.env = &dmenv.Dme{}
	p.advance()
	if p.pending != nil || len(p.choices) != 0 || len(p.fixedChoices) != 0 {
		t.Fatal("old project state survived replacement")
	}
}
