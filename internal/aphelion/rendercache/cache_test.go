package rendercache

import (
	"fmt"
	"math"
	"sdmm/internal/app/render/bucket/level/chunk"
	"testing"
)

func TestCacheHighlightIntersection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		units    []uint64
		selected map[uint64]struct{}
		complete bool
		want     bool
	}{
		{name: "no highlights", units: []uint64{3}, complete: true},
		{name: "empty submission", selected: map[uint64]struct{}{3: {}}, complete: true},
		{name: "small highlight hit", units: []uint64{9, 3, 7, 5}, selected: map[uint64]struct{}{5: {}}, complete: true, want: true},
		{name: "small highlight miss", units: []uint64{9, 3, 7, 5}, selected: map[uint64]struct{}{8: {}}, complete: true},
		{name: "large highlight hit", units: []uint64{7}, selected: map[uint64]struct{}{3: {}, 5: {}, 7: {}, 9: {}}, complete: true, want: true},
		{name: "large highlight miss", units: []uint64{8}, selected: map[uint64]struct{}{3: {}, 5: {}, 7: {}, 9: {}}, complete: true},
		{name: "incomplete index", selected: map[uint64]struct{}{3: {}}, want: true},
		{name: "incomplete without highlights"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := New()
			key := Key{}
			if !cache.PutWithUnitIDs(key, Versions{}, nil, tc.units, tc.complete) {
				t.Fatal("entry rejected")
			}
			entry, _ := cache.Get(key, Versions{})
			if got := IntersectsUnitIDs(entry, tc.selected); got != tc.want {
				t.Fatalf("intersection=%t, want %t", got, tc.want)
			}
		})
	}
	var absent *Entry
	if IntersectsUnitIDs(absent, map[uint64]struct{}{3: {}}) {
		t.Fatal("absent entry matched a highlight")
	}
}

func BenchmarkCacheHighlightIntersection(b *testing.B) {
	for _, count := range []int{1, 10000} {
		b.Run(fmt.Sprintf("highlights=%d", count), func(b *testing.B) {
			cache := New()
			units := make([]uint64, 625)
			for i := range units {
				units[i] = uint64(i + 1)
			}
			cache.PutWithUnitIDs(Key{}, Versions{}, nil, units, true)
			entry, _ := cache.Get(Key{}, Versions{})
			ids := make(map[uint64]struct{}, count)
			for i := 0; i < count; i++ {
				ids[uint64(i+10000)] = struct{}{}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if IntersectsUnitIDs(entry, ids) {
					b.Fatal("unrelated highlights matched")
				}
			}
		})
	}
}

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
	if !ok || !IntersectsUnitIDs(entry, map[uint64]struct{}{42: {}}) {
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
