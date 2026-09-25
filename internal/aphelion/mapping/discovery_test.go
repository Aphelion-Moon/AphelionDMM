package mapping

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverMapConfigurationUsesExactSourceAndReportsAmbiguity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "_maps")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "map_files", "station.dmm")
	if err := os.Mkdir(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"map_path":"map_files","map_file":"station.dmm"}`)
	if err := os.WriteFile(filepath.Join(dir, "station.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := DiscoverMapConfigurations(context.Background(), root, source)
	if err != nil || len(paths) != 1 || paths[0] != "_maps/station.json" {
		t.Fatal(paths, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alternate.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	paths, err = DiscoverMapConfigurations(context.Background(), root, source)
	if err != nil || len(paths) != 2 {
		t.Fatal("ambiguous configuration silently selected", paths, err)
	}
}
