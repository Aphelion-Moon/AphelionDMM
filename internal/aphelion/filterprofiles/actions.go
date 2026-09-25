package filterprofiles

import "sdmm/internal/dmapi/dm"

// Toggle and ShowAll route legacy menu/shortcut surfaces through the shared
// profile owner. Minimal embedders retain the original direct-filter behavior.
func Toggle(owner any, filter *dm.PathsFilter, path string) error {
	if controller, ok := owner.(interface {
		SetFilterVisibility(string, Scope, bool) error
	}); ok {
		return controller.SetFilterVisibility(path, ScopeSubtree, !filter.IsVisiblePath(path))
	}
	filter.TogglePath(path)
	return nil
}
func ShowAll(owner any, filter *dm.PathsFilter) error {
	if controller, ok := owner.(interface{ ShowAllFilterVisibility() error }); ok {
		return controller.ShowAllFilterVisibility()
	}
	filter.Clear()
	return nil
}
