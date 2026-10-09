package window

import "github.com/go-gl/glfw/v3.3/glfw"

// SetDropHandler registers a handler for files dropped onto the native window.
// The handler is always invoked on the UI thread through RunLater, with a copy
// of the dropped paths.
func (w *Window) SetDropHandler(handler func(paths []string)) {
	if handler == nil {
		w.handle.SetDropCallback(nil)
		return
	}
	w.handle.SetDropCallback(func(_ *glfw.Window, names []string) {
		paths := append([]string(nil), names...)
		RunLater(func() { handler(paths) })
	})
}
