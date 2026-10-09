package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/repath"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmvars"
)

type fakeTarget struct {
	revision uint64
	tiles    map[model.Coord]model.TileState
	canEdit  bool
	applied  []*repath.Transformer
	fail     error
}

func (t *fakeTarget) ID() string                { return "target" }
func (t *fakeTarget) Name() string              { return "test.dmm" }
func (t *fakeTarget) Path() string              { return "C:/maps/test.dmm" }
func (t *fakeTarget) Version() (uint64, uint64) { return 1, t.revision }
func (t *fakeTarget) CanEdit() bool             { return t.canEdit }
func (t *fakeTarget) CaptureInventory(known func(string) bool) (func(context.Context) (repath.Inventory, error), error) {
	tiles := t.tiles
	header := model.Snapshot{Revision: model.Revision(t.revision), MaxX: 2, MaxY: 1, MaxZ: 1}
	return func(ctx context.Context) (repath.Inventory, error) {
		return repath.BuildInventory(ctx, header, func(coord model.Coord) (model.TileState, bool) {
			state, ok := tiles[coord]
			return state, ok
		}, known)
	}, nil
}
func (t *fakeTarget) Apply(transformer *repath.Transformer, _ string, done func(repath.Report, error)) error {
	if t.fail != nil {
		return t.fail
	}
	t.applied = append(t.applied, transformer)
	report := repath.NewReport()
	for coord, tile := range t.tiles {
		after, changed, err := transformer.Tile(coord, tile, func() (model.StableID, error) { return model.NewStableID() }, repath.Defaults{}, &report)
		if err != nil {
			done(report, err)
			return nil
		}
		if changed {
			t.tiles[coord] = after
			report.Tiles++
		}
	}
	t.revision++
	done(report, nil)
	return nil
}

type fakeApp struct {
	env      *dmenv.Dme
	target   *fakeTarget
	settings repath.Settings
	memory   *repath.Memory
	saved    int
	searched []string
	later    chan func()
}

func (a *fakeApp) LoadedEnvironment() *dmenv.Dme { return a.env }
func (a *fakeApp) PathMigrationTarget() (Target, bool) {
	if a.target == nil {
		return nil, false
	}
	return a.target, true
}
func (a *fakeApp) PathMigrationSettings() *repath.Settings { return &a.settings }
func (a *fakeApp) PathMigrationMemory() *repath.Memory     { return a.memory }
func (a *fakeApp) SavePathMigrationMemory()                { a.saved++ }
func (a *fakeApp) ShowPathInSearch(path string)            { a.searched = append(a.searched, path) }
func (a *fakeApp) RunLater(job func())                     { a.later <- job }
func (a *fakeApp) BypassEnvironmentCache() bool            { return true }

// settle runs scheduled UI jobs and frames until the controller is idle.
func (a *fakeApp) settle(t *testing.T, c *Controller) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		c.Update()
		if !c.Busy() && !c.scriptsBusy && c.referenceCancel == nil && c.index != nil && (a.target == nil || c.installed.target != "") {
			select {
			case job := <-a.later:
				job()
				continue
			default:
				return
			}
		}
		select {
		case job := <-a.later:
			job()
		case <-deadline:
			t.Fatal("controller did not settle")
		}
	}
}

func testEnvironment(root string, paths ...string) *dmenv.Dme {
	objects := map[string]*dmenv.Object{}
	for _, path := range paths {
		variables := &dmvars.MutableVariables{}
		variables.Put("name", `"`+path+`"`)
		objects[path] = &dmenv.Object{Path: path, Vars: variables.ToImmutable()}
	}
	return &dmenv.Dme{RootDir: root, RootFile: filepath.Join(root, "test.dme"), Objects: objects}
}

