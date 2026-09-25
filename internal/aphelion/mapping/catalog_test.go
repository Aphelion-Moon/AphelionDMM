package mapping

import (
	"context"
	"os"
	"path/filepath"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"strings"
	"testing"
)

func TestConfiguredSlotsAndConnectorTranslation(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"modules.toml": "directory = \"\"\n[rooms.room]\nmodules = [\"module.dmm\",\"module.dmm\"]\n",
		"base.dmm":     "\"a\" = (/obj/modular_map_root{key = \"room\"; config_file = \"modules.toml\"})\n(5,4,2) = {\"\na\n\"}\n",
		"module.dmm":   "\"a\" = (/obj/a)\n\"b\" = (/obj/modular_map_connector)\n(1,1,1) = {\"\nab\n\"}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := &dmenv.Dme{RootDir: root, Objects: map[string]*dmenv.Object{}}
	for _, path := range []string{"/obj/modular_map_root", "/obj/modular_map_connector"} {
		env.Objects[path] = &dmenv.Object{Path: path, Vars: dmvars.FromParent(nil)}
	}
	s, err := LoadSource(context.Background(), filepath.Join(root, "base.dmm"), env)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := NewCatalog(env)
	defer c.Close()
	roots, diagnostics := c.Roots(context.Background(), s, Transform{}, "")
	if len(roots) != 1 || len(diagnostics) != 0 || len(roots[0].Candidates) != 2 {
		t.Fatalf("lost actual roots or list multiplicity: %+v %+v", roots, diagnostics)
	}
	module, err := c.Load(context.Background(), roots[0].Candidates[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	transform, err := Anchor(module, roots[0].Destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := transform.Apply(util.Point{X: 2, Y: 1, Z: 1}); got != roots[0].Destination {
		t.Fatalf("connector did not meet root: %+v", got)
	}
	if got := transform.Apply(util.Point{X: 1, Y: 1, Z: 1}); got.X != 4 || got.Y != roots[0].Destination.Y || got.Z != 2 {
		t.Fatalf("wrong nested/multi-Z parent transform: %+v", got)
	}
}

func TestBindingIdentityAndCandidateAdmission(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "base.dmm")
	config := filepath.Join(root, "modules.toml")
	if err := os.WriteFile(path, []byte("\"a\" = (/obj/modular_map_root{key = \"room\"; config_file = \"modules.toml\"})\n(1,1,1) = {\"\na\n\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := &dmenv.Dme{RootDir: root, Objects: map[string]*dmenv.Object{}}
	s, err := LoadSource(context.Background(), path, env)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	read := func(content string) ([]Root, []Diagnostic) {
		t.Helper()
		if err := os.WriteFile(config, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		c := NewCatalog(env)
		defer c.Close()
		return c.Roots(context.Background(), s, Transform{}, "")
	}
	a, _ := read("[rooms.room]\nmodules = [\"missing.dmm\"]\n")
	b, _ := read("[rooms.room]\nmodules = [\"different.dmm\"]\n")
	if len(a) != 1 || len(b) != 1 || a[0].ID == b[0].ID {
		t.Fatal("binding change silently reused an occurrence identity")
	}
	many, diagnostics := read("[rooms.room]\nmodules = [" + strings.Repeat("\"missing.dmm\",", 1025) + "]\n")
	if len(many) != 1 || len(many[0].Candidates) != 0 || len(diagnostics) == 0 {
		t.Fatal("oversized candidate list was expanded")
	}
}
