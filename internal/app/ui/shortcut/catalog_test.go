package shortcut

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestRegisteredBindingCatalogTracksPaneLifetimes(t *testing.T) {
	previous := shortcuts
	shortcuts = nil
	t.Cleanup(func() { shortcuts = previous })
	first, second, variables := &Shortcuts{}, &Shortcuts{}, &Shortcuts{}
	binding := Shortcut{Name: "pmap#layers", FirstKey: glfw.KeyLeftControl, FirstKeyAlt: glfw.KeyRightControl, SecondKey: glfw.Key1, SecondKeyAlt: glfw.KeyKP1}
	first.Add(binding)
	second.Add(binding)
	variables.Add(Shortcut{Name: "cpvareditor#value", FirstKey: glfw.KeyRightControl, SecondKey: glfw.KeyKP1})
	if len(Actions()) != 2 || len(SharedBindings()) != 1 || len(Reference()) != 2 {
		t.Fatal("live registry catalog duplicated panes or lost conflict/reference entries")
	}
	first.Dispose()
	if len(Actions()) != 2 || len(SharedBindings()) != 1 {
		t.Fatal("closing one pane removed another pane's binding")
	}
	second.Dispose()
	if len(Actions()) != 1 || len(SharedBindings()) != 0 {
		t.Fatal("closed pane remains in catalog")
	}
	variables.Dispose()
	if len(Actions()) != 0 {
		t.Fatal("disposed registrations retained")
	}
}
