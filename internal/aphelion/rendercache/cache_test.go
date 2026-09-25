package rendercache

import (
	"math"
	"sdmm/internal/app/render/bucket/level/chunk"
	"testing"
)

func TestCacheRebuildsForChunkPolicyAndAppearanceChanges(t *testing.T) {
	key := Key{Chunk: chunk.New(1, 1, 1, 1, 32), Layer: LayerKey(2.5)}
	base := Versions{Chunk: 3, Policy: 7, Appearance: 11}
	for _, changed := range []Versions{
		{Chunk: base.Chunk + 1, Policy: base.Policy, Appearance: base.Appearance},
		{Chunk: base.Chunk, Policy: base.Policy + 1, Appearance: base.Appearance},
		{Chunk: base.Chunk, Policy: base.Policy, Appearance: base.Appearance + 1},
	} {
		cache := New()
		if !cache.Put(key, base, nil) {
			t.Fatal("empty retained entry rejected")
		}
		if _, ok := cache.Get(key, changed); ok {
			t.Fatalf("stale entry matched %+v", changed)
		}
	}
}

func TestCacheBoundsSelectionIndexAndIncompleteFallback(t *testing.T) {
	cache := New()
	key := Key{Chunk: chunk.New(1, 1, 1, 1, 32), Layer: LayerKey(1)}
	ids := make([]uint64, MaxIndexedUnitsPerEntry+1)
	if cache.PutWithUnitIDs(key, Versions{}, nil, ids, true) {
		t.Fatal("oversized selection index was retained")
	}
	if cache.Len() != 0 || cache.Bytes() != 0 {
		t.Fatal("rejected selection index consumed cache capacity")
	}
	if !cache.PutWithUnitIDs(key, Versions{}, nil, nil, false) {
		t.Fatal("incomplete bounded index entry was rejected")
	}
	entry, ok := cache.Get(key, Versions{})
	if !ok || !entry.IntersectsUnitIDs(map[uint64]struct{}{42: {}}) {
		t.Fatal("incomplete selection index did not conservatively request streaming")
	}
}

func TestCacheBoundsEntriesAndLayerKeys(t *testing.T) {
	cache := New()
	var first, last Key
	for i := 0; i <= maxEntries; i++ {
		c := chunk.New(float32(i+1), 1, float32(i+1), 1, 32)
		key := Key{Chunk: c, Layer: 1}
		if i == 0 {
			first = key
		}
		last = key
		if !cache.Put(key, Versions{}, nil) {
			t.Fatalf("entry %d rejected", i)
		}
	}
	if cache.Len() != maxEntries {
		t.Fatalf("entries=%d, want %d", cache.Len(), maxEntries)
	}
	if _, ok := cache.Get(first, Versions{}); ok {
		t.Fatal("oldest entry survived eviction")
	}
	if _, ok := cache.Get(last, Versions{}); !ok {
		t.Fatal("newest entry was evicted")
	}
	if LayerKey(float32(math.NaN())) != LayerKey(float32(math.NaN())) {
		t.Fatal("NaN layer key is unstable")
	}
	cache.Clear()
	cache.DisposeRetired()
	if cache.Len() != 0 || cache.Bytes() != 0 {
		t.Fatal("clear retained cache bookkeeping")
	}
}
