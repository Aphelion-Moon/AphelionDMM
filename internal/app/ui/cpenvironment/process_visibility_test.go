package cpenvironment

import (
	"testing"

	"github.com/SpaiR/imgui-go"
)

func TestIconTintKeepsVisibleIconsUnchanged(t *testing.T) {
	base := imgui.Vec4{X: .2, Y: .4, Z: .6, W: 1}
	if got := iconTint(base, false); got != base {
		t.Fatalf("visible icon tint changed: got %v, want %v", got, base)
	}
}

func TestSelectedVisibilityToggleWithoutSelectionDoesNothing(t *testing.T) {
	_, _, ok := selectedVisibilityToggle("", func(string) bool {
		t.Fatal("visibility must not be read without a selection")
		return true
	})
	if ok {
		t.Fatal("empty selection produced a toggle target")
	}
}

func TestSelectedVisibilityToggleFlipsSelectedPath(t *testing.T) {
	visibility := map[string]bool{"/obj/a": true, "/obj/b": false}
	isVisible := func(path string) bool { return visibility[path] }

	path, visible, ok := selectedVisibilityToggle("/obj/a", isVisible)
	if !ok || path != "/obj/a" || visible {
		t.Fatalf("visible selection: got (%q, %v, %v), want (\"/obj/a\", false, true)", path, visible, ok)
	}

	path, visible, ok = selectedVisibilityToggle("/obj/b", isVisible)
	if !ok || path != "/obj/b" || !visible {
		t.Fatalf("hidden selection: got (%q, %v, %v), want (\"/obj/b\", true, true)", path, visible, ok)
	}
}

func TestIconTintDimsOnlyAlphaForHiddenIcons(t *testing.T) {
	base := imgui.Vec4{X: .2, Y: .4, Z: .6, W: 1}
	got := iconTint(base, true)
	if got.X != base.X || got.Y != base.Y || got.Z != base.Z {
		t.Fatalf("hidden icon changed color channels: got %v, want RGB of %v", got, base)
	}
	if want := base.W * hiddenIconAlpha; got.W != want {
		t.Fatalf("hidden icon alpha: got %v, want %v", got.W, want)
	}
	if got.W >= base.W {
		t.Fatalf("hidden icon is not dimmed: alpha %v", got.W)
	}
}
