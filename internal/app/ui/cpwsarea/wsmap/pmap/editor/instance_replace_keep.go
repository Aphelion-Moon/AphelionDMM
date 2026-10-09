// APHELION EDIT ADDITION START - REPLACE KEEP EDITS
package editor

import (
	"fmt"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
)

// InstanceReplaceKeepingEdits replaces i with the selected prefab's type while
// carrying over i's own variable edits (see editing.KeptEdits). It returns the
// names of edits that were not carried over. The caller commits the operation.
func (e *Editor) InstanceReplaceKeepingEdits(i *dmminstance.Instance, selected *dmmprefab.Prefab) ([]string, error) {
	env := e.app.LoadedEnvironment()
	if env == nil || selected == nil {
		return nil, fmt.Errorf("no environment or prefab")
	}
	object := env.Objects[selected.Path()]
	if object == nil {
		return nil, fmt.Errorf("%s is not defined by the loaded environment", selected.Path())
	}
	if !dm.IsPathBaseSame(i.Prefab().Path(), selected.Path()) {
		return nil, fmt.Errorf("cannot replace %s with %s: different base types", i.Prefab().Path(), selected.Path())
	}
	kept, dropped := editing.KeptEdits(explicitEdits(i.Prefab()), explicitEdits(selected), object.Vars.Value)
	vars := &dmvars.MutableVariables{}
	for _, edit := range kept {
		vars.Put(edit.Name, edit.Value)
	}
	immutable := vars.ToImmutable()
	immutable.LinkParent(object.Vars)
	e.InstanceReplace(i, dmmap.PrefabStorage.Get(selected.Path(), immutable))
	return dropped, nil
}

func explicitEdits(p *dmmprefab.Prefab) []editing.VarEdit {
	vars := p.Vars()
	if vars == nil {
		return nil
	}
	var out []editing.VarEdit
	for _, name := range vars.Iterate() {
		if value, ok := vars.ExplicitValue(name); ok {
			out = append(out, editing.VarEdit{Name: name, Value: value})
		}
	}
	return out
}

// APHELION EDIT ADDITION END
