package shortcut

import (
	"runtime"
	"testing"
	"weak"
)

type shortcutPayload [8 << 20]byte

func addPayloadShortcuts(holder *Shortcuts) weak.Pointer[shortcutPayload] {
	payload := new(shortcutPayload)
	for i := 0; i < len(payload); i += 4096 {
		payload[i] = 1
	}
	holder.Add(Shortcut{Name: "payload-action", Action: func() { runtime.KeepAlive(payload) }})
	holder.Add(Shortcut{Name: "payload-enabled", IsEnabled: func() bool {
		runtime.KeepAlive(payload)
		return true
	}})
	return weak.Make(payload)
}

func TestDisposeReleasesShortcutCallbacksAndKeepsOtherHolders(t *testing.T) {
	previous := shortcuts
	shortcuts = nil
	t.Cleanup(func() { shortcuts = previous })
	var holders [3]Shortcuts
	var pointers [3]weak.Pointer[shortcutPayload]
	for i := range holders {
		pointers[i] = addPayloadShortcuts(&holders[i])
	}
	runtime.GC()
	for _, pointer := range pointers {
		if pointer.Value() == nil {
			t.Fatal("registered shortcut lost its callback payload")
		}
	}
	for _, index := range []int{2, 0, 1} {
		holders[index].Dispose()
		holders[index].Dispose()
		runtime.GC()
		if pointers[index].Value() != nil {
			t.Fatalf("disposed holder %d retains callback payload", index)
		}
		for i := range holders {
			if len(holders[i].shortcuts) > 0 && pointers[i].Value() == nil {
				t.Fatalf("disposing another holder released live holder %d", i)
			}
		}
	}
	if len(shortcuts) != 0 {
		t.Fatal("disposed shortcuts remain registered")
	}
}
