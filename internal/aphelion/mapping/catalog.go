package mapping

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/BurntSushi/toml"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/util"
)

type Diagnostic struct {
	Count                                       int
	Severity, Code, Message, Source, Occurrence string
	Local, Destination                          util.Point
}
type Candidate struct {
	Slot  int
	Path  string
	Error string
}
type Root struct {
	ID, Parent, Config, Key string
	StableID, BindingHash   string
	Source                  Identity
	Local, Destination      util.Point
	AtomIndex               int
	Candidates              []Candidate
}
type moduleConfig struct {
	reuseConfigData
	candidates map[string][]Candidate
}
type Catalog struct {
	mapName        string
	deferred       error
	accepted       map[string]AcceptedSource
	environment    *dmenv.Dme
	reuseCache     *ReuseCache
	assets         map[string]*Source
	assetLeases    map[string]*reuseSourceLease
	configs        map[string]*moduleConfig
	candidateCount int
}

func (c *Catalog) MapName() string { return c.mapName }

func NewCatalog(environment *dmenv.Dme) *Catalog {
	return &Catalog{environment: environment, assets: make(map[string]*Source), configs: make(map[string]*moduleConfig)}
}

// NewCatalogWithReuseCache borrows validated immutable disk sources from a
// session-owned cache. Accepted snapshots remain request-scoped.
func NewCatalogWithReuseCache(environment *dmenv.Dme, cache *ReuseCache) *Catalog {
	catalog := NewCatalog(environment)
	catalog.reuseCache = cache
	catalog.assetLeases = make(map[string]*reuseSourceLease)
	return catalog
}

func (c *Catalog) Close() {
	for key, s := range c.assets {
		s.Close()
		if lease := c.assetLeases[key]; lease != nil {
			lease.release()
		}
	}
	c.assets = nil
	c.assetLeases = nil
	c.configs = nil
	c.accepted = nil
}
func (c *Catalog) Load(ctx context.Context, path string) (*Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := sourceKey(path)
	if s := c.assets[key]; s != nil {
		if _, accepted := c.accepted[key]; !accepted || s.Identity.DocumentID != "" {
			return s, nil
		}
		// SetAcceptedSources normally precedes all loads. If the request was
		// rebound after a disk source entered this catalogue, discard that local
		// view before honoring the stronger accepted-source authority.
		s.Close()
		if lease := c.assetLeases[key]; lease != nil {
			lease.release()
			delete(c.assetLeases, key)
		}
		delete(c.assets, key)
	}
	if len(c.assets) >= 256 {
		return nil, fmt.Errorf("reference catalogue limit of 256 loaded sources reached")
	}
	accepted, hasAccepted := c.accepted[key]
	if !hasAccepted && c.reuseCache != nil && c.environment != nil {
		environmentHash, err := c.environment.EnvironmentHash()
		if err != nil {
			return nil, err
		}
		if source, lease := c.reuseCache.acquireSource(path, environmentHash); source != nil {
			if err := ctx.Err(); err != nil {
				lease.release()
				return nil, err
			}
			view := source.sharedView()
			c.assets[key] = view
			c.assetLeases[key] = lease
			return view, nil
		}
	}
	var s *Source
	var err error
	if hasAccepted {
		s, err = FromAccepted(ctx, path, c.environment, accepted)
	} else {
		s, err = LoadSource(ctx, path, c.environment)
	}
	if err == nil {
		if err = ctx.Err(); err != nil {
			s.Close()
			return nil, err
		}
		if !hasAccepted && c.reuseCache != nil {
			if shared, lease := c.reuseCache.publishSource(s); shared != nil {
				s = shared.sharedView()
				c.assetLeases[key] = lease
			}
		}
		c.assets[key] = s
	}
	if errors.Is(err, ErrAcceptedDeferred) {
		c.deferred = err
	}
	return s, err
}

func (s *Source) isType(path, base string) bool {
	if path == base {
		return true
	}
	for object, depth := s.Identity.Environment.Objects[path], 0; object != nil && depth < 512; object, depth = object.Parent(), depth+1 {
		if object.Path == base {
			return true
		}
	}
	return false
}
func (s *Source) effective(atom Atom, name string) (string, bool) {
	if value, ok := atom.Vars[name]; ok {
		return value, true
	}
	if object := s.Identity.Environment.Objects[atom.Path]; object != nil && object.Vars != nil {
		return object.Vars.Value(name)
	}
	return "", false
}
func constantString(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	return strconv.Unquote(value)
}

