package mapping

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestReuseCacheReusesAndInvalidatesParsedInputs(t *testing.T) {
	root := t.TempDir()
	basePath := filepath.Join(root, "base.dmm")
	modulePath := filepath.Join(root, "module.dmm")
	configPath := filepath.Join(root, "modules.toml")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(basePath, `"a" = (/obj/modular_map_root{key = "room"; config_file = "modules.toml"})
(1,1,1) = {"
a
"}
`)
	write(modulePath, `"a" = (/obj/module_a)
(1,1,1) = {"
a
"}
`)
	write(configPath, `directory = ""
[rooms.room]
modules = ["module.dmm"]
`)
	env := &dmenv.Dme{RootDir: root, Objects: map[string]*dmenv.Object{}}
	for _, path := range []string{"/obj/modular_map_root", "/obj/module_a"} {
		env.Objects[path] = &dmenv.Object{Path: path, Vars: dmvars.FromParent(nil)}
	}

	cache := NewReuseCache()
	defer cache.Close()
	load := func() (*Source, *Source, []Root) {
		t.Helper()
		catalog := NewCatalogWithReuseCache(env, cache)
		base, err := catalog.Load(context.Background(), basePath)
		if err != nil {
			t.Fatal(err)
		}
		roots, diagnostics := catalog.Roots(context.Background(), base, Transform{}, "")
		if len(roots) != 1 || len(diagnostics) != 0 {
			t.Fatalf("roots=%+v diagnostics=%+v", roots, diagnostics)
		}
		module, err := catalog.Load(context.Background(), roots[0].Candidates[0].Path)
		if err != nil {
			t.Fatal(err)
		}
		catalog.Close()
		return base, module, roots
	}

	first, firstModule, firstRoots := load()
	second, secondModule, secondRoots := load()
	if first.Identity.ContentHash != second.Identity.ContentHash || firstModule.Identity.ContentHash != secondModule.Identity.ContentHash || firstRoots[0].BindingHash != secondRoots[0].BindingHash {
		t.Fatal("unchanged source/config identity changed across requests")
	}
	if cache.sourceHits != 2 || cache.sourceMisses != 2 || cache.configHits != 1 || cache.configMisses != 1 {
		t.Fatalf("first reuse counts: source %d hits/%d misses, config %d hits/%d misses", cache.sourceHits, cache.sourceMisses, cache.configHits, cache.configMisses)
	}

	write(modulePath, `"a" = (/obj/module_b)
(1,1,1) = {"
a
"}
`)
	third, thirdModule, _ := load()
	if thirdModule.Identity.ContentHash == secondModule.Identity.ContentHash {
		t.Fatal("changed module file reused its old source")
	}

	write(filepath.Join(root, "other.dmm"), `"a" = (/obj/module_a)
(1,1,1) = {"
a
"}
`)
	write(configPath, `directory = ""
[rooms.room]
modules = ["other.dmm"]
`)
	configCatalog := NewCatalogWithReuseCache(env, cache)
	configBase, err := configCatalog.Load(context.Background(), basePath)
	if err != nil {
		t.Fatal(err)
	}
	configRoots, diagnostics := configCatalog.Roots(context.Background(), configBase, Transform{}, "")
	if len(configRoots) != 1 || len(diagnostics) != 0 {
		t.Fatalf("changed config roots=%+v diagnostics=%+v", configRoots, diagnostics)
	}
	if _, err := configCatalog.Load(context.Background(), configRoots[0].Candidates[0].Path); err != nil {
		t.Fatal(err)
	}
	configCatalog.Close()
	if got, want := configRoots[0].Candidates[0].Path, filepath.Join(root, "other.dmm"); got != want {
		t.Fatalf("config cache served stale candidate %q, want %q", got, want)
	}

	// Reusing a disk source across a new environment is forbidden even when
	// the file itself is unchanged.
	env.Objects["/obj/unrelated"] = &dmenv.Object{Path: "/obj/unrelated", Vars: dmvars.FromParent(nil)}
	fourthCatalog := NewCatalogWithReuseCache(env, cache)
	fourth, err := fourthCatalog.Load(context.Background(), basePath)
	if err != nil {
		t.Fatal(err)
	}
	if roots, diagnostics := fourthCatalog.Roots(context.Background(), fourth, Transform{}, ""); len(roots) != 1 || len(diagnostics) != 0 {
		t.Fatalf("new environment roots=%+v diagnostics=%+v", roots, diagnostics)
	}
	if fourth.Identity.EnvironmentHash == third.Identity.EnvironmentHash {
		t.Fatal("changed environment reused old environment identity")
	}

	if cache.sourceHits != 4 || cache.sourceMisses != 5 || cache.configHits != 2 || cache.configMisses != 3 {
		t.Fatalf("final reuse counts: source %d hits/%d misses, config %d hits/%d misses", cache.sourceHits, cache.sourceMisses, cache.configHits, cache.configMisses)
	}
	t.Logf("source_reuse=%d source_miss=%d config_reuse=%d config_miss=%d", cache.sourceHits, cache.sourceMisses, cache.configHits, cache.configMisses)

	if _, err := fourth.DisplayMap(context.Background()); err != nil {
		t.Fatal("allocate display map from cache borrow:", err)
	}
	cache.mu.Lock()
	borrowed := cache.sources[reuseSourceKey{path: sourceKey(basePath), environmentHash: fourth.Identity.EnvironmentHash}]
	if borrowed == nil {
		cache.mu.Unlock()
		t.Fatal("fourth catalogue did not retain its parsed source in cache")
	}
	borrowedBytes := borrowed.source.lease.Bytes()
	cache.mu.Unlock()
	budget := resources.DefaultBudget()
	usedBeforeClose := budget.Used()
	cache.Close()
	if len(cache.sources) != 0 || len(cache.configs) != 0 {
		t.Fatal("closing reuse cache retained entries")
	}
	usedAfterCacheClose := budget.Used()
	if usedAfterCacheClose < borrowedBytes {
		t.Fatalf("closing cache released an outstanding catalogue source: used=%d source=%d", usedAfterCacheClose, borrowedBytes)
	}
	if got := fourth.Cell(util.Point{X: 1, Y: 1, Z: 1}); len(got) != 1 {
		t.Fatalf("outstanding source stopped working after cache close: %+v", got)
	}
	fourthCatalog.Close()
	usedAfterCatalogClose := budget.Used()
	if usedAfterCatalogClose > usedAfterCacheClose || usedAfterCacheClose-usedAfterCatalogClose < borrowedBytes {
		t.Fatalf("catalogue close did not release borrowed source reservation: before=%d after=%d source=%d", usedAfterCacheClose, usedAfterCatalogClose, borrowedBytes)
	}
	if usedBeforeClose < usedAfterCacheClose {
		t.Fatalf("closing cache increased reserved memory: before=%d after=%d", usedBeforeClose, usedAfterCacheClose)
	}
}

