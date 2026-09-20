// Package stamps stores reusable mechanical selections, without source paths or
// instance identities. The normal placement engine assigns identities per use.
package stamps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const MaxFileBytes = 8 << 20

type prefab struct {
	Path string            `json:"path"`
	Vars map[string]string `json:"vars"`
}
type tile struct {
	X       int      `json:"x"`
	Y       int      `json:"y"`
	Prefabs []prefab `json:"prefabs"`
}
type document struct {
	Format          string   `json:"format"`
	Version         int      `json:"version"`
	Name            string   `json:"name"`
	EnvironmentHash string   `json:"environment_hash"`
	Width           int      `json:"width"`
	Height          int      `json:"height"`
	HiddenPaths     []string `json:"hidden_paths"`
	Tiles           []tile   `json:"tiles"`
}

// Stamp owns its data. Public access never returns aliases to the saved values.
type Stamp struct {
	data    document
	preview string
}

func (s *Stamp) Name() string            { return s.data.Name }
func (s *Stamp) EnvironmentHash() string { return s.data.EnvironmentHash }

func Capture(name, environmentHash string, m *dmmap.Dmm, coords []util.Point, filter *dm.PathsFilter) (*Stamp, error) {
	if m == nil || filter == nil || len(coords) == 0 || len(coords) > engine.MaxTileChanges {
		return nil, fmt.Errorf("select 1 through %d tiles for a stamp", engine.MaxTileChanges)
	}
	d := document{Format: "aphelion-selection-stamp", Version: 1, Name: strings.TrimSpace(name), EnvironmentHash: environmentHash, HiddenPaths: filter.HiddenPaths()}
	minX, minY := coords[0].X, coords[0].Y
	for _, c := range coords {
		minX, minY = min(minX, c.X), min(minY, c.Y)
	}
	for _, c := range coords {
		if !m.HasTile(c) || c.Z != coords[0].Z {
			return nil, fmt.Errorf("stamp tiles must be inside the map on one level")
		}
		source := m.GetTile(c)
		if source == nil {
			return nil, fmt.Errorf("stamp source tile is missing")
		}
		t := tile{X: c.X - minX + 1, Y: c.Y - minY + 1}
		d.Width, d.Height = max(d.Width, t.X), max(d.Height, t.Y)
		for _, i := range source.Instances() {
			if i == nil || i.Prefab() == nil || i.Prefab().Vars() == nil {
				return nil, fmt.Errorf("stamp source contains a damaged prefab")
			}
			if !filter.IsVisiblePath(i.Prefab().Path()) {
				continue
			}
			p := prefab{Path: i.Prefab().Path(), Vars: make(map[string]string)}
			for _, name := range i.Prefab().Vars().Iterate() {
				value, ok := i.Prefab().Vars().Value(name)
				if !ok {
					return nil, fmt.Errorf("stamp variable has no value")
				}
				p.Vars[name] = value
			}
			t.Prefabs = append(t.Prefabs, p)
		}
		d.Tiles = append(d.Tiles, t)
	}
	sort.Slice(d.Tiles, func(i, j int) bool {
		if d.Tiles[i].Y != d.Tiles[j].Y {
			return d.Tiles[i].Y < d.Tiles[j].Y
		}
		return d.Tiles[i].X < d.Tiles[j].X
	})
	s := &Stamp{data: d}
	if _, err := s.encode(); err != nil {
		return nil, err
	}
	s.preview = s.buildPreview()
	return s, nil
}

func (d document) validate() error {
	if d.Format != "aphelion-selection-stamp" || d.Version != 1 {
		return fmt.Errorf("unsupported selection stamp format or version")
	}
	if !utf8.ValidString(d.Name) || strings.TrimSpace(d.Name) == "" || utf8.RuneCountInString(d.Name) > 128 || strings.ContainsFunc(d.Name, unicode.IsControl) {
		return fmt.Errorf("stamp name must contain 1 through 128 printable characters")
	}
	if err := model.ValidateSHA256("stamp environment hash", d.EnvironmentHash); err != nil {
		return err
	}
	if d.Width < 1 || d.Height < 1 || d.Width > model.MaxMapDimension || d.Height > model.MaxMapDimension || len(d.Tiles) == 0 || len(d.Tiles) > engine.MaxTileChanges {
		return fmt.Errorf("stamp dimensions or tile count exceed supported limits")
	}
	seen := make(map[[2]int]bool)
	minX, minY, maxX, maxY := d.Width, d.Height, 0, 0
	for _, t := range d.Tiles {
		coord := [2]int{t.X, t.Y}
		if t.X < 1 || t.Y < 1 || t.X > d.Width || t.Y > d.Height || seen[coord] {
			return fmt.Errorf("stamp has an out-of-bounds or duplicate tile")
		}
		seen[coord] = true
		minX, minY, maxX, maxY = min(minX, t.X), min(minY, t.Y), max(maxX, t.X), max(maxY, t.Y)
		for _, p := range t.Prefabs {
			if !validPath(p.Path) || p.Vars == nil {
				return fmt.Errorf("stamp has an invalid prefab")
			}
			for name, value := range p.Vars {
				if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || !utf8.ValidString(value) {
					return fmt.Errorf("stamp has invalid variable data")
				}
			}
		}
	}
	if minX != 1 || minY != 1 || maxX != d.Width || maxY != d.Height {
		return fmt.Errorf("stamp dimensions must match normalized tile bounds")
	}
	hidden := make(map[string]bool)
	for _, path := range d.HiddenPaths {
		if !validPath(path) || hidden[path] {
			return fmt.Errorf("stamp has an invalid or duplicate hidden path")
		}
		hidden[path] = true
	}
	return nil
}

