package prefs

import "sdmm/internal/app/render"

// DefaultAreaOverlayPercent is the default opacity of drawn areas.
const DefaultAreaOverlayPercent = 35

// AreaPolicyFromEditor converts editor preferences to the render area policy.
func AreaPolicyFromEditor(e Editor) render.AreaPolicy {
	percent := e.AreaOverlayPercent
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}
	return render.AreaPolicy{Alpha: float32(percent) / 100, HideBase: e.HideBaseArea}
}

// ApplyAreaPolicy installs the preferences as the render area policy. Changing
// it bumps the render policy revision so retained submissions are rebuilt.
func ApplyAreaPolicy(e Editor) {
	render.SetAreaPolicy(AreaPolicyFromEditor(e))
}
