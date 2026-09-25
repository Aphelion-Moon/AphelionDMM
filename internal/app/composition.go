// APHELION EDIT ADDITION START - COMPOSITION INSPECTOR
package app

func (a *app) DoOpenCompositionInspector() { a.layout.Composition.Open() }
func (a *app) ActiveMappingPath() string {
	if ws, ok := a.activeWsMap(); ok {
		return ws.Map().Dmm().Path.Absolute
	}
	return ""
}

// APHELION EDIT ADDITION END
