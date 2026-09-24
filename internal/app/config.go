package app

import (
	// APHELION EDIT CHANGE - CONFIG SNAPSHOTS - ORIGINAL: "os"
	"encoding/json"
	"path/filepath"
	"time"

	"sdmm/internal/app/config"
	// APHELION EDIT ADDITION START - CONFIG SNAPSHOTS
	"sdmm/internal/aphelion/configstore"
	// APHELION EDIT ADDITION END

	"github.com/rs/zerolog/log"
)

func (a *app) ConfigRegister(cfg config.Config) {
	if a.configs == nil {
		a.configs = make(map[string]config.Config)
	}

	configFilePath := configFilePath(a.configDir, cfg.Name())

	log.Printf("registering config [%s] by path [%s]...", cfg.Name(), configFilePath)

	// Load a raw configuration data.
	rawCfg := make(map[string]any)
	err := config.LoadV(configFilePath, &rawCfg)
	if err != nil {
		log.Print("unable to load config:", cfg.Name()) // Highly likely doesn't exist.
	} else {
		// Try to do a migration. The result var will be a nil, if there is nothing to migrate.
		if result, migrated := cfg.TryMigrate(rawCfg); migrated {
			log.Print("migrated config:", configFilePath)
			// APHELION EDIT CHANGE - CONFIG SNAPSHOTS - ORIGINAL: config.SaveV(configFilePath, result)
			if err := config.SaveV(configFilePath, result); err != nil {
				log.Error().Err(err).Str("config", cfg.Name()).Msg("Unable to persist migrated configuration")
				// Keep the valid migrated settings in memory even if disk is
				// temporarily unwritable. A later snapshot can retry persistence.
				if data, marshalErr := json.Marshal(result); marshalErr == nil {
					if loadErr := json.Unmarshal(data, cfg); loadErr != nil {
						log.Error().Err(loadErr).Msg("Unable to load migrated configuration")
					}
				}
				a.configs[cfg.Name()] = cfg
				return
			}
		}

		// Load migrated (or not) data.
		err = config.Load(configFilePath, cfg)
		if err != nil {
			log.Fatal().Msgf("unable to load config: %s", configFilePath)
		}
	}

	a.configs[cfg.Name()] = cfg

	log.Print("config registered:", cfg.Name())
}

const backgroundSavePeriod = time.Minute * 3

/* APHELION EDIT REMOVAL START - CONFIG SNAPSHOTS
func (a *app) runBackgroundConfigSave() {
	log.Printf("background configuration save every [%s]!", backgroundSavePeriod)
	go func() {
		for range time.Tick(backgroundSavePeriod) {
			a.configSave()
		}
	}()
}

func (a *app) configSave() {
	_ = os.MkdirAll(a.configDir, os.ModePerm)
	for _, cfg := range a.configs {
		a.configSaveV(cfg)
	}
}

func (a *app) configSaveV(cfg config.Config) {
	config.Save(configFilePath(a.configDir, cfg.Name()), cfg)
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - CONFIG SNAPSHOTS
func (a *app) runBackgroundConfigSave() {
	a.configWriter = configstore.NewWriter(func(path string, err error) {
		log.Error().Err(err).Str("path", path).Msg("Unable to save configuration")
	})
	a.nextConfigSave = time.Now().Add(backgroundSavePeriod)
}

// This is called by the UI owner. The disk worker never reads live settings.
func (a *app) processConfigSave() {
	if a.configWriter != nil && !time.Now().Before(a.nextConfigSave) {
		a.configSave()
		a.nextConfigSave = time.Now().Add(backgroundSavePeriod)
	}
}

func (a *app) configSave() {
	for _, cfg := range a.configs {
		if a.configWriter == nil {
			a.configSaveV(cfg)
			continue
		}
		data, err := json.Marshal(cfg)
		if err == nil {
			err = a.configWriter.Submit(configFilePath(a.configDir, cfg.Name()), data)
		}
		if err != nil {
			log.Error().Err(err).Str("config", cfg.Name()).Msg("Unable to capture configuration")
		}
	}
}

func (a *app) configSaveV(cfg config.Config) {
	if err := config.Save(configFilePath(a.configDir, cfg.Name()), cfg); err != nil {
		log.Error().Err(err).Str("config", cfg.Name()).Msg("Unable to save configuration")
	}
}

// APHELION EDIT ADDITION END

func (a *app) ConfigFind(name string) config.Config {
	if cfg, ok := a.configs[name]; ok {
		return cfg
	}
	log.Fatal().Msgf("unable to find config: %s", name)
	return nil
}

func configFilePath(dir, cfgName string) string {
	return filepath.FromSlash(dir + "/" + cfgName + ".json")
}
