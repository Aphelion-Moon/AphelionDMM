package mapping

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"sdmm/internal/util"
)

type MapConfiguration struct {
	MapName string           `json:"map_name"`
	MapPath string           `json:"map_path"`
	MapFile json.RawMessage  `json:"map_file"`
	Traits  []map[string]any `json:"traits"`
}
type FixedBinding struct {
	ID, Path, RequiredMap, TraitName string
	TraitIndex, Slot                 int
	Destination                      util.Point
	Error                            string
}
type fixedConfig struct {
	Templates map[string]struct {
		Directory   string
		MapFiles    []string `toml:"map_files"`
		RequiredMap string   `toml:"required_map"`
		Coordinates []int
		TraitName   string `toml:"trait_name"`
	}
}

// Fixed resolves only a selected single-file map configuration. Station defaults
// follow map_config.dm; Z remains local to that file, never a claimed world Z.
func (c *Catalog) Fixed(ctx context.Context, base *Source, mapConfigPath string, choices map[string]int) (placements []FixedPlacement, bindings []FixedBinding, diagnostics []Diagnostic) {
	fail := func(code, message, path string) {
		diagnostics = append(diagnostics, Diagnostic{Severity: "error", Code: code, Message: message, Source: path})
	}
	path, err := BoundPath(c.environment.RootDir, mapConfigPath)
	if err != nil {
		fail("map-config", err.Error(), mapConfigPath)
		return
	}
	data, err := readSmallConfig(path)
	if err != nil {
		fail("map-config", err.Error(), path)
		return
	}
	var config MapConfiguration
	if err = json.Unmarshal(data, &config); err != nil {
		fail("map-config", err.Error(), path)
		return
	}
	var filename string
	if err = json.Unmarshal(config.MapFile, &filename); err != nil {
		fail("trait-z", "Multiple-file map groups need an explicit trait-to-file mapping; no world Z was guessed", path)
		return
	}
	expected, err := BoundPath(c.environment.RootDir, filepath.ToSlash(filepath.Join("_maps", config.MapPath, filename)))
	if err != nil || !samePath(expected, base.Identity.Path) {
		fail("map-binding", "Selected map configuration does not identify this base source", path)
		return
	}
	if len(config.Traits) != base.Size.Z {
		fail("trait-z", "Trait records do not resolve every local source Z", path)
		return
	}
	c.mapName = config.MapName
	object := c.environment.Objects["/datum/controller/subsystem/automapper"]
	if object == nil || object.Vars == nil {
		fail("automapper-config", "Loaded environment has no resolved automapper config_file", path)
		return
	}
	value, _ := object.Vars.Value("config_file")
	relative, err := constantString(value)
	if err != nil {
		fail("automapper-config", "config_file is dynamic or unresolved", path)
		return
	}
	fixedPath, err := BoundPath(c.environment.RootDir, relative)
	if err != nil {
		fail("automapper-config", err.Error(), relative)
		return
	}
	fixedBytes, err := readSmallConfig(fixedPath)
	if err != nil {
		fail("automapper-config", err.Error(), fixedPath)
		return
	}
	var fixed fixedConfig
	if _, err = toml.Decode(string(fixedBytes), &fixed); err != nil {
		fail("automapper-config", err.Error(), fixedPath)
		return
	}
	names := make([]string, 0, len(fixed.Templates))
	for name := range fixed.Templates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ctx.Err() != nil {
			fail("cancelled", ctx.Err().Error(), fixedPath)
			return
		}
		entry := fixed.Templates[name]
		if entry.RequiredMap != filename {
			continue
		}
		b := FixedBinding{ID: name, RequiredMap: entry.RequiredMap, TraitName: entry.TraitName, Slot: choices[name]}
		if len(entry.Coordinates) != 3 || entry.Coordinates[0] < 1 || entry.Coordinates[1] < 1 || entry.Coordinates[2] < 1 {
			b.Error = "Fixed placement needs three positive coordinates"
		} else {
			b.TraitIndex = entry.Coordinates[2]
			levels := []int{}
			for index, traits := range config.Traits {
				value, present := traits[entry.TraitName]
				enabled := value == true || value == float64(1)
				if entry.TraitName == "Station" && !present {
					enabled = true
				}
				if enabled {
					levels = append(levels, index+1)
				}
			}
			if b.TraitIndex > len(levels) {
				b.Error = "Trait-relative index is unresolved within the selected map file"
			} else {
				b.Destination = util.Point{X: entry.Coordinates[0], Y: entry.Coordinates[1], Z: levels[b.TraitIndex-1]}
			}
		}
		if b.Error == "" {
			if b.Slot < 0 || b.Slot >= len(entry.MapFiles) {
				b.Error = "Fixed candidate slot is absent"
			} else {
				b.Path, err = BoundPath(c.environment.RootDir, filepath.ToSlash(filepath.Join(entry.Directory, entry.MapFiles[b.Slot])))
				if err != nil {
					b.Error = err.Error()
				}
			}
		}
		if b.Error == "" {
			source, e := c.Load(ctx, b.Path)
			if e != nil {
				b.Error = e.Error()
			} else if source.Origin != (util.Point{X: 1, Y: 1, Z: 1}) || source.Size.Z != 1 || len(source.grid) != source.Size.X*source.Size.Y {
				b.Error = "Fixed template origin/Z layout is unsupported"
			} else {
				placements = append(placements, FixedPlacement{ID: name, Source: source, Transform: Transform{Offset: util.Point{X: b.Destination.X - 1, Y: b.Destination.Y - 1, Z: b.Destination.Z - 1}}})
				for _, atoms := range source.dictionary {
					for _, a := range atoms {
						if a.Path == "/turf/template_noop" && (len(a.Vars) != 0 || !source.tgm) {
							diagnostics = append(diagnostics, Diagnostic{Severity: "warning", Code: "noop-text-layout", Message: "Structural noop preview may differ from the runtime's exact TGM/text shortcut; reservation confidence is partial", Source: b.Path, Occurrence: name})
							break
						}
					}
				}
			}
		}
		if b.Error != "" {
			fail("fixed-binding", b.Error, b.Path)
		}
		bindings = append(bindings, b)
	}
	return
}

func readSmallConfig(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("configuration exceeds 4 MiB")
	}
	return data, nil
}
func samePath(a, b string) bool {
	left, e := filepath.Abs(a)
	if e != nil {
		return false
	}
	right, e := filepath.Abs(b)
	if e != nil {
		return false
	}
	li, e := os.Stat(left)
	if e != nil {
		return false
	}
	ri, e := os.Stat(right)
	return e == nil && os.SameFile(li, ri)
}
