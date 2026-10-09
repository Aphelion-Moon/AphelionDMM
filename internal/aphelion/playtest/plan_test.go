package playtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMapConfigReusesTheStationConfig(t *testing.T) {
	root := t.TempDir()
	station := `{"version":1,"map_name":"MiniStation","map_path":"map_files/Ministation","map_file":"MiniStation.dmm","shuttles":{"cargo":"x"}}`
	write(t, filepath.Join(root, "_maps", "ministation.json"), station)
	data, name, err := MapConfig(root, filepath.Join(root, "_maps", "map_files", "Ministation", "MiniStation.dmm"))
	if err != nil || name != "MiniStation" || string(data) != station {
		t.Fatalf("config = %s %q %v", data, name, err)
	}
	// A map without a station config gets a minimal one.
	data, name, err = MapConfig(root, filepath.Join(root, "_maps", "test", "Box.dmm"))
	if err != nil || name != "Box" || !strings.Contains(string(data), `"map_path": "test"`) || !strings.Contains(string(data), `"map_file": "Box.dmm"`) {
		t.Fatalf("minimal = %s %q %v", data, name, err)
	}
	// Outside _maps the game cannot load it.
	if _, _, err := MapConfig(root, filepath.Join(root, "elsewhere", "x.dmm")); err == nil {
		t.Fatal("accepted a map outside _maps")
	}
	if err := WriteMapConfig(root, data); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "data", "next_map.json")); string(got) != string(data) {
		t.Fatal("next_map.json not written")
	}
}

func TestNeedsCompileOnlyForCompiledInputs(t *testing.T) {
	root := t.TempDir()
	dmb := filepath.Join(root, "tgstation.dmb")
	write(t, filepath.Join(root, "code", "a.dm"), "x")
	if need, why, _ := NeedsCompile(root, dmb); !need || !strings.Contains(why, "missing") {
		t.Fatal("missing dmb not reported")
	}
	write(t, dmb, "built")
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(root, "code", "a.dm"), old, old)
	write(t, filepath.Join(root, "_maps", "x.dmm"), "map edits never need a build")
	write(t, filepath.Join(root, "data", "y.dm"), "runtime data is skipped")
	if need, why, err := NeedsCompile(root, dmb); need || err != nil {
		t.Fatalf("current build reported stale: %s %v", why, err)
	}
	future := time.Now().Add(time.Hour)
	write(t, filepath.Join(root, "icons", "b.dmi"), "x")
	_ = os.Chtimes(filepath.Join(root, "icons", "b.dmi"), future, future)
	if need, why, _ := NeedsCompile(root, dmb); !need || !strings.Contains(why, "b.dmi") {
		t.Fatalf("newer icon missed: %v %q", need, why)
	}
}

func TestCommandsAreFixedAndLocal(t *testing.T) {
	c := Build(`C:\BYOND\bin`, `C:\repo\tgstation.dme`, 1337, false)
	if c.Compile != nil || c.Server[1] != `C:\repo\tgstation.dmb` || c.Server[2] != "1337" || c.Client[1] != "byond://127.0.0.1:1337" {
		t.Fatalf("commands = %+v", c)
	}
	if Build(`C:\BYOND\bin`, `C:\repo\tgstation.dme`, 1337, true).Compile[0] != filepath.Join(`C:\BYOND\bin`, "dm.exe") {
		t.Fatal("compile command")
	}
}
