// Package hotkeys provides testable matching and reference formatting for the UI.
package hotkeys

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/go-gl/glfw/v3.3/glfw"
)

type Binding struct {
	Name string
	Keys [][2]glfw.Key
}

type Entry struct{ Context, Action, Keys string }

// Pressed matches a chord without allowing extra modifiers to trigger a shorter
// action. Zero is an absent alternate key, never an input key.
func Pressed(keys [][2]glfw.Key, down, pressed func(glfw.Key) bool) bool {
	if len(keys) == 0 {
		return false
	}
	// Check the action key first. Modifier polling crosses the native UI boundary,
	// and idle frames should not pay for eight extra calls for every binding.
	for i := len(keys) - 1; i >= 0; i-- {
		pair := keys[i]
		read := down
		if i == len(keys)-1 {
			read = pressed
		}
		matched := (pair[0] != 0 && read(pair[0])) || (pair[1] != 0 && read(pair[1]))
		if !matched {
			return false
		}
	}
	for _, modifiers := range [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.KeyLeftSuper, glfw.KeyRightSuper}, {glfw.KeyLeftAlt, glfw.KeyRightAlt}, {glfw.KeyLeftShift, glfw.KeyRightShift}} {
		required := false
		for _, pair := range keys {
			for _, key := range pair {
				if key == modifiers[0] || key == modifiers[1] {
					required = true
				}
			}
		}
		if (down(modifiers[0]) || down(modifiers[1])) != required {
			return false
		}
	}
	return true
}

// Reference derives key labels from registered bindings, including alternatives.
// Multiple open maps register identical actions; each appears only once.
func Reference(bindings []Binding) []Entry {
	var result []Entry
	seen := make(map[Entry]bool)
	for _, action := range Catalog(bindings) {
		for _, chord := range action.Chords {
			binding := Binding{Name: action.Name, Keys: chord}
			var parts []string
			for _, pair := range binding.Keys {
				label := keyName(pair[0])
				if alternate := keyName(pair[1]); alternate != "" && alternate != label {
					label += " / " + alternate
				}
				parts = append(parts, label)
			}
			context, action, _ := strings.Cut(binding.Name, "#")
			if value, ok := map[string]string{"menu": "General", "pmap": "Map", "tilemenu": "Tile menu", "cpsearch": "Search", "cpenvironment": "Environment", "cpvareditor": "Variables", "wsempty": "Empty workspace"}[context]; ok {
				context = value
			}
			label, exists := labels[binding.Name]
			if !exists {
				label = humanize(action)
			}
			entry := Entry{context, label, strings.Join(parts, "+")}
			if !seen[entry] {
				seen[entry] = true
				result = append(result, entry)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Context != b.Context {
			return a.Context < b.Context
		}
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		return a.Keys < b.Keys
	})
	return result
}

var labels = map[string]string{
	"pmap#selectAddTool": "Select Add tool", "pmap#selectFillTool": "Select Fill tool",
	"pmap#selectSelectTool": "Select Grab tool", "pmap#selectMoveTool": "Select Move tool",
	"pmap#selectPickTool": "Select Pick tool", "pmap#selectDeleteTool": "Select Delete tool", "pmap#selectReplaceTool": "Select Replace tool",
	"pmap#rotateLeft": "Rotate selection left", "pmap#rotateRight": "Rotate selection right",
	"pmap#rotateHeldLeft": "Rotate held object left", "pmap#rotateHeldRight": "Rotate held object right",
	"pmap#repeatTransform":           "Repeat last transform",
	"pmap#mirrorSelectionHorizontal": "Mirror selection horizontally", "pmap#mirrorSelectionVertical": "Mirror selection vertically",
	"pmap#doDeselectAll": "Deselect", "menu#DoOpenJumpWindow": "Go to coordinates", "menu#showHotkeys": "Keyboard shortcuts",
}

func humanize(value string) string {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "Do"), "do")
	var output []rune
	for i, r := range value {
		if i > 0 && unicode.IsUpper(r) {
			output = append(output, ' ')
		}
		if i == 0 {
			r = unicode.ToUpper(r)
		} else {
			r = unicode.ToLower(r)
		}
		output = append(output, r)
	}
	return string(output)
}

func keyName(key glfw.Key) string {
	if key == 0 {
		return ""
	}
	if key >= glfw.KeyA && key <= glfw.KeyZ || key >= glfw.Key0 && key <= glfw.Key9 {
		return string(rune(key))
	}
	if key >= glfw.KeyF1 && key <= glfw.KeyF25 {
		return fmt.Sprintf("F%d", key-glfw.KeyF1+1)
	}
	if key >= glfw.KeyKP0 && key <= glfw.KeyKP9 {
		return fmt.Sprintf("Numpad %d", key-glfw.KeyKP0)
	}
	if name, ok := map[glfw.Key]string{
		glfw.KeyLeftControl: "Ctrl", glfw.KeyRightControl: "Ctrl", glfw.KeyLeftSuper: "Cmd", glfw.KeyRightSuper: "Cmd",
		glfw.KeyLeftShift: "Shift", glfw.KeyRightShift: "Shift", glfw.KeyLeftAlt: "Alt", glfw.KeyRightAlt: "Alt",
		glfw.KeyLeftBracket: "[", glfw.KeyRightBracket: "]", glfw.KeyEqual: "=", glfw.KeyMinus: "-",
		glfw.KeyKPAdd: "Numpad +", glfw.KeyKPSubtract: "Numpad -", glfw.KeyUp: "Up", glfw.KeyDown: "Down", glfw.KeyLeft: "Left", glfw.KeyRight: "Right",
		glfw.KeyDelete: "Delete", glfw.KeyEscape: "Esc", glfw.KeyEnter: "Enter", glfw.KeyKPEnter: "Numpad Enter", glfw.KeySpace: "Space", glfw.KeyTab: "Tab", glfw.KeyBackspace: "Backspace",
	}[key]; ok {
		return name
	}
	return fmt.Sprintf("Key %d", key)
}
