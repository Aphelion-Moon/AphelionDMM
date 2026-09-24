package wsmap

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/command"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const largePasteTimeout = 3 * time.Minute

func newLargeSelectionWorkspace(t *testing.T, width, height, sparseStep int) (*WsMap, *selectionTestApp, string, *resources.Budget) {
	t.Helper()
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		t.Skip("set APHELIONDMM_GL_TEST=1 for whole-level workspace verification")
	}
	workspaceContext(t)
	ctx := imgui.CreateContext(nil)
	t.Cleanup(ctx.Destroy)
	dir := t.TempDir()
	path := filepath.Join(dir, "large-map.dmm")
	if err := os.WriteFile(path, largeMapSource(width, height, sparseStep), 0600); err != nil {
		t.Fatal(err)
	}
	objects := make(map[string]*dmenv.Object)
	for _, objectPath := range []string{"/world", "/area/foo", "/turf/foo", "/obj/unknown"} {
		variables := &dmvars.MutableVariables{}
		variables.Put("dir", "2")
		if objectPath == "/world" {
			variables.Put("area", "/area/foo")
			variables.Put("turf", "/turf/foo")
			variables.Put("icon_size", "32")
		}
		objects[objectPath] = &dmenv.Object{Path: objectPath, Vars: variables.ToImmutable()}
	}
	environment := &dmenv.Dme{RootDir: dir, Objects: objects}
	previousLogger := log.Logger
	log.Logger = zerolog.New(io.Discard)
	t.Cleanup(func() { log.Logger = previousLogger })
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	dmmap.Init(environment)
	t.Cleanup(dmmap.Free)
	data, err := dmmdata.New(path)
	if err != nil {
		t.Fatal(err)
	}
	mapState, _ := dmmap.New(environment, data, path)
	app := &selectionTestApp{
		saveTestApp: &saveTestApp{environment: environment, commands: command.NewStorage(), jobs: make(chan func(), 8)},
		clipboard:   dmmclip.New(),
	}
	app.commands.SetStack(path)
	ws := New(app, mapState)
	workBudget := resources.NewFixedBudget(2 << 30)
	if err := ws.Map().Editor().SetEditWorkBudget(workBudget); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ws.Map().Editor().Close)
	return ws, app, path, workBudget
}

func largeMapSource(width, height, sparseStep int) []byte {
	var source strings.Builder
	source.Grow(width*height + 4096)
	source.WriteString("\"a\" = (/area/foo,/turf/foo)\n")
	source.WriteString("\"b\" = (/area/foo,/turf/foo,/obj/unknown{custom = ")
	payload := "kept by paste and stamp"
	if sparseStep > 0 {
		payload = strings.Repeat("unknown mechanical value ", 96)
	}
	source.WriteString(strconv.Quote(payload))
	source.WriteString("})\n\n(1,1,1) = {\"\n")
	for y := height; y >= 1; y-- {
		for x := 1; x <= width; x++ {
			if sparseStep > 0 && (x-1)%sparseStep == 0 && (y-1)%sparseStep == 0 || sparseStep == 0 && x == 1 && y == 1 {
				source.WriteByte('b')
			} else {
				source.WriteByte('a')
			}
		}
		source.WriteByte('\n')
	}
	source.WriteString("\"}\n")
	return []byte(source.String())
}

