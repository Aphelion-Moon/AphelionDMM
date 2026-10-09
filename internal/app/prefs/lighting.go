// APHELION EDIT ADDITION START - LIGHTING PREVIEW
package prefs

import "sdmm/internal/aphelion/lighting/maplight"

// Lighting holds the per-user lighting preview preferences. They are display
// settings only: they never enter a map, history entry or collaboration message.
type Lighting struct {
	// Preview is the View > Lighting Preview toggle.
	Preview bool
	// Darkness is the darkness strength in percent (0..100).
	Darkness int
	// Starlight lights tiles next to space.
	Starlight bool
	// OverlayLights approximates OVERLAY_LIGHT sources (labelled approximate).
	OverlayLights bool
	// ShowSources draws a marker, range ring and cone edges per emitter.
	ShowSources bool
}

// DefaultLighting returns the approved defaults: preview off, 70% darkness,
// starlight and overlay lights on, markers off.
func DefaultLighting() Lighting {
	return Lighting{Darkness: maplight.DefaultDarkness, Starlight: true, OverlayLights: true}
}

// LightingSettingsFromEditor converts the preferences to preview settings.
func LightingSettingsFromEditor(e Editor) maplight.Settings {
	return maplight.Settings{
		Enabled:       e.Lighting.Preview,
		Darkness:      e.Lighting.Darkness,
		Starlight:     e.Lighting.Starlight,
		OverlayLights: e.Lighting.OverlayLights,
		ShowSources:   e.Lighting.ShowSources,
	}.Normalize()
}

// ApplyLighting installs the preferences as the live preview settings.
func ApplyLighting(e Editor) { maplight.Apply(LightingSettingsFromEditor(e)) }

// APHELION EDIT ADDITION END
