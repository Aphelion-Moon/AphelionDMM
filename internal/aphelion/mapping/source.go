// Package mapping models editor scenarios and source provenance, not runtime state.
package mapping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type Atom struct {
	Path string
	Vars map[string]string
}
type Identity struct {
	StructuralHash                     string
	Path, ContentHash, EnvironmentHash string
	Environment                        *dmenv.Dme
	DocumentID                         string
	Revision                           uint64
	Generation                         uint64
}

// Source owns immutable explicit atoms and a BYOND-coordinate grid. Consumers
// receive copies; rendering receives a separate transient map without an Editor.
type Source struct {
	Identity   Identity
	Size       util.Point
	Origin     util.Point
	grid       map[util.Point]dmmdata.Key
	dictionary map[dmmdata.Key][]Atom
	disk       diskversion.State
	lease      *resources.Reservation
	tgm        bool
}

func LoadSource(ctx context.Context, path string, environment *dmenv.Dme) (*Source, error) {
	if environment == nil {
		return nil, fmt.Errorf("load a project before opening a reference")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.Size() < 0 || info.Size() > 128<<20 {
		return nil, fmt.Errorf("reference exceeds 128 MiB source limit")
	}
	// A one-character grid key still becomes a map entry during parsing.
	// Admit that expansion before handing the file to the inherited parser.
	lease, err := resources.DefaultBudget().Reserve(uint64(info.Size())*160 + 1<<20)
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() {
		if !accepted {
			lease.Release()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	digest := sha256.New()
	data, err := dmmdata.NewWithSourceCopy(abs, digest)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash, err := environment.EnvironmentHash()
	if err != nil {
		return nil, err
	}
	if data.MaxX <= 0 || data.MaxY <= 0 || data.MaxZ <= 0 || int64(data.MaxX)*int64(data.MaxY)*int64(data.MaxZ) > 2_000_000 {
		return nil, fmt.Errorf("reference dimensions are empty or exceed two million cells")
	}
	var instances uint64
	for _, key := range data.Grid {
		instances += uint64(len(data.Dictionary[key]))
	}
	if instances > 4_000_000 {
		return nil, fmt.Errorf("reference exceeds four million placed atoms")
	}
	if err := lease.Resize(max(lease.Bytes(), uint64(data.MaxX*data.MaxY*data.MaxZ)*256+instances*160+uint64(info.Size())*12)); err != nil {
		return nil, err
	}
	s := &Source{Identity: Identity{Path: abs, ContentHash: hex.EncodeToString(digest.Sum(nil)), EnvironmentHash: hash, Environment: environment}, Size: util.Point{X: data.MaxX, Y: data.MaxY, Z: data.MaxZ}, Origin: util.Point{X: data.MaxX, Y: data.MaxY, Z: data.MaxZ}, grid: make(map[util.Point]dmmdata.Key, len(data.Grid)), dictionary: make(map[dmmdata.Key][]Atom, len(data.Dictionary)), disk: data.DiskState, lease: lease}
	for key, prefabs := range data.Dictionary {
		atoms := make([]Atom, len(prefabs))
		for i, p := range prefabs {
			vars := make(map[string]string)
			for _, name := range p.Vars().Iterate() {
				value, _ := p.Vars().Value(name)
				vars[name] = value
			}
			atoms[i] = Atom{Path: p.Path(), Vars: vars}
		}
		s.dictionary[key] = atoms
	}
	for p, key := range data.Grid {
		if _, ok := data.Dictionary[key]; !ok {
			continue
		}
		s.grid[p] = key
		s.Origin.X = min(s.Origin.X, p.X)
		s.Origin.Y = min(s.Origin.Y, p.Y)
		s.Origin.Z = min(s.Origin.Z, p.Z)
	}
	accepted = true
	s.tgm = data.IsTgm
	s.Identity.StructuralHash = s.structuralHash()
	return s, nil
}

// Stable across disk formatting and editor stable IDs. Channel comparison and
// pin identity depend on explicit authored data, not serialization spelling.
func (s *Source) structuralHash() string {
	h := sha256.New()
	e := json.NewEncoder(h)
	_ = e.Encode(s.Size)
	for _, p := range s.Points() {
		_ = e.Encode(p)
		for _, atom := range s.atomsAt(p) {
			if atom.Vars == nil {
				atom.Vars = map[string]string{}
			}
			_ = e.Encode(atom)
		}
		_ = e.Encode(nil)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Source) Close() {
	if s != nil {
		s.lease.Release()
	}
}
func (s *Source) CheckFresh() error {
	if s.Identity.DocumentID != "" {
		return nil
	}
	return s.disk.Check(s.Identity.Path)
}
func (s *Source) atomsAt(p util.Point) []Atom { return s.dictionary[s.grid[p]] }
func cloneAtoms(atoms []Atom) []Atom {
	result := make([]Atom, len(atoms))
	for i, a := range atoms {
		result[i] = Atom{Path: a.Path, Vars: maps.Clone(a.Vars)}
	}
	return result
}
func (s *Source) Cell(p util.Point) []Atom { return cloneAtoms(s.atomsAt(p)) }

// DisplayMap is a private projection: it has no source filepath, backup,
// authoritative document, command history, selection, or save route.
func (s *Source) DisplayMap(ctx context.Context) (*dmmap.Dmm, error) {
	d := &dmmap.Dmm{MaxX: s.Size.X, MaxY: s.Size.Y, MaxZ: s.Size.Z}
	d.Tiles = make([]*dmmap.Tile, d.MaxX*d.MaxY*d.MaxZ)
	prefabs := make(map[dmmdata.Key][]*dmmprefab.Prefab, len(s.dictionary))
	for key, atoms := range s.dictionary {
		for _, a := range atoms {
			v := &dmvars.MutableVariables{}
			for n, value := range a.Vars {
				v.Put(n, value)
			}
			vars := v.ToImmutable()
			if obj := s.Identity.Environment.Objects[a.Path]; obj != nil {
				vars.LinkParent(obj.Vars)
			}
			prefabs[key] = append(prefabs[key], dmmprefab.New(0, a.Path, vars).Interned())
		}
	}
	for n := range d.Tiles {
		if n%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		p := util.Point{X: n%d.MaxX + 1, Y: n/d.MaxX%d.MaxY + 1, Z: n/(d.MaxX*d.MaxY) + 1}
		t := &dmmap.Tile{Coord: p}
		sourcePoint := p
		for _, prefab := range prefabs[s.grid[sourcePoint]] {
			t.InstancesAdd(prefab)
		}
		d.Tiles[n] = t
	}
	return d, nil
}

// BoundPath is for runtime-declared dependencies. Manually opened references
// may be elsewhere, but a config can never smuggle an absolute or escaping path.
func BoundPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" || strings.Contains(relative, ":") {
		return "", fmt.Errorf("invalid bound relative path %q", relative)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("reference path escapes project")
	}
	// Resolve existing prefixes too, so missing candidates under an escaping
	// symlink are diagnosed before any later source creation.
	probe := joined
	for {
		resolved, resolveErr := filepath.EvalSymlinks(probe)
		if resolveErr == nil {
			resolvedRoot, rootErr := filepath.EvalSymlinks(root)
			if rootErr != nil {
				return "", rootErr
			}
			within, e := filepath.Rel(resolvedRoot, resolved)
			if e != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("reference symlink escapes project")
			}
			break
		}
		if !os.IsNotExist(resolveErr) {
			return "", resolveErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", resolveErr
		}
		probe = parent
	}
	return joined, nil
}
