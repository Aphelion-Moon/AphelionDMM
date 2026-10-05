package repath

import (
	"context"
	"maps"
	"slices"
	"strings"

	"sdmm/internal/aphelion/collab/model"
)

const (
	maxVariants = 64
	maxSamples  = 8
)

// Variant is one distinct set of map-edited variables on an unknown path.
type Variant struct {
	Key    string // canonical "name=value" lines sorted by name
	Vars   map[string]string
	Count  int
	Sample model.Coord
}

type Entry struct {
	Path     string
	Count    int // instances
	Tiles    int // tiles holding at least one instance
	Variants []Variant
	Overflow int // instances whose variant exceeded maxVariants
	Samples  []model.Coord
}

// Inventory lists every unknown path of one accepted revision.
type Inventory struct {
	DocumentID model.DocumentID
	Revision   model.Revision
	Entries    []Entry
	Instances  int
	Tiles      int
}

func VariantKey(vars map[string]string) string {
	names := slices.Sorted(maps.Keys(vars))
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(vars[name])
		b.WriteByte('\n')
	}
	return b.String()
}

// BuildInventory scans the revision in z, y, x order. read must return a
// caller-owned tile; known reports paths defined by the loaded environment.
func BuildInventory(ctx context.Context, header model.Snapshot, read func(model.Coord) (model.TileState, bool), known func(string) bool) (Inventory, error) {
	inventory := Inventory{DocumentID: header.DocumentID, Revision: header.Revision}
	entries := make(map[string]*Entry)
	variants := make(map[string]map[string]int)
	for z := 1; z <= header.MaxZ; z++ {
		for y := 1; y <= header.MaxY; y++ {
			if err := ctx.Err(); err != nil {
				return Inventory{}, err
			}
			for x := 1; x <= header.MaxX; x++ {
				coord := model.Coord{X: x, Y: y, Z: z}
				tile, ok := read(coord)
				if !ok {
					continue
				}
				var counted map[string]bool
				unknownTile := false
				for _, prefab := range tile.Prefabs {
					if known(prefab.Path) {
						continue
					}
					if counted == nil {
						counted = map[string]bool{}
					}
					unknownTile = true
					inventory.Instances++
					entry := entries[prefab.Path]
					if entry == nil {
						entry = &Entry{Path: prefab.Path}
						entries[prefab.Path] = entry
						variants[prefab.Path] = map[string]int{}
					}
					entry.Count++
					if !counted[prefab.Path] {
						counted[prefab.Path] = true
						entry.Tiles++
						if len(entry.Samples) < maxSamples {
							entry.Samples = append(entry.Samples, coord)
						}
					}
					key := VariantKey(prefab.Vars)
					if at, ok := variants[prefab.Path][key]; ok {
						entry.Variants[at].Count++
					} else if len(entry.Variants) < maxVariants {
						variants[prefab.Path][key] = len(entry.Variants)
						entry.Variants = append(entry.Variants, Variant{Key: key, Vars: maps.Clone(prefab.Vars), Count: 1, Sample: coord})
					} else {
						entry.Overflow++
					}
				}
				if unknownTile {
					inventory.Tiles++
				}
			}
		}
	}
	for _, path := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[path]
		slices.SortStableFunc(entry.Variants, func(a, b Variant) int {
			if a.Count != b.Count {
				return b.Count - a.Count
			}
			return strings.Compare(a.Key, b.Key)
		})
		inventory.Entries = append(inventory.Entries, *entry)
	}
	return inventory, nil
}

// Paths lists the inventory's unknown paths in order.
func (inv Inventory) Paths() []string {
	paths := make([]string, len(inv.Entries))
	for n, entry := range inv.Entries {
		paths[n] = entry.Path
	}
	return paths
}
