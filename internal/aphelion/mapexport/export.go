// Package mapexport writes a selected region of a map to a new map file.
//
// The region is cropped from an owned editor snapshot, so the export sees one
// committed revision. Writing goes through dmmsave, which stages, reparses,
// validates and atomically replaces the destination like an ordinary save.
package mapexport

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/mapsave"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmsave"
	"sdmm/internal/util"
)

// ErrSourceDestination reports an export aimed at the open map or its backup.
var ErrSourceDestination = errors.New("cannot export over the currently open map")

// Region is an inclusive rectangle on one z-level. Contains reports whether a
// cell inside the rectangle is selected; nil selects the whole rectangle.
type Region struct {
	X1, Y1, X2, Y2, Z int
	Contains          func(util.Point) bool
}

func (r Region) Width() int  { return r.X2 - r.X1 + 1 }
func (r Region) Height() int { return r.Y2 - r.Y1 + 1 }

// SelectionRegion describes a working selection. Selections are single-level.
func SelectionRegion(selection editing.Selection) (Region, error) {
	if selection.Len() == 0 || selection.Level() < 1 {
		return Region{}, fmt.Errorf("nothing is selected")
	}
	bounds := selection.Bounds()
	region := Region{X1: int(bounds.X1), Y1: int(bounds.Y1), X2: int(bounds.X2), Y2: int(bounds.Y2), Z: selection.Level()}
	if region.Width() < 1 || region.Height() < 1 {
		return Region{}, fmt.Errorf("selection bounds %s are empty", bounds.String())
	}
	if selection.Sparse() {
		region.Contains = selection.Contains
	}
	return region, nil
}

// Crop returns a one-level snapshot sized to the region with its lower-left
// cell at (1,1,1). Selected tiles keep every prefab verbatim, including stable
// IDs and types the environment does not know. Unselected cells inside the
// rectangle receive copies of filler with fresh stable IDs.
func Crop(source model.Snapshot, region Region, filler []model.PrefabState) (model.Snapshot, error) {
	if region.Width() < 1 || region.Height() < 1 {
		return model.Snapshot{}, fmt.Errorf("export region is empty")
	}
	if region.X1 < 1 || region.Y1 < 1 || region.X2 > source.MaxX || region.Y2 > source.MaxY || region.Z < 1 || region.Z > source.MaxZ {
		return model.Snapshot{}, fmt.Errorf("export region (%d,%d)-(%d,%d) on level %d is outside the map (%d,%d,%d)",
			region.X1, region.Y1, region.X2, region.Y2, region.Z, source.MaxX, source.MaxY, source.MaxZ)
	}
	width, height := region.Width(), region.Height()
	selected := func(x, y int) bool {
		return region.Contains == nil || region.Contains(util.Point{X: x, Y: y, Z: region.Z})
	}

	cropped := model.Snapshot{
		ProtocolVersion: source.ProtocolVersion,
		SchemaVersion:   source.SchemaVersion,
		DocumentID:      source.DocumentID,
		Revision:        source.Revision,
		EnvironmentHash: source.EnvironmentHash,
		MaxX:            width,
		MaxY:            height,
		MaxZ:            1,
		Tiles:           make([]model.Tile, 0, width*height),
	}
	placed := make([]bool, width*height)
	for _, tile := range source.Tiles {
		coord := tile.Coord
		if coord.Z != region.Z || coord.X < region.X1 || coord.X > region.X2 || coord.Y < region.Y1 || coord.Y > region.Y2 || !selected(coord.X, coord.Y) {
			continue
		}
		x, y := coord.X-region.X1+1, coord.Y-region.Y1+1
		index := (y-1)*width + (x - 1)
		if placed[index] {
			return model.Snapshot{}, fmt.Errorf("map snapshot repeats tile (%d,%d,%d)", coord.X, coord.Y, coord.Z)
		}
		placed[index] = true
		prefabs := make([]model.PrefabState, len(tile.State.Prefabs))
		for i, prefab := range tile.State.Prefabs {
			prefabs[i] = model.PrefabState{StableID: prefab.StableID, Path: prefab.Path, Vars: maps.Clone(prefab.Vars)}
		}
		cropped.Tiles = append(cropped.Tiles, model.Tile{Coord: model.Coord{X: x, Y: y, Z: 1}, State: model.TileState{Prefabs: prefabs}})
	}
	for y := 1; y <= height; y++ {
		for x := 1; x <= width; x++ {
			if placed[(y-1)*width+(x-1)] {
				continue
			}
			if selected(x+region.X1-1, y+region.Y1-1) {
				return model.Snapshot{}, fmt.Errorf("map snapshot is missing selected tile (%d,%d,%d)", x+region.X1-1, y+region.Y1-1, region.Z)
			}
			prefabs := make([]model.PrefabState, len(filler))
			for i, prefab := range filler {
				id, err := model.NewStableID()
				if err != nil {
					return model.Snapshot{}, err
				}
				prefabs[i] = model.PrefabState{StableID: id, Path: prefab.Path, Vars: maps.Clone(prefab.Vars)}
				if prefabs[i].Vars == nil {
					prefabs[i].Vars = map[string]string{}
				}
			}
			cropped.Tiles = append(cropped.Tiles, model.Tile{Coord: model.Coord{X: x, Y: y, Z: 1}, State: model.TileState{Prefabs: prefabs}})
		}
	}
	if err := cropped.Validate(); err != nil {
		return model.Snapshot{}, fmt.Errorf("validate exported region: %w", err)
	}
	return cropped, nil
}

