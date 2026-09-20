package editing

import (
	"fmt"
	"sdmm/internal/util"
)

// RepeatTransform describes an action, never the contents or location of an old
// selection. A move records the resolved tile distance, independent of later
// preference changes. Exactly one of Orientation or Shift is set.
type RepeatTransform struct {
	Orientation PlacementTransform
	Shift       util.Point
}

func (action RepeatTransform) Valid() bool {
	if action.Orientation != 0 {
		return action.Orientation >= PlacementRotateRight && action.Orientation <= PlacementMirrorVertical && action.Shift == (util.Point{})
	}
	s := action.Shift
	return s.Z == 0 && (s.X == 0) != (s.Y == 0) && s.X >= -MaxSelectionMoveStep && s.X <= MaxSelectionMoveStep && s.Y >= -MaxSelectionMoveStep && s.Y <= MaxSelectionMoveStep
}

func (action RepeatTransform) Label() string {
	switch action.Orientation {
	case PlacementRotateRight:
		return "Rotate right"
	case PlacementRotateLeft:
		return "Rotate left"
	case PlacementMirrorHorizontal:
		return "Mirror horizontally"
	case PlacementMirrorVertical:
		return "Mirror vertically"
	}
	s := action.Shift
	if s.X < 0 {
		return fmt.Sprintf("Move left %d tile(s)", -s.X)
	}
	if s.X > 0 {
		return fmt.Sprintf("Move right %d tile(s)", s.X)
	}
	if s.Y < 0 {
		return fmt.Sprintf("Move down %d tile(s)", -s.Y)
	}
	return fmt.Sprintf("Move up %d tile(s)", s.Y)
}

// TransformRepeat remembers successful actions in request order. Completion
// may be delayed by networking. Clearing fences all outstanding completions.
// Like editor gesture state, it is owned by the UI thread.
type TransformRepeat struct {
	issued, remembered uint64
	last               RepeatTransform
}

func (history *TransformRepeat) Begin(action RepeatTransform) (func(), error) {
	if !action.Valid() {
		return nil, fmt.Errorf("invalid repeat transform")
	}
	history.issued++
	sequence := history.issued
	return func() {
		if sequence > history.remembered {
			history.remembered, history.last = sequence, action
		}
	}, nil
}

func (history *TransformRepeat) Last() (RepeatTransform, bool) {
	return history.last, history.last.Valid()
}

func (history *TransformRepeat) Clear() {
	history.issued++
	history.remembered = history.issued
	history.last = RepeatTransform{}
}
