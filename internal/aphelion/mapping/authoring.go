package mapping

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

type AuthoringProposal struct {
	sourceDone                     bool
	restage                        func() error
	changePreview                  string
	SourcePath, ConfigPath         string
	Width, Height                  int
	Origin                         util.Point
	Connector                      *util.Point
	Summary                        string
	output                         *dmmdata.DmmData
	config                         []byte
	sourceExpected, configExpected diskversion.State
	lease                          *resources.Reservation
	environmentHash                string
}

func (p *AuthoringProposal) ChangePreview() string { return p.changePreview }
func (p *AuthoringProposal) SourceWritten() bool   { return p.sourceDone }
func (p *AuthoringProposal) RestageConfiguration() error {
	if !p.sourceDone || p.restage == nil {
		return fmt.Errorf("no partial configuration write to recover")
	}
	if err := p.sourceExpected.Check(p.SourcePath); err != nil {
		return err
	}
	summary := p.Summary
	err := p.restage()
	p.Summary = summary
	return err
}

func insertedPreview(before, after []byte) string {
	start := 0
	for start < len(before) && start < len(after) && before[start] == after[start] {
		start++
	}
	endBefore, endAfter := len(before), len(after)
	for endBefore > start && endAfter > start && before[endBefore-1] == after[endAfter-1] {
		endBefore--
		endAfter--
	}
	return "Removed: " + string(before[start:endBefore]) + "\nInserted: " + string(after[start:endAfter])
}

// AddOverlayRecipe appends a new named declaration without rewriting existing
// tables. Coordinates and trait name are explicit user inputs, not world Z.
func (p *AuthoringProposal) AddOverlayRecipe(root, configName, name, requiredMap, trait string, destination util.Point) error {
	if p.sourceExpected.Exists() && !p.sourceDone {
		return fmt.Errorf("an overlay recipe requires a separate new source file")
	}
	if name == "" || len(name) > 128 || requiredMap == "" || filepath.Base(requiredMap) != requiredMap || trait == "" || destination.X < 1 || destination.Y < 1 || destination.Z < 1 {
		return fmt.Errorf("supply a name, map filename, trait and positive trait-relative coordinates")
	}
	path, err := BoundPath(root, configName)
	if err != nil {
		return err
	}
	input, state, err := readConfigVersion(path)
	if err != nil {
		return err
	}
	var before map[string]any
	if _, err = toml.Decode(string(input), &before); err != nil {
		return err
	}
	if templates, ok := before["templates"].(map[string]any); ok {
		if _, exists := templates[name]; exists {
			return fmt.Errorf("overlay name already exists")
		}
	}
	directory, err := filepath.Rel(root, filepath.Dir(p.SourcePath))
	if err != nil {
		return err
	}
	if _, err = BoundPath(root, filepath.Join(directory, filepath.Base(p.SourcePath))); err != nil {
		return err
	}
	if directory == "." {
		directory = ""
	}
	line := "\n"
	if strings.Contains(string(input), "\r\n") {
		line = "\r\n"
	}
	addition := fmt.Sprintf("\n[templates.%s]\ndirectory = %s\nmap_files = [%s]\nrequired_map = %s\ncoordinates = [%d, %d, %d]\ntrait_name = %s\n", strconv.Quote(name), strconv.Quote(filepath.ToSlash(directory)), strconv.Quote(filepath.Base(p.SourcePath)), strconv.Quote(requiredMap), destination.X, destination.Y, destination.Z, strconv.Quote(trait))
	output := append(append([]byte(nil), input...), []byte(strings.ReplaceAll(addition, "\n", line))...)
	var after map[string]any
	if _, err = toml.Decode(string(output), &after); err != nil {
		return err
	}
	templates, ok := after["templates"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid overlay declaration")
	}
	delete(templates, name)
	if _, exists := before["templates"]; !exists && len(templates) == 0 {
		delete(after, "templates")
	}
	if !reflect.DeepEqual(before, after) {
		return fmt.Errorf("overlay patch changed unrelated values")
	}
	p.ConfigPath = path
	p.config = output
	p.configExpected = state
	p.changePreview = insertedPreview(input, output)
	p.restage = func() error { return p.AddOverlayRecipe(root, configName, name, requiredMap, trait, destination) }
	p.Summary += fmt.Sprintf(" New overlay %s, map %s, trait %s at %d,%d,%d (trait-relative Z; world Z unresolved).", name, requiredMap, trait, destination.X, destination.Y, destination.Z)
	return nil
}