func TestReuseCacheNeverOverridesAcceptedSourceOrDeferredState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "base.dmm")
	if err := os.WriteFile(path, []byte(`"a" = (/obj/on-disk)
(1,1,1) = {"
a
"}
`), 0600); err != nil {
		t.Fatal(err)
	}
	env := &dmenv.Dme{RootDir: root, Objects: map[string]*dmenv.Object{}}
	cache := NewReuseCache()
	defer cache.Close()
	first := NewCatalogWithReuseCache(env, cache)
	if _, err := first.Load(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	first.Close()

	environmentHash, err := env.EnvironmentHash()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		ProtocolVersion: model.ProtocolVersion,
		SchemaVersion:   model.SchemaVersion,
		DocumentID:      "01890f3e-7b5c-7abc-8def-0123456789ab",
		Revision:        11,
		EnvironmentHash: environmentHash,
		MaxX:            1,
		MaxY:            1,
		MaxZ:            1,
		Tiles: []model.Tile{{
			Coord: model.Coord{X: 1, Y: 1, Z: 1},
			State: model.TileState{Prefabs: []model.PrefabState{{
				StableID: "01890f3e-7b5c-7abc-8def-0123456789ac",
				Path:     "/obj/accepted-unsaved",
				Vars:     map[string]string{},
			}}},
		}},
	}
	accepted := NewCatalogWithReuseCache(env, cache)
	accepted.SetAcceptedSources(map[string]AcceptedSource{
		path: {Snapshot: snapshotFixture{snapshot}, Generation: 4},
	})
	source, err := accepted.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	cell := source.Cell(util.Point{X: 1, Y: 1, Z: 1})
	if source.Identity.DocumentID != string(snapshot.DocumentID) || source.Identity.Generation != 4 || len(cell) != 1 || cell[0].Path != "/obj/accepted-unsaved" {
		t.Fatalf("cached disk source overrode accepted generation: identity=%+v cell=%+v", source.Identity, cell)
	}
	accepted.Close()

	deferred := NewCatalogWithReuseCache(env, cache)
	deferred.SetAcceptedSources(map[string]AcceptedSource{path: {Err: errors.New("accepted snapshot unavailable")}})
	if _, err := deferred.Load(context.Background(), path); !errors.Is(err, ErrAcceptedDeferred) {
		t.Fatalf("cached disk source overrode accepted deferred state: %v", err)
	}
	deferred.Close()
}
