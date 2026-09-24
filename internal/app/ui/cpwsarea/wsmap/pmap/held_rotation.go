// APHELION EDIT ADDITION START - HELD ROTATION
package pmap

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/util"
)

func (p *PaneMap) addHeldRotationShortcuts() {
	for _, binding := range []struct {
		name      string
		key       glfw.Key
		clockwise bool
	}{{"pmap#rotateHeldLeft", glfw.KeyQ, false}, {"pmap#rotateHeldRight", glfw.KeyE, true}} {
		p.shortcuts.Add(shortcut.Shortcut{Name: binding.name, FirstKey: binding.key, IsEnabled: tools.CanRotateHeld, AllowWhenItemActive: func() bool { return tools.OwnsGesture(p.editor) }, Action: func() {
			if err := tools.RotateHeld(binding.clockwise); err != nil {
				util.ShowErrorDialog(err.Error())
			}
		}})
	}
}

// APHELION EDIT ADDITION END