func (p *AuthoringProposal) ConfigurationPreview() string { return string(p.config) }

type AuthoringResult struct {
	SourceWritten, ConfigWritten bool
	Err                          error
}

func (p *AuthoringProposal) Close() {
	if p != nil {
		p.lease.Release()
	}
}

// PrepareTemplateExport copies accepted selected cells. Holes preserve both
// channels with explicit noops. It never removes or modifies the source room.
func PrepareTemplateExport(ctx context.Context, path string, snapshot model.Snapshot, selection editing.Selection, connector *util.Point) (*AuthoringProposal, error) {
	if selection.Len() == 0 {
		return nil, fmt.Errorf("select source cells first")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	b := selection.Bounds()
	origin := util.Point{X: int(b.X1), Y: int(b.Y1), Z: selection.Level()}
	w, h := int(b.X2-b.X1)+1, int(b.Y2-b.Y1)+1
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 2_000_000 {
		return nil, fmt.Errorf("export rectangle exceeds two million cells")
	}
	if !snapshot.Contains(model.Coord{X: origin.X, Y: origin.Y, Z: origin.Z}) || !snapshot.Contains(model.Coord{X: int(b.X2), Y: int(b.Y2), Z: origin.Z}) {
		return nil, fmt.Errorf("selection is outside the accepted source")
	}
	if connector != nil && !selection.Contains(*connector) {
		return nil, fmt.Errorf("connector must be on a selected source cell")
	}
	lease, err := resources.DefaultBudget().Reserve(uint64(w*h)*1024 + uint64(len(snapshot.Tiles))*128)
	if err != nil {
		return nil, err
	}
	p := &AuthoringProposal{SourcePath: path, Width: w, Height: h, Origin: origin, lease: lease, environmentHash: snapshot.EnvironmentHash, Summary: "Separate template export; source room remains unchanged. No area-spawn results are embedded."}
	if connector != nil {
		p.Connector = &util.Point{X: connector.X - origin.X + 1, Y: connector.Y - origin.Y + 1, Z: 1}
	}
	ok := false
	defer func() {
		if !ok {
			p.Close()
		}
	}()
	p.sourceExpected, err = diskversion.Capture(path)
	if err != nil {
		return nil, err
	}
	lookup := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	estimate := lease.Bytes()
	for _, tile := range snapshot.Tiles {
		lookup[tile.Coord] = tile.State
		if selection.Contains(util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}) {
			bytes := engine.EstimateTileStateBytes(tile.State)
			if bytes > (^uint64(0)-estimate)/8 {
				return nil, fmt.Errorf("export estimate exceeds addressable memory")
			}
			estimate += bytes * 8
		}
	}
	if err := lease.Resize(estimate); err != nil {
		return nil, err
	}
	out := snapshot
	out.MaxX = w
	out.MaxY = h
	out.MaxZ = 1
	out.Tiles = make([]model.Tile, 0, w*h)
	newAtom := func(path string) model.PrefabState {
		id, _ := model.NewStableID()
		return model.PrefabState{StableID: id, Path: path, Vars: map[string]string{}}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			point := util.Point{X: origin.X + x, Y: origin.Y + y, Z: origin.Z}
			state := model.TileState{}
			if selection.Contains(point) {
				state = model.CloneTileState(lookup[model.Coord{X: point.X, Y: point.Y, Z: point.Z}])
			} else {
				state.Prefabs = []model.PrefabState{newAtom("/turf/template_noop"), newAtom("/area/template_noop")}
			}
			if connector != nil && point == *connector {
				state.Prefabs = append(state.Prefabs, newAtom("/obj/modular_map_connector"))
			}
			out.Tiles = append(out.Tiles, model.Tile{Coord: model.Coord{X: x + 1, Y: y + 1, Z: 1}, State: state})
		}
	}
	p.output, err = mapadapter.Export(out, path, true, "\n")
	if err != nil {
		return nil, err
	}
	ok = true
	return p, nil
}

