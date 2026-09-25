package mapping

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

var ErrAcceptedDeferred = errors.New("accepted source refresh deferred")

// AcceptedSource pins the editor's committed revision. Current is checked only
// on the UI thread before publishing; Snapshot is materialized on the worker.
type AcceptedSource struct {
	Snapshot interface {
		Snapshot() model.Snapshot
		EstimatedBytes() uint64
	}
	Generation uint64
	Current    func() bool
	Err        error
}

func FromAccepted(ctx context.Context, path string, env *dmenv.Dme, accepted AcceptedSource) (*Source, error) {
	if accepted.Err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAcceptedDeferred, accepted.Err)
	}
	if accepted.Snapshot == nil || env == nil {
		return nil, fmt.Errorf("accepted source is unavailable")
	}
	lease, err := resources.DefaultBudget().Reserve(accepted.Snapshot.EstimatedBytes()*3 + 1<<20)
	if err != nil {
		return nil, err
	}
	published := false
	defer func() {
		if !published {
			lease.Release()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := accepted.Snapshot.Snapshot()
	hash, err := snapshot.Hash()
	if err != nil {
		return nil, err
	}
	envHash, err := env.EnvironmentHash()
	if err != nil {
		return nil, err
	}
	if envHash != snapshot.EnvironmentHash {
		return nil, fmt.Errorf("accepted source belongs to another environment")
	}
	count, err := snapshot.CellCount()
	if err != nil || count > 2_000_000 {
		return nil, fmt.Errorf("accepted source exceeds cell limit")
	}
	var atoms uint64
	for _, tile := range snapshot.Tiles {
		atoms += uint64(len(tile.State.Prefabs))
	}
	if atoms > 4_000_000 {
		return nil, fmt.Errorf("accepted source exceeds placed-atom limit")
	}
	if err := lease.Resize(max(lease.Bytes(), uint64(count)*256+atoms*192)); err != nil {
		return nil, err
	}
	s := &Source{Identity: Identity{Path: path, ContentHash: hash, EnvironmentHash: envHash, Environment: env, DocumentID: string(snapshot.DocumentID), Revision: uint64(snapshot.Revision), Generation: accepted.Generation}, Size: util.Point{X: snapshot.MaxX, Y: snapshot.MaxY, Z: snapshot.MaxZ}, Origin: util.Point{X: 1, Y: 1, Z: 1}, grid: make(map[util.Point]dmmdata.Key, len(snapshot.Tiles)), dictionary: make(map[dmmdata.Key][]Atom, len(snapshot.Tiles)), lease: lease}
	for i, tile := range snapshot.Tiles {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		key := dmmdata.Key(strconv.Itoa(i))
		s.grid[util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}] = key
		values := make([]Atom, len(tile.State.Prefabs))
		for j, prefab := range tile.State.Prefabs {
			values[j] = Atom{Path: prefab.Path, Vars: maps.Clone(prefab.Vars)}
		}
		s.dictionary[key] = values
	}
	published = true
	s.Identity.StructuralHash = s.structuralHash()
	return s, nil
}

// SetAcceptedSources installs a request-scoped override. It never changes disk
// inputs and cannot supply a source outside the caller's requested path.
func (c *Catalog) SetAcceptedSources(sources map[string]AcceptedSource) {
	c.accepted = make(map[string]AcceptedSource, len(sources))
	for path, source := range sources {
		c.accepted[sourceKey(path)] = source
	}
}
func sourceKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
func (c *Catalog) SourcePaths() []string {
	var paths []string
	for _, source := range c.assets {
		paths = append(paths, source.Identity.Path)
	}
	return paths
}

// Called on the UI thread after the worker transfers catalogue ownership.
func (c *Catalog) AcceptedCurrent() bool {
	for path := range c.accepted {
		if c.assets[path] == nil {
			delete(c.accepted, path)
			continue
		}
		accepted := c.accepted[path]
		if accepted.Current != nil && !accepted.Current() {
			return false
		}
	}
	return true
}

func (c *Catalog) DeferredError() error { return c.deferred }
