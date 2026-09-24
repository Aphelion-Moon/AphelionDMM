// Package mapopen prepares map contents without publishing a partial document.
package mapopen

import (
	"context"
	"fmt"
	"path/filepath"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
	"time"
)

// Builder's InternStep runs on the prefab cache owner. Build then transfers
// exclusive ownership to a worker; canonical prefabs and the DME are read-only.
type Builder struct {
	data        *dmmdata.DmmData
	environment *dmenv.Dme
	backup      string
	keys        []dmmdata.Key
	key, member int
	unknown     map[string]*dmmprefab.Prefab
}

func NewBuilder(environment *dmenv.Dme, data *dmmdata.DmmData, backup string) *Builder {
	b := &Builder{environment: environment, data: data, backup: backup, unknown: make(map[string]*dmmprefab.Prefab)}
	for key := range data.Dictionary {
		b.keys = append(b.keys, key)
	}
	return b
}
func (b *Builder) InternStep(limit int, deadline time.Time) bool {
	for count := 0; count < limit && b.key < len(b.keys); count++ {
		if count%32 == 0 && time.Now().After(deadline) {
			break
		}
		prefabs := b.data.Dictionary[b.keys[b.key]]
		if b.member == len(prefabs) {
			b.key++
			b.member = 0
			continue
		}
		prefab := prefabs[b.member]
		if object := b.environment.Objects[prefab.Path()]; object != nil {
			if !prefab.Vars().HasParent() {
				prefab.Vars().LinkParent(object.Vars)
			}
		} else {
			b.unknown[prefab.Path()] = prefab
		}
		prefabs[b.member] = dmmap.PrefabStorage.Put(prefab)
		b.member++
	}
	return b.key == len(b.keys)
}
func (b *Builder) Build(ctx context.Context) (*dmmap.Dmm, map[string]*dmmprefab.Prefab, error) {
	if b.key != len(b.keys) {
		return nil, nil, fmt.Errorf("map prefabs are not prepared")
	}
	readable, err := filepath.Rel(b.environment.RootDir, b.data.Filepath)
	if err != nil {
		readable = b.data.Filepath
	}
	d := &dmmap.Dmm{Name: filepath.Base(b.data.Filepath), Path: dmmap.DmmPath{Absolute: b.data.Filepath, Readable: readable}, MaxX: b.data.MaxX, MaxY: b.data.MaxY, MaxZ: b.data.MaxZ, Backup: b.backup, DiskState: b.data.DiskState}
	cells, err := (model.Snapshot{MaxX: d.MaxX, MaxY: d.MaxY, MaxZ: d.MaxZ}).CellCount()
	if err != nil {
		return nil, nil, err
	}
	d.Tiles = make([]*dmmap.Tile, cells)
	for n := range d.Tiles {
		if n%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		point := util.Point{X: n%d.MaxX + 1, Y: n/d.MaxX%d.MaxY + 1, Z: n/(d.MaxX*d.MaxY) + 1}
		tile := &dmmap.Tile{Coord: point}
		for _, prefab := range b.data.Dictionary[b.data.Grid[point]] {
			tile.InstancesAdd(prefab)
		}
		d.Tiles[n] = tile
	}
	return d, b.unknown, nil
}

// EstimateBytes includes display, compatibility copy, authoritative copies and
// preparation indexes. Source parser bytes already belong to this open request.
func (b *Builder) EstimateBytes() uint64 {
	sizes := make(map[dmmdata.Key]uint64, len(b.data.Dictionary))
	for key, prefabs := range b.data.Dictionary {
		size := uint64(1024)
		for _, prefab := range prefabs {
			size += 1024
			for _, name := range prefab.Vars().Iterate() {
				value, _ := prefab.Vars().Value(name)
				size += uint64(256 + 8*(len(name)+len(value)))
			}
		}
		sizes[key] = size
	}
	bytes := uint64(0)
	for _, key := range b.data.Grid {
		bytes += sizes[key]
	}
	return bytes
}
