// APHELION EDIT ADDITION START - REPEAT TRANSFORM
package editor

import "sdmm/internal/aphelion/editing"

func (e *Editor) LastTransform() (editing.RepeatTransform, bool) { return e.repeatTransforms.Last() }

// TrackRepeatTransform associates one UI action with its initial acceptance.
// Undo/redo does not change the recipe. Floating transforms succeed immediately
// but remain speculative; they do not submit or create history here.
func (e *Editor) TrackRepeatTransform(action editing.RepeatTransform, preview bool, apply func() error) error {
	remember, err := e.repeatTransforms.Begin(action)
	if err != nil {
		return err
	}
	previous := e.repeatAccepted
	e.repeatAccepted = remember
	defer func() { e.repeatAccepted = previous }()
	if err := apply(); err != nil {
		return err
	}
	if preview {
		remember()
	}
	return nil
}

// APHELION EDIT ADDITION END
