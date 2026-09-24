package command

import (
	"runtime"
	"testing"
)

func TestBoundHistoryKeepsOwnerAndLifetime(t *testing.T) {
	storage := NewStorage()
	storage.SetStack("other")
	target := storage.Bind("map")
	if !target.Valid() || storage.currentStackId != "other" {
		t.Fatal("binding must preserve the active tab")
	}
	undone := 0
	if !target.Push(Make("source edit", func() { undone++ }, func() {})) {
		t.Fatal("bound push failed")
	}
	if storage.HasUndo() || !storage.HasUndoV("map") {
		t.Fatal("bound push changed another map's history")
	}
	storage.UndoV("map")
	if undone != 1 || !storage.HasRedoV("map") {
		t.Fatal("source edit cannot be undone")
	}
	storage.DisposeStack("map")
	reopened := storage.Bind("map")
	if target.Valid() || target.Push(Make("late edit", func() {}, func() {})) {
		t.Fatal("a disposed target accepted a late command")
	}
	if !reopened.Valid() || storage.HasUndoV("map") || storage.HasRedoV("map") {
		t.Fatal("reopening resurrected disposed history")
	}
	storage.Free()
	if reopened.Valid() || storage.currentStackId != NullSpaceStackId || storage.HasUndo() {
		t.Fatal("Free retained the old stack lifetime")
	}
	storage.SetStack("map")
	if !storage.Bind("map").Push(Make("new session", func() {}, func() {})) {
		t.Fatal("storage is unusable after Free")
	}
}

func TestInvalidHistoryTargetsCannotCreateStacks(t *testing.T) {
	storage := NewStorage()
	for _, target := range []Target{{}, storage.Bind(""), storage.Bind(NullSpaceStackId)} {
		if target.Valid() || target.Push(Make("invalid", func() {}, func() {})) {
			t.Fatal("invalid history target accepted a command")
		}
	}
	if len(storage.commandStacks) != 1 || storage.HasUndo() {
		t.Fatal("invalid binding created command history")
	}
}

func TestRebindHistoryMovesItsLifetimeWithoutLosingCommands(t *testing.T) {
	storage := NewStorage()
	storage.SetStack("before.dmm")
	target := storage.Bind("before.dmm")
	undone := 0
	if !target.Push(Make("existing edit", func() { undone++ }, func() {})) {
		t.Fatal("could not seed history")
	}
	if !storage.CanRebindStack("before.dmm", "after.dmm") {
		t.Fatal("expected an unused destination stack to be available")
	}
	if !storage.RebindStack("before.dmm", "after.dmm") {
		t.Fatal("history stack did not rebind")
	}
	if target.Valid() != true || storage.currentStackId != "after.dmm" || storage.HasUndoV("before.dmm") || !storage.HasUndoV("after.dmm") {
		t.Fatal("rebind lost the live target, active id, or existing command")
	}
	if !target.Push(Make("later edit", func() {}, func() {})) || !storage.HasUndoV("after.dmm") {
		t.Fatal("bound target could not push after rebind")
	}
	storage.UndoV("after.dmm")
	if !storage.HasRedoV("after.dmm") || undone != 0 {
		t.Fatal("rebound history could not undo the newly pushed edit")
	}
	storage.UndoV("after.dmm")
	if undone != 1 {
		t.Fatal("rebind discarded the prior undo command")
	}
	if storage.CanRebindStack("after.dmm", "collision.dmm") == false {
		t.Fatal("expected a second unused destination to be available")
	}
	storage.Bind("collision.dmm")
	if storage.CanRebindStack("after.dmm", "collision.dmm") || storage.RebindStack("after.dmm", "collision.dmm") {
		t.Fatal("rebind replaced an existing destination stack")
	}
}

func TestDisposedHistoryReleasesPayloadWithLiveTarget(t *testing.T) {
	for _, action := range []string{"dispose", "free"} {
		t.Run(action, func(t *testing.T) {
			storage := NewStorage()
			storage.SetStack("map")
			target := storage.Bind("map")
			pointer := pushHistoryPayload(storage, true)
			if action == "dispose" {
				storage.DisposeStack("map")
			} else {
				storage.Free()
			}
			runtime.GC()
			if pointer.Value() != nil {
				t.Error("disposed history is retained by the old target")
			}
			if target.Valid() {
				t.Error("disposed target is valid")
			}
			runtime.KeepAlive(target)
		})
	}
}

func TestHistoryCompletionCannotEnterReopenedStack(t *testing.T) {
	for _, redo := range []bool{false, true} {
		storage := NewStorage()
		storage.SetStack("map")
		var acknowledge func(error)
		undoAction := func(done func(error)) { acknowledge = done }
		redoAction := func(done func(error)) { done(nil) }
		if redo {
			undoAction, redoAction = redoAction, undoAction
		}
		storage.Push(MakeAsync("old edit", undoAction, redoAction))
		storage.UndoV("map")
		if redo {
			storage.RedoV("map")
		}
		if acknowledge == nil {
			t.Fatal("fixture has no pending history operation")
		}
		storage.DisposeStack("map")
		reopened := storage.Bind("map")
		if !reopened.Push(Make("new map edit", func() {}, func() {})) {
			t.Fatal("reopened map cannot record commands")
		}
		acknowledge(nil)
		storage.UndoV("map")
		if storage.HasUndoV("map") || !storage.HasRedoV("map") {
			t.Fatal("old completion changed the reopened map's history")
		}
	}
}
