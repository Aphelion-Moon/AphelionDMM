package dmmsave

import (
	"errors"
	// APHELION EDIT ADDITION START - EXPECTED INPUT VALIDATION
	"sdmm/internal/aphelion/mapsave"
	// APHELION EDIT ADDITION END

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmsave/keygen"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

type saveProcess struct {
	cfg        Config
	dme        *dmenv.Dme
	dmm        *dmmap.Dmm
	initial    *dmmdata.DmmData
	output     *dmmdata.DmmData
	keygen     *keygen.KeyGen
	unusedKeys map[dmmdata.Key]bool
	// APHELION EDIT ADDITION START - EXPECTED INPUT VALIDATION
	expected *dmmdata.DmmData
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SAVE_INDEX
	stacks           map[util.Point]mapsave.TileStack
	initialContent   *mapsave.ContentIndex
	outputContent    *mapsave.ContentIndex
	initialLocations map[dmmdata.Key][]util.Point
	// APHELION EDIT ADDITION END
}

func makeSaveProcess(cfg Config, dme *dmenv.Dme, dmm *dmmap.Dmm, path string) (*saveProcess, error) {
	// Copy the dmm to avoid unneeded modifications.
	dmmCopy := dmm.Copy()
	dmm = &dmmCopy

	initial, err := dmmdata.New(dmm.Backup)
	if err != nil {
		log.Print("unable to read map backup:", dmm.Backup)
		return nil, err
	}

	output := &dmmdata.DmmData{
		Filepath:   path,
		IsTgm:      detectIsTgm(cfg.Format, initial.IsTgm),
		LineBreak:  initial.LineBreak,
		KeyLength:  initial.KeyLength,
		MaxX:       dmm.MaxX,
		MaxY:       dmm.MaxY,
		MaxZ:       dmm.MaxZ,
		Dictionary: make(dmmdata.DataDictionary),
		Grid:       make(dmmdata.DataGrid),
	}

	// Collect unused keys in map.
	// Use map instead of slice, because during the first phase (fill with reused keys) it's modified a lot.
	unusedKeys := make(map[dmmdata.Key]bool)
	for _, key := range initial.Keys() {
		unusedKeys[key] = true
	}

	/* APHELION EDIT REMOVAL START - EXPECTED INPUT VALIDATION
	return &saveProcess{
		cfg,
		dme,
		dmm,
		initial,
		output,
		keygen.New(output),
		unusedKeys,
	}, nil
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - EXPECTED INPUT VALIDATION
	sp := &saveProcess{cfg: cfg, dme: dme, dmm: dmm, initial: initial, output: output, keygen: keygen.New(output), unusedKeys: unusedKeys}
	if cfg.SanitizeVariables {
		sp.sanitizeVariables()
	}
	// APHELION EDIT ADDITION START - SAVE_INDEX
	sp.stacks = mapsave.Normalize(dmm)
	sp.initialContent = mapsave.NewContentIndex(initial.Dictionary)
	sp.outputContent = mapsave.NewContentIndex(output.Dictionary)
	sp.initialLocations = mapsave.LocationsByKey(initial)
	// APHELION EDIT ADDITION END
	sp.expected = mapsave.Expected(dmm, output.IsTgm)
	return sp, nil
	// APHELION EDIT ADDITION END
}

func detectIsTgm(saveFormat Format, isInitialTGM bool) bool {
	switch saveFormat {
	case FormatInitial:
		return isInitialTGM
	case FormatTGM:
		return true
	default:
		return false
	}
}

func (sp *saveProcess) sanitizeVariables() {
	log.Print("sanitizing variables...")

	for _, tile := range sp.dmm.Tiles {
		for _, instance := range tile.Instances() {
			prefab := instance.Prefab()
			if prefab.Vars().Len() == 0 {
				continue
			}

			// APHELION EDIT ADDITION START - COLLABORATION
			obj, exists := sp.dme.Objects[prefab.Path()]
			if !exists {
				continue
			}
			// APHELION EDIT ADDITION END
			vars := prefab.Vars()

			for _, varName := range prefab.Vars().Iterate() {
				origValue, _ := obj.Vars.Value(varName)
				prefValue, _ := prefab.Vars().Value(varName)

				if origValue == prefValue {
					log.Print("delete variable:", varName)
					vars = dmvars.Delete(vars, varName)
				}
			}

			if prefab.Vars().Len() != vars.Len() {
				instance.SetPrefab(dmmprefab.New(dmmprefab.IdNone, prefab.Path(), vars))
				log.Printf("instance sanitized: [%d#%s]", instance.Id(), prefab.Path())
			}
		}
	}
}

// Go through the dmm tiles and try to find a key in the initial map with the same content.
func (sp *saveProcess) handleReusedKeys() {
	log.Print("handle reused keys...")

	/* APHELION EDIT REMOVAL START - SAVE_INDEX
	// Cache the initial content, since we know it won't change.
	keyByPrefabs := make(map[uint64]dmmdata.Key, len(sp.initial.Dictionary))
	for key, prefabs := range sp.initial.Dictionary {
		keyByPrefabs[prefabs.Hash()] = key
	}
	for _, tile := range sp.dmm.Tiles {
		prefabs := tile.Instances().Sorted().Prefabs()
		if initialKey, ok := findKeyByTileContent(sp.initial, keyByPrefabs, prefabs); ok {
			sp.setOutputKeyContent(tile.Coord, initialKey, prefabs)
			delete(sp.unusedKeys, initialKey)
		}
	}
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - SAVE_INDEX
	for _, tile := range sp.dmm.Tiles {
		stack := sp.stacks[tile.Coord]
		if initialKey, ok := sp.initialContent.Find(stack.Hash, stack.Prefabs); ok {
			sp.setOutputKeyContent(tile.Coord, initialKey, stack)
			delete(sp.unusedKeys, initialKey)
		}
	}
	// APHELION EDIT ADDITION END

	log.Print("remaining count of unused keys:", len(sp.unusedKeys))
}

// Find all locations without keys and fill them with the content.
func (sp *saveProcess) handleLocationsWithoutKeys() error {
	log.Print("handle locations without keys...")
	log.Print("collecting locations without keys...")

	locsWithoutKey := sp.findLocationsWithoutKey()

	log.Print("count of locations without keys:", len(locsWithoutKey))

	sp.tryToReuseKeysByTheirInitialLocation(locsWithoutKey)

	err := sp.fillLocations(locsWithoutKey)
	if errors.Is(err, errRegenerateKeys) {
		sp.keygen.DropKeysPool()
		sp.output.Dictionary = make(dmmdata.DataDictionary)
		sp.output.Grid = make(dmmdata.DataGrid)
		// APHELION EDIT ADDITION START - SAVE_INDEX
		sp.outputContent = mapsave.NewContentIndex(sp.output.Dictionary)
		// APHELION EDIT ADDITION END
		sp.unusedKeys = nil
		return sp.handleLocationsWithoutKeys()
	} else if errors.Is(err, errKeysLimitExceeded) {
		return errKeysLimitExceeded
	}

	return nil
}

func (sp *saveProcess) findLocationsWithoutKey() map[util.Point]bool {
	locsWithoutKey := make(map[util.Point]bool)

	for z := 1; z <= sp.dmm.MaxZ; z++ {
		for y := 1; y <= sp.dmm.MaxY; y++ {
			for x := 1; x <= sp.dmm.MaxX; x++ {
				loc := util.Point{X: x, Y: y, Z: z}
				if _, ok := sp.output.Grid[loc]; !ok {
					locsWithoutKey[loc] = true
				}
			}
		}
	}

	return locsWithoutKey
}

// Try to find the most appropriate place of all unused keys.
// Appropriate means that the initial map has the same key by the same location.
func (sp *saveProcess) tryToReuseKeysByTheirInitialLocation(locsWithoutKey map[util.Point]bool) {
	if len(sp.unusedKeys) == 0 {
		return
	}

	log.Print("trying to match unused keys with its previous location...")

	/* APHELION EDIT REMOVAL START - SAVE_INDEX
	// Copy to modify the original map safely during its iteration.
	unusedKeysCpy := make(map[dmmdata.Key]bool)
	for key := range sp.unusedKeys {
		unusedKeysCpy[key] = true
	}
	keyByPrefabs := make(map[uint64]dmmdata.Key)
	for unusedKey := range unusedKeysCpy {
		for loc := range locsWithoutKey {
			prefabs := sp.dmm.GetTile(loc).Instances().Sorted().Prefabs()
			prefabsHash := prefabs.Hash()
			if cachedKey, ok := keyByPrefabs[prefabsHash]; ok && prefabs.Equals(sp.output.Dictionary[cachedKey]) {
				sp.output.Grid[loc] = cachedKey
				continue
			}
			if sp.initial.Grid[loc] == unusedKey {
				keyByPrefabs[prefabsHash] = unusedKey
				sp.setOutputKeyContent(loc, unusedKey, prefabs)
				delete(sp.unusedKeys, unusedKey)
				delete(locsWithoutKey, loc)
				break
			}
		}
	}
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - SAVE_INDEX
	// Candidate locations are indexed by their original key, so each unused
	// key examines only its own prior locations instead of every unmatched cell.
	reusedByContent := mapsave.NewContentIndex(nil)
	for _, unusedKey := range sp.initial.Keys() {
		if !sp.unusedKeys[unusedKey] {
			continue
		}
		for _, loc := range sp.initialLocations[unusedKey] {
			if !locsWithoutKey[loc] {
				continue
			}
			stack := sp.stacks[loc]
			if cachedKey, ok := reusedByContent.Find(stack.Hash, stack.Prefabs); ok {
				sp.output.Grid[loc] = cachedKey
				continue
			}
			sp.setOutputKeyContent(loc, unusedKey, stack)
			reusedByContent.Add(stack.Hash, unusedKey, stack.Prefabs)
			delete(sp.unusedKeys, unusedKey)
			delete(locsWithoutKey, loc)
			break
		}
	}
	// APHELION EDIT ADDITION END

	log.Print("remaining count of unused keys:", len(sp.unusedKeys))
	log.Print("count of locations without keys:", len(locsWithoutKey))
}

// File all locations without keys with the key and the content.
func (sp *saveProcess) fillLocations(locsWithoutKey map[util.Point]bool) error {
	log.Print("handling remaining locations...")

	// For logs.
	var (
		reusedKeys  []dmmdata.Key
		createdKeys []dmmdata.Key
	)

	for loc := range locsWithoutKey {
		// APHELION EDIT CHANGE - SAVE_INDEX - ORIGINAL: prefabs := sp.dmm.GetTile(loc).Instances().Sorted().Prefabs()
		stack := sp.stacks[loc]

		var key dmmdata.Key
		// APHELION EDIT CHANGE - SAVE_INDEX - ORIGINAL: if reusableKey, ok := findKeyByTileContent(sp.output, keyByPrefabs, prefabs); ok {
		if reusableKey, ok := sp.outputContent.Find(stack.Hash, stack.Prefabs); ok {
			key = reusableKey
		} else if len(sp.unusedKeys) != 0 {
			for unusedKey := range sp.unusedKeys { // Pick up the first available key.
				key = unusedKey
				delete(sp.unusedKeys, unusedKey)
				reusedKeys = append(reusedKeys, key)
				break
			}
		} else {
			var newSize int
			if key, newSize = sp.keygen.CreateKey(); newSize != 0 {
				if newSize == -1 {
					return errKeysLimitExceeded
				}
				sp.output.KeyLength = newSize
				log.Print("unable to create a key, changing key length:", newSize)
				return errRegenerateKeys
			}
			createdKeys = append(createdKeys, key)
		}

		// APHELION EDIT CHANGE - SAVE_INDEX - ORIGINAL: sp.setOutputKeyContent(loc, key, prefabs)
		sp.setOutputKeyContent(loc, key, stack)
	}

	log.Print("all tiles handled")
	log.Print("reused keys:", reusedKeys)
	log.Print("created keys:", createdKeys)

	return nil
}

/* APHELION EDIT REMOVAL START - SAVE_INDEX
func (sp *saveProcess) setOutputKeyContent(loc util.Point, key dmmdata.Key, prefabs dmmdata.Prefabs) {
	sp.output.Grid[loc] = key
	sp.output.Dictionary[key] = prefabs
}
APHELION EDIT REMOVAL END */
// APHELION EDIT ADDITION START - SAVE_INDEX
func (sp *saveProcess) setOutputKeyContent(loc util.Point, key dmmdata.Key, stack mapsave.TileStack) {
	sp.output.Grid[loc] = key
	sp.output.Dictionary[key] = stack.Prefabs
	sp.outputContent.Add(stack.Hash, key, stack.Prefabs)
}

// APHELION EDIT ADDITION END

/* APHELION EDIT REMOVAL START - SAVE_INDEX
func findKeyByTileContent(
	data *dmmdata.DmmData,
	keyByPrefabs map[uint64]dmmdata.Key,
	prefabs dmmdata.Prefabs,
) (dmmdata.Key, bool) {
	contentHash := prefabs.Hash()
	if key, ok := keyByPrefabs[contentHash]; ok && prefabs.Equals(data.Dictionary[key]) {
		return key, true
	}
	for key, dataContent := range data.Dictionary {
		if prefabs.Equals(dataContent) {
			keyByPrefabs[contentHash] = key
			return key, true
		}
	}
	return "", false
}
APHELION EDIT REMOVAL END */
