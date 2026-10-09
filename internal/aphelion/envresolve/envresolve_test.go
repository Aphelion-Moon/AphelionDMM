package envresolve

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNearestAncestor(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "outer.dme"))
	touch(t, filepath.Join(root, "repo", "repo.dme"))
	mapPath := filepath.Join(root, "repo", "maps", "deep", "a.dmm")
	touch(t, mapPath)
	res, err := Resolve(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "repo", "repo.dme"); res.Path != want || len(res.Candidates) != 0 {
		t.Fatalf("got %+v want %s", res, want)
	}
}

func TestResolvePrefersStemMatchingDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "station")
	touch(t, filepath.Join(dir, "other.dme"))
	touch(t, filepath.Join(dir, "station.dme"))
	res, err := Resolve(filepath.Join(dir, "maps", "a.dmm"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != filepath.Join(dir, "station.dme") {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveAmbiguous(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "station")
	touch(t, filepath.Join(dir, "b.dme"))
	touch(t, filepath.Join(dir, "a.dme"))
	res, err := Resolve(filepath.Join(dir, "a.dmm"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "" || len(res.Candidates) != 2 || res.Candidates[0] != filepath.Join(dir, "a.dme") || res.Candidates[1] != filepath.Join(dir, "b.dme") {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveNotFound(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "x", "a.dmm"))
	// A stray .dme above the temp root could exist; only assert on error shape when absent.
	res, err := Resolve(filepath.Join(root, "x", "a.dmm"))
	if err == nil && !strings.HasPrefix(res.Path, root) {
		t.Skip("ancestor of temp dir contains a .dme")
	}
	if err == nil {
		t.Fatalf("expected error, got %+v", res)
	}
}

func TestSameEnvironment(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.dme")
	if !SameEnvironment(a, filepath.Join(dir, "sub", "..", "a.dme")) {
		t.Fatal("cleaned paths should match")
	}
	if SameEnvironment(a, filepath.Join(dir, "b.dme")) {
		t.Fatal("different files should not match")
	}
	if runtime.GOOS == "windows" && !SameEnvironment(a, strings.ToUpper(a)) {
		t.Fatal("windows comparison must be case-insensitive")
	}
}

func TestOrderDropped(t *testing.T) {
	got := OrderDropped([]string{"a.dmm", "notes.txt", "b.TGM", "env.dme", "c.dmi", "second.dme"})
	want := []string{"env.dme", "second.dme", "a.dmm", "b.TGM"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
