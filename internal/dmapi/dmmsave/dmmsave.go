package dmmsave

import (
	"fmt"
	// APHELION EDIT ADDITION START - DISK_VERSION
	"sdmm/internal/aphelion/diskversion"
	// APHELION EDIT ADDITION END

	"sdmm/internal/dmapi/dmenv"

	"sdmm/internal/dmapi/dmmap"

	"github.com/rs/zerolog/log"
)

// APHELION EDIT CHANGE - ATOMIC_SAVE - ORIGINAL: func Save(dme *dmenv.Dme, dmm *dmmap.Dmm, cfg Config)
func Save(dme *dmenv.Dme, dmm *dmmap.Dmm, cfg Config) error {
	return SaveV(dme, dmm, dmm.Path.Absolute, cfg)
}

// APHELION EDIT ADDITION START - DISK_VERSION
func SaveWithDiskState(dme *dmenv.Dme, dmm *dmmap.Dmm, cfg Config, expected diskversion.State) (diskversion.State, error) {
	return SaveVWithDiskState(dme, dmm, dmm.Path.Absolute, cfg, expected)
}

func SaveVWithDiskState(dme *dmenv.Dme, dmm *dmmap.Dmm, path string, cfg Config, expected diskversion.State) (diskversion.State, error) {
	return saveV(dme, dmm, path, cfg, &expected)
}

// APHELION EDIT ADDITION END

// APHELION EDIT CHANGE - ATOMIC_SAVE - ORIGINAL: func SaveV(dme *dmenv.Dme, dmm *dmmap.Dmm, path string, cfg Config)
func SaveV(dme *dmenv.Dme, dmm *dmmap.Dmm, path string, cfg Config) error {
	_, err := saveV(dme, dmm, path, cfg, nil)
	return err
}

// APHELION EDIT ADDITION START - DISK_VERSION
func saveV(dme *dmenv.Dme, dmm *dmmap.Dmm, path string, cfg Config, expected *diskversion.State) (diskversion.State, error) {
	log.Printf("save started [%s]...", path)

	sp, err := makeSaveProcess(cfg, dme, dmm, path)
	if err != nil {
		log.Print("unable to start save process")
		return diskversion.State{}, fmt.Errorf("start save process: %w", err)
	}

	/* APHELION EDIT REMOVAL START - EXPECTED INPUT VALIDATION
	if cfg.SanitizeVariables {
		sp.sanitizeVariables()
	}
	APHELION EDIT REMOVAL END */

	sp.handleReusedKeys()
	if err = sp.handleLocationsWithoutKeys(); err != nil {
		log.Print("unable to handle locations without keys:", err)
		return diskversion.State{}, fmt.Errorf("assign map keys: %w", err)
	}
	// APHELION EDIT CHANGE - EXPECTED INPUT VALIDATION - ORIGINAL: if err := sp.output.Save(); err != nil {
	var saved diskversion.State
	if expected == nil {
		err = sp.save()
	} else {
		saved, err = sp.saveWithDiskState(*expected)
	}
	if err != nil {
		return diskversion.State{}, err
	}

	log.Print("save finished")
	return saved, nil
}

// APHELION EDIT ADDITION END
