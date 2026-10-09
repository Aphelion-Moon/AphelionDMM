package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/repath"
	repathui "sdmm/internal/aphelion/repath/ui"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/cpwsarea/wsprefs"
	"sdmm/internal/app/ui/layout"
)

func TestPathMigrationPreferencesDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(`{"Version":3,"Editor":{"SaveFormat":"TGM"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{configDir: dir}
	a.loadPreferencesConfig()
	settings := a.PathMigrationSettings()
	if settings == nil || *settings != repath.DefaultSettings() {
		t.Fatalf("settings = %#v", settings)
	}
	settings.ApplyCertainOnOpen, settings.Auto = true, repath.AutoModeHigh
	a.configSaveV(a.preferencesConfig())
	reopened := &app{configDir: dir}
	reopened.loadPreferencesConfig()
	if got := reopened.PathMigrationSettings(); !got.ApplyCertainOnOpen || got.Level() != repath.AutoHigh || reopened.preferencesConfig().Editor.SaveFormat != "TGM" {
		t.Fatalf("reopened = %#v", got)
	}
	names := map[string]bool{}
	for _, preference := range prefs.Make(nil, &reopened.preferencesConfig().Prefs)[wsprefs.GPEditor] {
		switch preference := preference.(type) {
		case wsprefs.BoolPref:
			names[preference.Name] = true
		case wsprefs.OptionPref:
			names[preference.Name] = true
		}
	}
	for _, name := range []string{"Path Migration: Open on Unknown Types", "Path Migration: Automatic Selection", "Path Migration: Allow Saving into Codebase"} {
		if !names[name] {
			t.Fatalf("preference %q is not offered", name)
		}
	}
}

func TestPathMigrationMemoryIsSeparateAndPersists(t *testing.T) {
	dir := t.TempDir()
	a := &app{configDir: dir}
	a.loadPathMigrationConfig()
	memory := a.PathMigrationMemory()
	if memory == nil || len(memory.Environments) != 0 {
		t.Fatal("memory is not empty by default")
	}
	if _, err := os.Stat(filepath.Join(dir, "pathmigration.json")); !os.IsNotExist(err) {
		t.Fatal("registering memory wrote a file without a decision")
	}
	plan := repath.NewPlan()
	plan.Paths["/obj/old"] = repath.Decision{Kind: repath.Apply, Via: repath.ViaRule, Rule: repath.RepathRule("/obj/old", "/obj/new", nil)}
	key := repath.EnvironmentKey(filepath.Join(dir, "x.dme"))
	memory.Remember(key, plan)
	a.SavePathMigrationMemory()
	reopened := &app{configDir: dir}
	reopened.loadPathMigrationConfig()
	if rules, errs := reopened.PathMigrationMemory().Rules(key); len(rules) != 1 || len(errs) != 0 {
		t.Fatalf("rules = %#v errs = %v", rules, errs)
	}
}

func TestOfferPathMigrationFollowsPreferences(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	context := imgui.CreateContext(nil)
	defer context.Destroy()
	tests := []struct {
		open, apply  bool
		wantOpen     bool
		wantOffering bool
	}{
		{open: true, wantOpen: true, wantOffering: true},
		{open: false, apply: true, wantOffering: true},
		{},
	}
	for _, test := range tests {
		a := &app{configDir: t.TempDir()}
		a.loadPreferencesConfig()
		settings := a.PathMigrationSettings()
		settings.OpenOnUnknown, settings.ApplyCertainOnOpen = test.open, test.apply
		a.layout = &layout.Layout{PathMigration: repathui.NewPanel(a)}
		a.offerPathMigration("C:/maps/old.dmm")
		if a.layout.PathMigrationOpen() != test.wantOpen {
			t.Fatalf("%+v: open = %v", test, a.layout.PathMigrationOpen())
		}
		if offering := a.layout.PathMigration.Controller.Offering("C:/maps/old.dmm"); offering != (test.wantOffering && test.apply) {
			t.Fatalf("%+v: automatic application pending = %v", test, offering)
		}
	}
}
