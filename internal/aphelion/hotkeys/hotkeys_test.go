package hotkeys

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"testing"
)

func TestPressedRequiresExactModifiersAndRealKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		keys    [][2]glfw.Key
		down    []glfw.Key
		pressed glfw.Key
		want    bool
	}{
		{"bare rotate", [][2]glfw.Key{{glfw.KeyQ, 0}}, nil, glfw.KeyQ, true},
		{"modified rotate", [][2]glfw.Key{{glfw.KeyQ, 0}}, []glfw.Key{glfw.KeyLeftControl}, glfw.KeyQ, false},
		{"save", [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.KeyS, 0}}, []glfw.Key{glfw.KeyRightControl}, glfw.KeyS, true},
		{"save all is not save", [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.KeyS, 0}}, []glfw.Key{glfw.KeyLeftControl, glfw.KeyLeftShift}, glfw.KeyS, false},
		{"unset alternative", [][2]glfw.Key{{glfw.KeyS, 0}}, nil, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			down := func(k glfw.Key) bool {
				for _, key := range tc.down {
					if key == k {
						return true
					}
				}
				return false
			}
			if got := Pressed(tc.keys, down, func(k glfw.Key) bool { return k == tc.pressed }); got != tc.want {
				t.Fatalf("pressed=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestPressedIdleDoesNotPollModifiers(t *testing.T) {
	downCalls := 0
	if Pressed([][2]glfw.Key{{glfw.KeyQ, 0}}, func(glfw.Key) bool { downCalls++; return false }, func(glfw.Key) bool { return false }) {
		t.Fatal("idle input matched")
	}
	if downCalls != 0 {
		t.Fatalf("idle action polled modifier state %d times", downCalls)
	}
}

func TestReferenceUsesActualKeysAndDeduplicatesPanes(t *testing.T) {
	b := Binding{Name: "pmap#rotateLeft", Keys: [][2]glfw.Key{{glfw.KeyQ, 0}}}
	rows := Reference([]Binding{b, b, {Name: "menu#DoSave", Keys: [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightControl}, {glfw.KeyS, 0}}}})
	if len(rows) != 2 {
		t.Fatalf("duplicated reference rows: %v", rows)
	}
	for _, row := range rows {
		if row.Action == "Rotate selection left" && row.Keys != "Q" {
			t.Fatal("rotation binding drift")
		}
		if row.Action == "Save" && row.Keys != "Ctrl+S" {
			t.Fatalf("bad key label: %s", row.Keys)
		}
	}
}
