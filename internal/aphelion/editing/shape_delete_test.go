package editing

import (
	"testing"

	"sdmm/internal/util"
)

func TestSelectionCursorResumesWithoutFillingMaskHoles(t *testing.T) {
	selection, err := MaskSelection([]util.Point{
		{X: 1, Y: 1, Z: 2}, {X: 1, Y: 2, Z: 2}, {X: 3, Y: 4, Z: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	cursor := selection.Cursor()
	for _, want := range []util.Point{{X: 1, Y: 1, Z: 2}, {X: 1, Y: 2, Z: 2}, {X: 3, Y: 4, Z: 2}} {
		if got, ok := cursor.Next(); !ok || got != want {
			t.Fatalf("cursor returned (%v, %t), want %v", got, ok, want)
		}
	}
	if _, ok := cursor.Next(); ok {
		t.Fatal("cursor returned a point beyond exact mask membership")
	}
}
