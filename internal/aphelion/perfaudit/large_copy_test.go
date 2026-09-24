package perfaudit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

// Uses the native DMM parser and exact raw prefab values. Environment resolution
// and viewport drawing are separate gates; this workload needs no private DME.
func TestRepresentativeWholeLevelCopy(t *testing.T) {
	path := os.Getenv("APHELION_AUDIT_MAP")
	if path == "" {
		t.Skip("set APHELION_AUDIT_MAP to a representative multi-Z map")
	}
	originalHash := auditFileHash(t, path)
	defer func() {
		if auditFileHash(t, path) != originalHash {
			t.Error("source map changed")
		}
	}()
	started := time.Now()
	stage := func(name string) {
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		t.Logf("stage=%s elapsed_ms=%d live_heap_bytes=%d cumulative_alloc_bytes=%d", name, time.Since(started).Milliseconds(), memory.HeapAlloc, memory.TotalAlloc)
		started = time.Now()
	}
	data, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if data.MaxZ < 2 {
		t.Fatal("representative whole-level workload requires multiple Z levels")
	}
	t.Logf("source_sha256=%s dimensions=%dx%dx%d", originalHash, data.MaxX, data.MaxY, data.MaxZ)
	stage("native_parse")
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	source := &dmmap.Dmm{MaxX: data.MaxX, MaxY: data.MaxY, MaxZ: data.MaxZ}
	var clipboard []dmmap.Tile
	serial := 0
	for z := 1; z <= data.MaxZ; z++ {
		for y := 1; y <= data.MaxY; y++ {
			for x := 1; x <= data.MaxX; x++ {
				coord := util.Point{X: x, Y: y, Z: z}
				tile := &dmmap.Tile{Coord: coord}
				for _, prefab := range data.Dictionary[data.Grid[coord]] {
					tile.InstancesAdd(prefab)
				}
				for _, instance := range tile.Instances() {
					serial++
					instance.SetStableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", serial))
				}
				source.Tiles = append(source.Tiles, tile)
				if z == 1 {
					clipboard = append(clipboard, tile.Copy())
				}
			}
		}
	}
	snapshot, err := mapadapter.Import(source, "01890f3e-7b5c-7abc-8def-0123456789ab", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	baseHash, _ := snapshot.Hash()
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ba")
	local, err := executor.NewLocal(document, actor)
	if err != nil {
		t.Fatal(err)
	}
	stage("import")
	ctx := context.Background()
	op, err := editing.BuildPlacementProposal(ctx, snapshot, actor, clipboard, func(string) bool { return true }, util.Point{X: 1, Y: 1, Z: data.MaxZ}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(op.Changes) != data.MaxX*data.MaxY {
		t.Fatal("whole-level proposal was truncated")
	}
	stage("proposal")
	accepted, err := local.Execute(ctx, op)
	if err != nil || accepted.Revision != 1 {
		t.Fatalf("single atomic execute: %v", err)
	}
	current, err := local.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stage("apply")
	// Construct expected values directly from parsed input, independent of the
	// proposal, operation engine, and save key allocator.
	expected := *data
	expected.Grid = make(map[util.Point]dmmdata.Key, len(data.Grid))
	for coord, key := range data.Grid {
		expected.Grid[coord] = key
	}
	for y := 1; y <= data.MaxY; y++ {
		for x := 1; x <= data.MaxX; x++ {
			expected.Grid[util.Point{X: x, Y: y, Z: data.MaxZ}] = data.Grid[util.Point{X: x, Y: y, Z: 1}]
		}
	}
	outputDir := t.TempDir()
	if root := os.Getenv("APHELION_AUDIT_OUTPUT"); root != "" {
		outputDir, err = os.MkdirTemp(root, "large-copy-")
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tgm := range []bool{false, true} {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("copied-tgm-%t.dmm", tgm))
		output, err := mapadapter.Export(current, outputPath, tgm, data.LineBreak)
		if err != nil {
			t.Fatal(err)
		}
		if err := output.Save(); err != nil {
			t.Fatal(err)
		}
		reparsed, err := dmmdata.New(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		assertRepresentativeMapValues(t, &expected, reparsed)
		stage(fmt.Sprintf("save_reparse_tgm_%t", tgm))
	}
	inverse, err := local.BuildInverse(ctx, op.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted, err := local.Execute(ctx, inverse); err != nil || accepted.Revision != 2 {
		t.Fatalf("whole-action undo: %v", err)
	}
	restored, err := local.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restoredHash, err := restored.Hash()
	if err != nil || restoredHash != baseHash {
		t.Fatal("undo did not exactly restore all original levels")
	}
	stage("undo")
}
