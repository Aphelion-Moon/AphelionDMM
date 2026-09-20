package hotkeys

import (
	"reflect"
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestCatalogGroupsActionsPreservesAlternativesAndOwnsKeys(t *testing.T) {
	redo := Binding{Name: "menu#DoRedo", Keys: [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.KeyY, 0}}}
	other := Binding{Name: redo.Name, Keys: [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.KeyLeftShift, glfw.KeyRightShift}, {glfw.KeyZ, 0}}}
	actions := Catalog([]Binding{redo, other, redo})
	if len(actions) != 1 || actions[0].Name != redo.Name || len(actions[0].Chords) != 2 {
		t.Fatalf("catalog lost action alternatives or duplicated panes: %#v", actions)
	}
	if !reflect.DeepEqual(actions, Catalog([]Binding{other, redo})) {
		t.Fatal("catalog depends on registration order")
	}
	actions[0].Chords[0][0][0] = glfw.KeyF12
	if redo.Keys[0][0] != glfw.KeyLeftControl || other.Keys[0][0] != glfw.KeyLeftControl {
		t.Fatal("catalog exposes mutable registry keys")
	}
}

func TestConflictsUseExactModifiersAndKeypadAlternatives(t *testing.T) {
	base := Binding{Name: "pmap#layers", Keys: [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.Key1, glfw.KeyKP1}}}
	for _, test := range []struct {
		name string
		keys [][2]glfw.Key
		want bool
	}{
		{"keypad alias", [][2]glfw.Key{{glfw.KeyRightControl, 0}, {glfw.KeyKP1, 0}}, true},
		{"extra shift", [][2]glfw.Key{{glfw.KeyLeftControl, 0}, {glfw.KeyLeftShift, 0}, {glfw.Key1, 0}}, false},
		{"bare key", [][2]glfw.Key{{glfw.Key1, 0}}, false},
		{"different modifier", [][2]glfw.Key{{glfw.KeyLeftAlt, 0}, {glfw.Key1, 0}}, false},
		{"different key", [][2]glfw.Key{{glfw.KeyLeftControl, 0}, {glfw.Key2, 0}}, false},
		{"unset key", [][2]glfw.Key{{0, 0}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := Binding{Name: "cpvareditor#value", Keys: test.keys}
			conflicts := Conflicts(Catalog([]Binding{base, base, other}))
			if (len(conflicts) == 1) != test.want {
				t.Fatalf("conflicts = %#v, want overlap %v", conflicts, test.want)
			}
		})
	}
	shifted := Binding{Name: "menu#first", Keys: [][2]glfw.Key{{glfw.KeyLeftShift, glfw.KeyRightShift}, {glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.KeyS, 0}}}
	ordered := Binding{Name: "menu#second", Keys: [][2]glfw.Key{{glfw.KeyLeftControl, 0}, {glfw.KeyLeftShift, 0}, {glfw.KeyS, 0}}}
	if len(Conflicts(Catalog([]Binding{shifted, ordered}))) != 1 {
		t.Fatal("modifier slot order hid an overlapping chord")
	}
	ordered.Name = shifted.Name
	if len(Conflicts(Catalog([]Binding{shifted, ordered}))) != 0 {
		t.Fatal("one action's alternative bindings conflict with themselves")
	}
}
