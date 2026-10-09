package tools

import "testing"

func TestModifiersSuppressedKeepsCanvasDragModifiers(t *testing.T) {
	cases := []struct {
		bg, item, text, drag, want bool
	}{
		{false, false, false, false, false},
		{false, true, false, true, false}, // canvas drag owns the active item
		{false, true, false, false, true}, // some other widget is active
		{false, true, true, true, true},   // text input always wins
		{true, false, false, true, true},  // background input blocked
	}
	for _, c := range cases {
		if got := modifiersSuppressed(c.bg, c.item, c.text, c.drag); got != c.want {
			t.Errorf("%+v = %v", c, got)
		}
	}
}
