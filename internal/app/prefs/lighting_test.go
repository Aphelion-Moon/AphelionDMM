package prefs

import (
	"testing"

	"sdmm/internal/aphelion/lighting/maplight"
	"sdmm/internal/app/ui/cpwsarea/wsprefs"
)

func TestDefaultLightingMatchesApprovedDecisions(t *testing.T) {
	d := DefaultLighting()
	if d.Preview {
		t.Fatal("the preview starts off")
	}
	if d.Darkness != 70 || !d.Starlight || !d.OverlayLights || d.ShowSources {
		t.Fatalf("defaults %+v: want darkness 70, starlight on, overlay lights on, markers off", d)
	}
}

func TestLightingSettingsFromEditorClampsAndMaps(t *testing.T) {
	s := LightingSettingsFromEditor(Editor{Lighting: Lighting{Preview: true, Darkness: 250, Starlight: true, ShowSources: true}})
	want := maplight.Settings{Enabled: true, Darkness: 100, Starlight: true, ShowSources: true}
	if s != want {
		t.Fatalf("got %+v want %+v", s, want)
	}
	if got := LightingSettingsFromEditor(Editor{Lighting: Lighting{Darkness: -4}}).Darkness; got != 0 {
		t.Fatalf("darkness %d", got)
	}
}

func TestApplyLightingInstallsLiveSettings(t *testing.T) {
	defer maplight.Apply(maplight.DefaultSettings())
	ApplyLighting(Editor{Lighting: Lighting{Darkness: 40, Starlight: true}})
	if got := maplight.Current(); got.Darkness != 40 || !got.Starlight || got.OverlayLights {
		t.Fatalf("live settings %+v", got)
	}
}

func TestPreferencesExposeLightingOptions(t *testing.T) {
	e := Prefs{Editor: Editor{Lighting: DefaultLighting()}}
	groups := Make(nopApp{}, &e)
	want := map[string]bool{"%##lighting_darkness": false, "##lighting_starlight": false, "##lighting_overlay_lights": false, "##lighting_show_sources": false}
	for _, p := range groups[wsprefs.GPEditor] {
		switch pref := p.(type) {
		case wsprefs.IntPref:
			if _, ok := want[pref.Label]; ok {
				want[pref.Label] = true
			}
		case wsprefs.BoolPref:
			if _, ok := want[pref.Label]; ok {
				want[pref.Label] = true
			}
		}
	}
	for label, seen := range want {
		if !seen {
			t.Errorf("preference %s is not listed in the Editor group", label)
		}
	}
}
