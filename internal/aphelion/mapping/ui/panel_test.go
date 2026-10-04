package mappingui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/util"
)

type fixtureApp struct {
	env    *dmenv.Dme
	opened []string
}

func TestReferenceTransformIsBoundToSourceAndRefresh(t *testing.T) {
	p := New(&fixtureApp{env: &dmenv.Dme{}})
	anchor := util.Point{X: 5, Y: 7, Z: 1}
	p.OpenSources("parent.dmm", "module.dmm", &anchor)
	p.offset = [3]int32{4, 6, 0}
	p.queue(nil)
	if p.pending.anchor == nil || *p.pending.anchor != anchor {
		t.Fatal("accepted-source refresh lost anchor")
	}
	p.referencePath = "unrelated.dmm"
	p.queue(nil)
	if p.pending.anchor != nil || p.offset != [3]int32{} {
		t.Fatal("new reference inherited old placement")
	}
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

func TestReferenceThumbnailReusesRequestDisplay(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"parent.dmm", "reference.dmm"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("\"a\" = (/turf/a,/area/a)\n(1,1,1) = {\"\na\n\"}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	app := &fixtureApp{env: &dmenv.Dme{RootDir: root}}
	p := New(app)
	p.open, p.environment = true, app.env
	defer p.Invalidate()
	for _, reference := range []string{"parent.dmm", "reference.dmm"} {
		p.pending = &request{environment: app.env, parent: "parent.dmm", reference: reference, thumbnail: true}
		p.advance()
		select {
		case completed := <-p.results:
			p.results = nil
			p.cancel()
			if completed.err != nil {
				completed.close()
				t.Fatal(completed.err)
			}
			if completed.thumbnailDisplay == nil || completed.thumbnailDisplay != completed.displays[1] {
				completed.close()
				t.Fatal("thumbnail duplicated its request's reference display")
			}
			same := reference == "parent.dmm"
			if (completed.displays[0] == completed.displays[1]) != same {
				completed.close()
				t.Fatal("display reuse did not follow source identity")
			}
			completed.close()
		case <-time.After(5 * time.Second):
			t.Fatal("reference request did not complete")
		}
	}
}
