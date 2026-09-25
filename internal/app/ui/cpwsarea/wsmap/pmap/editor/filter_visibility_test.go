package editor

import "testing"

func TestMissingVisibilityControllerLeavesVisibleStatus(t *testing.T) {
	e := &Editor{}
	if err := e.HideExactPath("/obj/example"); err == nil {
		t.Fatal("missing controller accepted hide")
	}
	if e.VisibilityStatus() == "" {
		t.Fatal("hide failure only reached a log")
	}
}
