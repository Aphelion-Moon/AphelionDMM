// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
package shortcut

import (
	"strings"

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

var settings = &hotkeys.Settings{}

var popupOpenBeforeFrame, modalOpen bool

// BeginFrame runs before ImGui.NewFrame, which can dismiss a popup in response
// to Escape. Retain that ownership until the next frame to prevent fallthrough.
func BeginFrame() { popupOpenBeforeFrame = imgui.IsPopupOpenV("", imgui.PopupFlagsAnyPopup) }

// SetModalOpen also covers queued dialogs before their first rendered frame.
func SetModalOpen(open bool) { modalOpen = open }

func BackgroundInputBlocked() bool {
	return modalOpen || popupOpenBeforeFrame || imgui.IsPopupOpenV("", imgui.PopupFlagsAnyPopup)
}

// ProcessPopup is called inside the focused popup after its widgets. Only the
// explicitly advertised actions may run; custom chords use the same registry.
func ProcessPopup(names ...string) {
	if modalOpen || imgui.IsAnyItemActive() || !imgui.IsPopupOpenV("", imgui.PopupFlagsAnyPopup) ||
		!imgui.IsWindowFocusedV(imgui.FocusedFlagsRootAndChildWindows) {
		return
	}
	processCandidates(func(s *Shortcut) bool {
		for _, name := range names {
			if s.Name == name {
				return true
			}
		}
		return false
	})
}

// UseSettings is called on the UI thread before opening panes.
func UseSettings(value *hotkeys.Settings) {
	if value == nil {
		value = &hotkeys.Settings{}
	}
	settings = value
}

func SetBindings(name string, chords [][][2]glfw.Key) error { return settings.Set(name, chords) }
func ResetBindings(name string)                             { settings.Reset(name) }
func ResetAllBindings()                                     { settings.ResetAll() }

func matchingWeight(s Shortcut) int {
	return settings.Match(s.Name, keys(s), func(k glfw.Key) bool { return imgui.IsKeyDown(int(k)) }, func(k glfw.Key) bool { return imgui.IsKeyPressed(int(k)) })
}

func effectiveChords(s Shortcut) [][][2]glfw.Key {
	if chords, ok := settings.Lookup(s.Name); ok {
		return chords
	}
	return [][][2]glfw.Key{keys(s)}
}

// Defaults includes disabled actions so they can still be customized or reset.
func Defaults() []hotkeys.Action {
	var bindings []hotkeys.Binding
	for _, s := range shortcuts {
		bindings = append(bindings, hotkeys.Binding{Name: s.Name, Keys: keys(*s)})
	}
	return hotkeys.Catalog(bindings)
}

func BindingsFor(name string) [][][2]glfw.Key {
	if chords, ok := settings.Lookup(name); ok {
		return chords
	}
	var bindings []hotkeys.Binding
	for _, s := range shortcuts {
		if s.Name == name {
			bindings = append(bindings, hotkeys.Binding{Name: name, Keys: keys(*s)})
		}
	}
	if actions := hotkeys.Catalog(bindings); len(actions) != 0 {
		return actions[0].Chords
	}
	return nil
}

func PreviewConflicts(name string, chords [][][2]glfw.Key) []hotkeys.Conflict {
	actions := Actions()
	var others []hotkeys.Action
	for _, action := range actions {
		if action.Name != name {
			others = append(others, action)
		}
	}
	others = append(others, hotkeys.Action{Name: name, Chords: chords})
	var result []hotkeys.Conflict
	for _, conflict := range hotkeys.Conflicts(others) {
		if conflict.First.Name == name || conflict.Second.Name == name {
			result = append(result, conflict)
		}
	}
	return result
}

func Label(name string) string {
	var labels []string
	for _, chord := range BindingsFor(name) {
		for _, entry := range hotkeys.Reference([]hotkeys.Binding{{Name: name, Keys: chord}}) {
			labels = append(labels, entry.Keys)
		}
	}
	return strings.Join(labels, "; ")
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
		for _, chord := range effectiveChords(*s) {
			bindings = append(bindings, hotkeys.Binding{Name: s.Name, Keys: chord})
		}
	}
	return bindings
}

// APHELION EDIT ADDITION END