// Apply reports per-file progress. A successful source write followed by a
// configuration conflict is intentionally recoverable partial completion.
func (p *AuthoringProposal) Apply(ctx context.Context) AuthoringResult {
	r := AuthoringResult{SourceWritten: p.sourceDone}
	if err := ctx.Err(); err != nil {
		r.Err = err
		return r
	}
	if err := p.sourceExpected.Check(p.SourcePath); err != nil {
		r.Err = err
		return r
	}
	if p.ConfigPath != "" {
		if err := p.configExpected.Check(p.ConfigPath); err != nil {
			r.Err = err
			return r
		}
	}
	if !p.sourceDone {
		var written diskversion.State
		written, r.Err = dmmdata.SaveAtomicWithState(p.SourcePath, p.output.WriteTGM, p.output.ValidateSaved, p.sourceExpected)
		if r.Err != nil {
			return r
		}
		p.sourceDone = true
		p.sourceExpected = written
	}
	r.SourceWritten = true
	if p.ConfigPath == "" {
		return r
	}
	if err := ctx.Err(); err != nil {
		r.Err = err
		return r
	}
	_, r.Err = dmmdata.SaveAtomicWithState(p.ConfigPath, func(w io.Writer) error { _, err := w.Write(p.config); return err }, func(path string) error {
		data, err := readSmallConfig(path)
		if err != nil {
			return err
		}
		var parsed map[string]any
		_, err = toml.Decode(string(data), &parsed)
		return err
	}, p.configExpected)
	r.ConfigWritten = r.Err == nil
	return r
}

