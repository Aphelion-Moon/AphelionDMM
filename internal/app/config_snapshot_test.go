package app

import (
	"os"
	"sdmm/internal/app/config"
	"testing"
)

type snapshotTestConfig struct{ Values []string }

func (*snapshotTestConfig) Name() string                                     { return "snapshot" }
func (*snapshotTestConfig) TryMigrate(map[string]any) (map[string]any, bool) { return nil, false }

func TestConfigurationWorkerUsesOwnedSnapshot(t *testing.T) {
	cfg := &snapshotTestConfig{Values: []string{"captured"}}
	a := &app{configDir: t.TempDir(), configs: map[string]config.Config{cfg.Name(): cfg}}
	a.runBackgroundConfigSave()
	a.configSave()
	cfg.Values[0] = "changed after capture"
	a.configWriter.Close()
	data, err := os.ReadFile(configFilePath(a.configDir, cfg.Name()))
	if err != nil || string(data) != `{"Values":["captured"]}` {
		t.Fatal(string(data), err)
	}
}
