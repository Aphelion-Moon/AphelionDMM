package render

import (
	"sdmm/internal/aphelion/rendercache"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/render/bucket"
	"testing"
)

func TestGeometryEvictionPreservesOtherLevelSubmissionsAndPreparation(t *testing.T) {
	m := levelBuildTestMap(1, 1, 2)
	r := &Render{Camera: newCamera(), bucket: bucket.New(), retained: rendercache.New()}
	keys := make([]rendercache.Key, 2)
	for z := 1; z <= 2; z++ {
		r.bucket.UpdateLevel(m, z, nil)
		for _, c := range r.bucket.Level(z).Chunks {
			keys[z-1] = rendercache.Key{Chunk: c}
		}
		r.retained.Put(keys[z-1], rendercache.Versions{}, &brush.Submission{})
		r.queueRetainedPreparation(keys[z-1], rendercache.Versions{}, 0)
	}
	r.evictGeometry(1)
	if _, found := r.retained.Get(keys[1], rendercache.Versions{}); !found {
		t.Fatal("evicting one level discarded another level's retained submission")
	}
	if _, found := r.retained.Get(keys[0], rendercache.Versions{}); found || !r.retained.HasRetired() {
		t.Fatal("evicted level's submission was not retired")
	}
	if len(r.retainedPending) != 1 || r.retainedPending[0].key != keys[1] || len(r.retainedQueued) != 1 {
		t.Fatal("eviction did not preserve only the other level's queued preparation")
	}
	if r.bucket.Level(1) != nil || r.bucket.Level(2) == nil {
		t.Fatal("wrong level geometry was dropped")
	}
}

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
