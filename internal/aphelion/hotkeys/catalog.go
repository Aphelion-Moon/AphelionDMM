package hotkeys

import (
	"fmt"
	"sort"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// Action groups all registered alternatives for one stable action name. Multiple
// panes can register the same action; their callbacks and focus remain UI-owned.
type Action struct {
	Name   string
	Chords [][][2]glfw.Key
}

type Conflict struct {
	First  Binding
	Second Binding
}

// Catalog returns an ordered, independent view of the registered bindings.
func Catalog(bindings []Binding) []Action {
	grouped := make(map[string]map[string][][2]glfw.Key)
	for _, binding := range bindings {
		if grouped[binding.Name] == nil {
			grouped[binding.Name] = make(map[string][][2]glfw.Key)
		}
		grouped[binding.Name][fmt.Sprint(binding.Keys)] = binding.Keys
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	actions := make([]Action, 0, len(names))
	for _, name := range names {
		chords := grouped[name]
		identities := make([]string, 0, len(chords))
		for identity := range chords {
			identities = append(identities, identity)
		}
		sort.Strings(identities)
		action := Action{Name: name}
		for _, identity := range identities {
			action.Chords = append(action.Chords, append([][2]glfw.Key(nil), chords[identity]...))
		}
		actions = append(actions, action)
	}
	return actions
}

// Conflicts finds chords that can match the same pressed key. These are potential
// overlaps: UI focus, visibility and enabled predicates are not binding data.
func Conflicts(actions []Action) []Conflict {
	var conflicts []Conflict
	for firstIndex, first := range actions {
		for _, second := range actions[firstIndex+1:] {
			if first.Name == second.Name {
				continue
			}
			for _, firstChord := range first.Chords {
				for _, secondChord := range second.Chords {
					if chordsOverlap(firstChord, secondChord) {
						conflicts = append(conflicts, Conflict{
							First:  Binding{Name: first.Name, Keys: append([][2]glfw.Key(nil), firstChord...)},
							Second: Binding{Name: second.Name, Keys: append([][2]glfw.Key(nil), secondChord...)},
						})
					}
				}
			}
		}
	}
	return conflicts
}

func chordsOverlap(first, second [][2]glfw.Key) bool {
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	// Non-modifier held keys do not exclude other shortcuts. The union satisfies
	// both chords' held-key requirements; Pressed rejects incompatible modifiers.
	down := make(map[glfw.Key]bool)
	for _, chord := range [][][2]glfw.Key{first, second} {
		for _, pair := range chord {
			for _, key := range pair {
				if key != 0 {
					down[key] = true
				}
			}
		}
	}
	for _, firstKey := range first[len(first)-1] {
		for _, secondKey := range second[len(second)-1] {
			if firstKey == 0 || firstKey != secondKey {
				continue
			}
			isDown := func(key glfw.Key) bool { return down[key] }
			isPressed := func(key glfw.Key) bool { return key == firstKey }
			if Pressed(first, isDown, isPressed) && Pressed(second, isDown, isPressed) {
				return true
			}
		}
	}
	return false
}
