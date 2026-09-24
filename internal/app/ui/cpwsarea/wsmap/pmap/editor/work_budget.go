package editor

import (
	"fmt"

	"sdmm/internal/aphelion/resources"
)

// SetEditWorkBudget replaces the byte budget used for large placement and
// stamp work. It is intended for workspace configuration and deterministic
// resource tests; nil restores the process-shared host budget.
func (e *Editor) SetEditWorkBudget(budget *resources.Budget) error {
	if e == nil {
		return fmt.Errorf("editor is unavailable")
	}
	if e.paste != nil || e.selectionMove != nil || len(e.unresolvedSubmissions) != 0 {
		return fmt.Errorf("cannot change edit work budget while an edit is active")
	}
	if budget == nil {
		budget = resources.DefaultBudget()
	}
	e.workBudget = budget
	return nil
}
