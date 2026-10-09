package editor

import (
	"context"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
)

func rfState(prefabs ...model.PrefabState) model.TileState { return model.TileState{Prefabs: prefabs} }

func rfPrefab(id, path string, kv ...string) model.PrefabState {
	p := model.PrefabState{StableID: model.StableID(id), Path: path}
	if len(kv) != 0 {
		p.Vars = map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			p.Vars[kv[i]] = kv[i+1]
		}
	}
	return p
}

func TestLintRandomFillAppliesFillSemanticsToComposedTiles(t *testing.T) {
	activateLint(t)
	rs, file := (&Editor{app: &editorTestApp{}, dmm: &dmmap.Dmm{}}).lintRules()
	window4 := func(id string) model.PrefabState { return rfPrefab(id, "/obj/structure/window", "dir", "4") }
	coord := func(x int) model.Coord { return model.Coord{X: x, Y: 1, Z: 1} }
	changes := []model.TileChange{
		// Appended identical window: skipped.
		{Coord: coord(1), Before: rfState(window4("a")), After: rfState(window4("a"), window4("b"))},
		// Appended second table: warned but kept, with a Replace candidate.
		{Coord: coord(2), Before: rfState(rfPrefab("t1", "/obj/structure/table")), After: rfState(rfPrefab("t1", "/obj/structure/table"), rfPrefab("t2", "/obj/structure/table/wood"))},
		// Clean tile: kept.
		{Coord: coord(3), After: rfState(rfPrefab("c", "/obj/structure/window", "dir", "8"))},
		// Identities missing (fresh payload data): the duplicate is still found.
		{Coord: coord(4), Before: rfState(window4("")), After: rfState(window4(""), window4(""))},
		// The composed state replaced the old window with the new one: no duplicate.
		{Coord: coord(5), Before: rfState(window4("old")), After: rfState(window4("new"))},
	}
	kept, stats, err := lintRandomFill(context.Background(), rs, file, changes)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, c := range kept {
		got = append(got, c.Coord.X)
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 5 {
		t.Fatalf("kept tiles = %v", got)
	}
	if stats.skipped != 2 || stats.skippedPath != "/obj/structure/window" || stats.warned != 1 || stats.last.X != 2 || !stats.canReplace || stats.paste {
		t.Fatalf("stats = %+v", stats)
	}
	message := stats.report().Message()
	if !strings.Contains(message, "Placed with a lint warning at 2,1,1") || !strings.Contains(message, "Skipped 2 tiles") {
		t.Fatalf("message = %q", message)
	}
	if changes[0].After.Prefabs[1].StableID != "b" {
		t.Fatal("input changes were modified")
	}
}

func TestLintRandomFillStopsOnCancel(t *testing.T) {
	activateLint(t)
	rs, file := (&Editor{app: &editorTestApp{}, dmm: &dmmap.Dmm{}}).lintRules()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	changes := []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, After: rfState(rfPrefab("a", "/obj/structure/table"))}}
	if _, _, err := lintRandomFill(ctx, rs, file, changes); err == nil {
		t.Fatal("cancelled lint completed")
	}
}

func TestLintRandomFillWithoutRulesKeepsEverything(t *testing.T) {
	changes := []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, After: rfState(rfPrefab("a", "/obj/structure/table"))}}
	kept, stats, err := lintRandomFill(context.Background(), nil, "", changes)
	if err != nil || len(kept) != 1 || !stats.report().Empty() {
		t.Fatalf("kept=%d stats=%+v err=%v", len(kept), stats, err)
	}
}

// The real window_spawner.yml rule (regex lookahead) is reported by the Map
// Lint scan once the lookahead subset is supported.
func TestLintScanReportsWindowSpawnerViolations(t *testing.T) {
	const spawnerRule = `help: "Window spawners should not overlap other structures."
/obj/effect/spawner/structure/window:
  banned_neighbors:
    /turf/closed:
    STRUCTURE:
      pattern: ^/obj/structure/(?!.*/directional).*$
    STRUCTURE_SPAWNER:
      pattern: ^/obj/effect/spawner/structure/(?!.*/hollow)(?!.*/directional).*$
  ignore:
    - /obj/structure/cable
`
	g := activateLintFiles(t, map[string]string{"window_spawner.yml": spawnerRule})
	if s := g.Status(); len(s.Unsupported) != 0 || s.RuleCount != 1 {
		t.Fatalf("status = %+v", s)
	}

	e := lintEditor(t, lintPrefab("/obj/effect/spawner/structure/window"), lintPrefab("/obj/structure/window"))
	run, err := e.PrepareLintScan(100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := run(context.Background())
	if err != nil || len(result.Findings) == 0 {
		t.Fatalf("scan = %+v err=%v", result, err)
	}
	for _, f := range result.Findings {
		if f.Rule != "window_spawner.yml" || !strings.Contains(f.Message, "/obj/structure/window") {
			t.Fatalf("finding = %+v", f)
		}
	}

	// A directional window is exempt through the lookahead.
	clean := lintEditor(t, lintPrefab("/obj/effect/spawner/structure/window"), lintPrefab("/obj/structure/window/directional"))
	run, err = clean.PrepareLintScan(100)
	if err != nil {
		t.Fatal(err)
	}
	if result, err = run(context.Background()); err != nil || len(result.Findings) != 0 {
		t.Fatalf("directional window flagged: %+v err=%v", result, err)
	}
}