func readConfigVersion(path string) ([]byte, diskversion.State, error) {
	entry, err := os.Lstat(path)
	if err != nil {
		return nil, diskversion.State{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, diskversion.State{}, err
	}
	defer func() { _ = f.Close() }()
	r := diskversion.NewReader(f)
	data, err := io.ReadAll(io.LimitReader(r, (4<<20)+1))
	if err != nil {
		return nil, diskversion.State{}, err
	}
	if len(data) > 4<<20 {
		return nil, diskversion.State{}, fmt.Errorf("configuration exceeds 4 MiB")
	}
	info, err := f.Stat()
	if err != nil {
		return nil, diskversion.State{}, err
	}
	state, err := r.State(info, entry)
	if err == nil {
		err = state.Check(path)
	}
	return data, state, err
}

func (p *AuthoringProposal) AddModuleRecipe(root, configName, key string, environment *dmenv.Dme) error {
	if environment == nil {
		return fmt.Errorf("module recipe requires its source environment")
	}
	hash, err := environment.EnvironmentHash()
	if err != nil || hash != p.environmentHash {
		return fmt.Errorf("module recipe environment changed")
	}
	if p.sourceExpected.Exists() && !p.sourceDone {
		return fmt.Errorf("a variant recipe requires a separate new source file")
	}
	path, err := BoundPath(root, configName)
	if err != nil {
		return err
	}
	input, state, err := readConfigVersion(path)
	if err != nil {
		return err
	}
	var config moduleConfig
	if _, err = toml.Decode(string(input), &config); err != nil {
		return err
	}
	directory := root
	if config.Directory != "" {
		directory, err = BoundPath(root, config.Directory)
		if err != nil {
			return err
		}
	}
	relative, err := filepath.Rel(directory, p.SourcePath)
	if err != nil {
		return err
	}
	bound, err := BoundPath(directory, relative)
	if err != nil || sourceKey(bound) != sourceKey(p.SourcePath) {
		return fmt.Errorf("new source must be inside the configured module directory")
	}
	// Runtime-bound modules require one actual connector, including existing ones.
	connectors := 0
	types := &Source{Identity: Identity{Environment: environment}}
	for _, key := range p.output.Grid {
		for _, prefab := range p.output.Dictionary[key] {
			if types.isType(prefab.Path(), "/obj/modular_map_connector") {
				connectors++
			}
		}
	}
	if connectors != 1 {
		return fmt.Errorf("variant export has %d connectors; exactly one is required", connectors)
	}
	output, err := appendModuleSlot(input, key, filepath.ToSlash(relative))
	if err != nil {
		return err
	}
	p.ConfigPath = path
	p.config = output
	p.configExpected = state
	p.changePreview = insertedPreview(input, output)
	p.restage = func() error { return p.AddModuleRecipe(root, configName, key, environment) }
	p.Summary += " Append one candidate slot to " + configName + " / " + key + ". Existing root and base substrate are unchanged; converting a room requires a separate cleanup proposal."
	return nil
}

var moduleAssignment = regexp.MustCompile(`(?m)^[ \t]*modules[ \t]*=[ \t]*\[`)

// Only the array's insertion point changes. Decode-before/after comparison
// verifies all other TOML values, while original text retains comments/order.
func appendModuleSlot(input []byte, key, name string) ([]byte, error) {
	var before map[string]any
	if _, err := toml.Decode(string(input), &before); err != nil {
		return nil, err
	}
	start, end, offset := -1, len(input), 0
	for _, line := range strings.SplitAfter(string(input), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			if start >= 0 {
				end = offset
				break
			}
			var section moduleConfig
			if _, err := toml.Decode(trim+"\nmodules=[]\n", &section); err == nil {
				if _, ok := section.Rooms[key]; ok {
					start = offset + len(line)
				}
			}
		}
		offset += len(line)
	}
	if start < 0 {
		return nil, fmt.Errorf("supported explicit rooms.%s table was not found", key)
	}
	match := moduleAssignment.FindIndex(input[start:end])
	if match == nil {
		return nil, fmt.Errorf("explicit modules array was not found")
	}
	open := start + match[1] - 1
	quote := byte(0)
	escaped, comment := false, false
	last := byte('[')
	close := -1
	for i := open + 1; i < end; i++ {
		ch := input[i]
		if comment {
			if ch == '\n' {
				comment = false
			}
			continue
		}
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
				last = ch
			}
			continue
		}
		if ch == '#' {
			comment = true
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == ']' {
			close = i
			break
		}
		if ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n' {
			last = ch
		}
	}
	if close < 0 {
		return nil, fmt.Errorf("modules array end is unresolved")
	}
	newline := "\n"
	if strings.Contains(string(input), "\r\n") {
		newline = "\r\n"
	}
	insertion := ""
	if last != '[' && last != ',' {
		insertion = ","
	}
	insertion += newline + " " + strconv.Quote(name) + newline
	output := []byte(string(input[:close]) + insertion + string(input[close:]))
	var after map[string]any
	if _, err := toml.Decode(string(output), &after); err != nil {
		return nil, err
	}
	rooms, ok := before["rooms"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid rooms table")
	}
	room, ok := rooms[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing room")
	}
	items, ok := room["modules"].([]any)
	if !ok {
		return nil, fmt.Errorf("unsupported modules array")
	}
	room["modules"] = append(items, name)
	if !reflect.DeepEqual(before, after) {
		return nil, fmt.Errorf("configuration patch changed unrelated values")
	}
	return output, nil
}
