package render

import (
	"sdmm/internal/app/render/bucket"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestLevelBuildSchedulesAllZAndPreemptsWithoutLosingProgress(t *testing.T) {
	dmm := levelBuildTestMap(49, 25, 3)
	r := &Render{Camera: newCamera(), bucket: bucket.New()}
	r.SetActiveLevel(dmm, 1)
	if len(r.levelBuilds) != 3 {
		t.Fatalf("queued levels=%d, want 3", len(r.levelBuilds))
	}
	for z := 1; z <= dmm.MaxZ; z++ {
		if r.LevelReady(z) {
			t.Fatalf("Z=%d ready before build", z)
		}
	}
	step := func() {
		t.Helper()
		b := &LevelBuildBudget{deadline: time.Now().Add(time.Second), remaining: 1}
		if !r.ProcessLevelBuildBudget(b) {
			t.Fatal("scheduler did not advance")
		}
	}
	step()
	step()
	if r.levelBuilds[1].next != 1 || r.LevelReady(1) {
		t.Fatal("active-Z progress was not retained")
	}
	r.SetActiveLevel(dmm, 3)
	step()
	step()
	if r.levelBuilds[1].next != 1 || r.levelBuilds[3].next != 1 {
		t.Fatal("priority switch discarded progress or failed to preempt")
	}
	if r.PickAt(0, 0, 3, nil) != nil {
		t.Fatal("partial Z geometry was pickable")
	}
	for steps := 0; len(r.levelBuilds) > 0 && steps < 100; steps++ {
		b := NewLevelBuildBudget()
		for b.Available() && r.ProcessLevelBuildBudget(b) {
		}
	}
	if len(r.levelBuilds) != 0 {
		t.Fatalf("unfinished levels: %v", r.levelBuilds)
	}
	for z := 1; z <= dmm.MaxZ; z++ {
		if !r.LevelReady(z) {
			t.Fatalf("Z=%d never became ready", z)
		}
	}
	generation := r.levelBuildGeneration
	r.InvalidateLevelBuilds(dmm)
	if r.levelBuildGeneration == generation || r.LevelReady(1) || r.bucket.Level(3) != nil {
		t.Fatal("same-pointer snapshot replacement retained stale geometry")
	}
}
func levelBuildTestMap(x, y, z int) *dmmap.Dmm {
	d := &dmmap.Dmm{MaxX: x, MaxY: y, MaxZ: z, Tiles: make([]*dmmap.Tile, x*y*z)}
	for iz := 1; iz <= z; iz++ {
		for iy := 1; iy <= y; iy++ {
			for ix := 1; ix <= x; ix++ {
				i := x*y*(iz-1) + x*(iy-1) + ix - 1
				d.Tiles[i] = &dmmap.Tile{Coord: util.Point{X: ix, Y: iy, Z: iz}}
			}
		}
	}
	return d
}
