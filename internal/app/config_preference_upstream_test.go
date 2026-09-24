package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sdmm/internal/app/prefs"
)

func TestCodeEditorVSCodiumFallback(t *testing.T) {
	for _, tc := range []struct {
		name, selected, available, want string
	}{
		{"code to codium", prefs.CodeEditorVSC, "codium", "VSCodium"},
		{"dreammaker to codium", prefs.CodeEditorDM, "codium", "VSCodium"},
		{"notepad to codium", prefs.CodeEditorNPP, "codium", "VSCodium"},
		{"codium to code", "VSCodium", "code", prefs.CodeEditorVSC},
		{"codium unavailable", "VSCodium", "", prefs.CodeEditorDefault},
		{"codium retained", "VSCodium", "codium", "VSCodium"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			if tc.available != "" {
				name := tc.available
				if runtime.GOOS == "windows" {
					name += ".exe"
					t.Setenv("PATHEXT", ".EXE")
				}
				// LookPath checks discovery only; no editor process is launched.
				if err := os.WriteFile(filepath.Join(dir, name), nil, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &preferencesConfig{Prefs: prefs.Prefs{Editor: prefs.Editor{CodeEditor: tc.selected}}}
			(&app{}).validateCodeEditor(cfg)
			if cfg.Editor.CodeEditor != tc.want {
				t.Fatalf("selected %q, want %q", cfg.Editor.CodeEditor, tc.want)
			}
		})
	}
}
