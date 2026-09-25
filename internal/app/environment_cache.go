package app

import "sdmm/internal/aphelion/envsnapshot"

// Rebuild uses the normal close/save/conflict lifecycle before replacing the
// environment. It cannot silently rebind an open authoritative document.
func (a *app) DoRebuildEnvironmentCache() {
	if a.loadedEnvironment == nil {
		return
	}
	path := a.loadedEnvironment.RootFile
	a.closeEnvironment(func(closed bool) {
		if closed {
			a.forceLoadEnvironmentWithOptions(path, nil, envsnapshot.Options{Enabled: true, Rebuild: true})
		}
	})
}
