package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/cpwsarea/wsprefs"
)

func TestSelectionMoveStepPreferencesMigrationAndRoundTrip(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 1}, {",\"SelectionMoveStep\":0", 1}, {",\"SelectionMoveStep\":-1", 1},
		{",\"SelectionMoveStep\":4", 4}, {fmt.Sprintf(",\"SelectionMoveStep\":%d", math.MaxInt), 1},
	} {
		t.Run(test.value, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "preferences.json")
			if err := os.WriteFile(path, []byte(`{"Version":3,"Editor":{"SaveFormat":"TGM","NudgeMode":"step_x/step_y"`+test.value+`}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			a := &app{configDir: dir}
			a.loadPreferencesConfig()
			cfg := a.preferencesConfig()
			if cfg.Editor.SelectionMoveStep != test.want || cfg.Editor.SaveFormat != prefs.SaveFormatTGM || cfg.Editor.NudgeMode != prefs.SaveNudgeModeStep {
				t.Fatal("preference migration changed defaults or existing settings")
			}
			found := false
			for _, preference := range prefs.Make(nil, &cfg.Prefs)[wsprefs.GPEditor] {
				if control, ok := preference.(wsprefs.IntPref); ok && control.Name == "Selection Move Step" {
					if control.Min != 1 || control.Max != 4095 || control.FGet() != test.want {
						t.Fatal("preference control has incorrect bounds or initial value")
					}
					control.FSet(8)
					found = true
				}
			}
			if !found {
				t.Fatal("selection step is missing from Editor preferences")
			}
			a.configSaveV(cfg)
			reopened := &app{configDir: dir}
			reopened.loadPreferencesConfig()
			if reopened.preferencesConfig().Editor.SelectionMoveStep != 8 {
				t.Fatal("selection step was not saved and restored")
			}
		})
	}
}
