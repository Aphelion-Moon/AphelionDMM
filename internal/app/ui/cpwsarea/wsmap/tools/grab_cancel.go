// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
package tools

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/hotkeys"
	"sdmm/internal/app/ui/shortcut"
)

func cancelGrabOnEscape() {
	if !shortcut.BackgroundInputBlocked() && !imgui.CurrentIO().WantTextInput() && imgui.IsKeyPressedV(int(glfw.KeyEscape), false) {
		switch t := Selected().(type) {
		case *ToolAdd:
			if t.shapeStroke != nil {
				t.OnDeselect()
				return
			}
		case *ToolDelete:
			if t.shapeStroke != nil {
				t.OnDeselect()
				return
			}
		case *ToolFill:
			if t.dragging {
				t.OnDeselect()
				return
			}
		}
	}
	grab, ok := Selected().(*ToolGrab)
	if !ok || grab.Stale() || shortcut.BackgroundInputBlocked() || imgui.CurrentIO().WantTextInput() {
		return
	}
	if hotkeys.Pressed([][2]glfw.Key{{glfw.KeyEscape, 0}}, func(key glfw.Key) bool { return imgui.IsKeyDown(int(key)) }, func(key glfw.Key) bool { return imgui.IsKeyPressedV(int(key), false) }) {
		grab.OnDeselect()
	}
}

// APHELION EDIT ADDITION END