func validPath(path string) bool {
	return strings.HasPrefix(path, "/") && utf8.ValidString(path) && !strings.ContainsRune(path, 0)
}

func (s *Stamp) encode() ([]byte, error) {
	if err := s.data.validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("stamp exceeds the %d-byte file limit", MaxFileBytes)
	}
	return data, nil
}

func (s *Stamp) Save(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".admmstamp") {
		return fmt.Errorf("choose an .admmstamp file")
	}
	data, err := s.encode()
	if err != nil {
		return err
	}
	return dmmdata.SaveAtomic(path, func(w io.Writer) error { _, err := w.Write(data); return err }, func(staged string) error {
		actual, err := os.ReadFile(staged)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, actual) {
			return fmt.Errorf("staged stamp differs from captured selection")
		}
		return nil
	})
}

func Load(path string) (*Stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("choose a regular stamp file no larger than %d bytes", MaxFileBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("stamp is too large or contains invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var d document
	if err := decoder.Decode(&d); err != nil {
		return nil, fmt.Errorf("read stamp: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("stamp contains trailing data")
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	s := &Stamp{data: d}
	s.preview = s.buildPreview()
	return s, nil
}

// PasteData preserves captured and currently hidden paths. Prefabs acquire the
// current environment's defaults; unknown paths retain their explicit values.
func (s *Stamp) PasteData(current *dm.PathsFilter, environment *dmenv.Dme) dmmclip.PasteData {
	filter := dm.NewPathsFilterEmpty()
	for _, path := range append(current.HiddenPaths(), s.data.HiddenPaths...) {
		if filter.IsVisiblePath(path) {
			filter.TogglePath(path)
		}
	}
	data := dmmclip.PasteData{Filter: filter.Copy()}
	for _, saved := range s.data.Tiles {
		t := dmmap.Tile{Coord: util.Point{X: saved.X, Y: saved.Y, Z: 1}}
		for _, p := range saved.Prefabs {
			vars := &dmvars.MutableVariables{}
			for _, name := range variableNames(p.Vars) {
				vars.Put(name, p.Vars[name])
			}
			values := vars.ToImmutable()
			if environment != nil {
				if object := environment.Objects[p.Path]; object != nil {
					values.LinkParent(object.Vars)
				}
			}
			t.InstancesAdd(dmmprefab.New(0, p.Path, values))
		}
		data.Buffer = append(data.Buffer, t)
	}
	return data
}

func variableNames(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Stamp) Preview() string {
	return s.preview
}

func (s *Stamp) buildPreview() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n%d x %d; %d selected tiles; %d captured hidden paths.\n", s.Name(), s.data.Width, s.data.Height, len(s.data.Tiles), len(s.data.HiddenPaths))
	for _, t := range s.data.Tiles[:min(len(s.data.Tiles), 12)] {
		fmt.Fprintf(&out, "\n(%d, %d): %d prefabs\n", t.X, t.Y, len(t.Prefabs))
		for _, p := range t.Prefabs[:min(len(t.Prefabs), 8)] {
			fmt.Fprintln(&out, previewText(p.Path))
			names := variableNames(p.Vars)
			for _, name := range names[:min(len(names), 8)] {
				fmt.Fprintf(&out, "  %s = %s\n", previewText(name), previewText(p.Vars[name]))
			}
			if len(names) > 8 {
				fmt.Fprintln(&out, "  More variables in stamp file.")
			}
		}
		if len(t.Prefabs) > 8 {
			fmt.Fprintln(&out, "More prefabs in stamp file.")
		}
	}
	if len(s.data.Tiles) > 12 {
		fmt.Fprintln(&out, "More tiles in stamp file.")
	}
	return out.String()
}

func previewText(value string) string {
	if len(value) > 256 {
		end := 256
		for !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end] + "… [truncated]"
	}
	return strconv.Quote(value)
}
