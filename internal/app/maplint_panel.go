// APHELION EDIT ADDITION START - PLACEMENT LINT
package app

import "sdmm/internal/app/ui/layout/lnode"

// DoOpenMapLintPanel shows the repository lint rule status and map scan.
func (a *app) DoOpenMapLintPanel() {
	a.ShowLayout(lnode.NameMapLint, true)
}

// APHELION EDIT ADDITION END
