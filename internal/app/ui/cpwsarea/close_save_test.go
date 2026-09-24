package cpwsarea

import (
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"testing"
)

func TestSaveBeforeClosePropagatesFailure(t *testing.T) {
	first, second := &saveCloseContent{result: true}, &saveCloseContent{}
	called, closed := false, true
	saveWorkspacesBeforeClose([]*workspace.Workspace{workspace.New(first), workspace.New(second)}, func(result bool) { called, closed = true, result })
	if first.saves != 1 || second.saves != 1 || !called || closed {
		t.Fatalf("save/close results: %d, %d, callback=%t closed=%t", first.saves, second.saves, called, closed)
	}
	second.result = true
	called, closed = false, false
	saveWorkspacesBeforeClose([]*workspace.Workspace{workspace.New(second)}, func(result bool) { called, closed = true, result })
	if !called || !closed {
		t.Fatal("successful save did not complete close")
	}
}

func TestSaveBeforeCloseWaitsForAsyncResult(t *testing.T) {
	content := &asyncSaveCloseContent{}
	called, closed := false, false
	saveWorkspacesBeforeClose([]*workspace.Workspace{workspace.New(content)}, func(result bool) { called, closed = true, result })
	if called || content.saves != 1 {
		t.Fatal("close completed before asynchronous save")
	}
	content.complete(true)
	if !called || !closed {
		t.Fatal("successful asynchronous save did not complete close")
	}
}

func TestCloseAfterSavesRechecksEarlierAndInitiallyCleanWorkspaces(t *testing.T) {
	first, second, clean := &dirtyAsyncCloseContent{}, &dirtyAsyncCloseContent{}, &dirtyAsyncCloseContent{}
	workspaces := []*workspace.Workspace{workspace.New(first), workspace.New(second), workspace.New(clean)}
	area := &WsArea{app: &guardTestApp{}, workspaces: workspaces}
	for _, edited := range []*dirtyAsyncCloseContent{first, clean} {
		var ready bool
		saveWorkspacesBeforeClose(workspaces[:2], func(saved bool) { ready = saved && area.savedWorkspacesStillClosable(workspaces) })
		first.complete(true)
		edited.dirty = true
		second.complete(true)
		if ready {
			t.Fatal("close discarded an edit made while a later save was running")
		}
		edited.dirty = false
	}
	if !area.savedWorkspacesStillClosable(workspaces) {
		t.Fatal("clean live workspaces blocked")
	}
	area.workspaces = workspaces[1:]
	if area.savedWorkspacesStillClosable(workspaces) {
		t.Fatal("closed workspace retained close authority")
	}
}

type dirtyAsyncCloseContent struct {
	asyncSaveCloseContent
	dirty bool
}

func (content *dirtyAsyncCloseContent) HasUnsavedChanges() bool { return content.dirty }

type saveCloseContent struct {
	guardTestContent
	result bool
	saves  int
}

func (content *saveCloseContent) Save() bool { content.saves++; return content.result }

type asyncSaveCloseContent struct {
	guardTestContent
	saves    int
	complete func(bool)
}

func (*asyncSaveCloseContent) Save() bool { return false }
func (content *asyncSaveCloseContent) SaveAsync(complete func(bool)) {
	content.saves++
	content.complete = complete
}

func TestWorkspaceDirtyStateIncludesAuthority(t *testing.T) {
	area := &WsArea{app: &guardTestApp{}}
	if !area.isWorkspaceUnsaved(workspace.New(&dirtyCloseContent{})) {
		t.Fatal("authoritative changes omitted from close check")
	}
}

type dirtyCloseContent struct{ guardTestContent }

func (*dirtyCloseContent) HasUnsavedChanges() bool { return true }
