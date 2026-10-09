package theme

import "github.com/SpaiR/imgui-go"

// ButtonTint is a button's fill in its three states.
type ButtonTint struct{ Normal, Hover, Active imgui.Vec4 }

// Button tints over the raised surface. Text stays parchment on every fill
// except Primary, which is the full accent with dark ink, as on the site's
// call-to-action buttons.
var (
	// SelectedButton marks the chosen segment of a toggle group.
	SelectedButton = tintOver(Meridian.Accent, 0.30, 0.38, 0.22)
	// Positive confirms (update, apply).
	Positive = tintOver(Meridian.Green, 0.38, 0.44, 0.30)
	// Negative cancels or destroys.
	Negative = tintOver(Meridian.Red, 0.45, 0.52, 0.36)
	// Primary is the active tool: full accent, AccentInk text.
	Primary = ButtonTint{
		Normal: Meridian.Accent,
		Hover:  Mix(Meridian.Accent, Meridian.Text, 0.25),
		Active: Mix(Meridian.Accent, Meridian.Bg0, 0.2),
	}
)

func tintOver(c imgui.Vec4, normal, hover, active float32) ButtonTint {
	return ButtonTint{Normal: Mix(Meridian.Bg3, c, normal), Hover: Mix(Meridian.Bg3, c, hover), Active: Mix(Meridian.Bg3, c, active)}
}
