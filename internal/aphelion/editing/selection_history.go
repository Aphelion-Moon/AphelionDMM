package editing

import "sdmm/internal/util"

// SelectionHistory tracks only selection geometry, never map contents. Each
// explicit selection owns one history. A late outcome can disable an earlier
// transform without replacing a newer active transform's geometry.
type SelectionHistory struct {
	origin          util.Bounds
	head            *SelectionChange
	originSelection Selection
}

type SelectionChange struct {
	previous  *SelectionChange
	area      util.Bounds
	enabled   bool
	selection Selection
}

func NewSelectionHistory(area util.Bounds) *SelectionHistory {
	return &SelectionHistory{origin: area}
}

func (history *SelectionHistory) Add(area util.Bounds) *SelectionChange {
	change := &SelectionChange{previous: history.head, area: area, enabled: true}
	history.head = change
	return change
}

func (change *SelectionChange) SetBounds(area util.Bounds) { change.area = area }
func (change *SelectionChange) SetApplied(applied bool)    { change.enabled = applied }

func NewMaskSelectionHistory(selection Selection) *SelectionHistory {
	return &SelectionHistory{origin: selection.Bounds(), originSelection: selection}
}
func (change *SelectionChange) SetSelection(selection Selection) {
	change.selection = selection
	change.area = selection.Bounds()
}
func (history *SelectionHistory) Selection() Selection {
	for change := history.head; change != nil; change = change.previous {
		if change.enabled {
			return change.selection
		}
	}
	return history.originSelection
}

// DiscardUnappliedTail is only for the end of the immediate transform action.
// Validation failures and cancelled/no-op gestures have no undo command. Later
// asynchronous outcomes and undo keep their nodes so newer geometry and redo
// remain valid.
func (history *SelectionHistory) DiscardUnappliedTail(change *SelectionChange) {
	if history.head == change && !change.enabled {
		history.head = change.previous
	}
}

func (history *SelectionHistory) Bounds() util.Bounds {
	for change := history.head; change != nil; change = change.previous {
		if change.enabled {
			return change.area
		}
	}
	return history.origin
}