func settleLargePaste(t *testing.T, ws *WsMap, app *selectionTestApp, target util.Point) {
	t.Helper()
	e := ws.Map().Editor()
	deadline := time.Now().Add(largePasteTimeout)
	for time.Now().Before(deadline) {
		for drained := 0; drained < cap(app.jobs); drained++ {
			select {
			case job := <-app.jobs:
				job()
			default:
				drained = cap(app.jobs)
			}
		}
		e.ProcessPasteWork()
		e.ProcessCollaborationUpdates()
		if grab, ok := tools.Selected().(*tools.ToolGrab); ok && grab.Placing() {
			// ToolGrab confirmation reads the canvas mouse independently of the
			// explicit target passed to UpdatePlacement. Seed it here so this
			// fixture is isolated from cursor state left by earlier GL tests.
			ws.Map().CanvasState().SetMousePosition((target.X-1)*32, (target.Y-1)*32, target.Z)
			grab.UpdatePlacement(target)
		}
		if e.PastePlacementProgress() == "" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("whole-level paste did not settle: %s", e.PastePlacementProgress())
}

func settleLargeHistory(t *testing.T, ws *WsMap, app *selectionTestApp, redo bool) {
	t.Helper()
	done := make(chan error, 1)
	complete := func(err error) { done <- err }
	var started bool
	if redo {
		started = app.commands.RedoAsyncV(ws.Map().Editor().Dmm().Path.Absolute, complete)
	} else {
		started = app.commands.UndoAsyncV(ws.Map().Editor().Dmm().Path.Absolute, complete)
	}
	if !started {
		t.Fatal("history operation was not admitted")
	}
	e := ws.Map().Editor()
	deadline := time.Now().Add(largePasteTimeout)
	for time.Now().Before(deadline) {
		for drained := 0; drained < cap(app.jobs); drained++ {
			select {
			case job := <-app.jobs:
				job()
			default:
				drained = cap(app.jobs)
			}
		}
		e.ProcessCollaborationUpdates()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("whole-level history operation did not settle")
}

func largeSnapshot(t *testing.T, ws *WsMap) model.Snapshot {
	t.Helper()
	snapshot, err := ws.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func snapshotStateIndex(snapshot model.Snapshot) map[model.Coord]model.TileState {
	states := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	for _, tile := range snapshot.Tiles {
		states[tile.Coord] = tile.State
	}
	return states
}

func assertLargeRegionMatches(t *testing.T, ws *WsMap, states map[model.Coord]model.TileState, x1, x2, y1, y2 int) {
	t.Helper()
	dmm := ws.Map().Editor().Dmm()
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			coord := model.Coord{X: x, Y: y, Z: 1}
			want, ok := states[coord]
			if !ok {
				t.Fatalf("authority omitted map tile %+v", coord)
			}
			actual, err := mapadapter.CaptureTile(dmm.GetTile(util.Point{X: x, Y: y, Z: 1}))
			if err != nil || !actual.Equal(want) {
				t.Fatalf("display differs from authority at %+v: err=%v", coord, err)
			}
		}
	}
}

func selectLargeRectangle(t *testing.T, ws *WsMap, app *selectionTestApp, width, height int) {
	t.Helper()
	grab := tools.Selected().(*tools.ToolGrab)
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: width, Y: height, Z: 1}})
	ws.Map().Editor().TileCopySelected()
	app.PathsFilter().Clear()
}

