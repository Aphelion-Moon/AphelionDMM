package window_test

import (
	"context"
	"path/filepath"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"testing"
	"time"
)

func TestNativePreparedOpenPublishesWholeAuthorityBeforeBoundedGeometry(t *testing.T) {
	original, app := newMouseNetworkWorkspace(t)
	owned := original.Map().Dmm().Copy()
	owned.Path.Absolute = filepath.Join(t.TempDir(), "prepared.dmm")
	owned.SetMapSize(100, 100, 1)
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
	expected := prepared.Hash
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
	renderer.ProcessLevelBuild()
	if !renderer.LevelLoading() {
		t.Fatal("initial graphics ignored chunk quota")
	}
	for steps := 0; renderer.LevelLoading() && steps < 100; steps++ {
		renderer.ProcessLevelBuild()
	}
	if renderer.LevelLoading() {
		t.Fatal("geometry did not finish")
	}
	if renderer.PickAt(16, 16, 1, func(i *dmminstance.Instance) bool { return i.Prefab().Path() == "/obj/foo" }) == nil {
		t.Fatal("prepared geometry did not contain source object")
	}
	after, err := ws.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterHash, _ := after.Hash()
	if afterHash != expected {
		t.Fatal("graphics preparation changed authority")
	}
}
