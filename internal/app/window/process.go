package window

import (
	"time"
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	"sdmm/internal/aphelion/diagnostics/uistage"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SHORTCUT FOCUS
	"sdmm/internal/app/ui/shortcut"
	// APHELION EDIT ADDITION END

	"sdmm/internal/platform"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
)

/* APHELION EDIT REMOVAL START - FRAME OWNER
var ticker = newTicker(60)
func newTicker(fps int) *time.Ticker { return time.NewTicker(time.Second / time.Duration(fps)) }
APHELION EDIT REMOVAL END */
// APHELION EDIT ADDITION START - FRAME OWNER
var frameInterval = time.Second / 60

// APHELION EDIT ADDITION END

func (w *Window) Process() {
	for !w.application.IsClosed() {
		// APHELION EDIT ADDITION START - FRAME OWNER
		started := time.Now()
		// APHELION EDIT ADDITION END
		// Override window closing behaviour to enforce our checks.
		if w.handle.ShouldClose() {
			w.application.CloseCheck()
			w.handle.SetShouldClose(false)
		}
		w.runFrame()
		// APHELION EDIT CHANGE - FRAME OWNER - ORIGINAL: <-ticker.C
		if remaining := frameInterval - time.Since(started); remaining > 0 {
			time.Sleep(remaining)
		}
	}
}

func (w *Window) runFrame() {
	// APHELION EDIT ADDITION START - FRAME OWNER
	if w.frameRunning {
		w.repaintRequested = true
		return
	}
	w.frameRunning = true
	defer func() { w.frameRunning = false }()
	// Consume native input before ImGui resolves the current frame's ownership.
	w.canReplayFrame = w.completedFrame
	glfw.PollEvents()
	w.canReplayFrame = false
	w.repaintRequested = false
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	defer uistage.Begin(uistage.Frame).End()
	// APHELION EDIT ADDITION END
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

/* APHELION EDIT REMOVAL START - BOUNDED DEFERRED WORK
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
APHELION EDIT REMOVAL END */
// APHELION EDIT ADDITION START - BOUNDED DEFERRED WORK
func runLaterJobs() { runLaterJobsBudget(64, 2*time.Millisecond) }

// APHELION EDIT ADDITION END

func runRepeatJobs() {
	for _, job := range repeatJobs {
		job()
	}
}

func (w *Window) endFrame() {
	imgui.Render()
	platform.Render(imgui.RenderedDrawData())
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	present := uistage.Begin(uistage.Present)
	// APHELION EDIT ADDITION END
	w.handle.SwapBuffers()
	// APHELION EDIT ADDITION START - COMPLETED FRAME REPAINT
	w.completedFrame = true
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	present.End()
	// APHELION EDIT ADDITION END
	// APHELION EDIT REMOVAL START - FRAME OWNER
	// glfw.PollEvents() moved before input resolution and rendering.
	// APHELION EDIT REMOVAL END
}
