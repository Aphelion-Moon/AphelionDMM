// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
package shortcut

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/hotkeys"
)

func keys(s Shortcut) [][2]glfw.Key {
	result := [][2]glfw.Key{{s.FirstKey, s.FirstKeyAlt}}
	if s.SecondKey != 0 {
		result = append(result, [2]glfw.Key{s.SecondKey, s.SecondKeyAlt})
	}
	if s.ThirdKey != 0 {
		result = append(result, [2]glfw.Key{s.ThirdKey, s.ThirdKeyAlt})
	}
	return result
}

func pressedExact(s Shortcut) bool {
	return hotkeys.Pressed(keys(s), func(k glfw.Key) bool { return imgui.IsKeyDown(int(k)) }, func(k glfw.Key) bool { return imgui.IsKeyPressed(int(k)) })
}

func Reference() []hotkeys.Entry {
	return hotkeys.Reference(registeredBindings())
}

func Actions() []hotkeys.Action {
	return hotkeys.Catalog(registeredBindings())
}

func SharedBindings() []hotkeys.Conflict {
	return hotkeys.Conflicts(Actions())
}

func registeredBindings() []hotkeys.Binding {
	bindings := make([]hotkeys.Binding, 0, len(shortcuts))
	for _, s := range shortcuts {
		bindings = append(bindings, hotkeys.Binding{Name: s.Name, Keys: keys(*s)})
	}
	return bindings
}

// APHELION EDIT ADDITION END
