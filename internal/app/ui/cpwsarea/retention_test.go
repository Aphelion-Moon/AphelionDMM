package cpwsarea

import (
	"runtime"
	"testing"
	"weak"

	"sdmm/internal/app/command"
	"sdmm/internal/app/ui/cpwsarea/workspace"
)

type workspacePayload [8 << 20]byte

type lifetimeContent struct {
	workspace.Content
	name     string
	payload  *workspacePayload
	disposed bool
	events   *[]string
}

func (c *lifetimeContent) Name() string           { return c.name }
func (c *lifetimeContent) Title() string          { return c.name }
func (c *lifetimeContent) CommandStackId() string { return c.name }
func (c *lifetimeContent) Dispose() {
	c.disposed = true
	*c.events = append(*c.events, c.name+":dispose")
}
func (c *lifetimeContent) OnFocusChange(focused bool) {
	if !focused {
		if !c.disposed {
			*c.events = append(*c.events, c.name+":deactivate-before-dispose")
		}
		*c.events = append(*c.events, c.name+":unfocus")
	}
}

type lifetimeApp struct {
	guardTestApp
	switches int
}

func (a *lifetimeApp) OnWorkspaceSwitched() { a.switches++ }

type commandContextContent struct {
	workspace.Content
	name    string
	focused bool
	owners  []bool
}

func (c *commandContextContent) Name() string               { return c.name }
func (c *commandContextContent) Title() string              { return c.name }
func (c *commandContextContent) CommandStackId() string     { return c.name }
func (c *commandContextContent) Focused() bool              { return c.focused }
func (c *commandContextContent) OnFocusChange(focused bool) { c.focused = focused }
func (c *commandContextContent) OnCommandContextChange(active bool) {
	c.owners = append(c.owners, active)
}

func addLifetimeWorkspace(area *WsArea, name string, events *[]string) weak.Pointer[workspacePayload] {
	payload := new(workspacePayload)
	for i := 0; i < len(payload); i += 4096 {
		payload[i] = 1
	}
	area.addWorkspace(workspace.New(&lifetimeContent{name: name, payload: payload, events: events}))
	area.app.CommandStorage().SetStack(name)
	return weak.Make(payload)
}

func TestCloseAllReleasesWorkspacePayloads(t *testing.T) {
	app := &lifetimeApp{guardTestApp: guardTestApp{commands: command.NewStorage()}}
	area := &WsArea{app: app}
	var events []string
	var pointers []weak.Pointer[workspacePayload]
	for _, name := range []string{"first", "middle", "last"} {
		pointers = append(pointers, addLifetimeWorkspace(area, name, &events))
	}
	closed := false
	area.CloseAllGuarded(nil, func(value bool) { closed = value })
	if !closed || len(area.workspaces) != 0 || len(events) != 3 {
		t.Fatalf("incomplete close: closed=%t workspaces=%d events=%v", closed, len(area.workspaces), events)
	}
	runtime.GC()
	for i, pointer := range pointers {
		if pointer.Value() != nil {
			t.Errorf("closed workspace %d retains its payload", i)
		}
	}
	runtime.KeepAlive(area)
}

func TestCloseClearsWorkspaceFocusWithoutAnotherFrame(t *testing.T) {
	previous := tmpFocusedWs
	t.Cleanup(func() { tmpFocusedWs = previous })
	app := &lifetimeApp{guardTestApp: guardTestApp{commands: command.NewStorage()}}
	area := &WsArea{app: app}
	var events []string
	pointer := addLifetimeWorkspace(area, "active", &events)
	area.activeWs, area.focusedWs = area.workspaces[0], area.workspaces[0]
	area.activeWsContentId = area.activeWs.Content().Id()
	tmpFocusedWs = area.activeWs
	// A refused close must leave every lifetime/focus reference intact.
	area.CloseGuarded(func() bool { return false }, nil)
	if area.ActiveWorkspace() == nil || area.focusedWs == nil || tmpFocusedWs == nil || len(events) != 0 || app.switches != 0 {
		t.Fatal("refused close changed workspace ownership")
	}
	area.Close()
	if area.ActiveWorkspace() != nil || area.WorkspaceTitle() != "" || area.activeWsContentId != "" || area.focusedWs != nil || tmpFocusedWs != nil {
		t.Fatal("closed workspace remains active, focused or queued for frame focus")
	}
	if app.switches != 1 || len(events) != 2 || events[0] != "active:dispose" || events[1] != "active:unfocus" {
		t.Fatalf("close must dispose before focus cleanup, once: switches=%d events=%v", app.switches, events)
	}
	// The usual end-of-frame cleanup must not reactivate disposed content or
	// emit another switch callback after immediate close cleanup.
	area.switchFocusedWorkspace(tmpFocusedWs)
	area.switchActiveWorkspace(nil)
	if len(events) != 2 || app.switches != 1 {
		t.Fatal("next frame repeated close callbacks")
	}
	runtime.GC()
	if pointer.Value() != nil {
		t.Fatal("closed active workspace retains payload")
	}
	runtime.KeepAlive(area)
}

func TestClosingInactiveWorkspacePreservesLiveFocus(t *testing.T) {
	previous := tmpFocusedWs
	t.Cleanup(func() { tmpFocusedWs = previous })
	app := &lifetimeApp{guardTestApp: guardTestApp{commands: command.NewStorage()}}
	area := &WsArea{app: app}
	var events []string
	first := addLifetimeWorkspace(area, "first", &events)
	last := addLifetimeWorkspace(area, "last", &events)
	app.commands.SetStack("first")
	area.activeWs, area.focusedWs = area.workspaces[0], area.workspaces[0]
	tmpFocusedWs = area.activeWs
	area.closeWorkspaceGentlyV(area.workspaces[1], nil, nil)
	if len(area.workspaces) != 1 || area.activeWs != area.workspaces[0] || area.focusedWs != area.workspaces[0] || tmpFocusedWs != area.workspaces[0] || app.switches != 0 {
		t.Fatal("closing inactive workspace changed another workspace's focus")
	}
	runtime.GC()
	if first.Value() == nil || last.Value() != nil {
		t.Fatal("inactive close retained its payload or released the live workspace")
	}
	runtime.KeepAlive(area)
}

func TestCommandContextFollowsActiveWorkspaceRatherThanPointerFocus(t *testing.T) {
	app := &lifetimeApp{guardTestApp: guardTestApp{commands: command.NewStorage()}}
	area := &WsArea{app: app}
	firstContent := &commandContextContent{name: "first-map"}
	secondContent := &commandContextContent{name: "second-map"}
	first := workspace.New(firstContent)
	second := workspace.New(secondContent)
	area.workspaces = []*workspace.Workspace{first, second}

	area.switchActiveWorkspace(first)
	first.OnFocusChange(false) // A palette child can take pointer focus.
	if len(firstContent.owners) != 1 || !firstContent.owners[0] {
		t.Fatalf("palette focus changed active command context: %v", firstContent.owners)
	}
	area.switchActiveWorkspace(second)
	if len(firstContent.owners) != 2 || firstContent.owners[1] ||
		len(secondContent.owners) != 1 || !secondContent.owners[0] {
		t.Fatalf("document switch did not transfer command context: first=%v second=%v", firstContent.owners, secondContent.owners)
	}
	area.switchActiveWorkspace(nil)
	if len(secondContent.owners) != 2 || secondContent.owners[1] {
		t.Fatalf("closing active document retained command context: %v", secondContent.owners)
	}
}
