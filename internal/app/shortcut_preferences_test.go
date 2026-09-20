package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/hotkeys"
	"sdmm/internal/app/ui/shortcut"
)

func TestShortcutPreferencesMigrationAndRoundTrip(t *testing.T) {
	defer shortcut.UseSettings(nil)
	var bindings shortcut.Shortcuts
	bindings.Add(shortcut.Shortcut{Name: "menu#DoSave", FirstKey: glfw.KeyLeftControl, FirstKeyAlt: glfw.KeyRightControl, SecondKey: glfw.KeyS})
	defer bindings.Dispose()
	for _, custom := range []string{"", `,"Shortcuts":null`, `,"Shortcuts":"invalid"`, `,"Shortcuts":{"menu#DoSave":[[[999999,0]]]}`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(`{"Version":3,"Editor":{"SaveFormat":"TGM"}`+custom+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		a := &app{configDir: dir}
		a.loadPreferencesConfig()
		if shortcut.Label("menu#DoSave") != "Ctrl+S" {
			t.Fatal("missing/invalid preference changed defaults")
		}
		chords, err := hotkeys.Parse("Ctrl+K; Alt+F8")
		if err != nil {
			t.Fatal(err)
		}
		if err := shortcut.SetBindings("menu#DoSave", chords); err != nil {
			t.Fatal(err)
		}
		a.configSaveV(a.preferencesConfig())
		reopened := &app{configDir: dir}
		reopened.loadPreferencesConfig()
		if shortcut.Label("menu#DoSave") != "Ctrl+K; Alt+F8" || reopened.preferencesConfig().Editor.SaveFormat != "TGM" {
			t.Fatal("custom binding or existing preferences lost")
		}
		shortcut.ResetBindings("menu#DoSave")
		reopened.configSaveV(reopened.preferencesConfig())
		a.loadPreferencesConfig()
		if shortcut.Label("menu#DoSave") != "Ctrl+S" {
			t.Fatal("reset did not survive preference reload")
		}
	}
}
