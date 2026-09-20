package window

import (
	"time"
	// APHELION EDIT ADDITION START - SHORTCUT FOCUS
	"sdmm/internal/app/ui/shortcut"
	// APHELION EDIT ADDITION END

	"sdmm/internal/platform"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
)

var ticker = newTicker(60)

func newTicker(fps int) *time.Ticker {
	return time.NewTicker(time.Second / time.Duration(fps))
}

func (w *Window) Process() {
	for !w.application.IsClosed() {
		// Override window closing behaviour to enforce our checks.
		if w.handle.ShouldClose() {
			w.application.CloseCheck()
			w.handle.SetShouldClose(false)
		}
		w.runFrame()
		<-ticker.C
	}
}

func (w *Window) runFrame() {
	w.startFrame()
	w.application.Process()
	w.endFrame()
	w.application.PostProcess()
}

func (w *Window) startFrame() {
	gl.Clear(gl.COLOR_BUFFER_BIT)
	platform.NewImGuiGLFWFrame()
	// APHELION EDIT ADDITION START - SHORTCUT FOCUS
	shortcut.BeginFrame()
	// APHELION EDIT ADDITION END
	imgui.NewFrame()
	runLaterJobs()
	runRepeatJobs()
}

func runLaterJobs() {
	// APHELION EDIT ADDITION START - COLLABORATION
	laterJobsMutex.Lock()
	jobs := laterJobs
	laterJobs = nil
	laterJobsMutex.Unlock()
	// APHELION EDIT ADDITION END
	for _, job := range jobs {
		job()
	}
}

func runRepeatJobs() {
	for _, job := range repeatJobs {
		job()
	}
}

func (w *Window) endFrame() {
	imgui.Render()
	platform.Render(imgui.RenderedDrawData())
	w.handle.SwapBuffers()
	glfw.PollEvents()
}
