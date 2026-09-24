// APHELION EDIT ADDITION START - COMPLETED FRAME REPAINT
package window

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"sdmm/internal/platform"
)

// Native Windows sizing can remain inside PollEvents. Draw data and its textures
// still belong to the last completed frame until startFrame drains retirement
// jobs. Repainting that data runs no UI callbacks, tools, or new ImGui frame.
func (w *Window) replayCompletedFrame() bool {
	if !w.canReplayFrame || !w.completedFrame || w.handle == nil {
		return false
	}
	width, height := w.handle.GetFramebufferSize()
	if width <= 0 || height <= 0 {
		return false
	}
	data := imgui.RenderedDrawData()
	for _, list := range data.CommandLists() {
		for _, command := range list.Commands() {
			if command.HasUserCallback() {
				return false
			}
		}
	}
	gl.Viewport(0, 0, int32(width), int32(height))
	gl.Clear(gl.COLOR_BUFFER_BIT)
	platform.Render(data)
	w.handle.SwapBuffers()
	return true
}

// APHELION EDIT ADDITION END
