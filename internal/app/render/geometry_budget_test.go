package render

import (
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render/bucket"
	"testing"
)

func TestGeometryPressureEvictsOnlyCacheAndRequeues(t *testing.T) {
	previous, owners := geometryCapacity, geometryOwners
	geometryCapacity = resources.NewFixedBudget(2000)
	geometryOwners = map[*Render]struct{}{}
	m := levelBuildTestMap(1, 1, 3)
	r := &Render{Camera: newCamera(), bucket: bucket.New()}
	t.Cleanup(func() { r.CancelLevelBuilds(); geometryCapacity, geometryOwners = previous, owners })
	r.SetActiveLevel(m, 1)
	for n := 0; n < 5 && !r.LevelReady(1); n++ {
		r.ProcessLevelBuild()
	}
	if !r.LevelReady(1) {
		t.Fatal("first level did not prepare")
	}
	r.SetActiveLevel(m, 3)
	for n := 0; n < 5 && !r.LevelReady(3); n++ {
		r.ProcessLevelBuild()
	}
	if !r.LevelReady(3) || r.LevelReady(1) || r.levelBuilds[1] == nil {
		t.Fatal("pressure did not evict and requeue the lower priority cache")
	}
	if len(m.Tiles) != 3 || m.MaxZ != 3 {
		t.Fatal("cache pressure changed parsed map")
	}
	r.SetActiveLevel(m, 1)
	for n := 0; n < 5 && !r.LevelReady(1); n++ {
		r.ProcessLevelBuild()
	}
	if !r.LevelReady(1) {
		t.Fatal("evicted level did not rebuild automatically")
	}
	r.CancelLevelBuilds()
	if geometryCapacity.Used() != 0 {
		t.Fatal("closed renderer retained geometry reservation")
	}
}
