package cpenvironment

import (
	"github.com/rs/zerolog/log"

	// APHELION EDIT ADDITION START - FILTER PROFILES
	"sdmm/internal/aphelion/filterprofiles"
	// APHELION EDIT ADDITION END
)

const (
	configName    = "cpenvironment"
	configVersion = 1
)

type cpenvironmentConfig struct {
	Version uint

	NodeScale int32

	// APHELION EDIT ADDITION START - FILTER PROFILES
	FilterProfiles   filterprofiles.Store
	ActiveProfileIDs map[string]string
	// APHELION EDIT ADDITION END
}

func (cpenvironmentConfig) Name() string {
	return configName
}

func (cpenvironmentConfig) TryMigrate(_ map[string]any) (result map[string]any, migrated bool) {
	// do nothing. yet...
	return nil, migrated
}

func (e *Environment) loadConfig() {
	e.app.ConfigRegister(&cpenvironmentConfig{
		Version:   configVersion,
		NodeScale: 100,
		// APHELION EDIT ADDITION START - FILTER PROFILES
		FilterProfiles:   filterprofiles.NewStore(),
		ActiveProfileIDs: make(map[string]string),
		// APHELION EDIT ADDITION END
	})
	// APHELION EDIT ADDITION START - FILTER PROFILES
	cfg := e.config()
	if cfg.FilterProfiles.Version == 0 {
		if len(cfg.FilterProfiles.Profiles) == 0 {
			cfg.FilterProfiles = filterprofiles.NewStore()
		} else {
			e.filterProfileConfigError = "Profile entries have no store version; data was retained and is read-only."
		}
	}
	if cfg.ActiveProfileIDs == nil {
		cfg.ActiveProfileIDs = make(map[string]string)
	}
	if err := cfg.FilterProfiles.Validate(); err != nil {
		e.filterProfileConfigError = err.Error()
		log.Warn().Err(err).Msg("Filter profile configuration needs repair")
	}
	// APHELION EDIT ADDITION END
}

func (e *Environment) config() *cpenvironmentConfig {
	if cfg, ok := e.app.ConfigFind(configName).(*cpenvironmentConfig); ok {
		return cfg
	}
	log.Fatal().Msg("can't find config")
	return nil
}
