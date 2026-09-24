package cpsearch

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"weak"

	"sdmm/internal/app/command"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type searchTestApp struct {
	current  *editor.Editor
	commands *command.Storage
}

func (a *searchTestApp) CurrentEditor() *editor.Editor           { return a.current }
func (*searchTestApp) DoEditInstance(*dmminstance.Instance)      {}
func (*searchTestApp) ShowLayout(string, bool)                   {}
func (*searchTestApp) DoSelectPrefab(*dmmprefab.Prefab)          {}
func (*searchTestApp) SelectedPrefab() (*dmmprefab.Prefab, bool) { return nil, false }
func (a *searchTestApp) CommandStorage() *command.Storage        { return a.commands }
func (*searchTestApp) Clipboard() *dmmclip.Clipboard             { return nil }
func (*searchTestApp) PathsFilter() *dm.PathsFilter              { return dm.NewPathsFilterEmpty() }
func (*searchTestApp) SyncPrefabs()                              {}
func (*searchTestApp) SyncVarEditor()                            {}
func (*searchTestApp) RunLater(f func())                         { f() }
func (*searchTestApp) Prefs() prefs.Prefs                        { return prefs.Prefs{} }
func (*searchTestApp) LoadedEnvironment() *dmenv.Dme             { return nil }

// The actual editor/query methods read fixture map contents. Authority and
// native map rendering are deliberately unused by these read-only search tests.
func searchFixture(tb testing.TB, cells, variants int) *Search {
	tb.Helper()
	dmmap.PrefabStorage.Free()
	tb.Cleanup(dmmap.PrefabStorage.Free)
	var prefabs []*dmmprefab.Prefab
	for i := 0; i < variants; i++ {
		vars := &dmvars.MutableVariables{}
		vars.Put("variant", fmt.Sprint(i))
		prefabs = append(prefabs, dmmap.PrefabStorage.Put(dmmprefab.New(uint64(100+i), "/obj/search", vars.ToImmutable())))
	}
	m := &dmmap.Dmm{MaxX: cells, MaxY: 1, MaxZ: 1}
	for x := 1; x <= cells; x++ {
		tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
		tile.InstancesAdd(prefabs[(x-1)%variants])
		tile.Instances()[0].SetStableID(fmt.Sprintf("00000000-0000-7000-8000-%012x", x))
		m.Tiles = append(m.Tiles, tile)
	}
	a := &searchTestApp{commands: command.NewStorage()}
	a.current = editor.New(a, nil, m)
	s := &Search{app: a, selectedResultIdx: -1, focusedResultIdx: -1, lastFocusedResultIdx: -1}
	return s
}

func TestSearchPreservesVariantAndMapOrder(t *testing.T) {
	s := searchFixture(t, 31, 7)
	var expected []*dmminstance.Instance
	for _, prefab := range dmmap.PrefabStorage.GetAllByPath("/obj/search") {
		expected = append(expected, s.app.CurrentEditor().InstancesFindByPrefabId(prefab.Id())...)
	}
	s.SearchByPath("/obj/search")
	if !reflect.DeepEqual(s.results(), expected) {
		t.Fatal("path search changed grouped variant/map ordering")
	}
	s.Search(103)
	if !reflect.DeepEqual(s.results(), s.app.CurrentEditor().InstancesFindByPrefabId(103)) {
		t.Fatal("numeric search changed")
	}
	s.SearchByPath("/obj")
	if len(s.results()) != 0 {
		t.Fatal("exact-path search unexpectedly matched descendants")
	}
	s.SearchByPath("invalid id")
	if len(s.results()) != 0 {
		t.Fatal("invalid query retained results")
	}
}

func TestSearchWithNoCurrentMap(t *testing.T) {
	s := &Search{app: &searchTestApp{}}
	s.Search(103)
	if len(s.results()) != 0 {
		t.Fatal("closed map retained search results")
	}
}

func TestSearchFilterResetsNavigation(t *testing.T) {
	s := searchFixture(t, 20, 1)
	s.SearchByPath("/obj/search")
	s.selectedResultIdx, s.focusedResultIdx, s.lastFocusedResultIdx = 19, 19, 19
	s.filterBound = util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 1}
	s.updateFilteredResults()
	if len(s.results()) != 2 || s.selectedResultIdx != -1 || s.focusedResultIdx != -1 {
		t.Fatal("filtered results kept an obsolete navigation index")
	}
}

func retainSearchInstance(s *Search) weak.Pointer[dmminstance.Instance] {
	i := dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, dmmprefab.New(1, "/obj/retained", dmvars.FromParent(nil)))
	s.resultsAll = append(s.resultsAll, i)
	s.resultsFiltered = append(s.resultsFiltered, i)
	return weak.Make(i)
}

func TestSearchFreeReleasesResultReferences(t *testing.T) {
	s := &Search{}
	ref := retainSearchInstance(s)
	s.Free()
	for range 5 {
		runtime.GC()
	}
	if ref.Value() != nil {
		t.Error("Free retained an instance in unused result capacity")
	}
	runtime.KeepAlive(s)
}

func BenchmarkSearchPath(b *testing.B) {
	for _, variants := range []int{1, 16, 128} {
		b.Run(fmt.Sprintf("variants%d", variants), func(b *testing.B) {
			s := searchFixture(b, 10000, variants)
			s.SearchByPath("/obj/search")
			for !s.ensureCurrent() {
			}
			var expected []*dmminstance.Instance
			for _, prefab := range dmmap.PrefabStorage.GetAllByPath("/obj/search") {
				expected = append(expected, s.app.CurrentEditor().InstancesFindByPrefabId(prefab.Id())...)
			}
			runtime.GC()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.SearchByPath("/obj/search")
				for !s.ensureCurrent() {
				}
			}
			b.StopTimer()
			if !reflect.DeepEqual(s.results(), expected) {
				b.Fatal("query results changed")
			}
			hash := sha256.New()
			for _, instance := range s.results() {
				_, _ = hash.Write([]byte(instance.StableID()))
			}
			b.Logf("results_sha256=%x", hash.Sum(nil))
		})
	}
}