func TestWholeLevelPaste256PreviewCancelCommitUndoRedoAndSave(t *testing.T) {
	const width, height = 512, 256
	ws, app, path, _ := newLargeSelectionWorkspace(t, width, height, 0)
	grab := activateSelectionWorkspace(t, ws)
	before := largeSnapshot(t, ws)
	beforeHash := resizeHash(t, before)
	statesBefore := snapshotStateIndex(before)
	selectLargeRectangle(t, ws, app, width/2, height)
	target := util.Point{X: width/2 + 1, Y: 1, Z: 1}
	ws.Map().CanvasState().SetMousePosition((target.X-1)*32, 0, target.Z)
	ws.Map().Editor().TilePasteSelected()
	settleLargePaste(t, ws, app, target)
	if !grab.Placing() || grab.PlacementError() != nil {
		t.Fatalf("full 256x256 placement did not become ready: %v", grab.PlacementError())
	}
	grab.CancelPlacement()
	settleLargePaste(t, ws, app, target)
	if got := resizeHash(t, largeSnapshot(t, ws)); got != beforeHash || app.commands.HasUndoV(path) {
		t.Fatal("cancelled full-level preview changed authority or history")
	}
	assertLargeRegionMatches(t, ws, statesBefore, target.X, width, 1, height)

	selectLargeRectangle(t, ws, app, width/2, height)
	ws.Map().CanvasState().SetMousePosition((target.X-1)*32, 0, target.Z)
	ws.Map().Editor().TilePasteSelected()
	settleLargePaste(t, ws, app, target)
	if !grab.Placing() || !grab.ConfirmPlacement() {
		t.Fatal("full 256x256 paste did not confirm through ToolGrab")
	}
	settleLargePaste(t, ws, app, target)
	after := largeSnapshot(t, ws)
	if after.Revision != before.Revision+1 || resizeHash(t, after) == beforeHash || !app.commands.HasUndoV(path) {
		t.Fatal("full-level paste was not exactly one durable command")
	}
	assertLargeRegionMatches(t, ws, snapshotStateIndex(before), 1, width/2, 1, height)
	unknown := ws.Map().Editor().Dmm().GetTile(util.Point{X: target.X, Y: 1, Z: 1})
	if !hasLargeUnknownValue(unknown, fmt.Sprintf("%q", "kept by paste and stamp")) {
		t.Fatal("paste lost an unknown prefab variable")
	}
	settleLargeHistory(t, ws, app, false)
	if got := resizeHash(t, largeSnapshot(t, ws)); got != beforeHash || app.commands.HasUndoV(path) || !app.commands.HasRedoV(path) {
		t.Fatal("one undo did not restore the whole-level paste exactly")
	}
	assertLargeRegionMatches(t, ws, statesBefore, target.X, width, 1, height)
	settleLargeHistory(t, ws, app, true)
	if got := resizeHash(t, largeSnapshot(t, ws)); got != resizeHash(t, after) || !app.commands.HasUndoV(path) || app.commands.HasRedoV(path) {
		t.Fatal("one redo did not restore the whole-level paste exactly")
	}
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("workspace Save rejected confirmed full-level paste")
	}
	saved, err := dmmdata.New(path)
	if err != nil || !hasLargeUnknownValueInData(saved, util.Point{X: target.X, Y: 1, Z: 1}, fmt.Sprintf("%q", "kept by paste and stamp")) {
		t.Fatalf("DMM save did not retain the unknown variable: %v", err)
	}
}

