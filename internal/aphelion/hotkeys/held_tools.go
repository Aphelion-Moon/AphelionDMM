package hotkeys

// HeldToolInput describes one temporary tool in priority order. Pressed must
// represent a new press, not key repeat: a key held while typing or invoking a
// shortcut cannot become a tool merely because the input guard later clears.
type HeldToolInput struct {
	Name          string
	Down, Pressed bool
}

// HeldTools remembers only presses admitted by the map's input guards. It owns
// a temporary selection until all admitted keys are released or the user makes
// an explicit different selection.
type HeldTools struct {
	original, current string
	eligible          map[string]bool
}

func (state *HeldTools) Update(selected string, blocked bool, inputs []HeldToolInput) string {
	if state.current != "" && selected != state.current {
		// An explicit toolbar/shortcut choice supersedes restoration. Held keys
		// need a fresh press before they can override that choice again.
		clear(state.eligible)
		state.original, state.current = "", ""
		return selected
	}
	for _, input := range inputs {
		if !input.Down {
			delete(state.eligible, input.Name)
		} else if !blocked && input.Pressed {
			if state.eligible == nil {
				state.eligible = make(map[string]bool)
			}
			state.eligible[input.Name] = true
		}
	}
	if blocked {
		return selected
	}
	for _, input := range inputs {
		if state.eligible[input.Name] {
			if state.current == "" {
				state.original = selected
			}
			state.current = input.Name
			return input.Name
		}
	}
	if state.current != "" {
		selected = state.original
		state.original, state.current = "", ""
	}
	return selected
}
