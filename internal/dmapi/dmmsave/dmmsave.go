package dmmsave

import (
	"fmt"

	"sdmm/internal/dmapi/dmenv"

	"sdmm/internal/dmapi/dmmap"

	"github.com/rs/zerolog/log"
)

// APHELION EDIT CHANGE - ATOMIC_SAVE - ORIGINAL: func Save(dme *dmenv.Dme, dmm *dmmap.Dmm, cfg Config)
func Save(dme *dmenv.Dme, dmm *dmmap.Dmm, cfg Config) error {
	return SaveV(dme, dmm, dmm.Path.Absolute, cfg)
}

// APHELION EDIT CHANGE - ATOMIC_SAVE - ORIGINAL: func SaveV(dme *dmenv.Dme, dmm *dmmap.Dmm, path string, cfg Config)
func SaveV(dme *dmenv.Dme, dmm *dmmap.Dmm, path string, cfg Config) error {
	log.Printf("save started [%s]...", path)

	sp, err := makeSaveProcess(cfg, dme, dmm, path)
	if err != nil {
		log.Print("unable to start save process")
		return fmt.Errorf("start save process: %w", err)
	}

	/* APHELION EDIT REMOVAL START - EXPECTED INPUT VALIDATION
	if cfg.SanitizeVariables {
		sp.sanitizeVariables()
	}
	APHELION EDIT REMOVAL END */

	sp.handleReusedKeys()
	if err = sp.handleLocationsWithoutKeys(); err != nil {
		log.Print("unable to handle locations without keys:", err)
		return fmt.Errorf("assign map keys: %w", err)
	}
	// APHELION EDIT CHANGE - EXPECTED INPUT VALIDATION - ORIGINAL: if err := sp.output.Save(); err != nil {
	if err := sp.save(); err != nil {
		return err
	}

	log.Print("save finished")
	return nil
}
