package editing

import "testing"

func TestTransformRepeatOrdersCompletionAndFencesReset(t *testing.T) {
	var history TransformRepeat
	left := RepeatTransform{Orientation: PlacementRotateLeft}
	right := RepeatTransform{Orientation: PlacementRotateRight}
	first, err := history.Begin(left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := history.Begin(right)
	if err != nil {
		t.Fatal(err)
	}
	second()
	first()
	if got, ok := history.Last(); !ok || got != right {
		t.Fatal("older completion replaced newer action")
	}
	history.Clear()
	second()
	if _, ok := history.Last(); ok {
		t.Fatal("late completion survived attachment reset")
	}
	if _, err := history.Begin(RepeatTransform{}); err == nil {
		t.Fatal("empty action accepted")
	}
}