func TestSparse512StampPreviewHistoryAndCommitLifetime(t *testing.T) {
	const width, height, stride = 512, 512, 8
	ws, app, path, workBudget := newLargeSelectionWorkspace(t, width, height, stride)
	grab := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before := largeSnapshot(t, ws)
	beforeHash := resizeHash(t, before)
	filter := dm.NewPathsFilterEmpty()
	filter.TogglePath("/area/foo")
	filter.TogglePath("/turf/foo")
	app.paths = filter
	points := make([]util.Point, 0, width*height)
	for y := 1; y <= height; y++ {
		for x := 1; x <= width; x++ {
			points = append(points, util.Point{X: x, Y: y, Z: 1})
		}
	}
	stamp, err := e.CaptureStamp("Sparse whole level", points)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stamp.Close)
	stampPath := filepath.Join(t.TempDir(), "sparse-whole-level.admmstamp")
	if err := stamp.Save(stampPath); err != nil {
		t.Fatal(err)
	}
	stampInfo, err := os.Stat(stampPath)
	if err != nil || stampInfo.Size() <= 8<<20 {
		t.Fatalf("whole-level stamp did not exercise streaming beyond 8 MiB: size=%d err=%v", stampInfo.Size(), err)
	}

	target := util.Point{X: 1, Y: 1, Z: 1}
	first, err := stamps.Load(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	if err := e.StartStamp(first, false); err != nil {
		t.Fatal(err)
	}
	first.Close() // The in-flight read lease must outlive the stamp owner.
	settleLargePaste(t, ws, app, target)
	if !grab.Placing() || grab.PlacementError() != nil {
		t.Fatalf("sparse 512x512 stamp did not become ready: %v", grab.PlacementError())
	}
	grab.CancelPlacement()
	settleLargePaste(t, ws, app, target)
	if got := resizeHash(t, largeSnapshot(t, ws)); got != beforeHash || app.commands.HasUndoV(path) {
		t.Fatal("cancelled sparse whole-level stamp changed authority or history")
	}

	second, err := stamps.Load(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	if err := e.StartStamp(second, false); err != nil {
		t.Fatal(err)
	}
	settleLargePaste(t, ws, app, target)
	if !grab.Placing() {
		t.Fatalf("sparse 512x512 stamp did not enter placement: error=%v progress=%q", grab.PlacementError(), e.PastePlacementProgress())
	}
	if err := grab.PlacementError(); err != nil {
		t.Fatalf("sparse 512x512 stamp placement is invalid: %v", err)
	}
	if !grab.ConfirmPlacement() {
		t.Fatalf("sparse 512x512 stamp did not confirm through ToolGrab: placement error=%v", grab.PlacementError())
	}
	settleLargePaste(t, ws, app, target)
	second.Close()
	after := largeSnapshot(t, ws)
	if after.Revision != before.Revision+1 || resizeHash(t, after) == beforeHash || !app.commands.HasUndoV(path) {
		t.Fatal("sparse whole-level stamp was not exactly one command")
	}
	if !hasLargeUnknownValue(e.Dmm().GetTile(target), fmt.Sprintf("%q", strings.Repeat("unknown mechanical value ", 96))) {
		t.Fatal("stamp lost an unknown variable")
	}
	settleLargeHistory(t, ws, app, false)
	if got := resizeHash(t, largeSnapshot(t, ws)); got != beforeHash || app.commands.HasUndoV(path) || !app.commands.HasRedoV(path) {
		t.Fatal("one undo did not restore the sparse stamp")
	}
	settleLargeHistory(t, ws, app, true)
	if got := resizeHash(t, largeSnapshot(t, ws)); got != resizeHash(t, after) || !app.commands.HasUndoV(path) || app.commands.HasRedoV(path) {
		t.Fatal("one redo did not restore the sparse stamp")
	}
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("workspace Save rejected confirmed sparse stamp")
	}
	saved, err := dmmdata.New(path)
	if err != nil || !hasLargeUnknownValueInData(saved, target, fmt.Sprintf("%q", strings.Repeat("unknown mechanical value ", 96))) {
		t.Fatalf("DMM save did not retain the stamp unknown variable: %v", err)
	}

	// Closing the editor after submission must keep operation/metadata buffers
	// admitted until the stale-generation completion has finished using them.
	baselineBudget := workBudget.Used()
	third, err := stamps.Load(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(third.Close)
	if err := e.StartStamp(third, false); err != nil {
		t.Fatal(err)
	}
	third.Close()
	settleLargePaste(t, ws, app, target)
	if !grab.Placing() {
		t.Fatalf("lifetime stamp did not enter placement: error=%v progress=%q", grab.PlacementError(), e.PastePlacementProgress())
	}
	if err := grab.PlacementError(); err != nil {
		t.Fatalf("lifetime stamp placement is invalid: %v", err)
	}
	if !grab.ConfirmPlacement() {
		t.Fatalf("lifetime stamp did not confirm: placement error=%v", grab.PlacementError())
	}
	e.Close()
	if used := workBudget.Used(); used <= baselineBudget {
		t.Fatalf("editor close released proposal memory before async resolution: baseline=%d used=%d", baselineBudget, used)
	}
	deadline := time.Now().Add(largePasteTimeout)
	for time.Now().Before(deadline) && workBudget.Used() > baselineBudget {
		select {
		case job := <-app.jobs:
			job()
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if used := workBudget.Used(); used != baselineBudget {
		t.Fatalf("stale completion leaked paste reservations: baseline=%d used=%d", baselineBudget, used)
	}
}

func hasLargeUnknownValue(tile *dmmap.Tile, value string) bool {
	if tile == nil {
		return false
	}
	for _, instance := range tile.Instances() {
		if instance.Prefab().Path() == "/obj/unknown" {
			got, ok := instance.Prefab().Vars().Value("custom")
			return ok && got == value
		}
	}
	return false
}

func hasLargeUnknownValueInData(data *dmmdata.DmmData, coord util.Point, value string) bool {
	if data == nil {
		return false
	}
	key, ok := data.Grid[coord]
	if !ok {
		return false
	}
	for _, prefab := range data.Dictionary[key] {
		if prefab.Path() == "/obj/unknown" {
			got, ok := prefab.Vars().Value("custom")
			return ok && got == value
		}
	}
	return false
}
