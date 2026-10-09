package app

import (
	"testing"

	"sdmm/internal/dmapi/dmmap"
)

func TestPrefabByPathActionsRefuseUnknownType(t *testing.T) {
	dmmap.Free()
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	// A zero app has no layout: reaching the selection or editor panels would
	// panic, so returning cleanly proves the unknown path was refused first.
	a := &app{}
	for name, action := range map[string]func(string){"edit": a.DoEditPrefabByPath, "select": a.DoSelectPrefabByPath} {
		func() {
			defer func() {
				if failure := recover(); failure != nil {
					t.Fatalf("%s by unknown path panicked: %v", name, failure)
				}
			}()
			action("/obj/audit_unknown")
		}()
	}
	if len(dmmap.PrefabStorage.GetAllByPath("/obj/audit_unknown")) != 0 {
		t.Fatal("refused action persisted an invented prefab")
	}
}
