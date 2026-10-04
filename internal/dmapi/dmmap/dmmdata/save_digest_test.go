package dmmdata

import (
	"fmt"
	"strings"
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestSemanticDigestCanonicalBytes(t *testing.T) {
	vars := &dmvars.MutableVariables{}
	// Deliberately unsorted names, UTF-8, escaped text, and a value larger than
	// the hash buffer protect the existing length-prefixed canonical stream.
	vars.Put("payload", `"`+strings.Repeat("x", 9000)+`"`)
	vars.Put("b", `"é\n\"x"`)
	vars.Put("a", "null")
	unknown := dmmprefab.New(dmmprefab.IdNone, "/obj/unknown", vars.ToImmutable())
	turf := dmmprefab.New(dmmprefab.IdNone, "/turf/test", (&dmvars.MutableVariables{}).ToImmutable())
	data := DmmData{
		MaxX: 2, MaxY: 1, MaxZ: 2,
		Dictionary: DataDictionary{"aa": {unknown, turf}, "bb": {turf}, "cc": {}},
		Grid: DataGrid{
			{X: 1, Y: 1, Z: 1}: "aa", {X: 2, Y: 1, Z: 1}: "bb",
			{X: 1, Y: 1, Z: 2}: "aa", {X: 2, Y: 1, Z: 2}: "cc",
		},
	}
	got, err := data.semanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	// Independently encoded with explicit big-endian lengths and UTF-8 bytes.
	const want = "f47a41ba3bd78d34fa6b20df6bd2e84b48a8df34cdeda72810c8bc79892b6762"
	if fmt.Sprintf("%x", got) != want {
		t.Fatalf("semantic digest=%x, want %s", got, want)
	}
	// Repeated short fields exercise cell framing across many buffer boundaries.
	repeated := repeatedDigestMap(128, Prefabs{turf})
	repeatedDigest, err := repeated.semanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	const repeatedWant = "628c75b5c0f53a86753fcf751126e96f225184b7e694d0f1f7696b8f67483fa1"
	if fmt.Sprintf("%x", repeatedDigest) != repeatedWant {
		t.Fatalf("repeated map digest=%x, want %s", repeatedDigest, repeatedWant)
	}
	data.Dictionary["aa"] = Prefabs{turf, unknown}
	if reordered, err := data.semanticDigest(); err != nil || reordered == got {
		t.Fatalf("ordered stack changed without changing digest: %x, %v", reordered, err)
	}
}

func repeatedDigestMap(size int, prefabs Prefabs) DmmData {
	data := DmmData{MaxX: size, MaxY: size, MaxZ: 1, Dictionary: DataDictionary{"a": prefabs}, Grid: make(DataGrid, size*size)}
	for y := 1; y <= size; y++ {
		for x := 1; x <= size; x++ {
			data.Grid[util.Point{X: x, Y: y, Z: 1}] = "a"
		}
	}
	return data
}

func TestSemanticDigestScratchDoesNotGrowWithMap(t *testing.T) {
	prefab := dmmprefab.New(dmmprefab.IdNone, "/turf/test", (&dmvars.MutableVariables{}).ToImmutable())
	small := repeatedDigestMap(1, Prefabs{prefab})
	large := repeatedDigestMap(128, Prefabs{prefab})
	allocations := func(data DmmData) float64 {
		return testing.AllocsPerRun(2, func() {
			if _, err := data.semanticDigest(); err != nil {
				t.Fatal(err)
			}
		})
	}
	baseline, expanded := allocations(small), allocations(large)
	if expanded > baseline {
		t.Fatalf("digest scratch allocations grow with cells: %.0f -> %.0f", baseline, expanded)
	}
}

func BenchmarkSemanticDigest(b *testing.B) {
	data := repeatedDigestMap(255, fixtureData("").Dictionary["a"])
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := data.semanticDigest(); err != nil {
			b.Fatal(err)
		}
	}
}
