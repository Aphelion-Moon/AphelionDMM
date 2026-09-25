package editor

import (
	"errors"

	"sdmm/internal/aphelion/filterprofiles"
)

// HideExactPath routes Alt-Pick through the shared local profile command.
func (e *Editor) HideExactPath(path string) (err error) {
	defer func() {
		e.visibilityError = ""
		if err != nil {
			e.visibilityError = "Unable to hide " + path + ": " + err.Error()
		}
	}()
	controller, ok := e.app.(interface {
		SetFilterVisibility(string, filterprofiles.Scope, bool) error
	})
	if !ok {
		return errors.New("filter profile controller is unavailable")
	}
	return controller.SetFilterVisibility(path, filterprofiles.ScopeExact, false)
}

func (e *Editor) VisibilityStatus() string {
	if e.visibilityError != "" {
		return e.visibilityError
	}
	if controller, ok := e.app.(interface{ FilterVisibilityStatus() string }); ok {
		return controller.FilterVisibilityStatus()
	}
	return ""
}
