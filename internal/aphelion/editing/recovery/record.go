// Package recovery preserves a damaged editor display without normalizing it
// into a valid operation or assigning missing instance identities.
package recovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

type variable struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Present bool   `json:"present"`
}

type prefab struct {
	Path      string     `json:"path"`
	Variables []variable `json:"variables"` // null means missing variable storage.
}

type instance struct {
	LocalID  uint64      `json:"local_id"`
	StableID string      `json:"stable_id"`
	Coord    model.Coord `json:"coord"`
	Prefab   *prefab     `json:"prefab"`
}

type tile struct {
	Coord     model.Coord `json:"coord"`
	Instances []*instance `json:"instances"`
}

type archive struct {
	Format            string         `json:"format"`
	Version           int            `json:"version"`
	RecordedAuthority model.Snapshot `json:"recorded_authority"`
	CapturedBefore    []model.Tile   `json:"captured_before"`
	MaxX              int            `json:"display_max_x"`
	MaxY              int            `json:"display_max_y"`
	MaxZ              int            `json:"display_max_z"`
	Display           []*tile        `json:"display"`
}

// Record is an immutable inspection/export reference. It contains map data only,
// never source filenames, connection configuration or free-form error messages.
type Record struct {
	encoded []byte
	preview string
}

func Capture(dmm *dmmap.Dmm, authority model.Snapshot, before map[model.Coord]model.TileState) (*Record, error) {
	if dmm == nil {
		return nil, fmt.Errorf("recovery display is unavailable")
	}
	a := archive{Format: "aphelion-local-edit-recovery", Version: 1, RecordedAuthority: model.CloneSnapshot(authority),
		MaxX: dmm.MaxX, MaxY: dmm.MaxY, MaxZ: dmm.MaxZ, Display: make([]*tile, len(dmm.Tiles))}
	for coord, state := range before {
		a.CapturedBefore = append(a.CapturedBefore, model.Tile{Coord: coord, State: model.CloneTileState(state)})
	}
	sort.Slice(a.CapturedBefore, func(i, j int) bool {
		l, r := a.CapturedBefore[i].Coord, a.CapturedBefore[j].Coord
		if l.Z != r.Z {
			return l.Z < r.Z
		}
		if l.Y != r.Y {
			return l.Y < r.Y
		}
		return l.X < r.X
	})
	for index, source := range dmm.Tiles {
		if source == nil {
			continue
		}
		t := &tile{Coord: model.Coord{X: source.Coord.X, Y: source.Coord.Y, Z: source.Coord.Z}, Instances: make([]*instance, len(source.Instances()))}
		a.Display[index] = t
		for index, source := range source.Instances() {
			if source == nil {
				continue
			}
			coord := source.Coord()
			i := &instance{LocalID: source.Id(), StableID: source.StableID(), Coord: model.Coord{X: coord.X, Y: coord.Y, Z: coord.Z}}
			t.Instances[index] = i
			if source.Prefab() == nil {
				continue
			}
			i.Prefab = &prefab{Path: source.Prefab().Path()}
			if vars := source.Prefab().Vars(); vars != nil {
				i.Prefab.Variables = make([]variable, 0, vars.Len())
				for _, name := range vars.Iterate() {
					value, ok := vars.Value(name)
					i.Prefab.Variables = append(i.Prefab.Variables, variable{name, value, ok})
				}
			}
		}
	}
	encoded, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, err
	}
	// JSON replaces invalid UTF-8. Refuse such an export instead of silently
	// claiming that altered bytes are a complete recovery reference.
	var roundTrip archive
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(a, roundTrip) {
		return nil, fmt.Errorf("recovery data cannot be represented losslessly as JSON")
	}
	return &Record{encoded: append(encoded, '\n'), preview: preview(a, before)}, nil
}

func (r *Record) Same(other *Record) bool {
	return r != nil && other != nil && bytes.Equal(r.encoded, other.encoded)
}
func (r *Record) Preview() string { return r.preview }

// Export does not submit, resolve, or discard anything. The retained immutable
// record remains exportable even if the editor subsequently changes or closes.
func (r *Record) Export(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return fmt.Errorf("choose a .json file for the recovery reference")
	}
	return dmmdata.SaveAtomic(path, func(w io.Writer) error {
		_, err := w.Write(r.encoded)
		return err
	}, func(staged string) error {
		actual, err := os.ReadFile(staged)
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, r.encoded) {
			return fmt.Errorf("staged recovery data differs from inspected contents")
		}
		return nil
	})
}

func preview(a archive, before map[model.Coord]model.TileState) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Display: %d x %d x %d; %d tile entries.\nRecorded authority: revision %d. Captured tiles: %d.\n", a.MaxX, a.MaxY, a.MaxZ, len(a.Display), a.RecordedAuthority.Revision, len(before))
	authority := make(map[model.Coord]model.TileState, len(a.RecordedAuthority.Tiles))
	for _, t := range a.RecordedAuthority.Tiles {
		authority[t.Coord] = t.State
	}
	shown, affected := 0, 0
	for index, t := range a.Display {
		var coord model.Coord
		if t != nil {
			coord = t.Coord
		}
		captured, pending := before[coord]
		if !pending && matches(t, authority[coord]) {
			continue
		}
		affected++
		if shown == 20 {
			continue
		}
		shown++
		fmt.Fprintf(&text, "\nTile entry %d (%d, %d, %d)\n", index, coord.X, coord.Y, coord.Z)
		for _, section := range []struct {
			name  string
			value any
		}{
			{"Current display", t}, {"Captured before (empty if uncaptured)", captured}, {"Recorded authority", authority[coord]},
		} {
			data, _ := json.MarshalIndent(section.value, "", "  ") // Only the JSON-safe fields already encoded above.
			fmt.Fprintf(&text, "%s:\n%s\n", section.name, bounded(string(data), 4096))
		}
	}
	fmt.Fprintf(&text, "\n%d changed or captured tile entries; %d shown. Export includes the complete display, captures and recorded authority.\n", affected, shown)
	return text.String()
}

func matches(t *tile, state model.TileState) bool {
	if t == nil || len(t.Instances) != len(state.Prefabs) {
		return false
	}
	for index, i := range t.Instances {
		p := state.Prefabs[index]
		if i == nil || i.Coord != t.Coord || i.StableID != string(p.StableID) || i.Prefab == nil || i.Prefab.Path != p.Path || i.Prefab.Variables == nil || len(i.Prefab.Variables) != len(p.Vars) {
			return false
		}
		for _, v := range i.Prefab.Variables {
			value, ok := p.Vars[v.Name]
			if !ok || !v.Present || v.Value != value {
				return false
			}
		}
	}
	return true
}

func bounded(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit] + "\n[Preview truncated; export for complete values]"
}
