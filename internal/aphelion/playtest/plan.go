// Package playtest launches a local test server on the open map.
//
// Meridian-Rift (tgstation) picks the station map at round start from
// data/next_map.json (PATH_TO_NEXT_MAP_JSON, code/datums/map_config.dm), with
// map_path and map_file relative to _maps. Maps load at runtime, so a map edit
// never needs a recompile; only code, interface and icon changes do.
//
// Process safety (docs/agent/security-and-networking.md): the executables are
// BYOND's dm, dreamdaemon and dreamseeker from one configured directory, run
// directly (no shell) with fixed arguments; the server binds to a local port
// and the client connects to 127.0.0.1. The only file written is
// data/next_map.json below the environment root.
package playtest

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const nextMapFile = "data/next_map.json"

// MapConfig returns the next_map.json contents that load mapFile. A station
// config in _maps/*.json naming the same file is reused, keeping its shuttles
// and job changes; otherwise a minimal config is written.
func MapConfig(root, mapFile string) ([]byte, string, error) {
	mapsDir := filepath.Join(root, "_maps")
	rel, err := filepath.Rel(mapsDir, mapFile)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil, "", fmt.Errorf("the map must be saved below %s to be loaded by the game", mapsDir)
	}
	rel = filepath.ToSlash(rel)
	mapPath, mapName := pathDir(rel), filepath.Base(rel)
	configs, _ := filepath.Glob(filepath.Join(mapsDir, "*.json"))
	for _, path := range configs {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var config struct {
			MapName string          `json:"map_name"`
			MapPath string          `json:"map_path"`
			MapFile json.RawMessage `json:"map_file"`
		}
		if json.Unmarshal(data, &config) != nil || config.MapPath != mapPath {
			continue
		}
		var single string
		var many []string
		if json.Unmarshal(config.MapFile, &single) == nil && strings.EqualFold(single, mapName) ||
			json.Unmarshal(config.MapFile, &many) == nil && containsFold(many, mapName) {
			return data, config.MapName, nil
		}
	}
	name := strings.TrimSuffix(mapName, filepath.Ext(mapName))
	data, _ := json.MarshalIndent(map[string]any{"version": 1, "map_name": name, "map_path": mapPath, "map_file": mapName}, "", "\t")
	return data, name, nil
}

func pathDir(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return "."
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// WriteMapConfig writes data/next_map.json below root.
func WriteMapConfig(root string, data []byte) error {
	path := filepath.Join(root, filepath.FromSlash(nextMapFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// compiledExtensions are inputs DreamMaker builds into the .dmb and .rsc.
var compiledExtensions = map[string]bool{".dm": true, ".dme": true, ".dmf": true, ".dmi": true}

// skippedDirs never feed the build.
var skippedDirs = map[string]bool{".git": true, "data": true, "_maps": true, "node_modules": true}

// NeedsCompile reports whether any compiled input below root is newer than
// dmb, or dmb is missing. It returns the first newer file for the log.
func NeedsCompile(root, dmb string) (bool, string, error) {
	info, err := os.Stat(dmb)
	if err != nil {
		return true, filepath.Base(dmb) + " is missing", nil
	}
	built := info.ModTime()
	newer := ""
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (skippedDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !compiledExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(built) {
			newer, _ = filepath.Rel(root, path)
			return fs.SkipAll
		}
		return nil
	})
	return newer != "", newer, err
}

// Commands are the fixed command lines of one launch.
type Commands struct {
	Compile []string // empty when the build is current
	Server  []string
	Client  []string
}

// Build returns the command lines for dme in root using BYOND's bin dir.
func Build(bin, dme string, port int, compile bool) Commands {
	dmb := strings.TrimSuffix(dme, filepath.Ext(dme)) + ".dmb"
	c := Commands{
		Server: []string{filepath.Join(bin, "dreamdaemon.exe"), dmb, fmt.Sprint(port), "-trusted", "-close"},
		Client: []string{filepath.Join(bin, "dreamseeker.exe"), fmt.Sprintf("byond://127.0.0.1:%d", port)},
	}
	if compile {
		c.Compile = []string{filepath.Join(bin, "dm.exe"), dme}
	}
	return c
}

// DefaultBin finds a BYOND install in the usual places.
func DefaultBin() string {
	for _, dir := range []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "BYOND", "bin"),
		filepath.Join(os.Getenv("ProgramFiles"), "BYOND", "bin"),
	} {
		if _, err := os.Stat(filepath.Join(dir, "dm.exe")); err == nil {
			return dir
		}
	}
	return ""
}

// ServerWait bounds how long the client waits for the server's port.
const ServerWait = 5 * time.Minute