func (s *Source) Points() []util.Point {
	points := make([]util.Point, 0, len(s.grid))
	for p := range s.grid {
		points = append(points, p)
	}
	sort.Slice(points, func(i, j int) bool {
		a, b := points[i], points[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return points
}
func (c *Catalog) Roots(ctx context.Context, s *Source, transform Transform, parent string) (roots []Root, diagnostics []Diagnostic) {
	for _, point := range s.Points() {
		if ctx.Err() != nil {
			return roots, append(diagnostics, Diagnostic{Severity: "error", Code: "cancelled", Message: ctx.Err().Error()})
		}
		for index, atom := range s.atomsAt(point) {
			if !s.isType(atom.Path, "/obj/modular_map_root") {
				continue
			}
			r := Root{Parent: parent, Source: s.Identity, Local: point, Destination: transform.Apply(point), AtomIndex: index, StableID: atom.StableID}
			r.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d,%d,%d|%d", parent, sourceKey(s.Identity.Path), s.Identity.StructuralHash, s.Identity.EnvironmentHash, point.X, point.Y, point.Z, index))))
			if s.Identity.DocumentID != "" && atom.StableID != "" {
				r.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%q|%q|%d|%q", parent, s.Identity.DocumentID, s.Identity.Generation, atom.StableID))))
			}
			configValue, _ := s.effective(atom, "config_file")
			keyValue, _ := s.effective(atom, "key")
			configName, configErr := constantString(configValue)
			r.Key, _ = constantString(keyValue)
			if configErr != nil || r.Key == "" {
				diagnostics = append(diagnostics, Diagnostic{Severity: "error", Code: "unresolved-root", Message: "Root config_file/key is not a resolved string", Source: s.Identity.Path, Occurrence: r.ID, Local: point, Destination: r.Destination})
				roots = append(roots, r)
				continue
			}
			path, err := BoundPath(c.environment.RootDir, configName)
			r.Config = path
			var config *moduleConfig
			if err == nil {
				config = c.configs[path]
				if config == nil {
					config, err = c.loadModuleConfig(path, s.Identity.EnvironmentHash)
					if err == nil {
						c.configs[path] = config
					}
				}
			}
			if err == nil {
				r.BindingHash = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%q|%q|%q", path, config.hash, r.Key))))
				if s.Identity.DocumentID == "" || atom.StableID == "" {
					r.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(r.ID+"|"+path+"|"+config.hash+"|"+r.Key)))
				}
				names := config.Rooms[r.Key].Modules
				cached, exists := config.candidates[r.Key]
				if len(names) > 1024 || (!exists && c.candidateCount+len(names) > 16384) {
					err = fmt.Errorf("candidate budget exceeded (1024 per key; 16384 unique bound slots)")
				} else if exists {
					r.Candidates = cached
				} else {
					for slot, name := range names {
						if err = ctx.Err(); err != nil {
							break
						}
						p, e := BoundPath(c.environment.RootDir, filepath.ToSlash(filepath.Join(config.Directory, name)))
						candidate := Candidate{Slot: slot, Path: p}
						if e != nil {
							candidate.Error = e.Error()
						} else if _, e = os.Stat(p); e != nil {
							candidate.Error = e.Error()
						}
						r.Candidates = append(r.Candidates, candidate)
					}
					if err == nil {
						config.candidates[r.Key] = r.Candidates
						c.candidateCount += len(r.Candidates)
					}
				}
				if len(r.Candidates) == 0 {
					err = fmt.Errorf("key %q has no candidate slots", r.Key)
				}
			}
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Severity: "error", Code: "config", Message: err.Error(), Source: r.Config, Occurrence: r.ID, Local: point, Destination: r.Destination})
			}
			roots = append(roots, r)
			if len(roots) >= 4096 {
				return roots, append(diagnostics, Diagnostic{Severity: "error", Code: "root-budget", Message: "Root occurrence limit reached"})
			}
		}
	}
	return
}

func (c *Catalog) loadModuleConfig(path, environmentHash string) (*moduleConfig, error) {
	input, err := readSmallConfig(path)
	if err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(input))
	if data := c.reuseCache.cachedConfig(path, environmentHash, hash); data != nil {
		return &moduleConfig{reuseConfigData: *data, candidates: make(map[string][]Candidate)}, nil
	}
	data := &reuseConfigData{hash: hash}
	if _, err := toml.Decode(string(input), data); err != nil {
		return nil, err
	}
	weight := uint64(len(input))*8 + 1024
	if c.reuseCache != nil {
		data = c.reuseCache.publishConfig(path, environmentHash, data, weight)
	}
	return &moduleConfig{reuseConfigData: *data, candidates: make(map[string][]Candidate)}, nil
}

func (s *Source) Connectors() []util.Point {
	var result []util.Point
	for _, p := range s.Points() {
		for _, a := range s.atomsAt(p) {
			if s.isType(a.Path, "/obj/modular_map_connector") {
				result = append(result, p)
			}
		}
	}
	return result
}
func Anchor(s *Source, root util.Point) (Transform, error) {
	if s.Size.Z != 1 || s.Origin != (util.Point{X: 1, Y: 1, Z: 1}) || len(s.grid) != s.Size.X*s.Size.Y {
		return Transform{}, fmt.Errorf("runtime-bound module requires single Z and origin (1,1,1)")
	}
	anchors := s.Connectors()
	if len(anchors) != 1 {
		return Transform{}, fmt.Errorf("module has %d placed connectors; exactly one is required", len(anchors))
	}
	c := anchors[0]
	return Transform{Offset: util.Point{X: root.X - c.X, Y: root.Y - c.Y, Z: root.Z - 1}}, nil
}