// Target names where and how an export is written.
type Target struct {
	// Path is the new map file chosen by the user.
	Path string
	// SourcePath and SourceBackup identify the open map; neither is ever written.
	SourcePath   string
	SourceBackup string
	// SourceBackup also supplies the format (DMM or TGM), line endings and key
	// length, so the export matches the map it came from.
	SanitizeVariables bool
	// Expected is the destination's disk state when the user chose it:
	// diskversion.Absent() for a new file, or the captured state of a file the
	// user agreed to replace. A destination that changed since is refused.
	Expected diskversion.State
}

// Write saves a cropped snapshot through the staged, validated atomic save path.
func Write(environment *dmenv.Dme, cropped model.Snapshot, target Target) (diskversion.State, error) {
	if strings.TrimSpace(target.Path) == "" {
		return diskversion.State{}, fmt.Errorf("export destination is empty")
	}
	path, err := filepath.Abs(target.Path)
	if err != nil {
		return diskversion.State{}, fmt.Errorf("resolve export destination: %w", err)
	}
	for _, protected := range []string{target.SourcePath, target.SourceBackup} {
		if SamePath(path, protected) {
			return diskversion.State{}, fmt.Errorf("%w: %s", ErrSourceDestination, path)
		}
	}
	name := filepath.Base(path)
	document, err := mapsave.Project(cropped, mapsave.Metadata{
		Name:   name,
		Path:   dmmap.DmmPath{Readable: name, Absolute: path},
		Backup: target.SourceBackup,
	})
	if err != nil {
		return diskversion.State{}, err
	}
	config := dmmsave.Config{Format: dmmsave.FormatInitial, SanitizeVariables: target.SanitizeVariables}
	// The projection is private to this call, so the owned save may consume it.
	return dmmsave.SaveOwnedVWithDiskState(environment, document, path, config, target.Expected)
}

// SamePath reports whether two paths name the same file, honoring Windows
// case-insensitivity and links when both exist.
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	absA, absB = filepath.Clean(absA), filepath.Clean(absB)
	if absA == absB || runtime.GOOS == "windows" && strings.EqualFold(absA, absB) {
		return true
	}
	infoA, errA := os.Stat(absA)
	infoB, errB := os.Stat(absB)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}
