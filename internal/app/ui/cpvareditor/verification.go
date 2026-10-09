// APHELION EDIT ADDITION START - OFFSET REFRESH VERIFICATION
package cpvareditor

import (
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

// NewForVerification returns an editor for native workspace fixtures. It skips
// config and shortcut registration, which the fixtures do not provide.
func NewForVerification(app App) *VarEditor {
	return &VarEditor{app: app}
}

// SetInstanceVariableForVerification runs the same instance edit the property
// grid performs when a value is committed.
func (v *VarEditor) SetInstanceVariableForVerification(instance *dmminstance.Instance, name, value string) {
	v.sessionEditMode = emInstance
	v.instance = instance
	v.prefab = instance.Prefab()
	v.setInstanceVariable(name, value)
}

// SetPrefabVariableForVerification runs the prefab-wide edit.
func (v *VarEditor) SetPrefabVariableForVerification(prefab *dmmprefab.Prefab, name, value string) {
	v.sessionEditMode = emPrefab
	v.instance = nil
	v.prefab = prefab
	v.setPrefabVariable(name, value)
}

// APHELION EDIT ADDITION END
