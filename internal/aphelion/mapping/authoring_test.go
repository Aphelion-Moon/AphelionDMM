package mapping

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
	"strings"
	"testing"
)

type cancelBetweenFiles struct {
	context.Context
	checks int
}

func (c *cancelBetweenFiles) Err() error {
	c.checks++
	if c.checks >= 2 {
		return context.Canceled
	}
	return nil
}

func TestOverlayRecipePartialWriteRecoveryPreservesExternalConfig(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "overlays.toml")
	initial := []byte("# retained owner comment\n")
	if err := os.WriteFile(config, initial, 0600); err != nil {
		t.Fatal(err)
	}
	id, _ := model.NewStableID()
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: id, Path: "/turf/floor", Vars: map[string]string{}}}}}}}
	p, err := PrepareTemplateExport(context.Background(), filepath.Join(root, "overlay.dmm"), snapshot, editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.AddOverlayRecipe(root, "overlays.toml", "test", "base.dmm", "Station", util.Point{X: 2, Y: 3, Z: 1}); err != nil {
		t.Fatal(err)
	}
	r := p.Apply(&cancelBetweenFiles{Context: context.Background()})
	if !r.SourceWritten || r.ConfigWritten || !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("partial completion lost: %+v", r)
	}
	if err := os.WriteFile(config, append(initial, []byte("# external addition\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if r := p.Apply(context.Background()); r.Err == nil || r.ConfigWritten {
		t.Fatal("conflicting configuration overwritten")
	}
	if err := p.RestageConfiguration(); err != nil {
		t.Fatal(err)
	}
	if r := p.Apply(context.Background()); r.Err != nil || !r.SourceWritten || !r.ConfigWritten {
		t.Fatalf("reviewed recovery failed: %+v", r)
	}
	data, _ := os.ReadFile(config)
	if !strings.Contains(string(data), "# external addition") || !strings.Contains(string(data), "[templates.\"test\"]") {
		t.Fatal("recovery lost unrelated config")
	}
}

func TestTemplateExportPreservesSourceAndRejectsAppearingTarget(t *testing.T) {
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), MaxX: 3, MaxY: 1, MaxZ: 1}
	for x := 1; x <= 3; x++ {
		id, _ := model.NewStableID()
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: x, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: id, Path: "/obj/unknown", Vars: map[string]string{"v": "1"}}}}})
	}
	selection, err := editing.MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := snapshot.Hash()
	path := filepath.Join(t.TempDir(), "module.dmm")
	proposal, err := PrepareTemplateExport(context.Background(), path, snapshot, selection, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proposal.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("preparation wrote target")
	}
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := proposal.Apply(context.Background()); result.Err == nil || result.SourceWritten {
		t.Fatal("appearing target overwritten")
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "external" {
		t.Fatal("conflict changed target")
	}
	path = filepath.Join(t.TempDir(), "export.dmm")
	proposal2, err := PrepareTemplateExport(context.Background(), path, snapshot, selection, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proposal2.Close()
	if result := proposal2.Apply(context.Background()); result.Err != nil || !result.SourceWritten {
		t.Fatal(result)
	}
	data, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	hole := data.Dictionary[data.Grid[util.Point{X: 2, Y: 1, Z: 1}]]
	if len(hole) != 2 || hole[0].Path() != "/turf/template_noop" || hole[1].Path() != "/area/template_noop" {
		t.Fatal("mask hole did not remain independent noops")
	}
	after, _ := snapshot.Hash()
	if before != after {
		t.Fatal("export changed source authority")
	}
}

func TestMinimalModulePatchPreservesCommentsAndOtherKeys(t *testing.T) {
	input := []byte("# owner comment\r\ndirectory = \"maps\"\r\n[rooms.\"one\"] # header\r\nmodules = [\r\n \"old.dmm\" # preserve comment\r\n]\r\n[rooms.two]\r\nmodules=[\"other.dmm\"]\r\n")
	output, err := appendModuleSlot(input, "one", "new.dmm")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "# preserve comment\r\n") || !strings.HasSuffix(string(output), "[rooms.two]\r\nmodules=[\"other.dmm\"]\r\n") {
		t.Fatal("unrelated text changed")
	}
	if !strings.Contains(string(output), "\"new.dmm\"") {
		t.Fatal("new slot absent")
	}
	if _, err := appendModuleSlot(input, "missing", "new.dmm"); err == nil {
		t.Fatal("missing table silently created")
	}
}