func stableID(t *testing.T) model.StableID {
	id, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fixture(t *testing.T) (*fakeApp, *Controller) {
	t.Helper()
	root := t.TempDir()
	env := testEnvironment(root, "/area", "/turf", "/obj", "/obj/item", "/obj/item/stamp", "/obj/item/stamp/head", "/obj/item/stamp/head/captain", "/obj/machinery", "/obj/machinery/keycard_auth", "/obj/structure", "/obj/structure/sign")
	target := &fakeTarget{revision: 1, canEdit: true, tiles: map[model.Coord]model.TileState{
		{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{
			{StableID: stableID(t), Path: "/obj/item/stamp/captain", Vars: map[string]string{}},
			{StableID: stableID(t), Path: "/obj/scripted", Vars: map[string]string{"x": "1"}},
			{StableID: stableID(t), Path: "/turf", Vars: map[string]string{}},
		}},
		{X: 2, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{
			{StableID: stableID(t), Path: "/obj/nothing_like_it_qq", Vars: map[string]string{}},
			{StableID: stableID(t), Path: "/turf", Vars: map[string]string{}},
		}},
	}}
	scripts := filepath.Join(root, "tools", "UpdatePaths", "Scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "100_SIGN.txt"), []byte("/obj/scripted : /obj/structure/sign{@OLD}\n/bad line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := repath.DefaultSettings()
	app := &fakeApp{env: env, target: target, settings: settings, memory: repath.NewMemory(), later: make(chan func(), 64)}
	c := NewController(app)
	app.settle(t, c)
	return app, c
}

func row(t *testing.T, c *Controller, path string) Row {
	t.Helper()
	for _, row := range c.Rows() {
		if row.Proposal.Entry.Path == path {
			return row
		}
	}
	t.Fatalf("no row for %s", path)
	return Row{}
}

func TestControllerAnalyzesWithCodebaseScripts(t *testing.T) {
	_, c := fixture(t)
	overview := c.Overview()
	if overview.Types != 3 || overview.Scripts != 1 || overview.Rules != 1 || len(overview.ScriptErrors) != 1 {
		t.Fatalf("overview = %#v", overview)
	}
	if got := StatusOf(row(t, c, "/obj/scripted")); got != "Auto" {
		t.Fatalf("scripted status = %s", got)
	}
	if got := StatusOf(row(t, c, "/obj/item/stamp/captain")); got != "Unresolved" {
		t.Fatalf("stamp status = %s", got)
	}
	if paths, instances := c.Pending(); paths != 1 || instances != 1 {
		t.Fatalf("pending = %d %d", paths, instances)
	}
}

func TestControllerAppliesChoicesAndReanalyzes(t *testing.T) {
	app, c := fixture(t)
	c.Choose("/obj/item/stamp/captain", Choice{Kind: ChoiceCandidate, Candidate: 0})
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceCustom, Custom: "/obj/machinery/keycard_auth"})
	if !c.CanApply() {
		t.Fatal("cannot apply")
	}
	c.Remember = true
	app.settings.RememberDecisions = true
	c.Apply()
	if c.Error != "" || len(app.target.applied) != 1 || !strings.Contains(c.Status, "Migrated 3 instances") {
		t.Fatalf("status %q error %q", c.Status, c.Error)
	}
	app.settle(t, c)
	if len(c.Rows()) != 0 {
		t.Fatalf("rows after apply = %#v", c.Rows())
	}
	tile := app.target.tiles[model.Coord{X: 1, Y: 1, Z: 1}]
	if tile.Prefabs[0].Path != "/obj/item/stamp/head/captain" || tile.Prefabs[1].Path != "/obj/structure/sign" || tile.Prefabs[1].Vars["x"] != "1" {
		t.Fatalf("tile = %#v", tile.Prefabs)
	}
	if app.saved != 1 || c.RememberedCount() != 2 {
		t.Fatalf("saved %d remembered %d", app.saved, c.RememberedCount())
	}
}

func TestControllerKeepsPersonChoicesAcrossReanalysis(t *testing.T) {
	app, c := fixture(t)
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceKeep})
	app.target.revision++
	app.settle(t, c)
	if got := StatusOf(row(t, c, "/obj/nothing_like_it_qq")); got != "Kept" {
		t.Fatalf("status = %s", got)
	}
}

func TestControllerNeverRemembersUnlessEnabled(t *testing.T) {
	app, c := fixture(t)
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceCustom, Custom: "/obj/machinery/keycard_auth"})
	c.Remember = true
	c.Apply()
	if app.saved != 0 || len(app.memory.Environments) != 0 {
		t.Fatal("remembered with RememberDecisions disabled")
	}
}

func TestControllerReportsInvalidCustomTarget(t *testing.T) {
	app, c := fixture(t)
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceCustom, Custom: "/obj/not_defined"})
	c.Apply()
	if !strings.Contains(c.Error, "not defined") || len(app.target.applied) != 0 {
		t.Fatalf("error %q", c.Error)
	}
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceCustom, Custom: "/turf"})
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "root type") {
		t.Fatalf("cross-base err = %v", err)
	}
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceCustom, Custom: "/turf", AllowCrossBase: true})
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestControllerApplyFailureIsReported(t *testing.T) {
	app, c := fixture(t)
	app.target.fail = errors.New("busy")
	c.Apply()
	if c.Error != "busy" || c.applying {
		t.Fatalf("error %q applying %v", c.Error, c.applying)
	}
}

