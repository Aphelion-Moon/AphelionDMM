package layout

import "github.com/rs/zerolog/log"

const (
	configName    = "layout"
	configVersion = 1
	// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: configState = 1
	configState = 2
)

type layoutConfig struct {
	// APHELION EDIT ADDITION START - COMPOSITION DOCK
	CompositionDocked bool
	// APHELION EDIT ADDITION END
	Version uint
	State   uint // When different with the configState const - layout will be reset.
}

func (layoutConfig) Name() string {
	return configName
}

func (layoutConfig) TryMigrate(_ map[string]any) (result map[string]any, migrated bool) {
	// do nothing. yet...
	return nil, migrated
}

func (l *Layout) loadConfig() {
	l.app.ConfigRegister(&layoutConfig{
		Version: configVersion,
		State:   configState,
	})
}

func (l *Layout) config() *layoutConfig {
	if cfg, ok := l.app.ConfigFind(configName).(*layoutConfig); ok {
		return cfg
	}
	log.Fatal().Msg("can't find config")
	return nil
}
