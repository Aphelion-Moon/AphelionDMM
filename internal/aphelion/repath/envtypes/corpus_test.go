package envtypes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/envsnapshot"
	"sdmm/internal/aphelion/repath"
	"sdmm/internal/aphelion/repath/updatepaths"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

func loadEnvironment(t *testing.T, path string) *repath.Index {
	t.Helper()
	env, err := dmenv.NewWithOptions(context.Background(), path, envsnapshot.Options{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	index, err := repath.NewIndex(context.Background(), New(env))
	if err != nil {
		t.Fatal(err)
	}
	return index
}

// TestRealMapMigration runs the whole pipeline against real content:
// APHELION_REPATH_DME (target environment), APHELION_REPATH_DMM (map), and
// optionally APHELION_REPATH_REFERENCE (source environment). Codebase
// UpdatePaths scripts are read from the target's tools/UpdatePaths/Scripts.
func TestRealMapMigration(t *testing.T) {
	dme, dmm := os.Getenv("APHELION_REPATH_DME"), os.Getenv("APHELION_REPATH_DMM")
	if dme == "" || dmm == "" {
		t.Skip("set APHELION_REPATH_DME and APHELION_REPATH_DMM")
	}
	ctx := context.Background()
	target := loadEnvironment(t, dme)
	sources := repath.Sources{Target: target}
	if reference := os.Getenv("APHELION_REPATH_REFERENCE"); reference != "" {
		sources.Reference = loadEnvironment(t, reference)
	}
	dir := filepath.Join(filepath.Dir(dme), "tools", "UpdatePaths", "Scripts")
	if entries, err := os.ReadDir(dir); err == nil {
		var scripts []updatepaths.Script
		for _, entry := range entries {
			if data, err := os.ReadFile(filepath.Join(dir, entry.Name())); err == nil && strings.HasSuffix(entry.Name(), ".txt") {
				script, _ := updatepaths.Parse(entry.Name(), data)
				scripts = append(scripts, script)
			}
		}
		sources.Scripts = updatepaths.NewSet(scripts...)
	}

	data, err := dmmdata.New(dmm)
	if err != nil {
		t.Fatal(err)
	}
	tiles := map[model.Coord]model.TileState{}
	for point, key := range data.Grid {
		var state model.TileState
		for _, prefab := range data.Dictionary[key] {
			vars := map[string]string{}
			for _, name := range prefab.Vars().Iterate() {
				vars[name], _ = prefab.Vars().ExplicitValue(name)
			}
			id, err := model.NewStableID()
			if err != nil {
				t.Fatal(err)
			}
			state.Prefabs = append(state.Prefabs, model.PrefabState{StableID: id, Path: prefab.Path(), Vars: vars})
		}
		tiles[model.Coord{X: point.X, Y: point.Y, Z: point.Z}] = state
	}
	read := func(coord model.Coord) (model.TileState, bool) {
		state, ok := tiles[coord]
		return state, ok
	}
	header := model.Snapshot{MaxX: data.MaxX, MaxY: data.MaxY, MaxZ: data.MaxZ}
	inventory, err := repath.BuildInventory(ctx, header, read, target.Exists)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := repath.Suggest(ctx, inventory, sources, repath.AutoHigh)
	if err != nil {
		t.Fatal(err)
	}
	tiers := map[string]int{}
	auto := 0
	var lines []string
	for _, proposal := range proposals {
		best := "none"
		if len(proposal.Candidates) != 0 {
			candidate := proposal.Candidates[0]
			best = fmt.Sprintf("%-7s %.2f %-14s %s %v", candidate.Tier, candidate.Score, candidate.Generator, candidate.Target, candidate.Reasons)
			tiers[candidate.Tier.String()]++
		} else {
			tiers["none"]++
		}
		if proposal.Auto >= 0 {
			auto++
		}
		lines = append(lines, fmt.Sprintf("%4d %s\n     -> %s", proposal.Entry.Count, proposal.Entry.Path, best))
	}
	sort.Strings(lines)
	for _, line := range lines {
		t.Log(line)
	}
	t.Logf("%d unknown types, %d instances; best tiers %v; %d selected automatically (Certain and High)", len(inventory.Entries), inventory.Instances, tiers, auto)

	plan := repath.DefaultPlan(proposals)
	transformer, err := repath.Compile(plan, target, inventory.Paths(), repath.Resolvers{Scripts: sources.Scripts})
	if err != nil {
		t.Fatal(err)
	}
	visit := func(visit func(model.Coord) bool) {
		for z := 1; z <= header.MaxZ; z++ {
			for y := 1; y <= header.MaxY; y++ {
				for x := 1; x <= header.MaxX; x++ {
					if !visit(model.Coord{X: x, Y: y, Z: z}) {
						return
					}
				}
			}
		}
	}
	changes, report, err := repath.BuildChanges(ctx, visit, read, transformer, model.NewStableID, repath.Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("automatic plan: %d instances rewritten on %d tiles (%d changes), unresolved %v", report.Instances(), report.Tiles, len(changes), report.Unresolved)
}
