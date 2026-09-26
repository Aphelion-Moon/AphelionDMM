package mappingui

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/app/window"
	"sdmm/internal/platform"
	"sdmm/internal/util"
)

// This captures the actual sidebar/header widgets at narrow widths and scaled
// fonts. It is visual layout evidence, not an uncoached mapper usability test.
func TestNativeCompositionLayout(t *testing.T) {
	output := os.Getenv("APHELION_COMPOSITION_LAYOUT_OUTPUT")
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" || output == "" {
		t.Skip("set native GL opt-in and layout output directory")
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
	win, err := glfw.CreateWindow(1280, 768, "Composition layout", nil, nil)
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
	io.SetDisplaySize(imgui.Vec2{X: 1280, Y: 768})
	io.SetDisplayFrameBufferScale(imgui.Vec2{X: 1, Y: 1})
	io.SetDeltaTime(1.0 / 60)
	platform.InitImGuiGL()
	defer platform.DisposeImGuiGL()
	if err = os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	for _, scale := range []float32{1, 1.5, 2} {
		for _, failed := range []bool{false, true} {
			p := New(&fixtureApp{})
			p.open, p.compose = true, true
			p.parentPath = "tramstation.dmm"
			p.focusRoot = "nested"
			p.contextPath = "maintenance_module.dmm"
			p.contextRoot = "nested"
			roots := []mapping.Root{{ID: "root", Key: "Engineering", Destination: util.Point{X: 100, Y: 140, Z: 2}}, {ID: "nested", Parent: "root", Key: "Maintenance attachment", Destination: util.Point{X: 101, Y: 142, Z: 2}, Candidates: []mapping.Candidate{{Slot: 0, Path: "maintenance_module.dmm"}, {Slot: 1, Path: "alternate_module.dmm"}}}}
			p.current = &result{roots: roots, projection: &mapping.Projection{Placements: []mapping.Placement{{Root: roots[1], Source: mapping.Identity{Path: p.contextPath}}}}}
			p.treeDirty, p.revealRoot = true, true
			if failed {
				p.operation = previewOperation{target: "alternate_module.dmm", err: errors.New("preview admission refused")}
			}
			window.SetPointSize(scale)
			for range 3 {
				imgui.NewFrame()
				imgui.SetNextWindowPos(imgui.Vec2{})
				imgui.SetNextWindowSize(imgui.Vec2{X: 328, Y: 768})
				imgui.BeginV("Composition", nil, imgui.WindowFlagsNoMove|imgui.WindowFlagsNoResize)
				p.sidebar()
				if imgui.ScrollMaxX() > 0 {
					t.Errorf("scale %g: sidebar requires horizontal scrolling", scale)
				}
				imgui.End()
				imgui.SetNextWindowPos(imgui.Vec2{X: 328})
				imgui.SetNextWindowSize(imgui.Vec2{X: 952, Y: 768})
				imgui.BeginV("Source", nil, imgui.WindowFlagsNoMove|imgui.WindowFlagsNoResize)
				p.ContextHeader(p.contextPath, true)
				if imgui.ScrollMaxX() > 0 {
					t.Errorf("scale %g: context header requires horizontal scrolling", scale)
				}
				imgui.End()
				imgui.Render()
				gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
				gl.DrawBuffer(gl.BACK)
				gl.ReadBuffer(gl.BACK)
				gl.ClearColor(.08, .08, .08, 1)
				gl.Clear(gl.COLOR_BUFFER_BIT)
				platform.Render(imgui.RenderedDrawData())
			}
			pixels := make([]byte, 1280*768*4)
			gl.ReadPixels(0, 0, 1280, 768, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&pixels[0]))
			raster := image.NewRGBA(image.Rect(0, 0, 1280, 768))
			for y := 0; y < 768; y++ {
				copy(raster.Pix[y*raster.Stride:(y+1)*raster.Stride], pixels[(767-y)*1280*4:(768-y)*1280*4])
			}
			name := fmt.Sprintf("composition-%d", int(scale*100))
			if failed {
				name += "-failed"
			}
			file, err := os.Create(filepath.Join(output, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, raster)
			closeErr := file.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
	}
	if code := gl.GetError(); code != gl.NO_ERROR {
		t.Fatalf("GL error: %x", code)
	}
}
