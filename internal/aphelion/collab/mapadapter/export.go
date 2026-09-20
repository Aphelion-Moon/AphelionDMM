package mapadapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sort"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const keyAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func Apply(target *dmmap.Dmm, snapshot model.Snapshot) error {
	return apply(target, snapshot, nil)
}

func ApplyWithEnvironment(target *dmmap.Dmm, snapshot model.Snapshot, environment *dmenv.Dme) error {
	defer uistage.Begin(uistage.ProjectionApply).End()
	return apply(target, snapshot, environment)
}

func apply(target *dmmap.Dmm, snapshot model.Snapshot, environment *dmenv.Dme) error {
	if target == nil {
		return fmt.Errorf("apply map: target is nil")
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("apply map: validate snapshot: %w", err)
	}
	cellCount, err := snapshot.CellCount()
	if err != nil {
		return fmt.Errorf("apply map: count snapshot cells: %w", err)
	}
	states := statesByCoord(snapshot)

	tiles := make([]*dmmap.Tile, 0, cellCount)
	for z := 1; z <= snapshot.MaxZ; z++ {
		for y := 1; y <= snapshot.MaxY; y++ {
			for x := 1; x <= snapshot.MaxX; x++ {
				coord := model.Coord{X: x, Y: y, Z: z}
				state := states[coord]
				point := util.Point{X: x, Y: y, Z: z}
				tile := &dmmap.Tile{Coord: point}
				instances := make(dmmap.Instances, len(state.Prefabs))
				for index, prefabState := range state.Prefabs {
					prefab := prefabFromState(prefabState)
					if environment != nil {
						if object, exists := environment.Objects[prefabState.Path]; exists {
							prefab.Vars().LinkParent(object.Vars)
						}
					}
					prefab = dmmap.PrefabStorage.Put(prefab)
					instance := dmminstance.New(point, prefab)
					instance.SetStableID(string(prefabState.StableID))
					instances[index] = instance
				}
				tile.Set(instances)
				tiles = append(tiles, tile)
			}
		}
	}
	target.MaxX = snapshot.MaxX
	target.MaxY = snapshot.MaxY
	target.MaxZ = snapshot.MaxZ
	target.Tiles = tiles
	return nil
}

func Export(snapshot model.Snapshot, path string, isTGM bool, lineBreak string) (*dmmdata.DmmData, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("export map: validate snapshot: %w", err)
	}
	cellCount, err := snapshot.CellCount()
	if err != nil {
		return nil, fmt.Errorf("export map: count snapshot cells: %w", err)
	}
	if lineBreak != "\n" && lineBreak != "\r\n" {
		return nil, fmt.Errorf("export map: unsupported line break %q", lineBreak)
	}
	states := statesByCoord(snapshot)

	uniqueStates := make([]model.TileState, 0)
	stateIndexes := make(map[[sha256.Size]byte]int)
	gridIndexes := make(map[util.Point]int, cellCount)
	for z := 1; z <= snapshot.MaxZ; z++ {
		for y := 1; y <= snapshot.MaxY; y++ {
			for x := 1; x <= snapshot.MaxX; x++ {
				coord := model.Coord{X: x, Y: y, Z: z}
				state := states[coord]
				digest := dmmStateDigest(state)
				index, exists := stateIndexes[digest]
				if !exists {
					index = len(uniqueStates)
					stateIndexes[digest] = index
					uniqueStates = append(uniqueStates, model.CloneTileState(state))
				}
				gridIndexes[util.Point{X: x, Y: y, Z: z}] = index
			}
		}
	}

	keyLength := requiredKeyLength(len(uniqueStates))
	keys := make([]dmmdata.Key, len(uniqueStates))
	dictionary := make(dmmdata.DataDictionary, len(uniqueStates))
	for index, state := range uniqueStates {
		key := dmmdata.Key(keyFor(index, keyLength))
		keys[index] = key
		prefabs := make(dmmdata.Prefabs, len(state.Prefabs))
		for prefabIndex, prefab := range state.Prefabs {
			prefabs[prefabIndex] = prefabFromState(prefab)
		}
		dictionary[key] = prefabs
	}
	grid := make(dmmdata.DataGrid, len(gridIndexes))
	for point, index := range gridIndexes {
		grid[point] = keys[index]
	}
	return &dmmdata.DmmData{
		Filepath:   path,
		IsTgm:      isTGM,
		LineBreak:  lineBreak,
		KeyLength:  keyLength,
		MaxX:       snapshot.MaxX,
		MaxY:       snapshot.MaxY,
		MaxZ:       snapshot.MaxZ,
		Dictionary: dictionary,
		Grid:       grid,
	}, nil
}

func prefabFromState(state model.PrefabState) *dmmprefab.Prefab {
	variables := &dmvars.MutableVariables{}
	keys := make([]string, 0, len(state.Vars))
	for name := range state.Vars {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		variables.Put(name, state.Vars[name])
	}
	return dmmprefab.New(dmmprefab.IdNone, state.Path, variables.ToImmutable())
}

func dmmStateDigest(state model.TileState) [sha256.Size]byte {
	var encoded bytes.Buffer
	writeAdapterUint64(&encoded, uint64(len(state.Prefabs)))
	for _, prefab := range state.Prefabs {
		writeAdapterString(&encoded, prefab.Path)
		keys := make([]string, 0, len(prefab.Vars))
		for name := range prefab.Vars {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		writeAdapterUint64(&encoded, uint64(len(keys)))
		for _, name := range keys {
			writeAdapterString(&encoded, name)
			writeAdapterString(&encoded, prefab.Vars[name])
		}
	}
	return sha256.Sum256(encoded.Bytes())
}

func requiredKeyLength(count int) int {
	length := 1
	capacity := len(keyAlphabet)
	for capacity < count {
		length++
		capacity *= len(keyAlphabet)
	}
	return length
}

func keyFor(index int, length int) string {
	key := make([]byte, length)
	for position := length - 1; position >= 0; position-- {
		key[position] = keyAlphabet[index%len(keyAlphabet)]
		index /= len(keyAlphabet)
	}
	return string(key)
}

func writeAdapterString(buffer *bytes.Buffer, value string) {
	writeAdapterUint64(buffer, uint64(len(value)))
	_, _ = buffer.WriteString(value)
}

func writeAdapterUint64(buffer *bytes.Buffer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = buffer.Write(encoded[:])
}
