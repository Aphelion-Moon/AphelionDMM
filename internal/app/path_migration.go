// APHELION EDIT ADDITION START - PATH MIGRATION
package app

import (
	"context"

	"sdmm/internal/aphelion/repath"
	repathui "sdmm/internal/aphelion/repath/ui"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/layout/lnode"

	"github.com/rs/zerolog/log"
)

const pathMigrationConfigName = "pathmigration"

// pathMigrationConfig stores remembered decisions apart from preferences.
// Nothing is added unless a person enables and uses Remember.
type pathMigrationConfig struct {
	repath.Memory
}

func (pathMigrationConfig) Name() string { return pathMigrationConfigName }

func (pathMigrationConfig) TryMigrate(map[string]any) (map[string]any, bool) { return nil, false }

func (a *app) loadPathMigrationConfig() {
	cfg := &pathMigrationConfig{Memory: *repath.NewMemory()}
	a.ConfigRegister(cfg)
	if cfg.Environments == nil {
		cfg.Environments = map[string]repath.EnvironmentMemory{}
	}
}

func (a *app) pathMigrationConfig() *pathMigrationConfig {
	if cfg, ok := a.ConfigFind(pathMigrationConfigName).(*pathMigrationConfig); ok {
		return cfg
	}
	return nil
}

func (a *app) PathMigrationSettings() *repath.Settings {
	return a.preferencesConfig().PathMigration
}

func (a *app) PathMigrationMemory() *repath.Memory {
	if cfg := a.pathMigrationConfig(); cfg != nil {
		return &cfg.Memory
	}
	return nil
}

func (a *app) SavePathMigrationMemory() {
	if cfg := a.pathMigrationConfig(); cfg != nil {
		a.configSaveV(cfg)
	}
}

func (a *app) BypassEnvironmentCache() bool {
	return a.preferencesConfig().Application.BypassEnvironmentCache
}

func (a *app) ShowPathInSearch(path string) {
	a.DoSearchPrefabByPath(path)
	a.ShowLayout(lnode.NameSearch, true)
}

func (a *app) PathMigrationTarget() (repathui.Target, bool) {
	ws, ok := a.activeWsMap()
	if !ok {
		return nil, false
	}
	return pathMigrationTarget{ws: ws}, true
}

func (a *app) DoOpenPathMigration() {
	a.ShowLayout(lnode.NamePathMigration, true)
}

// offerPathMigration runs after a map with unknown types is installed.
func (a *app) offerPathMigration(mapPath string) {
	settings := a.PathMigrationSettings()
	if settings == nil {
		return
	}
	if settings.OpenOnUnknown || settings.ApplyCertainOnOpen {
		a.layout.PathMigration.OfferFor(mapPath)
	}
	if settings.OpenOnUnknown {
		a.DoOpenPathMigration()
	} else {
		log.Info().Str("map", mapPath).Msg("map has unknown types; Edit > Resolve Unknown Types can migrate them")
	}
}

type pathMigrationTarget struct {
	ws *wsmap.WsMap
}

func (t pathMigrationTarget) editor() *editor.Editor { return t.ws.Map().Editor() }

func (t pathMigrationTarget) ID() string   { return t.ws.Id() }
func (t pathMigrationTarget) Name() string { return t.ws.Map().Dmm().Name }
func (t pathMigrationTarget) Path() string { return t.ws.Map().Dmm().Path.Absolute }

func (t pathMigrationTarget) Version() (uint64, uint64) {
	generation, revision := t.editor().SaveVersion()
	return generation, uint64(revision)
}

func (t pathMigrationTarget) CanEdit() bool {
	return t.editor().CanStartMapEdit() && !t.editor().HasPastePlacement()
}

func (t pathMigrationTarget) CaptureInventory(known func(string) bool) (func(context.Context) (repath.Inventory, error), error) {
	capture, _, err := t.editor().CaptureAcceptedTiles(context.Background())
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (repath.Inventory, error) {
		return repath.BuildInventory(ctx, capture.Header(), capture.Tile, known)
	}, nil
}

func (t pathMigrationTarget) Apply(transformer *repath.Transformer, label string, done func(repath.Report, error)) error {
	return t.editor().ApplyPathMigration(transformer, label, func(result editor.PathMigrationResult) {
		done(result.Report, result.Err)
	})
}

// APHELION EDIT ADDITION END
