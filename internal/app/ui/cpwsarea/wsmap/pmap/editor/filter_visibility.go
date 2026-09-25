package editor

import (
	"errors"

	"sdmm/internal/aphelion/filterprofiles"
)

// HideExactPath routes Alt-Pick through the shared local profile command.
func (e *Editor) HideExactPath(path string) error {
	controller, ok := e.app.(interface {
		SetFilterVisibility(string, filterprofiles.Scope, bool) error
	})
	if !ok {
		return errors.New("filter profile controller is unavailable")
	}
	return controller.SetFilterVisibility(path, filterprofiles.ScopeExact, false)
}
