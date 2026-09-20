package hotkeys

import (
	"fmt"
	"strings"

	"github.com/go-gl/glfw/v3.3/glfw"
)

var modifierPairs = [][2]glfw.Key{
	{glfw.KeyLeftControl, glfw.KeyRightControl},
	{glfw.KeyLeftSuper, glfw.KeyRightSuper},
	{glfw.KeyLeftAlt, glfw.KeyRightAlt},
	{glfw.KeyLeftShift, glfw.KeyRightShift},
}

var editableKeyNames = func() map[glfw.Key]string {
	names := map[glfw.Key]string{
		glfw.KeySpace: "Space", glfw.KeyApostrophe: "Apostrophe", glfw.KeyComma: "Comma",
		glfw.KeyMinus: "-", glfw.KeyPeriod: "Period", glfw.KeySlash: "Slash", glfw.KeySemicolon: "Semicolon",
		glfw.KeyEqual: "=", glfw.KeyLeftBracket: "[", glfw.KeyBackslash: "Backslash", glfw.KeyRightBracket: "]", glfw.KeyGraveAccent: "Grave",
		glfw.KeyWorld1: "World1", glfw.KeyWorld2: "World2",
		glfw.KeyEscape: "Esc", glfw.KeyEnter: "Enter", glfw.KeyTab: "Tab", glfw.KeyBackspace: "Backspace",
		glfw.KeyInsert: "Insert", glfw.KeyDelete: "Delete", glfw.KeyRight: "Right", glfw.KeyLeft: "Left", glfw.KeyDown: "Down", glfw.KeyUp: "Up",
		glfw.KeyPageUp: "PageUp", glfw.KeyPageDown: "PageDown", glfw.KeyHome: "Home", glfw.KeyEnd: "End",
		glfw.KeyCapsLock: "CapsLock", glfw.KeyScrollLock: "ScrollLock", glfw.KeyNumLock: "NumLock", glfw.KeyPrintScreen: "PrintScreen", glfw.KeyPause: "Pause",
		glfw.KeyKPDecimal: "KPDecimal", glfw.KeyKPDivide: "KPDivide", glfw.KeyKPMultiply: "KPMultiply", glfw.KeyKPSubtract: "KPSubtract",
		glfw.KeyKPAdd: "KPAdd", glfw.KeyKPEnter: "KPEnter", glfw.KeyKPEqual: "KPEqual", glfw.KeyMenu: "Menu",
		glfw.KeyLeftControl: "LeftCtrl", glfw.KeyRightControl: "RightCtrl", glfw.KeyLeftSuper: "LeftCmd", glfw.KeyRightSuper: "RightCmd",
		glfw.KeyLeftAlt: "LeftAlt", glfw.KeyRightAlt: "RightAlt", glfw.KeyLeftShift: "LeftShift", glfw.KeyRightShift: "RightShift",
	}
	for key := glfw.KeyA; key <= glfw.KeyZ; key++ {
		names[key] = keyName(key)
	}
	for key := glfw.Key0; key <= glfw.Key9; key++ {
		names[key] = keyName(key)
	}
	for key := glfw.KeyF1; key <= glfw.KeyF25; key++ {
		names[key] = keyName(key)
	}
	for key := glfw.KeyKP0; key <= glfw.KeyKP9; key++ {
		names[key] = keyName(key)
	}
	return names
}()

func modifierGroup(key glfw.Key) int {
	for i, pair := range modifierPairs {
		if key == pair[0] || key == pair[1] {
			return i + 1
		}
	}
	return 0
}

func validateChords(chords [][][2]glfw.Key) error {
	if len(chords) > 4 {
		return fmt.Errorf("use at most four alternative bindings")
	}
	for _, chord := range chords {
		if len(chord) == 0 || len(chord) > 5 {
			return fmt.Errorf("use modifiers followed by one action key")
		}
		seen := make(map[int]bool)
		for i, pair := range chord {
			if _, ok := editableKeyNames[pair[0]]; !ok {
				return fmt.Errorf("unknown action key")
			}
			if pair[1] != 0 {
				if _, ok := editableKeyNames[pair[1]]; !ok {
					return fmt.Errorf("unknown alternate key")
				}
			}
			group := modifierGroup(pair[0])
			if i < len(chord)-1 {
				if group == 0 || seen[group] || (pair[1] != 0 && modifierGroup(pair[1]) != group) {
					return fmt.Errorf("each modifier may appear once, before the action key")
				}
				seen[group] = true
			} else {
				if group != 0 || modifierGroup(pair[1]) != 0 {
					return fmt.Errorf("finish the binding with an action key")
				}
				for _, key := range pair {
					if !seen[1] && !seen[2] && (key == glfw.KeyS || key == glfw.KeyD || key == glfw.KeyR) {
						return fmt.Errorf("held-tool keys S, D and R require Ctrl or Cmd")
					}
					if key == glfw.KeySpace || (len(chord) == 1 && key == glfw.KeyEscape) {
						return fmt.Errorf("keys Space and unmodified Esc are reserved for camera dragging and cancellation; use Reset for default bindings")
					}
				}
			}
		}
	}
	return nil
}

// Parse accepts semicolon-separated alternatives, with slash-separated key
// aliases. Named modifiers include both physical sides unless specified.
func Parse(text string) ([][][2]glfw.Key, error) {
	var result [][][2]glfw.Key
	if strings.TrimSpace(text) == "" {
		return result, nil
	}
	for _, alternative := range strings.Split(text, ";") {
		var chord [][2]glfw.Key
		for _, slot := range strings.Split(alternative, "+") {
			var pair [2]glfw.Key
			switch strings.ToLower(strings.TrimSpace(slot)) {
			case "ctrl":
				pair = modifierPairs[0]
			case "cmd", "super":
				pair = modifierPairs[1]
			case "alt":
				pair = modifierPairs[2]
			case "shift":
				pair = modifierPairs[3]
			default:
				aliases := strings.Split(slot, "/")
				if len(aliases) > 2 {
					return nil, fmt.Errorf("use at most two keys per slot")
				}
				for i, alias := range aliases {
					for key, name := range editableKeyNames {
						if strings.EqualFold(strings.TrimSpace(alias), name) {
							pair[i] = key
							break
						}
					}
					if pair[i] == 0 {
						return nil, fmt.Errorf("unknown key %q", strings.TrimSpace(alias))
					}
				}
			}
			chord = append(chord, pair)
		}
		result = append(result, chord)
	}
	return result, validateChords(result)
}

// Format returns editable text, including aliases that reference labels collapse.
func Format(chords [][][2]glfw.Key) string {
	var alternatives []string
	for _, chord := range chords {
		var slots []string
		for _, pair := range chord {
			name := editableKeyNames[pair[0]]
			if pair[1] != 0 {
				name += "/" + editableKeyNames[pair[1]]
			}
			for _, modifiers := range modifierPairs {
				if pair == modifiers {
					name = keyName(pair[0])
					break
				}
			}
			slots = append(slots, name)
		}
		alternatives = append(alternatives, strings.Join(slots, "+"))
	}
	return strings.Join(alternatives, "; ")
}
