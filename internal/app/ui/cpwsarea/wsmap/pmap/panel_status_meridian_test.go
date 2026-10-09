package pmap

import (
	"testing"

	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

func segmentTexts(segments []statusSegment) []string {
	out := make([]string, len(segments))
	for i, s := range segments {
		out[i] = s.text
	}
	return out
}

func TestStatusSegmentsSeparateConcerns(t *testing.T) {
	ready := statusSegments(tools.ActionContext{Available: true}, "X:001 Y:002")
	if got := segmentTexts(ready); len(got) != 2 || got[0] != "X:001 Y:002" || got[1] != "Ready" || !ready[0].mono || !ready[1].main {
		t.Fatalf("ready = %+v", ready)
	}

	moving := statusSegments(tools.ActionContext{
		Available: true, Action: "Move instance", Badge: "Move tile",
		Modifiers:    tools.ToolModifiers{Ctrl: true, Shift: true},
		Target:       "/obj/machinery/door/airlock/public/glass",
		HasFootprint: true, Footprint: util.Bounds{X1: 1, Y1: 1, X2: 3, Y2: 2},
		Scope: "selection", LintWarning: "two doors",
	}, "X:126 Y:127")
	want := []string{"X:126 Y:127", "Move tile · Move instance  [Ctrl+Shift]", "/obj/machinery/door/airlock/public/glass", "3×2", "selection", "Lint warning"}
	got := segmentTexts(moving)
	if len(got) != len(want) {
		t.Fatalf("segments = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segment %d = %q, want %q", i, got[i], want[i])
		}
	}
	if !moving[2].mono || !moving[3].mono {
		t.Fatal("target and footprint must be monospace")
	}

	blocked := statusSegments(tools.ActionContext{Available: false, Reason: "No movable instance under the pointer"}, "out of bounds")
	if got := segmentTexts(blocked); len(got) != 3 || got[1] != "Unavailable" || got[2] != "No movable instance under the pointer" {
		t.Fatalf("unavailable = %q", got)
	}
	if blocked[1].color == (blocked[2].color) {
		t.Fatal("the unavailable state must be coloured apart from its reason")
	}
}
