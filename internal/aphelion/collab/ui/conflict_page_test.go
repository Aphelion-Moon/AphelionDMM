package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/collab/model"
)

func conflictPageSession(t *testing.T) (*SessionClient, []model.OperationID) {
	t.Helper()
	session := largeConflictSession(t, 1)
	first := session.network.Conflicts()[0]
	ids := []model.OperationID{first.OperationID}
	for len(ids) < 45 {
		operation := model.CloneOperation(first.Draft)
		var err error
		operation.OperationID, err = model.NewOperationID()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.network.Execute(context.Background(), operation); err == nil {
			t.Fatal("unconnected transport accepted operation")
		}
		ids = append(ids, operation.OperationID)
	}
	return session, ids
}

type conflictPagePanelApp struct {
	session *SessionClient
	active  bool
	view    ViewModel
}

func (app *conflictPagePanelApp) CollaborationViewModel(page int) ViewModel {
	app.view = BuildViewModel(app.session.StatusForConflictPage(page))
	return app.view
}
func (app *conflictPagePanelApp) HasActiveCollaboration() bool                                 { return app.active }
func (*conflictPagePanelApp) DoLeaveCollaborationSession()                                     {}
func (*conflictPagePanelApp) DoRetryCollaborationSession()                                     {}
func (*conflictPagePanelApp) DoUpdateCollaborationDisplayName(string)                          {}
func (*conflictPagePanelApp) DoCopyCollaborationInvitation(InvitationRole, string)             {}
func (*conflictPagePanelApp) DoResolveCollaborationConflict(model.OperationID, ConflictAction) {}
func (*conflictPagePanelApp) DoResolveAllCollaborationConflicts(BulkDraftAction, bool)         {}

func TestConflictPanelNativeNavigationAndSessionReset(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	context := imgui.CreateContext(nil)
	defer context.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()
	session, ids := conflictPageSession(t)
	app := &conflictPagePanelApp{session: session, active: true}
	panel := &Panel{}
	panel.Init(app)
	frame := func(navigationOnly bool) imgui.Vec2 {
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{X: 0, Y: 0})
		imgui.SetNextWindowSize(imgui.Vec2{X: 800, Y: 600})
		imgui.Begin("Recovery fixture")
		if navigationOnly {
			panel.renderConflictNavigation(app.CollaborationViewModel(panel.conflictPage))
		} else {
			panel.Process(0)
		}
		min, max := imgui.ItemRectMin(), imgui.ItemRectMax()
		imgui.End()
		imgui.Render()
		return imgui.Vec2{X: (min.X + max.X) / 2, Y: (min.Y + max.Y) / 2}
	}
	frame(false)
	for _, wantPage := range []int{1, 2, 2} {
		next := frame(true)
		io.SetMousePosition(next)
		frame(true)
		io.SetMouseButtonDown(0, true)
		frame(true)
		io.SetMouseButtonDown(0, false)
		frame(true)
		if panel.conflictPage != wantPage {
			t.Fatalf("Next button selected page %d, want %d", panel.conflictPage, wantPage)
		}
	}
	frame(false)
	if app.view.Conflicts[0].OperationID != ids[40] {
		t.Fatal("full panel did not use selected page")
	}
	session.sessionID = "replacement-session"
	frame(false)
	if panel.conflictPage != 0 || app.view.Conflicts[0].OperationID != ids[0] {
		t.Fatal("new session inherited prior page")
	}
	panel.conflictPage = 2
	app.active = false
	frame(false)
	if panel.conflictPage != 0 || panel.sessionID != "" {
		t.Fatal("inactive panel retained navigation state")
	}
}

func TestConflictPagesReachEveryDraftWithoutDiscard(t *testing.T) {
	session, ids := conflictPageSession(t)
	var visited []model.OperationID
	for page := 0; page < 3; page++ {
		view := BuildViewModel(session.StatusForConflictPage(page))
		if view.ConflictPage != page || view.ConflictPageCount != 3 || view.ConflictCount != len(ids) || view.CanLeave {
			t.Fatal("incorrect page metadata or recovery guard")
		}
		for _, conflict := range view.Conflicts {
			visited = append(visited, conflict.OperationID)
		}
	}
	if len(visited) != len(ids) {
		t.Fatal("paging hid drafts")
	}
	for i := range ids {
		if visited[i] != ids[i] {
			t.Fatal("paging repeated or skipped a draft")
		}
	}
	if session.network.ConflictCount() != len(ids) {
		t.Fatal("navigation discarded recovery data")
	}
	path := filepath.Join(t.TempDir(), "last-draft.json")
	if err := session.ExportConflict(visited[len(visited)-1], path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var exported struct{ Operation model.Operation }
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Operation.OperationID != ids[len(ids)-1] {
		t.Fatal("last page export targeted wrong draft")
	}
	for _, id := range ids[40:] {
		if _, err := session.DiscardConflict(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	view := BuildViewModel(session.StatusForConflictPage(2))
	if view.ConflictPage != 1 || view.ConflictPageCount != 2 || len(view.Conflicts) != 20 || view.Conflicts[0].OperationID != ids[20] {
		t.Fatal("shrinking last page left navigation stranded")
	}
	if BuildViewModel(session.StatusForConflictPage(-1)).ConflictPage != 0 {
		t.Fatal("negative page was not clamped")
	}
}