func TestApplyCertainOnOpen(t *testing.T) {
	root := t.TempDir()
	app, _ := fixture(t)
	app.settings.ApplyCertainOnOpen = true
	app.env = testEnvironment(root, "/turf", "/obj", "/obj/structure", "/obj/structure/sign", "/obj/item/stamp/head/captain")
	scripts := filepath.Join(root, "tools", "UpdatePaths", "Scripts")
	_ = os.MkdirAll(scripts, 0o755)
	_ = os.WriteFile(filepath.Join(scripts, "1_A.txt"), []byte("/obj/scripted : /obj/structure/sign{@OLD}\n"), 0o644)
	c := NewController(app)
	c.OfferFor("C:/maps/test.dmm")
	app.settle(t, c)
	if len(app.target.applied) != 1 || !strings.Contains(c.Status, "automatically") {
		t.Fatalf("applied %d status %q", len(app.target.applied), c.Status)
	}
	if got := app.target.tiles[model.Coord{X: 1, Y: 1, Z: 1}].Prefabs[0].Path; got != "/obj/item/stamp/captain" {
		t.Fatalf("a non-Certain choice was applied automatically: %s", got)
	}
}

func TestSaveCodebaseScriptIsConfinedAndOptIn(t *testing.T) {
	app, c := fixture(t)
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceCustom, Custom: "/obj/machinery/keycard_auth"})
	if _, err := c.SaveCodebaseScript("1_X.txt", false); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("err = %v", err)
	}
	app.settings.WriteCodebaseScripts = true
	for _, name := range []string{"../1_X.txt", "X.txt", "1_X.txt/../../a.txt"} {
		if _, err := c.SaveCodebaseScript(name, false); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	path, err := c.SaveCodebaseScript("200_PORT.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "/obj/nothing_like_it_qq : /obj/machinery/keycard_auth{@OLD}") {
		t.Fatalf("script = %q err %v", data, err)
	}
	if _, err := c.SaveCodebaseScript("200_PORT.txt", false); !errors.Is(err, ErrScriptExists) {
		t.Fatalf("overwrite err = %v", err)
	}
	if _, err := c.SaveCodebaseScript("200_PORT.txt", true); err != nil {
		t.Fatal(err)
	}
	app.settle(t, c)
	if c.Overview().Scripts != 2 {
		t.Fatal("saved script was not reloaded")
	}
}

func TestReferenceEnvironmentIsLoadedOffThreadAndCancellable(t *testing.T) {
	app, c := fixture(t)
	release := make(chan struct{})
	original := ParseEnvironment
	t.Cleanup(func() { ParseEnvironment = original })
	ParseEnvironment = func(ctx context.Context, path string, _ bool, progress func(string)) (*dmenv.Dme, error) {
		progress("Parsing")
		select {
		case <-release:
			return testEnvironment(filepath.Dir(path), "/obj", "/obj/item", "/obj/item/stamp", "/obj/item/stamp/captain"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c.LoadReference(app.env.RootFile)
	if _, _, _, err := c.ReferenceState(); !strings.Contains(err, "different environment") {
		t.Fatalf("err = %q", err)
	}
	c.LoadReference(filepath.Join(t.TempDir(), "other.dme"))
	if _, loading, progress, _ := c.ReferenceState(); !loading || progress == "" {
		t.Fatal("reference is not loading")
	}
	c.UnloadReference()
	close(release)
	app.settle(t, c)
	if path, loading, _, _ := c.ReferenceState(); path != "" || loading || c.reference != nil {
		t.Fatal("cancelled reference was installed")
	}
}

func TestFindAndCustomMatches(t *testing.T) {
	app, c := fixture(t)
	c.Find("/obj/scripted")
	if len(app.searched) != 1 {
		t.Fatal("find did not search")
	}
	if got := c.CustomMatches("/obj/item/stamp/"); len(got) != 2 || got[0] != "/obj/item/stamp/head" {
		t.Fatalf("matches = %v", got)
	}
}

func TestPersonChoicesReturnAfterUndo(t *testing.T) {
	app, c := fixture(t)
	before := map[model.Coord]model.TileState{}
	for coord, tile := range app.target.tiles {
		before[coord] = model.CloneTileState(tile)
	}
	c.Choose("/obj/nothing_like_it_qq", Choice{Kind: ChoiceCustom, Custom: "/obj/machinery/keycard_auth"})
	c.Apply()
	app.settle(t, c)
	if slices.ContainsFunc(c.Rows(), func(row Row) bool { return row.Proposal.Entry.Path == "/obj/nothing_like_it_qq" }) {
		t.Fatal("applied row is still listed")
	}
	app.target.tiles = before // undo
	app.target.revision++
	app.settle(t, c)
	if got := row(t, c, "/obj/nothing_like_it_qq").Choice; got.Kind != ChoiceCustom || got.Custom != "/obj/machinery/keycard_auth" {
		t.Fatalf("choice after undo = %#v", got)
	}
}
