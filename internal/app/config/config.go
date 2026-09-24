package config

import (
	"encoding/json"
	"os"
	// APHELION EDIT ADDITION START - CONFIG SNAPSHOTS
	"sdmm/internal/aphelion/configstore"
	// APHELION EDIT ADDITION END

	"github.com/rs/zerolog/log"
)

type Config interface {
	Name() string
	TryMigrate(rawCfg map[string]any) (result map[string]any, migrated bool)
}

/* APHELION EDIT REMOVAL START - CONFIG SNAPSHOTS
func Save(filepath string, cfg Config) {
	SaveV(filepath, cfg)
}

func SaveV(filepath string, cfg any) {
	log.Print("saving:", filepath)
	f, err := os.Create(filepath)
	if err != nil {
		log.Print("unable to create file by path:", filepath)
		return
	}
	// APHELION EDIT CHANGE - STATIC_ANALYSIS - ORIGINAL: defer f.Close()
	defer func() { _ = f.Close() }()

	if j, err := json.Marshal(cfg); err == nil {
		_, _ = f.Write(j)
	} else {
		log.Print("unable to save data by path:", filepath)
	}
}

APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - CONFIG SNAPSHOTS
func Save(path string, cfg Config) error { return SaveV(path, cfg) }

func SaveV(path string, cfg any) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return configstore.Write(path, data)
}

// APHELION EDIT ADDITION END

func Load(filepath string, cfg Config) error {
	return LoadV(filepath, cfg)
}

func LoadV(filepath string, cfg any) error {
	log.Print("reading:", filepath)
	f, err := os.Open(filepath)
	if err != nil {
		return err
	}
	// APHELION EDIT CHANGE - STATIC_ANALYSIS - ORIGINAL: defer f.Close()
	defer func() { _ = f.Close() }()

	var j []byte
	if j, err = os.ReadFile(filepath); err == nil {
		err = json.Unmarshal(j, cfg)
	}

	return err
}
