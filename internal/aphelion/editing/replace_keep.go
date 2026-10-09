package editing

import "strings"

// VarEdit is one explicit variable edit as DM source text.
type VarEdit struct {
	Name  string
	Value string
}

// KeptEdits merges an instance's edits into a replacement type. The selected
// prefab's own edits come first and win. An old edit is carried over unless
// the new type does not declare the variable (it would fail to load) or its
// value already equals the new type's default (the edit became redundant,
// e.g. dir after switching to the matching directional helper). dropped
// names the old edits that were not carried over, in their original order.
func KeptEdits(old, selected []VarEdit, defaults func(name string) (string, bool)) (kept []VarEdit, dropped []string) {
	kept = append(kept, selected...)
	overridden := make(map[string]bool, len(selected))
	for _, edit := range selected {
		overridden[edit.Name] = true
	}
	for _, edit := range old {
		initial, declared := defaults(edit.Name)
		switch {
		case overridden[edit.Name], !declared, strings.TrimSpace(initial) == strings.TrimSpace(edit.Value):
			dropped = append(dropped, edit.Name)
		default:
			kept = append(kept, edit)
		}
	}
	return kept, dropped
}
