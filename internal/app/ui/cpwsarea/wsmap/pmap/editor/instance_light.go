// APHELION EDIT ADDITION START - LIGHT SWITCH
package editor

import (
	"fmt"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
)

// InstanceSwitchLight turns the light i on or off with the edits the game
// honours (lighting.LightSwitch) and reports whether it is now on. The caller
// commits the operation.
func (e *Editor) InstanceSwitchLight(i *dmminstance.Instance) (bool, error) {
	env := e.app.LoadedEnvironment()
	if env == nil {
		return false, fmt.Errorf("no environment")
	}
	object := env.Objects[i.Prefab().Path()]
	if object == nil {
		return false, fmt.Errorf("%s is not defined by the loaded environment", i.Prefab().Path())
	}
	sw, ok := lighting.LightSwitch(i.Prefab().Path(), i.Prefab().Vars().Value, object.Vars.Value, lighting.DefaultProfiles())
	if !ok {
		return false, fmt.Errorf("%s is not a light", i.Prefab().Path())
	}
	changed := make(map[string]lighting.VarChange, len(sw.Toggle))
	for _, c := range sw.Toggle {
		changed[c.Name] = c
	}
	vars := &dmvars.MutableVariables{}
	for _, edit := range explicitEdits(i.Prefab()) {
		if _, replaced := changed[edit.Name]; !replaced {
			vars.Put(edit.Name, edit.Value)
		}
	}
	for _, c := range sw.Toggle {
		if !c.Remove {
			vars.Put(c.Name, c.Value)
		}
	}
	immutable := vars.ToImmutable()
	immutable.LinkParent(object.Vars)
	e.InstanceReplace(i, dmmap.PrefabStorage.Get(i.Prefab().Path(), immutable))
	return !sw.On, nil
}

// APHELION EDIT ADDITION END
