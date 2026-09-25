package window_test

import (
	"context"
	"os"
	"path/filepath"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestNativePreparedOpenPublishesWholeAuthorityBeforeBoundedGeometry(t *testing.T) {
	original, app := newMouseNetworkWorkspace(t)
	owned := original.Map().Dmm().Copy()
	owned.Path.Absolute = filepath.Join(t.TempDir(), "prepared.dmm")
	input, err := os.ReadFile(original.Map().Dmm().Path.Absolute)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(owned.Path.Absolute, input, 0600); err != nil {
		t.Fatal(err)
	}
	owned.DiskState, err = diskversion.Capture(owned.Path.Absolute)
	if err != nil {
		t.Fatal(err)
	}
	owned.SetMapSize(100, 100, 3)
	upper := owned.GetTile(util.Point{X: 2, Y: 2, Z: 3})
	substrate := upper.Instances()
	upper.Set(nil)
	for _, value := range []string{"\"first\"", "\"second\""} {
		upper.InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/obj/unknown_upper_deck", dmvars.Set(&dmvars.Variables{}, "marker", value)))
	}
	upper.Set(append(upper.Instances(), substrate...))
	type result struct {
		prepared *editor.PreparedOpen
		err      error
	}
	done := make(chan result, 1)
	go func() {
		prepared, err := editor.PrepareOpen(context.Background(), app.environment, &owned)
		done <- result{prepared, err}
	}()
	var prepared *editor.PreparedOpen
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		prepared = result.prepared
	case <-time.After(10 * time.Second):
		t.Fatal("owned preparation stalled")
	}
	expected := prepared.Hash()
	ws := wsmap.NewPrepared(app, prepared)
	t.Cleanup(ws.Dispose)
	snapshot, err := ws.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	actual, err := snapshot.Hash()
	if err != nil || actual != expected {
		t.Fatal("installed authority differs", err)
	}
	renderer := ws.Map().Canvas().Render()
	if renderer.LevelReady(1) || renderer.LevelReady(2) || renderer.LevelReady(3) {
		t.Fatal("prepared geometry reported ready before building")
	}
	// Save before any renderer progress or upper-deck visit. Authority must
	// preserve ordered unknown atoms independently of visual readiness.
	if !saveWorkspaceAsync(t, ws) {
		t.Fatal("untouched unvisited-deck save failed")
	}
	untouched, err := dmmdata.New(owned.Path.Absolute)
	if err != nil {
		t.Fatal(err)
	}
	if untouched.MaxZ != 3 || untouched.MaxX != 100 || untouched.MaxY != 100 {
		t.Fatal("untouched save lost dimensions")
	}
	for z := 1; z <= 3; z++ {
		if len(untouched.Dictionary[untouched.Grid[util.Point{X: 1, Y: 1, Z: z}]]) == 0 {
			t.Fatalf("untouched save lost unvisited Z %d", z)
		}
	}
	got := untouched.Dictionary[untouched.Grid[upper.Coord]]
	if len(got) != len(upper.Instances()) {
		t.Fatal("untouched save lost upper-deck atoms")
	}
	for i, instance := range upper.Instances() {
		if got[i].ContentKey() != instance.Prefab().ContentKey() {
			t.Fatalf("untouched save changed content/order at index %d: got %s want %s", i, got[i].ContentKey(), instance.Prefab().ContentKey())
		}
	}
	if renderer.PickAt(16, 16, 1, func(i *dmminstance.Instance) bool { return true }) != nil {
		t.Fatal("partial geometry was pickable")
	}
	renderer.ProcessLevelBuild()
	if !renderer.LevelLoading() {
		t.Fatal("initial graphics ignored chunk quota")
	}
	for steps := 0; renderer.LevelLoading() && steps < 100; steps++ {
		renderer.ProcessLevelBuild()
	}
	if renderer.LevelLoading() || !renderer.LevelReady(1) {
		t.Fatal("active geometry did not finish")
	}
	if renderer.PickAt(16, 16, 1, func(i *dmminstance.Instance) bool { return i.Prefab().Path() == "/obj/foo" }) == nil {
		t.Fatal("prepared geometry did not contain source object")
	}
	renderer.SetActiveLevel(ws.Map().Dmm(), 3)
	if !renderer.LevelLoading() {
		t.Fatal("cold level switch synchronously built all geometry")
	}
	for steps := 0; renderer.LevelLoading() && steps < 100; steps++ {
		renderer.ProcessLevelBuild()
	}
	if renderer.LevelLoading() || renderer.PickAt(16, 16, 3, func(i *dmminstance.Instance) bool { return true }) == nil {
		t.Fatal("cold level geometry did not complete")
	}
	for steps := 0; (!renderer.LevelReady(1) || !renderer.LevelReady(2)) && steps < 100; steps++ {
		renderer.ProcessLevelBuild()
	}
	if !renderer.LevelReady(1) || !renderer.LevelReady(2) || !renderer.LevelReady(3) {
		t.Fatal("background warm-up did not finish every Z")
	}
	renderer.SetActiveLevel(ws.Map().Dmm(), 1)
	if renderer.LevelLoading() {
		t.Fatal("warm level switch rebuilt geometry")
	}
	after, err := ws.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterHash, _ := after.Hash()
	if afterHash != expected {
		t.Fatal("graphics preparation changed authority")
	}
	if !saveWorkspaceAsync(t, ws) {
		t.Fatal("prepared multi-Z save failed")
	}
	reopened, err := dmmdata.New(owned.Path.Absolute)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.MaxZ != 3 || reopened.MaxX != 100 || reopened.MaxY != 100 {
		t.Fatal("save/reopen lost dimensions")
	}
	for z := 1; z <= 3; z++ {
		if len(reopened.Dictionary[reopened.Grid[util.Point{X: 1, Y: 1, Z: z}]]) == 0 {
			t.Fatalf("save/reopen lost Z %d", z)
		}
	}
}
