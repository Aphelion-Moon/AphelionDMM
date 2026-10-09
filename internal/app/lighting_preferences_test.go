package app

import (
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/aphelion/lighting/maplight"
)

// A preferences file written before the lighting preview existed must load with
// the approved defaults, and the View toggle must persist through the config.
func TestLightingPreferencesDefaultsMigrationAndToggle(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"Version":3,"Editor":{"SaveFormat":"TGM","NudgeMode":"step_x/step_y"}}`
	if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	defer maplight.Apply(maplight.DefaultSettings())
	defer maplight.SetPersist(nil)
	a := &app{configDir: dir}
	a.loadPreferencesConfig()
	cfg := a.preferencesConfig()
	l := cfg.Editor.Lighting
	if l.Preview || l.Darkness != 70 || !l.Starlight || !l.OverlayLights || l.ShowSources {
		t.Fatalf("legacy file did not get the approved lighting defaults: %+v", l)
	}
	if live := maplight.Current(); live.Enabled || live.Darkness != 70 || !live.Starlight {
		t.Fatalf("live settings not installed: %+v", live)
	}

	maplight.SetEnabled(true) // View > Lighting Preview
	if !cfg.Editor.Lighting.Preview || !maplight.Current().Enabled {
		t.Fatal("toggle must update the live setting and the preference")
	}
	a.configSaveV(cfg)
	reopened := &app{configDir: dir}
	reopened.loadPreferencesConfig()
	if !reopened.preferencesConfig().Editor.Lighting.Preview {
		t.Fatal("toggle did not persist")
	}
}
